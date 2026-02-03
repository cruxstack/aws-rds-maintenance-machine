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

// testEngineWithMockServer creates a test engine with a mock RDS server.
func testEngineWithMockServer(t *testing.T) (*Engine, func()) {
	t.Helper()

	timing := mock.TimingConfig{
		BaseWaitMs:    10,
		RandomRangeMs: 0,
		FastMode:      true,
	}
	mockState := mock.NewState(timing)
	mockState.SeedDemoClusters()
	mockState.Start()

	// Create mock server
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	mockServer := mock.NewServer(mockState, logger, false)
	server := httptest.NewServer(mockServer)

	// Create client manager pointing to mock server
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

	cleanup := func() {
		server.Close()
		mockState.Stop()
	}

	return engine, cleanup
}

// TestBuildInstanceTypeChangeSteps_WaitStepsHaveInstanceID verifies that all wait_instance_available
// steps for modified instances have explicit instance_id parameters set.
// This is a regression test for the bug where missing instance_id caused all instances
// to be modified in parallel instead of sequentially.
func TestBuildInstanceTypeChangeSteps_WaitStepsHaveInstanceID(t *testing.T) {
	engine, cleanup := testEngineWithMockServer(t)
	defer cleanup()

	// Create an operation for instance type change
	params, _ := json.Marshal(types.InstanceTypeChangeParams{
		TargetInstanceType: "db.r6g.xlarge",
	})

	op := &types.Operation{
		ID:         "test-op-1",
		Type:       types.OperationTypeInstanceTypeChange,
		State:      types.StateCreated,
		ClusterID:  "demo-multi", // Cluster with writer + 2 readers
		Region:     "us-east-1",
		Parameters: params,
		CreatedAt:  time.Now(),
	}

	// Build the steps
	err := engine.buildInstanceTypeChangeSteps(context.Background(), op)
	if err != nil {
		t.Fatalf("buildInstanceTypeChangeSteps failed: %v", err)
	}

	// Verify that we have steps
	if len(op.Steps) == 0 {
		t.Fatal("expected steps to be generated")
	}

	// Find all wait_instance_available steps that are NOT for the temp instance
	// and verify they have instance_id parameter set
	var tempInstanceWaitFound bool
	var modifyWaitSteps []types.Step

	for i, step := range op.Steps {
		if step.Action == "wait_instance_available" {
			if step.Name == "Wait for temp instance" {
				tempInstanceWaitFound = true
				// Temp instance wait should NOT have instance_id (it's populated at runtime)
				continue
			}

			// This is a wait step for a modified instance
			modifyWaitSteps = append(modifyWaitSteps, step)

			// Parse the parameters
			var params struct {
				InstanceID string `json:"instance_id"`
			}
			if len(step.Parameters) == 0 {
				t.Errorf("Step %d (%s): wait_instance_available has empty parameters - this would cause parallel modifications!", i, step.Name)
				continue
			}

			if err := json.Unmarshal(step.Parameters, &params); err != nil {
				t.Errorf("Step %d (%s): failed to unmarshal parameters: %v", i, step.Name, err)
				continue
			}

			if params.InstanceID == "" {
				t.Errorf("Step %d (%s): wait_instance_available missing instance_id parameter - this would cause parallel modifications!", i, step.Name)
			}
		}
	}

	if !tempInstanceWaitFound {
		t.Error("expected to find a 'Wait for temp instance' step")
	}

	if len(modifyWaitSteps) == 0 {
		t.Error("expected to find wait steps for modified instances")
	}

	t.Logf("Found %d wait steps for modified instances, all with instance_id set", len(modifyWaitSteps))
}

// TestBuildStorageTypeChangeSteps_WaitStepsHaveInstanceID verifies that storage type change
// operations also have explicit instance_id parameters on wait steps.
func TestBuildStorageTypeChangeSteps_WaitStepsHaveInstanceID(t *testing.T) {
	engine, cleanup := testEngineWithMockServer(t)
	defer cleanup()

	params, _ := json.Marshal(types.StorageTypeChangeParams{
		TargetStorageType: "gp3",
	})

	op := &types.Operation{
		ID:         "test-op-2",
		Type:       types.OperationTypeStorageTypeChange,
		State:      types.StateCreated,
		ClusterID:  "demo-multi",
		Region:     "us-east-1",
		Parameters: params,
		CreatedAt:  time.Now(),
	}

	err := engine.buildStorageTypeChangeSteps(context.Background(), op)
	if err != nil {
		t.Fatalf("buildStorageTypeChangeSteps failed: %v", err)
	}

	// Verify all non-temp wait steps have instance_id
	for i, step := range op.Steps {
		if step.Action == "wait_instance_available" && step.Name != "Wait for temp instance" {
			var params struct {
				InstanceID string `json:"instance_id"`
			}
			if len(step.Parameters) == 0 {
				t.Errorf("Step %d (%s): wait_instance_available has empty parameters", i, step.Name)
				continue
			}
			if err := json.Unmarshal(step.Parameters, &params); err != nil {
				t.Errorf("Step %d (%s): failed to unmarshal parameters: %v", i, step.Name, err)
				continue
			}
			if params.InstanceID == "" {
				t.Errorf("Step %d (%s): wait_instance_available missing instance_id parameter", i, step.Name)
			}
		}
	}
}

