package machine

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/mock"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/rds"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/storage"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/types"
)

// TestHandleWaitInstanceAvailable_RejectsMissingInstanceID verifies that
// handleWaitInstanceAvailable correctly rejects wait steps that are missing
// instance_id when they are NOT for the temp instance.
// This is a regression test for the bug where missing instance_id caused
// the handler to fall back to the temp instance ID, causing all modifications
// to run in parallel.
func TestHandleWaitInstanceAvailable_RejectsMissingInstanceID(t *testing.T) {
	timing := mock.TimingConfig{
		BaseWaitMs:    10,
		RandomRangeMs: 0,
		FastMode:      true,
	}
	mockState := mock.NewState(timing)
	mockState.SeedDemoClusters()
	mockState.Start()
	defer mockState.Stop()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	mockServer := mock.NewServer(mockState, logger, false)
	server := httptest.NewServer(mockServer)
	defer server.Close()

	clientManager := rds.NewClientManager(rds.ClientManagerConfig{
		BaseConfig: aws.Config{
			Region:      "us-east-1",
			Credentials: aws.AnonymousCredentials{},
		},
		DemoMode: true,
		BaseURL:  server.URL,
	})

	engine := &Engine{
		operations:          make(map[string]*types.Operation),
		events:              make(map[string][]types.Event),
		logger:              logger,
		handlers:            make(map[string]StepHandler),
		store:               &storage.NullStore{},
		defaultWaitTimeout:  5 * time.Second,
		defaultPollInterval: 100 * time.Millisecond,
		clientManager:       clientManager,
	}

	// Create an operation with a completed create_temp_instance step
	// so findCreatedInstanceID would return something if the fallback was triggered
	tempInstanceResult, _ := json.Marshal(map[string]string{
		"instance_id": "demo-multi-maint-temp",
	})

	op := &types.Operation{
		ID:        "test-op-handler",
		Type:      types.OperationTypeInstanceTypeChange,
		State:     types.StateRunning,
		ClusterID: "demo-multi",
		Region:    "us-east-1",
		Steps: []types.Step{
			{
				ID:     "step-1",
				Name:   "Create temp instance",
				Action: "create_temp_instance",
				State:  types.StepStateCompleted,
				Result: tempInstanceResult,
			},
			{
				ID:     "step-2",
				Name:   "Wait for temp instance",
				Action: "wait_instance_available",
				State:  types.StepStatePending,
				// No Parameters - this is OK for temp instance wait
			},
			{
				ID:     "step-3",
				Name:   "Wait for instance: demo-multi-writer",
				Action: "wait_instance_available",
				State:  types.StepStatePending,
				// No Parameters - this should FAIL! Missing instance_id
			},
		},
		CreatedAt: time.Now(),
	}

	ctx := context.Background()

	// Test 1: Wait for temp instance (no instance_id) - should succeed using fallback
	tempWaitStep := &op.Steps[1]
	err := engine.handleWaitInstanceAvailable(ctx, op, tempWaitStep)
	// Note: This might fail because the mock instance doesn't exist, but it should
	// NOT fail with "instance_id required" error - it should use the fallback
	if err != nil && err.Error() == "invalid parameter: instance_id required for step \"Wait for temp instance\" - missing parameter would cause parallel modifications" {
		t.Error("Wait for temp instance should allow fallback to findCreatedInstanceID")
	}

	// Test 2: Wait for modified instance (no instance_id) - should FAIL
	modifyWaitStep := &op.Steps[2]
	err = engine.handleWaitInstanceAvailable(ctx, op, modifyWaitStep)
	if err == nil {
		t.Error("Expected error when instance_id is missing for non-temp wait step, but got nil")
	} else if err.Error() != "invalid parameter: instance_id required for step \"Wait for instance: demo-multi-writer\" - missing parameter would cause parallel modifications" {
		// Check that we got the right type of error
		t.Logf("Got error: %v", err)
		// The error message should indicate this is about parallel modifications
		if !containsAny(err.Error(), "instance_id required", "parallel") {
			t.Errorf("Expected error about missing instance_id causing parallel modifications, got: %v", err)
		}
	}
}

// TestHandleWaitInstanceAvailable_AcceptsExplicitInstanceID verifies that
// handleWaitInstanceAvailable works correctly when instance_id is provided.
func TestHandleWaitInstanceAvailable_AcceptsExplicitInstanceID(t *testing.T) {
	timing := mock.TimingConfig{
		BaseWaitMs:    10,
		RandomRangeMs: 0,
		FastMode:      true,
	}
	mockState := mock.NewState(timing)
	mockState.SeedDemoClusters()
	mockState.Start()
	defer mockState.Stop()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	mockServer := mock.NewServer(mockState, logger, false)
	server := httptest.NewServer(mockServer)
	defer server.Close()

	clientManager := rds.NewClientManager(rds.ClientManagerConfig{
		BaseConfig: aws.Config{
			Region:      "us-east-1",
			Credentials: aws.AnonymousCredentials{},
		},
		DemoMode: true,
		BaseURL:  server.URL,
	})

	engine := &Engine{
		operations:          make(map[string]*types.Operation),
		events:              make(map[string][]types.Event),
		logger:              logger,
		handlers:            make(map[string]StepHandler),
		store:               &storage.NullStore{},
		defaultWaitTimeout:  5 * time.Second,
		defaultPollInterval: 100 * time.Millisecond,
		clientManager:       clientManager,
	}

	// Create wait params with explicit instance_id
	waitParams, _ := json.Marshal(map[string]string{
		"instance_id": "demo-multi-writer", // This instance exists in mock
	})

	op := &types.Operation{
		ID:        "test-op-explicit-id",
		Type:      types.OperationTypeInstanceTypeChange,
		State:     types.StateRunning,
		ClusterID: "demo-multi",
		Region:    "us-east-1",
		Steps: []types.Step{
			{
				ID:         "step-1",
				Name:       "Wait for instance: demo-multi-writer",
				Action:     "wait_instance_available",
				State:      types.StepStatePending,
				Parameters: waitParams, // Explicit instance_id
			},
		},
		CreatedAt: time.Now(),
	}

	ctx := context.Background()

	// This should succeed - instance_id is explicitly provided
	step := &op.Steps[0]
	err := engine.handleWaitInstanceAvailable(ctx, op, step)
	if err != nil {
		// The wait might time out in tests, but it shouldn't fail due to missing instance_id
		if containsAny(err.Error(), "instance_id required", "missing parameter") {
			t.Errorf("Should not fail with instance_id error when instance_id is provided: %v", err)
		}
		// Other errors (like timeout) are acceptable for this test
		t.Logf("Got expected non-instance_id error: %v", err)
	}
}

