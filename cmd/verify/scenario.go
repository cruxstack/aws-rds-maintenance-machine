package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsrds "github.com/aws/aws-sdk-go-v2/service/rds"

	"github.com/mpz/devops/tools/rds-maint-machine/internal/app"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/config"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/machine"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/notifiers"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/rds"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/types"
)

// TestScenario defines a test case with input events and expected outcomes.
type TestScenario struct {
	Name            string            `yaml:"name" json:"name"`
	Description     string            `yaml:"description,omitempty" json:"description,omitempty"`
	Action          string            `yaml:"action" json:"action"`
	OperationType   string            `yaml:"operation_type,omitempty" json:"operation_type,omitempty"`
	ClusterID       string            `yaml:"cluster_id,omitempty" json:"cluster_id,omitempty"`
	Params          map[string]any    `yaml:"params,omitempty" json:"params,omitempty"`
	ConfigOverrides map[string]string `yaml:"config_overrides,omitempty" json:"config_overrides,omitempty"`
	ExpectedCalls   []ExpectedCall    `yaml:"expected_calls" json:"expected_calls"`
	ExpectedActions []string          `yaml:"expected_actions,omitempty" json:"expected_actions,omitempty"` // Expected AWS actions
	MockResponses   []MockResponse    `yaml:"mock_responses" json:"mock_responses"`
	ExpectError     bool              `yaml:"expect_error,omitempty" json:"expect_error,omitempty"`
	ExpectSteps     int               `yaml:"expect_steps,omitempty" json:"expect_steps,omitempty"` // Expected number of steps
}

// ExpectedCall defines an HTTP API call the test expects.
type ExpectedCall struct {
	Service string `yaml:"service" json:"service"`
	Method  string `yaml:"method" json:"method"`
	Path    string `yaml:"path" json:"path"`
	Action  string `yaml:"action,omitempty" json:"action,omitempty"` // AWS action
}