// TestBuildInstanceCycleSteps_WaitStepsHaveInstanceID verifies that instance cycle (reboot)
// operations have explicit instance_id parameters on wait steps.
func TestBuildInstanceCycleSteps_WaitStepsHaveInstanceID(t *testing.T) {
	engine, cleanup := testEngineWithMockServer(t)
	defer cleanup()

	op := &types.Operation{
		ID:         "test-op-3",
		Type:       types.OperationTypeInstanceCycle,
		State:      types.StateCreated,
		ClusterID:  "demo-multi",
		Region:     "us-east-1",
		Parameters: json.RawMessage(`{}`),
		CreatedAt:  time.Now(),
	}

	err := engine.buildInstanceCycleSteps(context.Background(), op)
	if err != nil {
		t.Fatalf("buildInstanceCycleSteps failed: %v", err)
	}

	// Verify all non-temp wait steps have instance_id
	for i, step := range op.Steps {
		if step.Action == "wait_instance_available" && step.Name != "Wait for temp instance" {
			var params struct {
				InstanceID string `json:"instance_id"`
			}
			if len(step.Parameters) == 0 {
				t.Errorf("Step %d (%s): wait_instance_available has empty parameters", i, step.Name)
				continue
			}
			if err := json.Unmarshal(step.Parameters, &params); err != nil {
				t.Errorf("Step %d (%s): failed to unmarshal parameters: %v", i, step.Name, err)
				continue
			}
			if params.InstanceID == "" {
				t.Errorf("Step %d (%s): wait_instance_available missing instance_id parameter", i, step.Name)
			}
		}
	}
}

// TestWaitStepSequentialExecution verifies that modify and wait steps are properly
// interleaved to ensure sequential execution (modify -> wait -> modify -> wait).
func TestWaitStepSequentialExecution(t *testing.T) {
	engine, cleanup := testEngineWithMockServer(t)
	defer cleanup()

	params, _ := json.Marshal(types.InstanceTypeChangeParams{
		TargetInstanceType: "db.r6g.xlarge",
	})

	op := &types.Operation{
		ID:         "test-op-4",
		Type:       types.OperationTypeInstanceTypeChange,
		State:      types.StateCreated,
		ClusterID:  "demo-multi",
		Region:     "us-east-1",
		Parameters: params,
		CreatedAt:  time.Now(),
	}

	err := engine.buildInstanceTypeChangeSteps(context.Background(), op)
	if err != nil {
		t.Fatalf("buildInstanceTypeChangeSteps failed: %v", err)
	}

	// After the failover steps, we should see alternating modify_instance and wait_instance_available
	// Pattern: modify_instance -> wait_instance_available -> modify_instance -> wait_instance_available
	var inModifySection bool
	var lastWasModify bool
	var modifyCount, waitCount int

	for _, step := range op.Steps {
		if step.Action == "modify_instance" {
			inModifySection = true
			modifyCount++
			if lastWasModify {
				t.Error("Found consecutive modify_instance steps without wait_instance_available in between - this allows parallel modifications!")
			}
			lastWasModify = true
		} else if step.Action == "wait_instance_available" && inModifySection && step.Name != "Wait for temp instance" {
			waitCount++
			if !lastWasModify {
				t.Error("Found wait_instance_available without preceding modify_instance")
			}
			lastWasModify = false
		} else if step.Action == "failover_to_instance" && inModifySection {
			// Failover steps can appear after modify section
			inModifySection = false
		}
	}

	if modifyCount == 0 {
		t.Error("expected modify_instance steps")
	}

	if modifyCount != waitCount {
		t.Errorf("modify_instance count (%d) != wait_instance_available count (%d) - each modify should have a corresponding wait", modifyCount, waitCount)
	}

	t.Logf("Found %d modify_instance steps with %d corresponding wait_instance_available steps", modifyCount, waitCount)
}

