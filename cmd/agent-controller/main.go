package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/steveyegge/beads/internal/controller"
)

func main() {
	var (
		namespace     = flag.String("namespace", "gastown-test", "K8s namespace for agent pods (NEVER use 'gastown' - that is production)")
		interval      = flag.Duration("interval", 10*time.Second, "Reconciliation loop interval")
		staleTimeout  = flag.Duration("stale-timeout", 15*time.Minute, "Duration before inactive agent is stale")
		kubeconfig    = flag.String("kubeconfig", "", "Path to kubeconfig file (uses in-cluster config if empty)")
		daemonAddr    = flag.String("daemon-addr", "", "BD daemon address (host:port). Falls back to BD_DAEMON_HOST env var")
		daemonToken   = flag.String("daemon-token", "", "BD daemon auth token. Falls back to BD_DAEMON_TOKEN env var")
		image         = flag.String("image", controller.DefaultImage, "Container image for agent pods")
		bdDaemonHost  = flag.String("bd-daemon-host", "", "BD_DAEMON_HOST to set in agent pods (daemon service address)")
		bdDaemonPort  = flag.String("bd-daemon-port", "9876", "BD_DAEMON_PORT to set in agent pods")
		apiKeySecret  = flag.String("api-key-secret", "", "K8s secret name containing ANTHROPIC_API_KEY")
	)
	flag.Parse()

	// Safety check: never deploy to production namespace
	if *namespace == "gastown" {
		fmt.Fprintln(os.Stderr, "FATAL: refusing to deploy to 'gastown' namespace (production). Use 'gastown-test'.")
		os.Exit(1)
	}

	logger := log.New(os.Stdout, "[agent-controller] ", log.LstdFlags|log.Lmsgprefix)

	// Resolve daemon address
	addr := *daemonAddr
	if addr == "" {
		host := os.Getenv("BD_DAEMON_HOST")
		if host == "" {
			logger.Fatal("daemon address required: use --daemon-addr or set BD_DAEMON_HOST")
		}
		addr = host
	}

	token := *daemonToken
	if token == "" {
		token = os.Getenv("BD_DAEMON_TOKEN")
	}

	// Create K8s client
	k8sClient, err := controller.NewK8sClient(*namespace, *kubeconfig)
	if err != nil {
		logger.Fatalf("failed to create K8s client: %v", err)
	}

	// Create beads client
	beadsClient := controller.NewBeadsClient(addr, token)

	// Build config
	cfg := controller.Config{
		ReconcileInterval: *interval,
		StaleThreshold:    *staleTimeout,
		PodTemplate: controller.PodTemplateConfig{
			Image:        *image,
			Namespace:    *namespace,
			BDDaemonHost: *bdDaemonHost,
			BDDaemonPort: *bdDaemonPort,
			APIKeySecret: *apiKeySecret,
		},
	}

	// Create controller
	ctrl := controller.New(k8sClient, beadsClient, cfg, logger)

	// Set up signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		logger.Printf("received signal %v, shutting down", sig)
		cancel()
	}()

	logger.Printf("starting agent-controller (namespace=%s, daemon=%s, interval=%s)",
		*namespace, addr, *interval)

	if err := ctrl.Start(ctx); err != nil && err != context.Canceled {
		logger.Fatalf("controller error: %v", err)
	}

	logger.Printf("agent-controller stopped")
}