// runScenario executes a single test scenario with mock HTTP servers.
func runScenario(ctx context.Context, scenario TestScenario, verbose bool, logger *slog.Logger) error {
	startTime := time.Now()

	fmt.Printf("\n> Running: %s\n", scenario.Name)
	if scenario.Description != "" {
		fmt.Printf("  %s\n", scenario.Description)
	}

	// Set up mock servers
	rdsMock := NewMockServer("RDS", scenario.MockResponses, verbose)
	slackMock := NewMockServer("Slack", scenario.MockResponses, verbose)

	// Generate TLS cert for mock servers at runtime
	tlsCert, certPool, err := generateSelfSignedCert()
	if err != nil {
		return fmt.Errorf("generate cert: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
	}

	// Create listeners on dynamic ports to avoid conflicts
	rdsListener, err := tls.Listen("tcp", "localhost:0", tlsConfig)
	if err != nil {
		return fmt.Errorf("create rds listener: %w", err)
	}
	defer rdsListener.Close()

	slackListener, err := tls.Listen("tcp", "localhost:0", tlsConfig)
	if err != nil {
		return fmt.Errorf("create slack listener: %w", err)
	}
	defer slackListener.Close()

	// Get the assigned addresses
	rdsAddr := rdsListener.Addr().(*net.TCPAddr)
	slackAddr := slackListener.Addr().(*net.TCPAddr)

	rdsServer := &http.Server{Handler: rdsMock}
	slackServer := &http.Server{Handler: slackMock}

	go func() {
		if err := rdsServer.Serve(rdsListener); err != http.ErrServerClosed {
			logger.Error("rds mock server error", slog.String("error", err.Error()))
		}
	}()

	go func() {
		if err := slackServer.Serve(slackListener); err != http.ErrServerClosed {
			logger.Error("slack mock server error", slog.String("error", err.Error()))
		}
	}()

	// Give servers a moment to start
	time.Sleep(50 * time.Millisecond)

	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		rdsServer.Shutdown(shutdownCtx)
		slackServer.Shutdown(shutdownCtx)
	}()

	// Create HTTP client that trusts our self-signed cert
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: certPool,
			},
		},
		Timeout: 10 * time.Second,
	}

	// Also update the default transport for any other HTTP calls
	http.DefaultTransport = &http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs: certPool,
		},
	}

	// Apply config overrides
	for key, value := range scenario.ConfigOverrides {
		os.Setenv(key, value)
	}

	// Create config
	cfg, err := config.NewConfig()
	if err != nil {
		return fmt.Errorf("config creation failed: %w", err)
	}
	cfg.SlackEnabled = false // Disable Slack for tests

	// Create AWS config with custom HTTP client
	awsCfg := aws.Config{
		Region:     "us-east-1",
		HTTPClient: httpClient,
		// Disable retries to fail faster in tests
		RetryMaxAttempts: 1,
	}

	// Create RDS client pointing to mock server with dynamic port
	rdsEndpoint := fmt.Sprintf("https://localhost:%d", rdsAddr.Port)
	_ = slackAddr // Slack mock not currently used but available
	rdsClient := rds.NewClientWithRDS(awsrds.NewFromConfig(awsCfg, func(o *awsrds.Options) {
		o.BaseEndpoint = aws.String(rdsEndpoint)
	}))

	// Create ClientManager for tests (uses the same endpoint for all regions)
	clientManager := rds.NewClientManager(rds.ClientManagerConfig{
		BaseConfig: awsCfg,
		DemoMode:   true,
		BaseURL:    rdsEndpoint,
	})

	// Pre-populate with our test client
	_ = rdsClient // Keep reference for direct API tests

	// Create engine with mock RDS client
	engine := machine.NewEngine(machine.EngineConfig{
		ClientManager:       clientManager,
		Logger:              logger,
		Notifier:            &notifiers.NullNotifier{},
		DefaultRegion:       "us-east-1",
		DefaultWaitTimeout:  5 * time.Second,
		DefaultPollInterval: 100 * time.Millisecond,
	})

	// Create app with mocked engine
	appInst := app.NewWithEngine(cfg, engine, &notifiers.NullNotifier{})

	if verbose {
		fmt.Printf("\n  Application Output:\n")
	}

	// Convert params map to JSON for the request
	var paramsJSON json.RawMessage
	if scenario.Params != nil {
		paramsJSON, err = json.Marshal(scenario.Params)
		if err != nil {
			return fmt.Errorf("marshal params: %w", err)
		}
	}

	// Execute the scenario action
	var processErr error
	var operation *types.Operation

	switch scenario.Action {
	case "create_operation":
		req := app.CreateOperationRequest{
			Type:      types.OperationType(scenario.OperationType),
			ClusterID: scenario.ClusterID,
			Params:    paramsJSON,
		}
		operation, processErr = appInst.CreateOperation(ctx, req)

	case "get_cluster_info":
		_, processErr = rdsClient.GetClusterInfo(ctx, scenario.ClusterID)

	default:
		return fmt.Errorf("unknown action: %s", scenario.Action)
	}

	if scenario.ExpectError {
		if processErr == nil {
			return fmt.Errorf("expected error but succeeded")
		}
		if verbose {
			fmt.Printf("  Expected error occurred: %v\n", processErr)
		}
	} else {
		if processErr != nil {
			return fmt.Errorf("unexpected error: %w", processErr)
		}
	}

	// Validate step count if specified
	if scenario.ExpectSteps > 0 && operation != nil {
		if len(operation.Steps) != scenario.ExpectSteps {
			return fmt.Errorf("expected %d steps but got %d", scenario.ExpectSteps, len(operation.Steps))
		}
		if verbose {
			fmt.Printf("  Steps created: %d\n", len(operation.Steps))
			for i, step := range operation.Steps {
				fmt.Printf("    [%d] %s: %s\n", i+1, step.Action, step.Name)
			}
		}
	}

	// Validate expected calls
	time.Sleep(200 * time.Millisecond)

	rdsReqs := rdsMock.GetRequests()
	slackReqs := slackMock.GetRequests()

	allReqs := make(map[string][]RequestRecord)
	allReqs["rds"] = rdsReqs
	allReqs["slack"] = slackReqs

	// Validate expected actions if specified
	if len(scenario.ExpectedActions) > 0 {
		if err := validateExpectedActions(scenario.ExpectedActions, rdsReqs); err != nil {
			fmt.Printf("\n  Validation:\n")
			fmt.Printf("    FAILED: %v\n", err)
			fmt.Printf("\n  Captured RDS actions:\n")
			for i, req := range rdsReqs {
				fmt.Printf("      [%d] %s\n", i+1, req.Action)
			}
			return err
		}
	}

	if err := validateExpectedCalls(scenario.ExpectedCalls, allReqs); err != nil {
		fmt.Printf("\n  Validation:\n")
		fmt.Printf("    FAILED: %v\n", err)
		fmt.Printf("\n  Captured requests:\n")
		if len(rdsReqs) > 0 {
			fmt.Printf("    RDS (%d):\n", len(rdsReqs))
			for i, req := range rdsReqs {
				fmt.Printf("      [%d] %s %s [%s]\n", i+1, req.Method, req.Path, req.Action)
			}
		}
		if len(slackReqs) > 0 {
			fmt.Printf("    Slack (%d):\n", len(slackReqs))
			for i, req := range slackReqs {
				fmt.Printf("      [%d] %s %s\n", i+1, req.Method, req.Path)
			}
		}
		return err
	}

	duration := time.Since(startTime)
	fmt.Printf("  PASSED (%.2fs)\n", duration.Seconds())
	return nil
}

// validateExpectedCalls verifies that all expected calls were made.
func validateExpectedCalls(expected []ExpectedCall, allReqs map[string][]RequestRecord) error {
	for _, exp := range expected {
		reqs := allReqs[exp.Service]
		found := false
		for _, req := range reqs {
			methodMatch := req.Method == exp.Method
			pathMatch := matchPath(req.Path, exp.Path)
			actionMatch := exp.Action == "" || req.Action == exp.Action

			if methodMatch && pathMatch && actionMatch {
				found = true
				break
			}
		}
		if !found {
			if exp.Action != "" {
				return fmt.Errorf("expected call not found: %s %s %s [%s]", exp.Service, exp.Method, exp.Path, exp.Action)
			}
			return fmt.Errorf("expected call not found: %s %s %s", exp.Service, exp.Method, exp.Path)
		}
	}
	return nil
}

// validateExpectedActions verifies that all expected AWS actions were called.
func validateExpectedActions(expected []string, reqs []RequestRecord) error {
	actionCounts := make(map[string]int)
	for _, req := range reqs {
		if req.Action != "" {
			actionCounts[req.Action]++
		}
	}

	for _, exp := range expected {
		if actionCounts[exp] == 0 {
			return fmt.Errorf("expected action not found: %s", exp)
		}
	}
	return nil
}
