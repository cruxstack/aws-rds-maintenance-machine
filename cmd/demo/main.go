// Package main provides the demo mode entry point for the RDS maintenance machine.
// This starts both the mock RDS server and the main HTTP server together.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/app"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/config"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/constants"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/httputil"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/mock"
)

var (
	appInst    *app.App
	mockServer *mock.Server
	mockState  *mock.State
	logger     *slog.Logger
)

func main() {
	// Parse flags
	port := flag.Int("port", 8080, "HTTP server port")
	mockPort := flag.Int("mock-port", 9080, "Mock RDS server port")
	baseWait := flag.Int("base-wait", 500, "Base wait time in ms for state transitions")
	randomRange := flag.Int("random-range", 200, "Random additional wait in ms")
	fastMode := flag.Bool("fast", false, "Fast mode (minimal waits)")
	verbose := flag.Bool("verbose", false, "Verbose logging")
	flag.Parse()

	// Load .env file if present
	godotenv.Load()

	// Set up logger
	logLevel := slog.LevelInfo
	if *verbose {
		logLevel = slog.LevelDebug
	}
	logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))

	// Check if ports are available before starting
	if err := checkPortAvailable(*mockPort); err != nil {
		logger.Error("port unavailable", slog.Int("port", *mockPort), slog.String("error", err.Error()))
		fmt.Fprintf(os.Stderr, "\nError: Port %d is already in use.\n", *mockPort)
		fmt.Fprintf(os.Stderr, "Run 'make demo-stop' to kill existing demo processes, or use -mock-port to specify a different port.\n\n")
		os.Exit(1)
	}
	if err := checkPortAvailable(*port); err != nil {
		logger.Error("port unavailable", slog.Int("port", *port), slog.String("error", err.Error()))
		fmt.Fprintf(os.Stderr, "\nError: Port %d is already in use.\n", *port)
		fmt.Fprintf(os.Stderr, "Run 'make demo-stop' to kill existing demo processes, or use -port to specify a different port.\n\n")
		os.Exit(1)
	}

	// Create a context that will be cancelled when we need to shut down
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Channel to signal fatal errors from either server
	fatalErr := make(chan error, 2)

	// Create timing config
	timing := mock.TimingConfig{
		BaseWaitMs:    *baseWait,
		RandomRangeMs: *randomRange,
		FastMode:      *fastMode,
	}

	// Initialize mock state with demo clusters
	mockState = mock.NewState(timing)
	mockState.SeedDemoClusters()
	mockState.Start()

	// Create mock server
	mockServer = mock.NewServer(mockState, logger, *verbose)

	// Start mock RDS server
	mockAddr := fmt.Sprintf(":%d", *mockPort)
	mockHTTPServer := &http.Server{
		Addr:    mockAddr,
		Handler: mockServer,
	}

	go func() {
		logger.Info("mock RDS server starting", slog.String("addr", mockAddr))
		if err := mockHTTPServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("mock server failed", slog.String("error", err.Error()))
			fatalErr <- fmt.Errorf("mock server: %w", err)
		}
	}()

	// Wait briefly for mock server to start
	time.Sleep(100 * time.Millisecond)

	// Check if mock server failed to start
	select {
	case err := <-fatalErr:
		logger.Error("failed to start", slog.String("error", err.Error()))
		os.Exit(1)
	default:
	}

	// Configure main app to use mock endpoint
	mockEndpoint := fmt.Sprintf("http://localhost:%d", *mockPort)
	os.Setenv("RDS_ENDPOINT", mockEndpoint)
	os.Setenv("APP_DEMO_MODE", "true")
	os.Setenv("APP_MOCK_ENDPOINT", mockEndpoint)
	os.Setenv("APP_PORT", fmt.Sprintf("%d", *port))

	// Set dummy AWS credentials to avoid SDK trying to fetch real credentials
	// The mock server doesn't validate signatures
	os.Setenv("AWS_ACCESS_KEY_ID", "demo-access-key")
	os.Setenv("AWS_SECRET_ACCESS_KEY", "demo-secret-key")
	os.Setenv("AWS_REGION", "us-east-1")

	// For demo mode, use shorter timeouts
	os.Setenv("APP_DEFAULT_WAIT_TIMEOUT", "60") // 1 minute
	os.Setenv("APP_DEFAULT_POLL_INTERVAL", "1") // 1 second

	cfg, err := config.NewConfig()
	if err != nil {
		logger.Error("config init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	appInst, err = app.New(ctx, cfg)
	if err != nil {
		logger.Error("app init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	handler := httputil.NewRequestHandler(appInst, logger)

	appAddr := fmt.Sprintf(":%d", *port)
	mainServer := &http.Server{
		Addr:         appAddr,
		Handler:      handler,
		ReadTimeout:  constants.DefaultReadTimeout,
		WriteTimeout: constants.DefaultWriteTimeout,
		IdleTimeout:  constants.DefaultIdleTimeout,
	}

	// Start main server in goroutine
	go func() {
		logger.Info("main server starting", slog.String("addr", appAddr))
		if err := mainServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("main server failed", slog.String("error", err.Error()))
			fatalErr <- fmt.Errorf("main server: %w", err)
		}
	}()

	// Wait briefly for main server to start
	time.Sleep(100 * time.Millisecond)

	// Check if main server failed to start
	select {
	case err := <-fatalErr:
		logger.Error("failed to start", slog.String("error", err.Error()))
		shutdownServers(mockHTTPServer, mainServer, mockState)
		os.Exit(1)
	default:
	}

	// Print startup message
	fmt.Println()
	fmt.Println("==============================================")
	fmt.Println("  RDS Maintenance Machine - DEMO MODE")
	fmt.Println("==============================================")
	fmt.Println()
	fmt.Printf("  Web UI:      http://localhost:%d\n", *port)
	fmt.Printf("  Mock RDS:    http://localhost:%d\n", *mockPort)
	fmt.Printf("  Mock State:  http://localhost:%d/mock/state\n", *mockPort)
	fmt.Println()
	fmt.Println("  Demo Clusters:")
	fmt.Println("    - demo-single     (1 instance)")
	fmt.Println("    - demo-multi      (3 instances)")
	fmt.Println("    - demo-autoscaled (4 instances, 2 autoscaled)")
	fmt.Println("    - demo-upgrade    (2 instances, ready for upgrade)")
	fmt.Println()
	fmt.Println("  Timing:")
	if *fastMode {
		fmt.Println("    Mode: FAST (minimal waits)")
	} else {
		fmt.Printf("    Base wait: %dms, Random: 0-%dms\n", *baseWait, *randomRange)
	}
	fmt.Println()
	fmt.Println("  Press Ctrl+C to stop")
	fmt.Println("==============================================")
	fmt.Println()

	// Wait for shutdown signal or fatal error
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-quit:
		logger.Info("received shutdown signal")
	case err := <-fatalErr:
		logger.Error("server crashed, shutting down all servers", slog.String("error", err.Error()))
	}

	// Shutdown both servers
	shutdownServers(mockHTTPServer, mainServer, mockState)
	cancel()
	logger.Info("all servers stopped")
}

// shutdownServers gracefully shuts down both HTTP servers.
func shutdownServers(mockServer, mainServer *http.Server, mockState *mock.State) {
	ctx, cancel := context.WithTimeout(context.Background(), constants.DemoShutdownTimeout)
	defer cancel()

	// Stop mock state transitions
	mockState.Stop()

	// Shutdown both servers concurrently
	done := make(chan struct{}, 2)

	go func() {
		if err := mockServer.Shutdown(ctx); err != nil {
			logger.Error("mock server shutdown error", slog.String("error", err.Error()))
		}
		done <- struct{}{}
	}()

	go func() {
		mainServer.SetKeepAlivesEnabled(false)
		if err := mainServer.Shutdown(ctx); err != nil {
			logger.Error("main server shutdown error", slog.String("error", err.Error()))
		}
		done <- struct{}{}
	}()

	// Wait for both to finish
	<-done
	<-done
}

// checkPortAvailable tests if a TCP port is available for binding.
func checkPortAvailable(port int) error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}
	ln.Close()
	return nil
}
