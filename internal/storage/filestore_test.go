package storage

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mpz/devops/tools/rds-maint-machine/internal/types"
)

func TestFileStore_AtomicWrite(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()

	store, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()

	// Create a valid operation
	op := createTestOperation("test-op-1", "test-cluster")

	// Save operation
	if err := store.SaveOperation(ctx, op); err != nil {
		t.Fatalf("SaveOperation failed: %v", err)
	}

	// Verify no temp files remain
	files, _ := filepath.Glob(filepath.Join(tmpDir, "operations", op.ID, "*.tmp"))
	if len(files) > 0 {
		t.Errorf("temp files remaining after save: %v", files)
	}
	files, _ = filepath.Glob(filepath.Join(tmpDir, "operations", op.ID, ".tmp-*"))
	if len(files) > 0 {
		t.Errorf("temp files remaining after save: %v", files)
	}

	// Verify operation can be read back
	loaded, err := store.GetOperation(ctx, op.ID)
	if err != nil {
		t.Fatalf("GetOperation failed: %v", err)
	}
	if loaded.ID != op.ID {
		t.Errorf("expected ID %s, got %s", op.ID, loaded.ID)
	}
}

func TestFileStore_CorruptedOperationRecovery(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()

	// Create a valid operation
	validOp := createTestOperation("valid-op", "test-cluster")
	if err := store.SaveOperation(ctx, validOp); err != nil {
		t.Fatalf("SaveOperation failed: %v", err)
	}

	// Create a corrupted operation (invalid JSON)
	corruptDir := filepath.Join(tmpDir, "operations", "corrupt-op")
	if err := os.MkdirAll(corruptDir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(corruptDir, "operation.json"), []byte("not valid json{"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Create an operation with invalid data (missing required fields)
	invalidDir := filepath.Join(tmpDir, "operations", "invalid-op")
	if err := os.MkdirAll(invalidDir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	invalidOp := map[string]interface{}{
		"id":    "invalid-op",
		"type":  "unknown_type", // Invalid type
		"state": "created",
	}
	invalidData, _ := json.Marshal(invalidOp)
	if err := os.WriteFile(filepath.Join(invalidDir, "operation.json"), invalidData, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// LoadAll should recover valid operations and skip corrupted ones
	ops, events, err := store.LoadAll(ctx)
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	// Should have loaded only the valid operation
	if len(ops) != 1 {
		t.Errorf("expected 1 operation, got %d", len(ops))
	}
	if _, ok := ops["valid-op"]; !ok {
		t.Error("expected valid-op to be loaded")
	}
	if _, ok := ops["corrupt-op"]; ok {
		t.Error("corrupt-op should not be loaded")
	}
	if _, ok := ops["invalid-op"]; ok {
		t.Error("invalid-op should not be loaded")
	}

	// Events map should exist for valid operation
	if _, ok := events["valid-op"]; !ok {
		t.Error("expected events for valid-op")
	}
}

func TestFileStore_CorruptedEventRecovery(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()

	// Create a valid operation
	op := createTestOperation("test-op", "test-cluster")
	op.State = types.StateRunning
	op.Steps[0].State = types.StepStateInProgress
	if err := store.SaveOperation(ctx, op); err != nil {
		t.Fatalf("SaveOperation failed: %v", err)
	}

	// Add a valid event
	validEvent := types.Event{
		ID:          "event-1",
		OperationID: op.ID,
		Type:        "step_started",
		Message:     "Step started",
		Timestamp:   time.Now(),
	}
	if err := store.AppendEvent(ctx, validEvent); err != nil {
		t.Fatalf("AppendEvent failed: %v", err)
	}

	// Add a corrupted event file
	eventsDir := filepath.Join(tmpDir, "operations", op.ID, "events")
	if err := os.WriteFile(filepath.Join(eventsDir, "0002-corrupt.json"), []byte("not json{"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Add another valid event (should work despite corrupted file)
	validEvent2 := types.Event{
		ID:          "event-3",
		OperationID: op.ID,
		Type:        "step_completed",
		Message:     "Step completed",
		Timestamp:   time.Now(),
	}
	if err := store.AppendEvent(ctx, validEvent2); err != nil {
		t.Fatalf("AppendEvent failed: %v", err)
	}

	// LoadAll should recover valid events and skip corrupted ones
	ops, events, err := store.LoadAll(ctx)
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	if len(ops) != 1 {
		t.Errorf("expected 1 operation, got %d", len(ops))
	}

	opEvents := events[op.ID]
	if len(opEvents) != 2 {
		t.Errorf("expected 2 valid events, got %d", len(opEvents))
	}
}

func TestFileStore_TempFileCleanup(t *testing.T) {
	tmpDir := t.TempDir()

	// Create directories
	opsDir := filepath.Join(tmpDir, "operations", "test-op")
	eventsDir := filepath.Join(opsDir, "events")
	if err := os.MkdirAll(eventsDir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	// Create some orphaned temp files
	orphanedFiles := []string{
		filepath.Join(opsDir, "operation.json.tmp"),
		filepath.Join(opsDir, ".tmp-abc123"),
		filepath.Join(eventsDir, ".tmp-event"),
	}
	for _, f := range orphanedFiles {
		if err := os.WriteFile(f, []byte("temp data"), 0644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}
	}

	// Create a valid operation file
	validOp := createTestOperation("test-op", "test-cluster")
	validData, _ := json.MarshalIndent(validOp, "", "  ")
	if err := os.WriteFile(filepath.Join(opsDir, "operation.json"), validData, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Create store and call LoadAll (which should clean up temp files)
	store, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()
	_, _, err = store.LoadAll(ctx)
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	// Verify temp files are removed
	for _, f := range orphanedFiles {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("orphaned temp file should be removed: %s", f)
		}
	}

	// Verify valid operation file still exists
	if _, err := os.Stat(filepath.Join(opsDir, "operation.json")); err != nil {
		t.Error("valid operation file should still exist")
	}
}

// createTestOperation creates a valid operation for testing.
// It can be used as a starting point and modified for specific test cases.
func createTestOperation(id, clusterID string) *types.Operation {
	return &types.Operation{
		ID:        id,
		Type:      types.OperationTypeInstanceTypeChange,
		State:     types.StateCreated,
		ClusterID: clusterID,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Steps: []types.Step{
			{
				ID:     "step-1",
				Name:   "Test Step",
				State:  types.StepStatePending,
				Action: "test_action",
			},
		},
	}
}
