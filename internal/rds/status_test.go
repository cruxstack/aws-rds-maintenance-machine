package rds

import "testing"

func TestInstanceStatus_IsTransitional(t *testing.T) {
	tests := []struct {
		status   InstanceStatus
		expected bool
	}{
		// Transitional statuses
		{StatusBackingUp, true},
		{StatusConfiguringEnhancedMonitoring, true},
		{StatusConfiguringIAMDatabaseAuth, true},
		{StatusConfiguringLogExports, true},
		{StatusConfiguringPerformanceInsights, true},
		{StatusConvertingToVPC, true},
		{StatusCreating, true},
		{StatusMaintenance, true},
		{StatusModifying, true},
		{StatusMovingToVPC, true},
		{StatusRebooting, true},
		{StatusResettingMasterCredentials, true},
		{StatusRenaming, true},
		{StatusStarting, true},
		{StatusStorageConfigUpgrade, true},
		{StatusStorageOptimization, true},
		{StatusUpgrading, true},

		// Non-transitional statuses
		{StatusAvailable, false},
		{StatusDeleting, false},
		{StatusStopped, false},
		{StatusFailed, false},
		{StatusStorageFull, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := tt.status.IsTransitional(); got != tt.expected {
				t.Errorf("InstanceStatus(%q).IsTransitional() = %v, want %v", tt.status, got, tt.expected)
			}
		})
	}
}

func TestInstanceStatus_IsAvailable(t *testing.T) {
	tests := []struct {
		status   InstanceStatus
		expected bool
	}{
		{StatusAvailable, true},
		{StatusModifying, false},
		{StatusCreating, false},
		{StatusConfiguringEnhancedMonitoring, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := tt.status.IsAvailable(); got != tt.expected {
				t.Errorf("InstanceStatus(%q).IsAvailable() = %v, want %v", tt.status, got, tt.expected)
			}
		})
	}
}

func TestInstanceStatus_IsError(t *testing.T) {
	tests := []struct {
		status   InstanceStatus
		expected bool
	}{
		{StatusFailed, true},
		{StatusInaccessibleEncryptionCredentials, true},
		{StatusIncompatibleParameters, true},
		{StatusStorageFull, true},

		{StatusAvailable, false},
		{StatusModifying, false},
		{StatusDeleting, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := tt.status.IsError(); got != tt.expected {
				t.Errorf("InstanceStatus(%q).IsError() = %v, want %v", tt.status, got, tt.expected)
			}
		})
	}
}

func TestInstanceStatus_ShouldWaitForAvailable(t *testing.T) {
	// This should return true for all transitional statuses
	transitional := []InstanceStatus{
		StatusConfiguringEnhancedMonitoring,
		StatusConfiguringIAMDatabaseAuth,
		StatusCreating,
		StatusModifying,
		StatusRebooting,
		StatusUpgrading,
	}

	for _, status := range transitional {
		if !status.ShouldWaitForAvailable() {
			t.Errorf("InstanceStatus(%q).ShouldWaitForAvailable() should be true", status)
		}
	}

	// Error statuses should not wait
	errorStatuses := []InstanceStatus{
		StatusFailed,
		StatusIncompatibleParameters,
	}

	for _, status := range errorStatuses {
		if status.ShouldWaitForAvailable() {
			t.Errorf("InstanceStatus(%q).ShouldWaitForAvailable() should be false", status)
		}
	}
}

func TestInstanceStatus_CanFailover(t *testing.T) {
	tests := []struct {
		status   InstanceStatus
		expected bool
	}{
		// Only available instances can be failover targets
		{StatusAvailable, true},

		// Transitional statuses cannot be failover targets
		{StatusModifying, false},
		{StatusCreating, false},
		{StatusRebooting, false},
		{StatusUpgrading, false},
		{StatusBackingUp, false},
		{StatusMaintenance, false},

		// Error statuses cannot be failover targets
		{StatusFailed, false},
		{StatusStorageFull, false},
		{StatusIncompatibleParameters, false},

		// Other statuses cannot be failover targets
		{StatusDeleting, false},
		{StatusStopped, false},
		{StatusStopping, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := tt.status.CanFailover(); got != tt.expected {
				t.Errorf("InstanceStatus(%q).CanFailover() = %v, want %v", tt.status, got, tt.expected)
			}
		})
	}
}

func TestInstanceStatus_IsDeleting(t *testing.T) {
	tests := []struct {
		status   InstanceStatus
		expected bool
	}{
		// Deletion statuses
		{StatusDeleting, true},
		{StatusDeletePrecheck, true},

		// Non-deletion statuses
		{StatusAvailable, false},
		{StatusModifying, false},
		{StatusStopped, false},
		{StatusFailed, false},
		{StatusCreating, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := tt.status.IsDeleting(); got != tt.expected {
				t.Errorf("InstanceStatus(%q).IsDeleting() = %v, want %v", tt.status, got, tt.expected)
			}
		})
	}
}

func TestInstanceStatus_IsStopped(t *testing.T) {
	tests := []struct {
		status   InstanceStatus
		expected bool
	}{
		// Stopped/stopping statuses
		{StatusStopped, true},
		{StatusStopping, true},

		// Non-stopped statuses
		{StatusAvailable, false},
		{StatusModifying, false},
		{StatusDeleting, false},
		{StatusFailed, false},
		{StatusStarting, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := tt.status.IsStopped(); got != tt.expected {
				t.Errorf("InstanceStatus(%q).IsStopped() = %v, want %v", tt.status, got, tt.expected)
			}
		})
	}
}

func TestInstanceStatus_CanPerformOperations(t *testing.T) {
	tests := []struct {
		status   InstanceStatus
		expected bool
	}{
		// Statuses where operations can be performed
		{StatusAvailable, true},
		{StatusStorageFull, true},

		// Statuses where operations cannot be performed
		{StatusModifying, false},
		{StatusCreating, false},
		{StatusDeleting, false},
		{StatusStopped, false},
		{StatusFailed, false},
		{StatusRebooting, false},
		{StatusUpgrading, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := tt.status.CanPerformOperations(); got != tt.expected {
				t.Errorf("InstanceStatus(%q).CanPerformOperations() = %v, want %v", tt.status, got, tt.expected)
			}
		})
	}
}

func TestClusterStatus_IsTransitional(t *testing.T) {
	tests := []struct {
		status   ClusterStatus
		expected bool
	}{
		{ClusterStatusModifying, true},
		{ClusterStatusUpgrading, true},
		{ClusterStatusFailingOver, true},
		{ClusterStatusBackingUp, true},

		{ClusterStatusAvailable, false},
		{ClusterStatusDeleting, false},
		{ClusterStatusFailed, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := tt.status.IsTransitional(); got != tt.expected {
				t.Errorf("ClusterStatus(%q).IsTransitional() = %v, want %v", tt.status, got, tt.expected)
			}
		})
	}
}
