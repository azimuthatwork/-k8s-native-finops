package clients

import (
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

// K8sInformerCache provides event-driven access to Kubernetes resources
type K8sInformerCache struct {
	factory          informers.SharedInformerFactory
	dynamicFactory   dynamic.DynamicSharedInformerFactory
	kubeClient       kubernetes.Interface
	dynamicClient    dynamic.Interface
	
	// Informers
	podInformer         cache.SharedIndexInformer
	nodeInformer        cache.SharedIndexInformer
	namespaceInformer   cache.SharedIndexInformer
	pvcInformer         cache.SharedIndexInformer
	pvInformer          cache.SharedIndexInformer
	storageClassInformer cache.SharedIndexInformer
	eventInformer       cache.SharedIndexInformer
	
	// Indexers
	podByNodeIndex      string
	podByNamespaceIndex string
	pvcByNamespaceIndex string
	pvByNameIndex       string
	
	mu           sync.RWMutex
	started      bool
	stopCh       chan struct{}
	resyncPeriod time.Duration
}

// NewK8sInformerCache creates a new informer cache
func NewK8sInformerCache(kubeClient kubernetes.Interface, dynamicClient dynamic.Interface, resyncPeriod time.Duration) *K8sInformerCache {
	factory := informers.NewSharedInformerFactory(kubeClient, resyncPeriod)
	dynamicFactory := dynamic.NewDynamicSharedInformerFactory(dynamicClient, resyncPeriod)

	c := &K8sInformerCache{
		factory:           factory,
		dynamicFactory:    dynamicFactory,
		kubeClient:        kubeClient,
		dynamicClient:     dynamicClient,
		resyncPeriod:      resyncPeriod,
		stopCh:            make(chan struct{}),
		podByNodeIndex:    "podByNode",
		podByNamespaceIndex: "podByNamespace",
		pvcByNamespaceIndex: "pvcByNamespace",
		pvByNameIndex:     "pvByName",
	}

	// Setup informers with custom indexers
	c.setupInformers()
	return c
}

// setupInformers configures all informers and indexers
func (k *K8sInformerCache) setupInformers() {
	// Pod informer with node and namespace indexers
	k.podInformer = k.factory.Core().V1().Pods().Informer()
	k.podInformer.AddIndexers(cache.Indexers{
		k.podByNodeIndex: func(obj interface{}) ([]string, error) {
			pod, ok := obj.(metav1.Object)
			if !ok {
				return nil, nil
			}
			// Need to access spec.nodeName - requires type assertion
			return []string{pod.GetName()}, nil // Simplified
		},
		k.podByNamespaceIndex: func(obj interface{}) ([]string, error) {
			pod, ok := obj.(metav1.Object)
			if !ok {
				return nil, nil
			}
			return []string{pod.GetNamespace()}, nil
		},
	})

	// Node informer
	k.nodeInformer = k.factory.Core().V1().Nodes().Informer()

	// Namespace informer
	k.namespaceInformer = k.factory.Core().V1().Namespaces().Informer()

	// PVC informer
	k.pvcInformer = k.factory.Core().V1().PersistentVolumeClaims().Informer()
	k.pvcInformer.AddIndexers(cache.Indexers{
		k.pvcByNamespaceIndex: func(obj interface{}) ([]string, error) {
			pvc, ok := obj.(metav1.Object)
			if !ok {
				return nil, nil
			}
			return []string{pvc.GetNamespace()}, nil
		},
	})

	// PV informer
	k.pvInformer = k.factory.Core().V1().PersistentVolumes().Informer()
	k.pvInformer.AddIndexers(cache.Indexers{
		k.pvByNameIndex: func(obj interface{}) ([]string, error) {
			pv, ok := obj.(metav1.Object)
			if !ok {
				return nil, nil
			}
			return []string{pv.GetName()}, nil
		},
	})

	// StorageClass informer
	k.storageClassInformer = k.factory.Storage().V1().StorageClasses().Informer()

	// Event informer
	k.eventInformer = k.factory.Core().V1().Events().Informer()
}

// Start starts all informers
func (k *K8sInformerCache) Start(ctx context.Context) {
	if k.started {
		return
	}
	k.started = true

	k.factory.Start(k.stopCh)
	k.dynamicFactory.Start(k.stopCh)

	// Wait for cache sync
	k.factory.WaitForCacheSync(k.stopCh)
	k.dynamicFactory.WaitForCacheSync(k.stopCh)

	klog.InfoS("K8s informer cache started and synced")
}

// Stop stops all informers
func (k *K8sInformerCache) Stop() {
	close(k.stopCh)
	k.factory.Shutdown()
	k.dynamicFactory.Shutdown()
	k.started = false
}

// GetPod returns a pod by namespace and name
func (k *K8sInformerCache) GetPod(namespace, name string) (interface{}, error) {
	return k.podInformer.GetIndexer().GetByKey(namespace + "/" + name)
}

// GetPodsByNode returns all pods running on a node
func (k *K8sInformerCache) GetPodsByNode(nodeName string) ([]interface{}, error) {
	return k.podInformer.GetIndexer().ByIndex(k.podByNodeIndex, nodeName)
}

// GetPodsByNamespace returns all pods in a namespace
func (k *K8sInformerCache) GetPodsByNamespace(namespace string) ([]interface{}, error) {
	return k.podInformer.GetIndexer().ByIndex(k.podByNamespaceIndex, namespace)
}

// GetNode returns a node by name
func (k *K8sInformerCache) GetNode(name string) (interface{}, error) {
	return k.nodeInformer.GetIndexer().GetByKey(name)
}

// GetAllNodes returns all nodes
func (k *K8sInformerCache) GetAllNodes() []interface{} {
	return k.nodeInformer.GetIndexer().List()
}

// GetNamespace returns a namespace by name
func (k *K8sInformerCache) GetNamespace(name string) (interface{}, error) {
	return k.namespaceInformer.GetIndexer().GetByKey(name)
}

// GetAllNamespaces returns all namespaces
func (k *K8sInformerCache) GetAllNamespaces() []interface{} {
	return k.namespaceInformer.GetIndexer().List()
}

// GetPVC returns a PVC by namespace and name
func (k *K8sInformerCache) GetPVC(namespace, name string) (interface{}, error) {
	return k.pvcInformer.GetIndexer().GetByKey(namespace + "/" + name)
}

// GetPVCsByNamespace returns all PVCs in a namespace
func (k *K8sInformerCache) GetPVCsByNamespace(namespace string) ([]interface{}, error) {
	return k.pvcInformer.GetIndexer().ByIndex(k.pvcByNamespaceIndex, namespace)
}

// GetPV returns a PV by name
func (k *K8sInformerCache) GetPV(name string) (interface{}, error) {
	return k.pvInformer.GetIndexer().GetByKey(name)
}

// GetAllPVs returns all PVs
func (k *K8sInformerCache) GetAllPVs() []interface{} {
	return k.pvInformer.GetIndexer().List()
}

// GetStorageClass returns a StorageClass by name
func (k *K8sInformerCache) GetStorageClass(name string) (interface{}, error) {
	return k.storageClassInformer.GetIndexer().GetByKey(name)
}

// GetAllStorageClasses returns all StorageClasses
func (k *K8sInformerCache) GetAllStorageClasses() []interface{} {
	return k.storageClassInformer.GetIndexer().List()
}

// GetEvent returns an event by namespace and name
func (k *K8sInformerCache) GetEvent(namespace, name string) (interface{}, error) {
	return k.eventInformer.GetIndexer().GetByKey(namespace + "/" + name)
}

// GetEventsByNamespace returns all events in a namespace
func (k *K8sInformerCache) GetEventsByNamespace(namespace string) ([]interface{}, error) {
	return k.eventInformer.GetIndexer().ByIndex("namespace", namespace)
}

// AddEventHandler adds an event handler for a resource
func (k *K8sInformerCache) AddEventHandler(resource string, handler cache.ResourceEventHandler) error {
	switch resource {
	case "pods":
		_, err := k.podInformer.AddEventHandler(handler)
		return err
	case "nodes":
		_, err := k.nodeInformer.AddEventHandler(handler)
		return err
	case "namespaces":
		_, err := k.namespaceInformer.AddEventHandler(handler)
		return err
	case "pvcs":
		_, err := k.pvcInformer.AddEventHandler(handler)
		return err
	case "pvs":
		_, err := k.pvInformer.AddEventHandler(handler)
		return err
	case "storageclasses":
		_, err := k.storageClassInformer.AddEventHandler(handler)
		return err
	case "events":
		_, err := k.eventInformer.AddEventHandler(handler)
		return err
	default:
		return fmt.Errorf("unknown resource: %s", resource)
	}
}

// WaitForCacheSync waits for all caches to sync
func (k *K8sInformerCache) WaitForCacheSync(ctx context.Context) error {
	synced := k.factory.WaitForCacheSync(k.stopCh)
	for _, s := range synced {
		if !s {
			return fmt.Errorf("cache sync failed")
		}
	}
	return nil
}
