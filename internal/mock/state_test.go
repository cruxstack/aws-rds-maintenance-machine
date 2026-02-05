package mock

import (
	"testing"
	"time"
)

// waitForInstanceStatus polls until the instance reaches the expected status or times out.
// Returns true if the expected status was reached, false if timed out.
func waitForInstanceStatus(state *State, instanceID, expectedStatus string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		inst, ok := state.GetInstance(instanceID)
		if ok && inst.Status == expectedStatus {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		<-ticker.C
	}
}

func TestIsTransitionalStatus(t *testing.T) {
	tests := []struct {
		status   string
		expected bool
	}{
		// All transitional statuses should return true
		{"backing-up", true},
		{"configuring-enhanced-monitoring", true},
		{"configuring-iam-database-auth", true},
		{"configuring-log-exports", true},
		{"converting-to-vpc", true},
		{"creating", true},
		{"maintenance", true},
		{"modifying", true},
		{"moving-to-vpc", true},
		{"rebooting", true},
		{"resetting-master-credentials", true},
		{"renaming", true},
		{"starting", true},
		{"storage-config-upgrade", true},
		{"storage-optimization", true},
		{"upgrading", true},

		// Non-transitional statuses should return false
		{"available", false},
		{"deleting", false},
		{"stopped", false},
		{"failed", false},
		{"unknown-status", false},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			if got := IsTransitionalStatus(tt.status); got != tt.expected {
				t.Errorf("IsTransitionalStatus(%q) = %v, want %v", tt.status, got, tt.expected)
			}
		})
	}
}

func TestAllTransitionalStatuses(t *testing.T) {
	statuses := AllTransitionalStatuses()

	// Should have at least 16 transitional statuses
	if len(statuses) < 16 {
		t.Errorf("Expected at least 16 transitional statuses, got %d", len(statuses))
	}

	// Check for specific statuses we care about
	expected := []string{
		"configuring-enhanced-monitoring",
		"configuring-iam-database-auth",
		"creating",
		"modifying",
		"rebooting",
	}

	for _, exp := range expected {
		found := false
		for _, s := range statuses {
			if s == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected transitional status %q not found in AllTransitionalStatuses()", exp)
		}
	}
}

func TestState_TransitionalStatusTransitions(t *testing.T) {
	timing := TimingConfig{
		BaseWaitMs:    10,
		RandomRangeMs: 0,
		FastMode:      true,
	}

	state := NewState(timing)
	state.SeedDemoClusters()
	state.Start()
	defer state.Stop()

	const pollTimeout = 500 * time.Millisecond

	// Test that "configuring-enhanced-monitoring" transitions to available
	t.Run("configuring-enhanced-monitoring transitions to available", func(t *testing.T) {
		err := state.SetInstanceStatus("demo-single-writer", "configuring-enhanced-monitoring")
		if err != nil {
			t.Fatalf("Failed to set instance status: %v", err)
		}

		inst, ok := state.GetInstance("demo-single-writer")
		if !ok {
			t.Fatal("Instance not found")
		}
		if inst.Status != "configuring-enhanced-monitoring" {
			t.Errorf("Expected status 'configuring-enhanced-monitoring', got %q", inst.Status)
		}

		// Poll for transition
		if !waitForInstanceStatus(state, "demo-single-writer", "available", pollTimeout) {
			inst, _ := state.GetInstance("demo-single-writer")
			t.Errorf("Expected status 'available' after transition, got %q", inst.Status)
		}
	})

	// Test that all transitional statuses eventually become available
	t.Run("all transitional statuses become available", func(t *testing.T) {
		for _, status := range AllTransitionalStatuses() {
			// Reset instance to available first
			_ = state.SetInstanceStatus("demo-multi-reader-1", "available")
			waitForInstanceStatus(state, "demo-multi-reader-1", "available", pollTimeout)

			err := state.SetInstanceStatus("demo-multi-reader-1", status)
			if err != nil {
				t.Fatalf("Failed to set instance status to %q: %v", status, err)
			}

			// Poll for transition
			if !waitForInstanceStatus(state, "demo-multi-reader-1", "available", pollTimeout) {
				inst, _ := state.GetInstance("demo-multi-reader-1")
				t.Errorf("Status %q did not transition to 'available', still %q", status, inst.Status)
			}
		}
	})
}