// containsAny returns true if s contains any of the substrings.
func containsAny(s string, substrings ...string) bool {
	for _, sub := range substrings {
		if len(sub) > 0 && len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}

// TestStepTimingPreservedAcrossRetries verifies that step StartedAt is preserved
// across retries so that the total duration reflects all time spent on a step,
// not just the last attempt. This is important for users making scheduling decisions.
func TestStepTimingPreservedAcrossRetries(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	engine := &Engine{
		operations:          make(map[string]*types.Operation),
		events:              make(map[string][]types.Event),
		logger:              logger,
		handlers:            make(map[string]StepHandler),
		store:               &storage.NullStore{},
		defaultWaitTimeout:  5 * time.Second,
		defaultPollInterval: 10 * time.Millisecond,
	}

	// Register a handler that always fails (to simulate retries)
	failCount := 0
	engine.handlers["test_action"] = func(ctx context.Context, op *types.Operation, step *types.Step) error {
		failCount++
		if failCount < 3 {
			return context.DeadlineExceeded // Simulate a retryable error
		}
		return nil // Succeed on third attempt
	}

	op := &types.Operation{
		ID:        "test-timing-op",
		Type:      types.OperationTypeInstanceTypeChange,
		State:     types.StateCreated,
		ClusterID: "test-cluster",
		Region:    "us-east-1",
		Steps: []types.Step{
			{
				ID:         "step-1",
				Name:       "Test step",
				Action:     "test_action",
				State:      types.StepStatePending,
				MaxRetries: 5,
			},
		},
		CreatedAt: time.Now(),
	}

	engine.operations[op.ID] = op

	// Execute the step - it should fail twice and succeed on third try
	step := &op.Steps[0]

	// First execution - sets StartedAt
	originalStartedAt := time.Now()
	err := engine.executeStep(context.Background(), op, step)
	if err == nil {
		t.Fatal("Expected first execution to fail")
	}

	firstStartedAt := step.StartedAt
	if firstStartedAt == nil {
		t.Fatal("StartedAt should be set after first execution")
	}

	// Verify StartedAt is close to when we started (within 100ms)
	if firstStartedAt.Sub(originalStartedAt) > 100*time.Millisecond {
		t.Errorf("StartedAt should be close to execution time, got diff of %v", firstStartedAt.Sub(originalStartedAt))
	}

	// Small delay to ensure time difference is measurable
	time.Sleep(50 * time.Millisecond)

	// Second execution (simulating retry) - StartedAt should NOT change
	step.State = types.StepStatePending // Reset state as retry logic would
	err = engine.executeStep(context.Background(), op, step)
	if err == nil {
		t.Fatal("Expected second execution to fail")
	}

	secondStartedAt := step.StartedAt
	if secondStartedAt == nil {
		t.Fatal("StartedAt should still be set after second execution")
	}

	// CRITICAL: StartedAt should be preserved (same as first execution)
	if !secondStartedAt.Equal(*firstStartedAt) {
		t.Errorf("StartedAt should be preserved across retries.\n  First:  %v\n  Second: %v\n  This would cause inaccurate duration reporting!",
			firstStartedAt, secondStartedAt)
	}

	// Small delay before third attempt
	time.Sleep(50 * time.Millisecond)

	// Third execution - should succeed, StartedAt still preserved
	step.State = types.StepStatePending
	err = engine.executeStep(context.Background(), op, step)
	if err != nil {
		t.Fatalf("Expected third execution to succeed, got: %v", err)
	}

	finalStartedAt := step.StartedAt
	if !finalStartedAt.Equal(*firstStartedAt) {
		t.Errorf("StartedAt should remain from first attempt even after success.\n  First: %v\n  Final: %v",
			firstStartedAt, finalStartedAt)
	}

	// Verify that the elapsed time reflects total time (including retries)
	// We had ~100ms of delays, so duration should be > 100ms
	elapsed := time.Since(*finalStartedAt)
	if elapsed < 100*time.Millisecond {
		t.Errorf("Duration should reflect total time including retries, got only %v", elapsed)
	}

	t.Logf("Step timing correctly preserved: StartedAt=%v, elapsed=%v (includes %d retry attempts)",
		finalStartedAt, elapsed, failCount-1)
}