// TestModifyAndWaitStepsHaveMatchingInstanceIDs verifies that each modify_instance step
// is followed by a wait_instance_available step with the same instance_id.
func TestModifyAndWaitStepsHaveMatchingInstanceIDs(t *testing.T) {
	engine, cleanup := testEngineWithMockServer(t)
	defer cleanup()

	params, _ := json.Marshal(types.InstanceTypeChangeParams{
		TargetInstanceType: "db.r6g.xlarge",
	})

	op := &types.Operation{
		ID:         "test-op-5",
		Type:       types.OperationTypeInstanceTypeChange,
		State:      types.StateCreated,
		ClusterID:  "demo-multi",
		Region:     "us-east-1",
		Parameters: params,
		CreatedAt:  time.Now(),
	}

	err := engine.buildInstanceTypeChangeSteps(context.Background(), op)
	if err != nil {
		t.Fatalf("buildInstanceTypeChangeSteps failed: %v", err)
	}

	// Walk through steps and verify modify -> wait pairs have matching instance IDs
	var lastModifyInstanceID string

	for i, step := range op.Steps {
		if step.Action == "modify_instance" {
			var modifyParams struct {
				InstanceID string `json:"instance_id"`
			}
			if err := json.Unmarshal(step.Parameters, &modifyParams); err != nil {
				t.Errorf("Step %d: failed to unmarshal modify_instance params: %v", i, err)
				continue
			}
			lastModifyInstanceID = modifyParams.InstanceID
		} else if step.Action == "wait_instance_available" && step.Name != "Wait for temp instance" {
			var waitParams struct {
				InstanceID string `json:"instance_id"`
			}
			if err := json.Unmarshal(step.Parameters, &waitParams); err != nil {
				t.Errorf("Step %d: failed to unmarshal wait_instance_available params: %v", i, err)
				continue
			}

			if lastModifyInstanceID == "" {
				t.Errorf("Step %d (%s): wait_instance_available without preceding modify_instance", i, step.Name)
				continue
			}

			if waitParams.InstanceID != lastModifyInstanceID {
				t.Errorf("Step %d (%s): wait_instance_available instance_id (%s) doesn't match preceding modify_instance instance_id (%s) - would wait for wrong instance!",
					i, step.Name, waitParams.InstanceID, lastModifyInstanceID)
			}

			lastModifyInstanceID = "" // Reset for next pair
		}
	}
}

// TestValidateExcludedInstances_UnknownInstance verifies that excluding an unknown instance ID
// returns an error.
func TestValidateExcludedInstances_UnknownInstance(t *testing.T) {
	engine, cleanup := testEngineWithMockServer(t)
	defer cleanup()

	params, _ := json.Marshal(types.InstanceTypeChangeParams{
		TargetInstanceType: "db.r6g.xlarge",
		ExcludeInstances:   []string{"nonexistent-instance"},
	})

	op := &types.Operation{
		ID:         "test-validate-1",
		Type:       types.OperationTypeInstanceTypeChange,
		State:      types.StateCreated,
		ClusterID:  "demo-multi",
		Region:     "us-east-1",
		Parameters: params,
		CreatedAt:  time.Now(),
	}

	err := engine.buildInstanceTypeChangeSteps(context.Background(), op)
	if err == nil {
		t.Fatal("expected error for unknown excluded instance, got nil")
	}

	if !containsString(err.Error(), "not found in cluster") {
		t.Errorf("expected error to mention 'not found in cluster', got: %v", err)
	}
}

// TestValidateExcludedInstances_AllExcluded verifies that excluding all instances
// returns an error.
func TestValidateExcludedInstances_AllExcluded(t *testing.T) {
	engine, cleanup := testEngineWithMockServer(t)
	defer cleanup()

	// demo-single cluster has only one instance
	params, _ := json.Marshal(types.InstanceTypeChangeParams{
		TargetInstanceType: "db.r6g.xlarge",
		ExcludeInstances:   []string{"demo-single-writer"},
	})

	op := &types.Operation{
		ID:         "test-validate-2",
		Type:       types.OperationTypeInstanceTypeChange,
		State:      types.StateCreated,
		ClusterID:  "demo-single",
		Region:     "us-east-1",
		Parameters: params,
		CreatedAt:  time.Now(),
	}

	err := engine.buildInstanceTypeChangeSteps(context.Background(), op)
	if err == nil {
		t.Fatal("expected error when all instances are excluded, got nil")
	}

	if !containsString(err.Error(), "all non-autoscaled instances are excluded") {
		t.Errorf("expected error to mention 'all non-autoscaled instances are excluded', got: %v", err)
	}
}

