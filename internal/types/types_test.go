package types

import (
	"testing"
	"time"
)

func TestOperation_Validate(t *testing.T) {
	tests := []struct {
		name    string
		op      Operation
		wantErr bool
	}{
		{
			name: "valid operation",
			op: Operation{
				ID:        "test-op",
				Type:      OperationTypeInstanceTypeChange,
				State:     StateCreated,
				ClusterID: "test-cluster",
				CreatedAt: time.Now(),
				Steps: []Step{
					{
						ID:     "step-1",
						Name:   "Test Step",
						State:  StepStatePending,
						Action: "test_action",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "missing ID",
			op: Operation{
				Type:      OperationTypeInstanceTypeChange,
				State:     StateCreated,
				ClusterID: "test-cluster",
				CreatedAt: time.Now(),
			},
			wantErr: true,
		},
		{
			name: "invalid type",
			op: Operation{
				ID:        "test-op",
				Type:      "invalid_type",
				State:     StateCreated,
				ClusterID: "test-cluster",
				CreatedAt: time.Now(),
			},
			wantErr: true,
		},
		{
			name: "invalid state",
			op: Operation{
				ID:        "test-op",
				Type:      OperationTypeInstanceTypeChange,
				State:     "invalid_state",
				ClusterID: "test-cluster",
				CreatedAt: time.Now(),
			},
			wantErr: true,
		},
		{
			name: "missing cluster ID",
			op: Operation{
				ID:        "test-op",
				Type:      OperationTypeInstanceTypeChange,
				State:     StateCreated,
				CreatedAt: time.Now(),
			},
			wantErr: true,
		},
		{
			name: "negative step index",
			op: Operation{
				ID:               "test-op",
				Type:             OperationTypeInstanceTypeChange,
				State:            StateCreated,
				ClusterID:        "test-cluster",
				CreatedAt:        time.Now(),
				CurrentStepIndex: -1,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.op.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Operation.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestEvent_Validate(t *testing.T) {
	tests := []struct {
		name    string
		event   Event
		wantErr bool
	}{
		{
			name: "valid event",
			event: Event{
				ID:          "event-1",
				OperationID: "op-1",
				Type:        "step_started",
				Timestamp:   time.Now(),
			},
			wantErr: false,
		},
		{
			name: "missing ID",
			event: Event{
				OperationID: "op-1",
				Type:        "step_started",
				Timestamp:   time.Now(),
			},
			wantErr: true,
		},
		{
			name: "missing operation ID",
			event: Event{
				ID:        "event-1",
				Type:      "step_started",
				Timestamp: time.Now(),
			},
			wantErr: true,
		},
		{
			name: "missing type",
			event: Event{
				ID:          "event-1",
				OperationID: "op-1",
				Timestamp:   time.Now(),
			},
			wantErr: true,
		},
		{
			name: "missing timestamp",
			event: Event{
				ID:          "event-1",
				OperationID: "op-1",
				Type:        "step_started",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.event.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Event.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
