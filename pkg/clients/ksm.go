package clients

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/prompb"
	"k8s.io/klog/v2"
)

// KSMClient fetches and parses metrics from kube-state-metrics
type KSMClient struct {
	url          string
	client       *http.Client
	cache        *KSMCache
	mu           sync.RWMutex
	scrapeInterval time.Duration
	stopCh       chan struct{}
}

// KSMCache holds the parsed metrics from KSM
type KSMCache struct {
	PodRequests      map[string]model.SampleValue // key: namespace/pod/container -> cpu/mem requests
	PodLimits        map[string]model.SampleValue // key: namespace/pod/container -> cpu/mem limits
	NodeCapacity     map[string]model.SampleValue // key: node -> cpu/mem capacity
	NodeAllocatable  map[string]model.SampleValue // key: node -> cpu/mem allocatable
	PodOwner         map[string]string            // key: namespace/pod -> owner (workload)
	NamespaceLabels  map[string]map[string]string // key: namespace -> labels
	PodLabels        map[string]map[string]string // key: namespace/pod -> labels
	PodPhase         map[string]string            // key: namespace/pod -> phase
	PVCapacity       map[string]model.SampleValue // key: pv -> capacity bytes
	PVCRequests      map[string]model.SampleValue // key: namespace/pvc -> storage request bytes
	PVInfo           map[string]map[string]string // key: pv -> info (storageclass, etc)
	PVCInfo          map[string]map[string]string // key: namespace/pvc -> info
	ResourceQuota    map[string]model.SampleValue // key: namespace/quota -> hard/used
	LimitRange       map[string]model.SampleValue // key: namespace/limit -> default/request
	LastUpdate       time.Time
}

// NewKSMClient creates a new KSM client
func NewKSMClient(url string, scrapeInterval time.Duration) *KSMClient {
	return &KSMClient{
		url:            url,
		client:         &http.Client{Timeout: 10 * time.Second},
		cache:          &KSMCache{},
		scrapeInterval: scrapeInterval,
		stopCh:         make(chan struct{}),
	}
}

// Start begins the periodic scraping
func (k *KSMClient) Start(ctx context.Context) {
	k.scrape(ctx)
	ticker := time.NewTicker(k.scrapeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-k.stopCh:
			return
		case <-ticker.C:
			k.scrape(ctx)
		}
	}
}

// Stop stops the scraper
func (k *KSMClient) Stop() {
	close(k.stopCh)
}

// scrape fetches and parses metrics from KSM
func (k *KSMClient) scrape(ctx context.Context) {
	req, err := http.NewRequestWithContext(ctx, "GET", k.url+"/metrics", nil)
	if err != nil {
		klog.ErrorS(err, "Failed to create KSM request")
		return
	}

	resp, err := k.client.Do(req)
	if err != nil {
		klog.ErrorS(err, "Failed to scrape KSM", "url", k.url)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		klog.ErrorS(nil, "KSM returned non-OK status", "status", resp.StatusCode)
		return
	}

	// Parse Prometheus text format
	parser := NewPrometheusTextParser()
	metricFamilies, err := parser.Parse(resp.Body)
	if err != nil {
		klog.ErrorS(err, "Failed to parse KSM metrics")
		return
	}

	newCache := &KSMCache{
		PodRequests:     make(map[string]model.SampleValue),
		PodLimits:       make(map[string]model.SampleValue),
		NodeCapacity:    make(map[string]model.SampleValue),
		NodeAllocatable: make(map[string]model.SampleValue),
		PodOwner:        make(map[string]string),
		NamespaceLabels: make(map[string]map[string]string),
		PodLabels:       make(map[string]map[string]string),
		PodPhase:        make(map[string]string),
		PVCapacity:      make(map[string]model.SampleValue),
		PVCRequests:     make(map[string]model.SampleValue),
		PVInfo:          make(map[string]map[string]string),
		PVCInfo:         make(map[string]map[string]string),
		ResourceQuota:   make(map[string]model.SampleValue),
		LimitRange:      make(map[string]model.SampleValue),
		LastUpdate:      time.Now(),
	}

	// Process each metric family
	for _, mf := range metricFamilies {
		k.processMetricFamily(mf, newCache)
	}

	k.mu.Lock()
	k.cache = newCache
	k.mu.Unlock()

	klog.V(4).InfoS("KSM scrape completed", "metrics", len(metricFamilies), "duration", time.Since(newCache.LastUpdate))
}

