package mock

import (
	"testing"
	"time"
)

// TestModifyInstance_AsyncBehavior verifies that ModifyInstance simulates AWS async behavior
// where the API returns immediately but status change is delayed.
func TestModifyInstance_AsyncBehavior(t *testing.T) {
	timing := TimingConfig{
		BaseWaitMs:    10,
		RandomRangeMs: 0,
		FastMode:      true,
	}

	state := NewState(timing)
	state.SeedDemoClusters()
	state.Start()
	defer state.Stop()

	// Get an available instance
	instanceID := "demo-single-writer"
	inst, ok := state.GetInstance(instanceID)
	if !ok {
		t.Fatal("Instance not found")
	}
	if inst.Status != "available" {
		t.Fatalf("Expected instance to be available, got %s", inst.Status)
	}

	// Modify the instance
	err := state.ModifyInstance(instanceID, "db.r6g.xlarge", "", nil)
	if err != nil {
		t.Fatalf("Failed to modify instance: %v", err)
	}

	// Immediately after modify, instance should STILL be available
	// This simulates the AWS race condition
	inst, _ = state.GetInstance(instanceID)
	if inst.Status != "available" {
		t.Errorf("Instance status changed immediately to %s, expected to remain 'available' briefly", inst.Status)
	}

	// After a short delay, status should change to modifying
	time.Sleep(100 * time.Millisecond)
	inst, _ = state.GetInstance(instanceID)
	if inst.Status != "modifying" {
		t.Errorf("Expected status to be 'modifying' after delay, got %s", inst.Status)
	}

	// Wait for modification to complete
	if !waitForInstanceStatus(state, instanceID, "available", 500*time.Millisecond) {
		inst, _ = state.GetInstance(instanceID)
		t.Errorf("Instance did not return to available, stuck at %s", inst.Status)
	}

	// Verify the instance type was changed
	inst, _ = state.GetInstance(instanceID)
	if inst.InstanceType != "db.r6g.xlarge" {
		t.Errorf("Instance type not updated, got %s", inst.InstanceType)
	}
}

// TestConcurrentModifications_RaceCondition simulates the race condition
// where multiple modifications are issued before the first one's status updates.
func TestConcurrentModifications_RaceCondition(t *testing.T) {
	timing := TimingConfig{
		BaseWaitMs:    50,
		RandomRangeMs: 0,
		FastMode:      false, // Use realistic timing
	}

	state := NewState(timing)
	state.SeedDemoClusters()
	state.Start()
	defer state.Stop()

	instances := []string{"demo-multi-writer", "demo-multi-reader-1", "demo-multi-reader-2"}

	// Rapidly modify all instances (simulating the bug scenario)
	for _, id := range instances {
		err := state.ModifyInstance(id, "db.r6g.xlarge", "", nil)
		if err != nil {
			t.Fatalf("Failed to modify %s: %v", id, err)
		}

		// Check status immediately - should still be available
		inst, _ := state.GetInstance(id)
		if inst.Status != "available" {
			t.Logf("Warning: %s already changed to %s", id, inst.Status)
		}
	}

	// All instances modified within milliseconds
	// Now check that they all got modified (simulating the problem)
	time.Sleep(100 * time.Millisecond)

	modifyingCount := 0
	for _, id := range instances {
		inst, _ := state.GetInstance(id)
		if inst.Status == "modifying" {
			modifyingCount++
		}
	}

	// In the bug scenario, all instances would be modifying simultaneously
	if modifyingCount == len(instances) {
		t.Logf("Race condition reproduced: %d instances all modifying simultaneously", modifyingCount)
	} else {
		t.Logf("Only %d/%d instances modifying", modifyingCount, len(instances))
	}
}

// TestCreateInstance_AsyncBehavior verifies create instance async behavior.
func TestCreateInstance_AsyncBehavior(t *testing.T) {
	timing := TimingConfig{
		BaseWaitMs:    10,
		RandomRangeMs: 0,
		FastMode:      true,
	}

	state := NewState(timing)
	state.SeedDemoClusters()
	state.Start()
	defer state.Stop()

	inst := &MockInstance{
		ID:           "test-async-instance",
		ClusterID:    "demo-single",
		InstanceType: "db.r6g.large",
		IsWriter:     false,
		IsAutoScaled: false,
	}

	err := state.CreateInstance(inst)
	if err != nil {
		t.Fatalf("Failed to create instance: %v", err)
	}

	// For create, the instance starts in "creating" state
	// The async aspect is that it takes time to become available
	created, _ := state.GetInstance("test-async-instance")
	if created.Status != "creating" {
		t.Errorf("Instance status should be 'creating', got %s", created.Status)
	}

	// Wait for it to become available
	if !waitForInstanceStatus(state, "test-async-instance", "available", 500*time.Millisecond) {
		created, _ = state.GetInstance("test-async-instance")
		t.Errorf("Instance did not become available, stuck at %s", created.Status)
	}
}

// TestDeleteInstance_AsyncBehavior verifies delete instance async behavior.
func TestDeleteInstance_AsyncBehavior(t *testing.T) {
	timing := TimingConfig{
		BaseWaitMs:    10,
		RandomRangeMs: 0,
		FastMode:      true,
	}

	state := NewState(timing)
	state.SeedDemoClusters()
	state.Start()
	defer state.Stop()

	// Use an instance that exists in demo clusters
	instanceID := "demo-multi-reader-1"

	// Verify instance exists and is available
	inst, ok := state.GetInstance(instanceID)
	if !ok {
		t.Fatalf("Instance %s not found in demo clusters", instanceID)
	}
	if inst.Status != "available" {
		t.Fatalf("Instance not in available state, got %s", inst.Status)
	}

	err := state.DeleteInstance(instanceID)
	if err != nil {
		t.Fatalf("Failed to delete instance: %v", err)
	}

	// Immediately after delete API call, instance should still be available
	inst, ok = state.GetInstance(instanceID)
	if !ok {
		t.Error("Instance disappeared immediately")
	} else if inst.Status != "available" {
		t.Errorf("Instance status immediately changed to %s, expected to remain 'available' briefly", inst.Status)
	}

	// After delay, should transition to deleting
	// Use polling instead of fixed sleep since the background ticker runs every 100ms
	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		inst, ok = state.GetInstance(instanceID)
		if !ok {
			// Instance was fully deleted - that's fine too
			break
		}
		if inst.Status == "deleting" {
			break
		}
		if time.Now().After(deadline) {
			t.Errorf("Expected status to be 'deleting' after delay, got %s", inst.Status)
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
}
