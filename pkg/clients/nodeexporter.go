package clients

import (
	"context"
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

// NodeExporterClient fetches node-level metrics from Node Exporter
type NodeExporterClient struct {
	url            string
	client         *http.Client
	cache          *NodeExporterCache
	mu             sync.RWMutex
	scrapeInterval time.Duration
	stopCh         chan struct{}
}

// NodeExporterCache holds parsed node metrics
type NodeExporterCache struct {
	CPUSecondsTotal     map[string]model.SampleValue // key: node/cpu/mode
	MemoryBytes         map[string]model.SampleValue // key: node/metric (total, available, etc)
	NetworkBytesTotal   map[string]model.SampleValue // key: node/device/direction (transmit/receive)
	FilesystemBytes     map[string]model.SampleValue // key: node/device/metric (size, free, avail)
	FilesystemInodes    map[string]model.SampleValue // key: node/device/metric (total, free)
	NetstatTCP          map[string]model.SampleValue // key: node/state
	LastUpdate          time.Time
}

// NewNodeExporterClient creates a new Node Exporter client
func NewNodeExporterClient(url string, scrapeInterval time.Duration) *NodeExporterClient {
	return &NodeExporterClient{
		url:            url,
		client:         &http.Client{Timeout: 10 * time.Second},
		cache:          &NodeExporterCache{},
		scrapeInterval: scrapeInterval,
		stopCh:         make(chan struct{}),
	}
}

// Start begins periodic scraping
func (n *NodeExporterClient) Start(ctx context.Context) {
	n.scrape(ctx)
	ticker := time.NewTicker(n.scrapeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-n.stopCh:
			return
		case <-ticker.C:
			n.scrape(ctx)
		}
	}
}

// Stop stops the scraper
func (n *NodeExporterClient) Stop() {
	close(n.stopCh)
}

// scrape fetches and parses metrics from Node Exporter
func (n *NodeExporterClient) scrape(ctx context.Context) {
	req, err := http.NewRequestWithContext(ctx, "GET", n.url+"/metrics", nil)
	if err != nil {
		klog.ErrorS(err, "Failed to create Node Exporter request")
		return
	}

	resp, err := n.client.Do(req)
	if err != nil {
		klog.ErrorS(err, "Failed to scrape Node Exporter", "url", n.url)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		klog.ErrorS(nil, "Node Exporter returned non-OK status", "status", resp.StatusCode)
		return
	}

	parser := NewPrometheusTextParser()
	metricFamilies, err := parser.Parse(resp.Body)
	if err != nil {
		klog.ErrorS(err, "Failed to parse Node Exporter metrics")
		return
	}

	newCache := &NodeExporterCache{
		CPUSecondsTotal:    make(map[string]model.SampleValue),
		MemoryBytes:        make(map[string]model.SampleValue),
		NetworkBytesTotal:  make(map[string]model.SampleValue),
		FilesystemBytes:    make(map[string]model.SampleValue),
		FilesystemInodes:   make(map[string]model.SampleValue),
		NetstatTCP:         make(map[string]model.SampleValue),
		LastUpdate:         time.Now(),
	}

	for _, mf := range metricFamilies {
		n.processMetricFamily(mf, newCache)
	}

	n.mu.Lock()
	n.cache = newCache
	n.mu.Unlock()

	klog.V(4).InfoS("Node Exporter scrape completed", "metrics", len(metricFamilies), "duration", time.Since(newCache.LastUpdate))
}

// processMetricFamily processes a single metric family from Node Exporter
func (n *NodeExporterClient) processMetricFamily(mf *prompb.MetricFamily, cache *NodeExporterCache) {
	name := mf.GetName()
	for _, m := range mf.GetMetric() {
		lbls := labelsFromMetric(m)
		node := lbls.Get("node")
		if node == "" {
			// Try instance label
			node = lbls.Get("instance")
			if node != "" {
				// Strip port if present
				if idx := strings.LastIndex(node, ":"); idx != -1 {
					node = node[:idx]
				}
			}
		}
		if node == "" {
			continue
		}

		value := model.SampleValue(m.GetGauge().GetValue())
		if m.GetCounter() != nil {
			value = model.SampleValue(m.GetCounter().GetValue())
		}

		switch name {
		case "node_cpu_seconds_total":
			cpu := lbls.Get("cpu")
			mode := lbls.Get("mode")
			if cpu != "" && mode != "" {
				cache.CPUSecondsTotal[fmt.Sprintf("%s/%s/%s", node, cpu, mode)] = value
			}
		case "node_memory_MemTotal_bytes", "node_memory_MemAvailable_bytes", 
		     "node_memory_MemFree_bytes", "node_memory_Cached_bytes",
		     "node_memory_Buffers_bytes", "node_memory_SwapTotal_bytes",
		     "node_memory_SwapFree_bytes":
			metricName := strings.TrimPrefix(name, "node_memory_")
			metricName = strings.TrimSuffix(metricName, "_bytes")
			cache.MemoryBytes[fmt.Sprintf("%s/%s", node, metricName)] = value
		case "node_network_receive_bytes_total", "node_network_transmit_bytes_total",
		     "node_network_receive_packets_total", "node_network_transmit_packets_total",
		     "node_network_receive_errs_total", "node_network_transmit_errs_total",
		     "node_network_receive_drop_total", "node_network_transmit_drop_total":
			device := lbls.Get("device")
			if device != "" {
				direction := "receive"
				if strings.Contains(name, "transmit") {
					direction = "transmit"
				}
				metricType := strings.TrimPrefix(name, fmt.Sprintf("node_network_%s_", direction))
				metricType = strings.TrimSuffix(metricType, "_total")
				cache.NetworkBytesTotal[fmt.Sprintf("%s/%s/%s/%s", node, device, direction, metricType)] = value
			}
		case "node_filesystem_size_bytes", "node_filesystem_free_bytes", 
		     "node_filesystem_avail_bytes", "node_filesystem_readonly":
			device := lbls.Get("device")
			fstype := lbls.Get("fstype")
			mountpoint := lbls.Get("mountpoint")
			if device != "" {
				key := fmt.Sprintf("%s/%s", node, device)
				if mountpoint != "" {
					key += "/" + mountpoint
				}
				metricName := strings.TrimPrefix(name, "node_filesystem_")
				metricName = strings.TrimSuffix(metricName, "_bytes")
				cache.FilesystemBytes[fmt.Sprintf("%s/%s", key, metricName)] = value
			}
		case "node_filesystem_inodes_total", "node_filesystem_inodes_free":
			device := lbls.Get("device")
			fstype := lbls.Get("fstype")
			mountpoint := lbls.Get("mountpoint")
			if device != "" {
				key := fmt.Sprintf("%s/%s", node, device)
				if mountpoint != "" {
					key += "/" + mountpoint
				}
				metricName := strings.TrimPrefix(name, "node_filesystem_inodes_")
				cache.FilesystemInodes[fmt.Sprintf("%s/%s", key, metricName)] = value
			}
		case "node_netstat_Tcp_CurrEstab", "node_netstat_Tcp_Tw", "node_netstat_Tcp_Alloc",
		     "node_netstat_Tcp_InSegs", "node_netstat_Tcp_OutSegs", "node_netstat_Tcp_RetransSegs":
			state := strings.TrimPrefix(name, "node_netstat_Tcp_")
			cache.NetstatTCP[fmt.Sprintf("%s/%s", node, state)] = value
		}
	}
}