// processMetricFamily processes a single metric family
func (k *KSMClient) processMetricFamily(mf *prompb.MetricFamily, cache *KSMCache) {
	name := mf.GetName()
	for _, m := range mf.GetMetric() {
		lbls := labelsFromMetric(m)
		
		switch name {
		case "kube_pod_container_resource_requests":
			key := buildPodContainerKey(lbls)
			if key != "" {
				cache.PodRequests[key] = model.SampleValue(m.GetGauge().GetValue())
			}
		case "kube_pod_container_resource_limits":
			key := buildPodContainerKey(lbls)
			if key != "" {
				cache.PodLimits[key] = model.SampleValue(m.GetGauge().GetValue())
			}
		case "kube_node_status_capacity":
			node := lbls.Get("node")
			resource := lbls.Get("resource")
			if node != "" && resource != "" {
				cache.NodeCapacity[node+"/"+resource] = model.SampleValue(m.GetGauge().GetValue())
			}
		case "kube_node_status_allocatable":
			node := lbls.Get("node")
			resource := lbls.Get("resource")
			if node != "" && resource != "" {
				cache.NodeAllocatable[node+"/"+resource] = model.SampleValue(m.GetGauge().GetValue())
			}
		case "kube_pod_owner":
			podKey := buildPodKey(lbls)
			owner := lbls.Get("owner_name")
			kind := lbls.Get("owner_kind")
			if podKey != "" && owner != "" {
				cache.PodOwner[podKey] = fmt.Sprintf("%s/%s", kind, owner)
			}
		case "kube_namespace_labels":
			ns := lbls.Get("namespace")
			if ns != "" {
				if cache.NamespaceLabels[ns] == nil {
					cache.NamespaceLabels[ns] = make(map[string]string)
				}
				for _, l := range m.GetLabel() {
					if l.GetName() != "namespace" {
						cache.NamespaceLabels[ns][l.GetName()] = l.GetValue()
					}
				}
			}
		case "kube_pod_labels":
			podKey := buildPodKey(lbls)
			if podKey != "" {
				cache.PodLabels[podKey] = make(map[string]string)
				for _, l := range m.GetLabel() {
					name := l.GetName()
					// Only allowlist labels
					if name == "app" || name == "version" || name == "cost-center" || name == "team" || name == "environment" {
						cache.PodLabels[podKey][name] = l.GetValue()
					}
				}
			}
		case "kube_pod_status_phase":
			podKey := buildPodKey(lbls)
			phase := lbls.Get("phase")
			if podKey != "" && phase != "" {
				cache.PodPhase[podKey] = phase
			}
		case "kube_persistentvolume_capacity_bytes":
			pv := lbls.Get("persistentvolume")
			if pv != "" {
				cache.PVCapacity[pv] = model.SampleValue(m.GetGauge().GetValue())
			}
		case "kube_persistentvolumeclaim_resource_requests_storage_bytes":
			key := buildPVCKey(lbls)
			if key != "" {
				cache.PVCRequests[key] = model.SampleValue(m.GetGauge().GetValue())
			}
		case "kube_persistentvolume_info":
			pv := lbls.Get("persistentvolume")
			if pv != "" {
				cache.PVInfo[pv] = make(map[string]string)
				for _, l := range m.GetLabel() {
					if l.GetName() != "persistentvolume" {
						cache.PVInfo[pv][l.GetName()] = l.GetValue()
					}
				}
			}
		case "kube_persistentvolumeclaim_info":
			key := buildPVCKey(lbls)
			if key != "" {
				cache.PVCInfo[key] = make(map[string]string)
				for _, l := range m.GetLabel() {
					if l.GetName() != "namespace" && l.GetName() != "persistentvolumeclaim" {
						cache.PVCInfo[key][l.GetName()] = l.GetValue()
					}
				}
			}
		case "kube_resourcequota":
			ns := lbls.Get("namespace")
			quota := lbls.Get("resourcequota")
			resource := lbls.Get("resource")
			if ns != "" && quota != "" && resource != "" {
				cache.ResourceQuota[ns+"/"+quota+"/"+resource] = model.SampleValue(m.GetGauge().GetValue())
			}
		case "kube_limitrange":
			ns := lbls.Get("namespace")
			limit := lbls.Get("limitrange")
			resource := lbls.Get("resource")
			if ns != "" && limit != "" && resource != "" {
				cache.LimitRange[ns+"/"+limit+"/"+resource] = model.SampleValue(m.GetGauge().GetValue())
			}
		}
	}
}

// GetCache returns a copy of the current cache
func (k *KSMClient) GetCache() *KSMCache {
	k.mu.RLock()
	defer k.mu.RUnlock()
	
	// Return a shallow copy (maps are reference types, but we don't modify them after creation)
	return k.cache
}

// Helper functions

func labelsFromMetric(m *prompb.Metric) labels.Labels {
	lbls := make(labels.Labels, 0, len(m.GetLabel()))
	for _, l := range m.GetLabel() {
		lbls = append(lbls, labels.Label{Name: l.GetName(), Value: l.GetValue()})
	}
	return lbls
}

func buildPodContainerKey(lbls labels.Labels) string {
	ns := lbls.Get("namespace")
	pod := lbls.Get("pod")
	container := lbls.Get("container")
	resource := lbls.Get("resource")
	if ns == "" || pod == "" || container == "" || resource == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s/%s/%s", ns, pod, container, resource)
}

func buildPodKey(lbls labels.Labels) string {
	ns := lbls.Get("namespace")
	pod := lbls.Get("pod")
	if ns == "" || pod == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s", ns, pod)
}

func buildPVCKey(lbls labels.Labels) string {
	ns := lbls.Get("namespace")
	pvc := lbls.Get("persistentvolumeclaim")
	if ns == "" || pvc == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s", ns, pvc)
}

// PrometheusTextParser parses Prometheus text format
type PrometheusTextParser struct{}

func NewPrometheusTextParser() *PrometheusTextParser {
	return &PrometheusTextParser{}
}

func (p *PrometheusTextParser) Parse(body io.Reader) ([]*prompb.MetricFamily, error) {
	// Use Prometheus's text parser
	var parser expfmt.TextParser
	metricFamilies, err := parser.TextToMetricFamilies(body)
	if err != nil {
		return nil, err
	}
	
	// Convert to prompb format
	result := make([]*prompb.MetricFamily, 0, len(metricFamilies))
	for name, mf := range metricFamilies {
		pb := &prompb.MetricFamily{
			Name: &name,
		}
		for _, m := range mf.GetMetric() {
			pb.Metric = append(pb.Metric, m)
		}
		result = append(result, pb)
	}
	return result, nil
}