func TestState_SetInstanceTransitionalStatus(t *testing.T) {
	timing := TimingConfig{
		BaseWaitMs:    50,
		RandomRangeMs: 0,
		FastMode:      false,
	}

	state := NewState(timing)
	state.SeedDemoClusters()
	state.Start()
	defer state.Stop()

	const pollTimeout = 500 * time.Millisecond

	// Test setting a transitional status chain
	t.Run("transitional status chain", func(t *testing.T) {
		// Set the instance to "creating" with a pending transitional status
		err := state.SetInstanceStatus("demo-single-writer", "creating")
		if err != nil {
			t.Fatalf("Failed to set instance status: %v", err)
		}

		err = state.SetInstanceTransitionalStatus("demo-single-writer", "configuring-enhanced-monitoring")
		if err != nil {
			t.Fatalf("Failed to set transitional status: %v", err)
		}

		// Poll for first transition (creating -> configuring-enhanced-monitoring)
		if !waitForInstanceStatus(state, "demo-single-writer", "configuring-enhanced-monitoring", pollTimeout) {
			inst, _ := state.GetInstance("demo-single-writer")
			t.Errorf("Expected 'configuring-enhanced-monitoring', got %q", inst.Status)
		}

		// Poll for second transition (configuring-enhanced-monitoring -> available)
		if !waitForInstanceStatus(state, "demo-single-writer", "available", pollTimeout) {
			inst, _ := state.GetInstance("demo-single-writer")
			t.Errorf("Expected 'available', got %q", inst.Status)
		}
	})

	// Test invalid transitional status
	t.Run("invalid transitional status", func(t *testing.T) {
		err := state.SetInstanceTransitionalStatus("demo-single-writer", "invalid-status")
		if err == nil {
			t.Error("Expected error for invalid transitional status")
		}
	})
}

func TestState_StoppedInstancesDoNotBecomeAvailable(t *testing.T) {
	timing := TimingConfig{
		BaseWaitMs:    10,
		RandomRangeMs: 0,
		FastMode:      true,
	}

	state := NewState(timing)
	state.SeedDemoClusters()
	state.Start()
	defer state.Stop()

	// Set instance to stopped
	err := state.SetInstanceStatus("demo-single-writer", "stopped")
	if err != nil {
		t.Fatalf("Failed to set instance status: %v", err)
	}

	// Wait a reasonable time and verify it stays stopped
	// Use a shorter poll here to verify negative case - instance should NOT change
	time.Sleep(100 * time.Millisecond)

	inst, _ := state.GetInstance("demo-single-writer")
	if inst.Status != "stopped" {
		t.Errorf("Stopped instance should remain stopped, got %q", inst.Status)
	}
}

func TestState_StartStopInstance(t *testing.T) {
	timing := TimingConfig{
		BaseWaitMs:    10,
		RandomRangeMs: 0,
		FastMode:      true,
	}

	state := NewState(timing)
	state.SeedDemoClusters()
	state.Start()
	defer state.Stop()

	const pollTimeout = 500 * time.Millisecond

	// Stop the instance
	err := state.StopInstance("demo-single-writer")
	if err != nil {
		t.Fatalf("Failed to stop instance: %v", err)
	}

	inst, _ := state.GetInstance("demo-single-writer")
	if inst.Status != "stopping" {
		t.Errorf("Expected 'stopping', got %q", inst.Status)
	}

	// Poll for stopping -> stopped
	if !waitForInstanceStatus(state, "demo-single-writer", "stopped", pollTimeout) {
		inst, _ := state.GetInstance("demo-single-writer")
		t.Errorf("Expected 'stopped', got %q", inst.Status)
	}

	// Start the instance
	err = state.StartInstance("demo-single-writer")
	if err != nil {
		t.Fatalf("Failed to start instance: %v", err)
	}

	inst, _ = state.GetInstance("demo-single-writer")
	if inst.Status != "starting" {
		t.Errorf("Expected 'starting', got %q", inst.Status)
	}

	// Poll for starting -> (configuring-performance-insights) -> available
	// Demo clusters have PI enabled, so there's a transitional status
	if !waitForInstanceStatus(state, "demo-single-writer", "available", pollTimeout) {
		inst, _ := state.GetInstance("demo-single-writer")
		t.Errorf("Expected 'available', got %q", inst.Status)
	}
}
