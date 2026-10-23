//go:build ignore

package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	finopsv1alpha1 "github.com/yourorg/kube-native-finops/api/v1alpha1"
	"github.com/yourorg/kube-native-finops/pkg/allocation"
	"github.com/yourorg/kube-native-finops/pkg/api"
	"github.com/yourorg/kube-native-finops/pkg/clients"
	"github.com/yourorg/kube-native-finops/pkg/storage"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(finopsv1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var tsdbDataDir string
	var tsdbRetention string
	var maxActiveSeries int

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", true, "Enable leader election for controller manager.")
	flag.StringVar(&tsdbDataDir, "tsdb-data-dir", "/data/tsdb", "Directory for TSDB data")
	flag.StringVar(&tsdbRetention, "tsdb-retention", "336h", "TSDB retention duration (e.g., 336h for 14 days)")
	flag.IntVar(&maxActiveSeries, "tsdb-max-series", 500000, "Maximum active series in TSDB")
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	// Create a custom registry for FinOps metrics
	reg := prometheus.NewRegistry()
	// Register Go and process collectors
	reg.MustRegister(prometheus.NewGoCollector())
	reg.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))

	// Parse retention duration
	retentionDur, err := time.ParseDuration(tsdbRetention)
	if err != nil {
		setupLog.Error(err, "Invalid retention duration", "retention", tsdbRetention)
		os.Exit(1)
	}

	// Initialize TSDB
	tsdbConfig := storage.DefaultTSDBConfig(tsdbDataDir)
	tsdbConfig.RetentionDuration = retentionDur
	tsdbConfig.MaxActiveSeries = maxActiveSeries
	tsdbConfig.MetricsRegisterer = reg

	tsdbInstance, err := storage.NewTSDB(tsdbConfig)
	if err != nil {
		setupLog.Error(err, "Failed to initialize TSDB")
		os.Exit(1)
	}
	defer tsdbInstance.Close()

	setupLog.Info("TSDB initialized", "dataDir", tsdbDataDir, "retention", retentionDur)

	// Initialize clients
	clientFactory, err := clients.NewClientFactory()
	if err != nil {
		setupLog.Error(err, "Failed to create client factory")
		os.Exit(1)
	}

	// Initialize allocation engine
	allocEngine := allocation.NewEngine(clientFactory, tsdbInstance)

	// Initialize API gateway
	queryAPI := api.NewQueryAPI(tsdbInstance.Querier(), reg)

	// Initialize client factory
	clientConfig := &clients.ClientConfig{
		KSMURL:                "http://kube-state-metrics:8080",
		KSMScrapeInterval:     30 * time.Second,
		MetricsScrapeInterval: 30 * time.Second,
		NodeExporterURL:       "http://node-exporter:9100",
		NodeExporterInterval:  30 * time.Second,
		CloudProvider:         "static",
		CloudRegion:           "us-east-1",
		InformerResyncPeriod:  10 * time.Minute,
		CacheTTL:              5 * time.Minute,
		SpotCacheTTL:          1 * time.Minute,
		RedisConfig: clients.RedisConfig{
			Address:  os.Getenv("REDIS_ADDRESS"),
			Password: os.Getenv("REDIS_PASSWORD"),
			DB:       0,
			Enabled:  os.Getenv("REDIS_ENABLED") == "true",
		},
	}

	clientFactory, err := clients.NewClientFactory(ctx, clientConfig)
	if err != nil {
		setupLog.Error(err, "Failed to create client factory")
		os.Exit(1)
	}
	defer clientFactory.Stop()

	// Initialize dashboard handler with caching
	dashboardHandler := api.NewDashboardHandler(
		setupLog,
		queryAPI.PromClient(),
		clientFactory,
		clientFactory.DashboardCache(),
		5*time.Minute,
	)

	// Setup metrics server
	metricsServer := server.New(server.Options{
		BindAddress: metricsAddr,
	})

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "cost-controller-leader",
	})
	if err != nil {
		setupLog.Error(err, "Unable to start manager")
		os.Exit(1)
	}

	// Setup reconcilers
	if err = (&allocation.CostAllocationReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Engine: allocEngine,
		TSDB:   tsdbInstance,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Unable to create controller", "controller", "CostAllocation")
		os.Exit(1)
	}

	if err = (&allocation.NodeCostReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Engine: allocEngine,
		TSDB:   tsdbInstance,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Unable to create controller", "controller", "NodeCost")
		os.Exit(1)
	}

	if err = (&allocation.PVCostReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Engine: allocEngine,
		TSDB:   tsdbInstance,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Unable to create controller", "controller", "PVCost")
		os.Exit(1)
	}

	// +kubebuilder:scaffold:builder

	// Add health checks
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "Unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "Unable to set up ready check")
		os.Exit(1)
	}

	// Start custom metrics endpoint on a separate port
	go func() {
		r := chi.NewRouter()
		r.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{
			Registry:          reg,
			EnableOpenMetrics: true,
		}))
		r.HandleFunc("/api/v1/query", queryAPI.HandleQuery)
		r.HandleFunc("/api/v1/query_range", queryAPI.HandleQueryRange)
		r.HandleFunc("/api/v1/series", queryAPI.HandleSeries)
		r.HandleFunc("/api/v1/labels", queryAPI.HandleLabels)
		r.HandleFunc("/api/v1/label/", queryAPI.HandleLabelValues)

		// Dashboard endpoints
		r.Route("/api/v1/dashboard", func(r chi.Router) {
			dashboardHandler.RegisterRoutes(r)
		})
		r.HandleFunc("/api/v1/dashboard/workloads", handleDashboardWorkloads)
		r.HandleFunc("/api/v1/dashboard/nodes", handleDashboardNodes)
		r.HandleFunc("/api/v1/dashboard/storage", handleDashboardStorage)
		r.HandleFunc("/api/v1/dashboard/network", handleDashboardNetwork)
		r.HandleFunc("/api/v1/recommendations", handleRecommendations)
		r.HandleFunc("/api/v1/budgets", handleBudgets)

		server := &http.Server{
			Addr:    ":8082",
			Handler: r,
		}

		setupLog.Info("Starting API server", "addr", ":8082")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			setupLog.Error(err, "API server failed")
		}
	}()

	// Start manager
	ctx := ctrl.SetupSignalHandler()
	setupLog.Info("Starting manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "Problem running manager")
		os.Exit(1)
	}
}

// Placeholder handlers for other dashboard endpoints
func handleDashboardWorkloads(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"success","data":[]}`))
}

func handleDashboardNodes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"success","data":[]}`))
}

func handleDashboardStorage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"success","data":[]}`))
}

func handleDashboardNetwork(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"success","data":[]}`))
}

func handleRecommendations(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"success","data":[]}`))
}

func handleBudgets(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"success","data":[]}`))
}