// TestValidateExcludedInstances_ValidExclusion verifies that excluding a valid subset
// of instances works correctly.
func TestValidateExcludedInstances_ValidExclusion(t *testing.T) {
	engine, cleanup := testEngineWithMockServer(t)
	defer cleanup()

	// demo-multi has writer + 2 readers, exclude one reader
	params, _ := json.Marshal(types.InstanceTypeChangeParams{
		TargetInstanceType: "db.r6g.xlarge",
		ExcludeInstances:   []string{"demo-multi-reader-1"},
	})

	op := &types.Operation{
		ID:         "test-validate-3",
		Type:       types.OperationTypeInstanceTypeChange,
		State:      types.StateCreated,
		ClusterID:  "demo-multi",
		Region:     "us-east-1",
		Parameters: params,
		CreatedAt:  time.Now(),
	}

	err := engine.buildInstanceTypeChangeSteps(context.Background(), op)
	if err != nil {
		t.Fatalf("expected no error for valid exclusion, got: %v", err)
	}

	// Verify the excluded instance is not in the steps
	for _, step := range op.Steps {
		if step.Action == "modify_instance" {
			var stepParams struct {
				InstanceID string `json:"instance_id"`
			}
			if err := json.Unmarshal(step.Parameters, &stepParams); err == nil {
				if stepParams.InstanceID == "demo-multi-reader-1" {
					t.Error("excluded instance demo-multi-reader-1 should not appear in modify steps")
				}
			}
		}
	}
}

// TestValidateExcludedInstances_WriterExcluded verifies that excluding the writer
// skips failover steps.
func TestValidateExcludedInstances_WriterExcluded(t *testing.T) {
	engine, cleanup := testEngineWithMockServer(t)
	defer cleanup()

	params, _ := json.Marshal(types.InstanceTypeChangeParams{
		TargetInstanceType: "db.r6g.xlarge",
		ExcludeInstances:   []string{"demo-multi-writer"},
	})

	op := &types.Operation{
		ID:         "test-validate-4",
		Type:       types.OperationTypeInstanceTypeChange,
		State:      types.StateCreated,
		ClusterID:  "demo-multi",
		Region:     "us-east-1",
		Parameters: params,
		CreatedAt:  time.Now(),
	}

	err := engine.buildInstanceTypeChangeSteps(context.Background(), op)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// When writer is excluded, there should be NO failover steps (except delete temp)
	var failoverCount int
	for _, step := range op.Steps {
		if step.Action == "failover_to_instance" {
			failoverCount++
		}
	}

	if failoverCount > 0 {
		t.Errorf("expected 0 failover steps when writer is excluded, got %d", failoverCount)
	}
}

// TestValidateExcludedInstances_InstanceCycle verifies validation works for instance cycle.
func TestValidateExcludedInstances_InstanceCycle(t *testing.T) {
	engine, cleanup := testEngineWithMockServer(t)
	defer cleanup()

	params, _ := json.Marshal(types.InstanceCycleParams{
		ExcludeInstances: []string{"nonexistent-instance"},
	})

	op := &types.Operation{
		ID:         "test-validate-5",
		Type:       types.OperationTypeInstanceCycle,
		State:      types.StateCreated,
		ClusterID:  "demo-multi",
		Region:     "us-east-1",
		Parameters: params,
		CreatedAt:  time.Now(),
	}

	err := engine.buildInstanceCycleSteps(context.Background(), op)
	if err == nil {
		t.Fatal("expected error for unknown excluded instance in instance cycle")
	}
}

// TestValidateExcludedInstances_StorageTypeChange verifies validation works for storage changes.
func TestValidateExcludedInstances_StorageTypeChange(t *testing.T) {
	engine, cleanup := testEngineWithMockServer(t)
	defer cleanup()

	params, _ := json.Marshal(types.StorageTypeChangeParams{
		TargetStorageType: "gp3",
		ExcludeInstances:  []string{"nonexistent-instance"},
	})

	op := &types.Operation{
		ID:         "test-validate-6",
		Type:       types.OperationTypeStorageTypeChange,
		State:      types.StateCreated,
		ClusterID:  "demo-multi",
		Region:     "us-east-1",
		Parameters: params,
		CreatedAt:  time.Now(),
	}

	err := engine.buildStorageTypeChangeSteps(context.Background(), op)
	if err == nil {
		t.Fatal("expected error for unknown excluded instance in storage type change")
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && stringContainsHelper(s, substr)))
}

func stringContainsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