// GetCache returns a copy of the current cache
func (n *NodeExporterClient) GetCache() *NodeExporterCache {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.cache
}

// GetNodeCPUUsage calculates CPU usage percentage for a node
func (n *NodeExporterClient) GetNodeCPUUsage(node string) (usagePct float64, found bool) {
	cache := n.GetCache()
	
	var total, idle model.SampleValue
	for key, value := range cache.CPUSecondsTotal {
		parts := strings.Split(key, "/")
		if len(parts) != 3 || parts[0] != node {
			continue
		}
		total += value
		if parts[2] == "idle" || parts[2] == "iowait" {
			idle += value
		}
	}
	
	if total == 0 {
		return 0, false
	}
	
	usagePct = float64((total - idle) / total * 100)
	return usagePct, true
}

// GetNodeMemoryUsage returns memory usage in bytes
func (n *NodeExporterClient) GetNodeMemoryUsage(node string) (total, available, used model.SampleValue, found bool) {
	cache := n.GetCache()
	
	totalKey := fmt.Sprintf("%s/MemTotal", node)
	availKey := fmt.Sprintf("%s/MemAvailable", node)
	
	totalVal, totalOk := cache.MemoryBytes[totalKey]
	availVal, availOk := cache.MemoryBytes[availKey]
	
	if !totalOk || !availOk {
		return 0, 0, 0, false
	}
	
	usedVal := totalVal - availVal
	return totalVal, availVal, usedVal, true
}

// GetNodeNetworkEgress returns network egress bytes for a node
func (n *NodeExporterClient) GetNodeNetworkEgress(node string) (bytes model.SampleValue, found bool) {
	cache := n.GetCache()
	
	var total model.SampleValue
	found = false
	for key, value := range cache.NetworkBytesTotal {
		parts := strings.Split(key, "/")
		if len(parts) != 4 || parts[0] != node || parts[2] != "transmit" {
			continue
		}
		if parts[3] == "bytes" {
			total += value
			found = true
		}
	}
	return total, found
}

// GetNodeFilesystemUsage returns filesystem usage for a node
func (n *NodeExporterClient) GetNodeFilesystemUsage(node string) (map[string]FilesystemUsage, bool) {
	cache := n.GetCache()
	
	result := make(map[string]FilesystemUsage)
	found := false
	
	for key, value := range cache.FilesystemBytes {
		parts := strings.Split(key, "/")
		if len(parts) < 3 || parts[0] != node {
			continue
		}
		
		device := parts[1]
		mountpoint := ""
		if len(parts) > 3 {
			mountpoint = strings.Join(parts[2:len(parts)-1], "/")
		}
		metric := parts[len(parts)-1]
		
		fsKey := device
		if mountpoint != "" {
			fsKey += ":" + mountpoint
		}
		
		usage, ok := result[fsKey]
		if !ok {
			usage = FilesystemUsage{Device: device, Mountpoint: mountpoint}
		}
		
		switch metric {
		case "size":
			usage.Size = value
		case "free":
			usage.Free = value
		case "avail":
			usage.Avail = value
		case "readonly":
			usage.ReadOnly = value == 1
		}
		result[fsKey] = usage
		found = true
	}
	
	return result, found
}

type FilesystemUsage struct {
	Device     string
	Mountpoint string
	Size       model.SampleValue
	Free       model.SampleValue
	Avail      model.SampleValue
	ReadOnly   bool
}

func (f FilesystemUsage) Used() model.SampleValue {
	return f.Size - f.Free
}

func (f FilesystemUsage) UsedPct() float64 {
	if f.Size == 0 {
		return 0
	}
	return float64(f.Used()) / float64(f.Size) * 100
}
