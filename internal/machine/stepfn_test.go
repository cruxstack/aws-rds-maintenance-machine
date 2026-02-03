package machine

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/mpz/devops/tools/rds-maint-machine/internal/storage"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/types"
)

// testEngine creates a minimal engine for testing.
func testEngine() *Engine {
	return &Engine{
		operations:          make(map[string]*types.Operation),
		events:              make(map[string][]types.Event),
		logger:              slog.New(slog.NewTextHandler(os.Stderr, nil)),
		handlers:            make(map[string]StepHandler),
		store:               &storage.NullStore{},
		defaultWaitTimeout:  5 * time.Second,
		defaultPollInterval: 1 * time.Second,
	}
}

func TestExecuteCurrentStep_OperationNotFound(t *testing.T) {
	e := testEngine()

	_, err := e.ExecuteCurrentStep(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent operation")
	}
}

func TestExecuteCurrentStep_CompletedOperation(t *testing.T) {
	e := testEngine()

	op := &types.Operation{
		ID:    "test-op",
		State: types.StateCompleted,
	}
	e.operations[op.ID] = op
	e.events[op.ID] = []types.Event{}

	result, err := e.ExecuteCurrentStep(context.Background(), op.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Completed {
		t.Error("expected Completed to be true")
	}
	if result.Continue {
		t.Error("expected Continue to be false")
	}
}

func TestExecuteCurrentStep_FailedOperation(t *testing.T) {
	e := testEngine()

	op := &types.Operation{
		ID:    "test-op",
		State: types.StateFailed,
		Error: "some error",
	}
	e.operations[op.ID] = op
	e.events[op.ID] = []types.Event{}

	result, err := e.ExecuteCurrentStep(context.Background(), op.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Failed {
		t.Error("expected Failed to be true")
	}
	if result.Error != "some error" {
		t.Errorf("expected error 'some error', got %q", result.Error)
	}
}

func TestExecuteCurrentStep_PausedOperation(t *testing.T) {
	e := testEngine()

	op := &types.Operation{
		ID:          "test-op",
		State:       types.StatePaused,
		PauseReason: "waiting for approval",
	}
	e.operations[op.ID] = op
	e.events[op.ID] = []types.Event{}

	result, err := e.ExecuteCurrentStep(context.Background(), op.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.NeedsIntervention {
		t.Error("expected NeedsIntervention to be true")
	}
	if result.PauseReason != "waiting for approval" {
		t.Errorf("expected pause reason, got %q", result.PauseReason)
	}
}

func TestExecuteCurrentStep_CreatedOperation(t *testing.T) {
	e := testEngine()

	op := &types.Operation{
		ID:    "test-op",
		State: types.StateCreated,
	}
	e.operations[op.ID] = op
	e.events[op.ID] = []types.Event{}

	_, err := e.ExecuteCurrentStep(context.Background(), op.ID)
	if err == nil {
		t.Fatal("expected error for created (not started) operation")
	}
}

func TestExecuteCurrentStep_AllStepsComplete(t *testing.T) {
	e := testEngine()

	op := &types.Operation{
		ID:               "test-op",
		State:            types.StateRunning,
		Steps:            []types.Step{},
		CurrentStepIndex: 0,
		CreatedAt:        time.Now(),
	}
	e.operations[op.ID] = op
	e.events[op.ID] = []types.Event{}

	result, err := e.ExecuteCurrentStep(context.Background(), op.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Completed {
		t.Error("expected Completed to be true")
	}
	if op.State != types.StateCompleted {
		t.Errorf("expected operation state to be completed, got %s", op.State)
	}
}

func TestExecuteCurrentStep_ExecuteSimpleStep(t *testing.T) {
	e := testEngine()

	// Register a simple handler that always succeeds
	e.handlers["test_action"] = func(ctx context.Context, op *types.Operation, step *types.Step) error {
		step.Result, _ = json.Marshal(map[string]string{"result": "success"})
		return nil
	}

	op := &types.Operation{
		ID:    "test-op",
		State: types.StateRunning,
		Steps: []types.Step{
			{
				ID:     "step-1",
				Name:   "Test Step",
				Action: "test_action",
				State:  types.StepStatePending,
			},
		},
		CurrentStepIndex: 0,
		CreatedAt:        time.Now(),
	}
	e.operations[op.ID] = op
	e.events[op.ID] = []types.Event{}

	result, err := e.ExecuteCurrentStep(context.Background(), op.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.StepState != types.StepStateCompleted {
		t.Errorf("expected step state completed, got %s", result.StepState)
	}
	if result.Continue {
		t.Error("expected Continue to be false (no more steps)")
	}
	if !result.Completed {
		t.Error("expected Completed to be true")
	}
}

func TestExecuteCurrentStep_WaitingStep(t *testing.T) {
	e := testEngine()

	// Register a handler that puts the step into waiting state
	e.handlers["wait_action"] = func(ctx context.Context, op *types.Operation, step *types.Step) error {
		step.State = types.StepStateWaiting
		step.WaitCondition = "waiting for something"
		return nil
	}

	op := &types.Operation{
		ID:    "test-op",
		State: types.StateRunning,
		Steps: []types.Step{
			{
				ID:     "step-1",
				Name:   "Wait Step",
				Action: "wait_action",
				State:  types.StepStatePending,
			},
			{
				ID:     "step-2",
				Name:   "Next Step",
				Action: "test_action",
				State:  types.StepStatePending,
			},
		},
		CurrentStepIndex: 0,
		CreatedAt:        time.Now(),
	}
	e.operations[op.ID] = op
	e.events[op.ID] = []types.Event{}

	result, err := e.ExecuteCurrentStep(context.Background(), op.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.NeedsWait {
		t.Error("expected NeedsWait to be true")
	}
	if !result.Continue {
		t.Error("expected Continue to be true")
	}
	if result.WaitCondition != "waiting for something" {
		t.Errorf("expected wait condition, got %q", result.WaitCondition)
	}
}

func TestPollCurrentStep_OperationNotFound(t *testing.T) {
	e := testEngine()

	_, err := e.PollCurrentStep(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent operation")
	}
}

func TestPollCurrentStep_CompletedOperation(t *testing.T) {
	e := testEngine()

	op := &types.Operation{
		ID:    "test-op",
		State: types.StateCompleted,
	}
	e.operations[op.ID] = op
	e.events[op.ID] = []types.Event{}

	result, err := e.PollCurrentStep(context.Background(), op.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Ready {
		t.Error("expected Ready to be true for completed operation")
	}
}

func TestPollCurrentStep_PausedOperation(t *testing.T) {
	e := testEngine()

	op := &types.Operation{
		ID:    "test-op",
		State: types.StatePaused,
	}
	e.operations[op.ID] = op
	e.events[op.ID] = []types.Event{}

	result, err := e.PollCurrentStep(context.Background(), op.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.NeedsIntervention {
		t.Error("expected NeedsIntervention to be true")
	}
}

func TestPollCurrentStep_NonWaitingStep(t *testing.T) {
	e := testEngine()

	op := &types.Operation{
		ID:    "test-op",
		State: types.StateRunning,
		Steps: []types.Step{
			{
				ID:    "step-1",
				Name:  "Test Step",
				State: types.StepStatePending,
			},
		},
		CurrentStepIndex: 0,
	}
	e.operations[op.ID] = op
	e.events[op.ID] = []types.Event{}

	result, err := e.PollCurrentStep(context.Background(), op.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Non-waiting step should not be ready yet
	if result.Ready {
		t.Error("expected Ready to be false for pending step")
	}
}

func TestPollCurrentStep_CompletedStep(t *testing.T) {
	e := testEngine()

	op := &types.Operation{
		ID:    "test-op",
		State: types.StateRunning,
		Steps: []types.Step{
			{
				ID:    "step-1",
				Name:  "Test Step",
				State: types.StepStateCompleted,
			},
		},
		CurrentStepIndex: 0,
	}
	e.operations[op.ID] = op
	e.events[op.ID] = []types.Event{}

	result, err := e.PollCurrentStep(context.Background(), op.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Ready {
		t.Error("expected Ready to be true for completed step")
	}
	if !result.Continue {
		t.Error("expected Continue to be true")
	}
}
