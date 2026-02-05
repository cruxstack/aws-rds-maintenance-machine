// Package types defines core types for the RDS maintenance state machine.
package types

import (
	"encoding/json"
	"strconv"
	"time"
)

// OperationType identifies the type of maintenance operation.
type OperationType string

const (
	// OperationTypeInstanceTypeChange changes instance types across the cluster.
	OperationTypeInstanceTypeChange OperationType = "instance_type_change"
	// OperationTypeStorageTypeChange changes storage type across the cluster.
	OperationTypeStorageTypeChange OperationType = "storage_type_change"
	// OperationTypeEngineUpgrade upgrades the engine major version.
	OperationTypeEngineUpgrade OperationType = "engine_upgrade"
	// OperationTypeInstanceCycle reboots all instances in the cluster to apply pending changes.
	OperationTypeInstanceCycle OperationType = "instance_cycle"
)

// OperationState represents the current state of an operation.
type OperationState string

const (
	// StateCreated indicates operation was created but not started.
	StateCreated OperationState = "created"
	// StateRunning indicates operation is actively executing.
	StateRunning OperationState = "running"
	// StatePaused indicates operation is paused waiting for intervention.
	StatePaused OperationState = "paused"
	// StateCompleted indicates operation finished successfully.
	StateCompleted OperationState = "completed"
	// StateFailed indicates operation failed and cannot continue.
	StateFailed OperationState = "failed"
	// StateRollingBack indicates operation is being rolled back.
	StateRollingBack OperationState = "rolling_back"
	// StateRolledBack indicates operation was rolled back.
	StateRolledBack OperationState = "rolled_back"
)

// StepState represents the current state of a step within an operation.
type StepState string

const (
	// StepStatePending indicates step has not started.
	StepStatePending StepState = "pending"
	// StepStateInProgress indicates step is currently executing.
	StepStateInProgress StepState = "in_progress"
	// StepStateWaiting indicates step is waiting for a condition.
	StepStateWaiting StepState = "waiting"
	// StepStateCompleted indicates step finished successfully.
	StepStateCompleted StepState = "completed"
	// StepStateFailed indicates step failed.
	StepStateFailed StepState = "failed"
	// StepStateSkipped indicates step was skipped.
	StepStateSkipped StepState = "skipped"
)

// Operation represents a maintenance operation on an RDS cluster.
type Operation struct {
	// ID is the unique identifier for this operation.
	ID string `json:"id"`
	// Type identifies what kind of maintenance operation this is.
	Type OperationType `json:"type"`
	// State is the current state of the operation.
	State OperationState `json:"state"`
	// ClusterID is the RDS cluster identifier.
	ClusterID string `json:"cluster_id"`
	// Region is the AWS region for this cluster.
	Region string `json:"region"`
	// Parameters contains operation-specific parameters.
	Parameters json.RawMessage `json:"parameters"`
	// Steps lists all steps in this operation.
	Steps []Step `json:"steps"`
	// CurrentStepIndex is the index of the currently executing step.
	CurrentStepIndex int `json:"current_step_index"`
	// Error contains the error message if the operation failed.
	Error string `json:"error,omitempty"`
	// PauseReason explains why the operation is paused.
	PauseReason string `json:"pause_reason,omitempty"`
	// WaitTimeout is the timeout for wait operations in seconds.
	// If 0, the engine's default timeout is used.
	WaitTimeout int `json:"wait_timeout,omitempty"`
	// PauseBeforeSteps is a list of step indices where the operation should auto-pause.
	// When execution reaches a step in this list, it will pause before starting that step.
	PauseBeforeSteps []int `json:"pause_before_steps,omitempty"`
	// CreatedAt is when the operation was created.
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt is when the operation was last updated.
	UpdatedAt time.Time `json:"updated_at"`
	// StartedAt is when the operation started executing.
	StartedAt *time.Time `json:"started_at,omitempty"`
	// CompletedAt is when the operation finished.
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// Step represents a single step in a maintenance operation.
type Step struct {
	// ID is the unique identifier for this step.
	ID string `json:"id"`
	// Name is a human-readable name for the step.
	Name string `json:"name"`
	// Description describes what this step does.
	Description string `json:"description"`
	// State is the current state of this step.
	State StepState `json:"state"`
	// Action is the action to perform (e.g., "create_instance", "failover").
	Action string `json:"action"`
	// Parameters contains step-specific parameters.
	Parameters json.RawMessage `json:"parameters,omitempty"`
	// Result contains the result of the step execution.
	Result json.RawMessage `json:"result,omitempty"`
	// Error contains the error message if the step failed.
	Error string `json:"error,omitempty"`
	// StartedAt is when the step started.
	StartedAt *time.Time `json:"started_at,omitempty"`
	// CompletedAt is when the step completed.
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	// WaitCondition describes what the step is waiting for.
	WaitCondition string `json:"wait_condition,omitempty"`
	// RetryCount tracks how many times this step has been retried.
	RetryCount int `json:"retry_count"`
	// MaxRetries is the maximum number of retries allowed.
	MaxRetries int `json:"max_retries"`
}

// Duration returns the duration of the operation, or time since start if still running.
func (op *Operation) Duration() time.Duration {
	if op.StartedAt == nil {
		return 0
	}
	endTime := time.Now()
	if op.CompletedAt != nil {
		endTime = *op.CompletedAt
	}
	return endTime.Sub(*op.StartedAt)
}

// Duration returns the duration of the step, or time since start if still running.
func (s *Step) Duration() time.Duration {
	if s.StartedAt == nil {
		return 0
	}
	endTime := time.Now()
	if s.CompletedAt != nil {
		endTime = *s.CompletedAt
	}
	return endTime.Sub(*s.StartedAt)
}

// InstanceTypeChangeParams contains parameters for instance type change operation.
type InstanceTypeChangeParams struct {
	// TargetInstanceType is the new instance type (e.g., "db.r6g.xlarge").
	TargetInstanceType string `json:"target_instance_type"`
	// ExcludeInstances is a list of instance IDs to exclude from the operation.
	// These instances will not be modified. Useful for excluding specific readers
	// that need to stay on a different instance type.
	ExcludeInstances []string `json:"exclude_instances,omitempty"`
	// SkipTempInstance skips creating a temporary instance for the operation.
	// By default (false), a temp instance is created for redundancy.
	// Set to true to skip temp instance creation (faster but less safe).
	SkipTempInstance bool `json:"skip_temp_instance,omitempty"`
}

// StorageTypeChangeParams contains parameters for storage type change operation.
type StorageTypeChangeParams struct {
	// TargetStorageType is the new storage type (e.g., "io1", "gp3").
	TargetStorageType string `json:"target_storage_type"`
	// IOPS is the provisioned IOPS (required for io1/io2).
	IOPS *int32 `json:"iops,omitempty"`
	// StorageThroughput is the storage throughput in MiBps (for gp3).
	StorageThroughput *int32 `json:"storage_throughput,omitempty"`
	// ExcludeInstances is a list of instance IDs to exclude from the operation.
	// These instances will not be modified. Useful for excluding specific readers
	// that need to stay on a different storage configuration.
	ExcludeInstances []string `json:"exclude_instances,omitempty"`
	// SkipTempInstance skips creating a temporary instance for the operation.
	// By default (false), a temp instance is created for redundancy.
	// Set to true to skip temp instance creation (faster but less safe).
	SkipTempInstance bool `json:"skip_temp_instance,omitempty"`
}

// EngineUpgradeParams contains parameters for engine upgrade operation using Blue-Green deployment.
type EngineUpgradeParams struct {
	// TargetEngineVersion is the new engine version (e.g., "16.4").
	TargetEngineVersion string `json:"target_engine_version"`
	// SwitchoverTimeout is the timeout in seconds for the switchover operation.
	// If 0, defaults to 300 seconds (5 minutes).
	SwitchoverTimeout int `json:"switchover_timeout,omitempty"`
	// DBClusterParameterGroupName is the cluster parameter group to use for the green environment.
	// If empty, will automatically create/use appropriate parameter group:
	// - For default PG: uses default.aurora-postgresqlXX for target version
	// - For custom PG: creates {cluster}-{version}-upgraded with migrated settings
	DBClusterParameterGroupName string `json:"db_cluster_parameter_group_name,omitempty"`
	// DBInstanceParameterGroupName is the instance parameter group to use for the green environment.
	// If empty, will automatically create/use appropriate parameter group:
	// - For default PG: uses default.aurora-postgresqlXX for target version
	// - For custom PG: creates {cluster}-{version}-instance-upgraded with migrated settings
	DBInstanceParameterGroupName string `json:"db_instance_parameter_group_name,omitempty"`
	// PauseBeforeSwitchover controls whether to auto-pause before the switchover step.
	// Defaults to true if not specified (nil).
	PauseBeforeSwitchover *bool `json:"pause_before_switchover,omitempty"`
	// SkipProxyRetarget controls whether to skip RDS Proxy validation and retargeting.
	// By default (nil or false), the operation will:
	// 1. Discover RDS Proxies pointing at this cluster
	// 2. Validate proxy health (fail if unhealthy)
	// 3. Deregister proxies before Blue-Green deployment (required by AWS)
	// 4. Re-register proxies after switchover
	// Set to true to skip all proxy-related steps (useful if no proxies exist).
	SkipProxyRetarget *bool `json:"skip_proxy_retarget,omitempty"`
	// PauseBeforeProxyDeregister controls whether to auto-pause before deregistering proxy targets.
	// Defaults to true if not specified (nil).
	// WARNING: Deregistering proxy targets will cause applications using the proxy to fail
	// until the upgrade completes and the cluster is re-registered.
	PauseBeforeProxyDeregister *bool `json:"pause_before_proxy_deregister,omitempty"`
	// PauseBeforeCleanup controls whether to auto-pause before cleanup step.
	// Defaults to true if not specified (nil).
	// This allows verification that the upgrade was successful before deleting old resources.
	PauseBeforeCleanup *bool `json:"pause_before_cleanup,omitempty"`
}

// InstanceCycleParams contains parameters for instance cycle (reboot) operation.
// This operation has no required parameters - it will reboot all instances in the cluster.
type InstanceCycleParams struct {
	// ExcludeInstances is a list of instance IDs to exclude from the operation.
	// These instances will not be rebooted. Useful for excluding specific instances
	// that should not be restarted (e.g., writer to avoid failover).
	ExcludeInstances []string `json:"exclude_instances,omitempty"`
	// SkipTempInstance skips creating a temporary instance for the operation.
	// By default (false), a temp instance is created for redundancy.
	// Set to true to skip temp instance creation (faster but less safe).
	SkipTempInstance bool `json:"skip_temp_instance,omitempty"`
}

// ClusterSummary contains summary information about an RDS cluster for listing.
type ClusterSummary struct {
	// ClusterID is the cluster identifier.
	ClusterID string `json:"cluster_id"`
	// Engine is the database engine (e.g., "aurora-postgresql").
	Engine string `json:"engine"`
	// EngineVersion is the current engine version.
	EngineVersion string `json:"engine_version"`
	// Status is the current cluster status.
	Status string `json:"status"`
}

// ClusterInfo contains information about an RDS cluster.
type ClusterInfo struct {
	// ClusterID is the cluster identifier.
	ClusterID string `json:"cluster_id"`
	// Engine is the database engine (e.g., "aurora-postgresql").
	Engine string `json:"engine"`
	// EngineVersion is the current engine version.
	EngineVersion string `json:"engine_version"`
	// Status is the current cluster status.
	Status string `json:"status"`
	// Instances contains info about each instance in the cluster.
	Instances []InstanceInfo `json:"instances"`
}

// InstanceInfo contains information about an RDS instance.
type InstanceInfo struct {
	// InstanceID is the instance identifier.
	InstanceID string `json:"instance_id"`
	// InstanceType is the current instance type.
	InstanceType string `json:"instance_type"`
	// Role indicates if this is a writer or reader.
	Role string `json:"role"`
	// Status is the current instance status.
	Status string `json:"status"`
	// IsAutoScaled indicates if this instance was created by autoscaling.
	IsAutoScaled bool `json:"is_auto_scaled"`
	// StorageType is the storage type.
	StorageType string `json:"storage_type,omitempty"`
	// IOPS is the provisioned IOPS.
	IOPS *int32 `json:"iops,omitempty"`
}

// Event represents an event that occurred during an operation.
type Event struct {
	// ID is the unique event identifier.
	ID string `json:"id"`
	// OperationID is the operation this event belongs to.
	OperationID string `json:"operation_id"`
	// Type is the event type (e.g., "step_started", "step_completed").
	Type string `json:"type"`
	// Message is a human-readable message.
	Message string `json:"message"`
	// Data contains additional event data.
	Data json.RawMessage `json:"data,omitempty"`
	// Timestamp is when the event occurred.
	Timestamp time.Time `json:"timestamp"`
}

// InterventionRequest represents a request for human intervention.
type InterventionRequest struct {
	// Type is the type of intervention (e.g., "approval", "decision").
	Type string `json:"type"`
	// Message describes what intervention is needed.
	Message string `json:"message"`
	// Options lists available choices for the intervention.
	Options []string `json:"options,omitempty"`
}

// InterventionResponse represents a human response to an intervention request.
type InterventionResponse struct {
	// Action is the chosen action (e.g., "continue", "rollback", "abort").
	Action string `json:"action"`
	// Comment is an optional comment from the operator.
	Comment string `json:"comment,omitempty"`
}

// ValidOperationStates contains all valid operation states.
var ValidOperationStates = map[OperationState]bool{
	StateCreated:     true,
	StateRunning:     true,
	StatePaused:      true,
	StateCompleted:   true,
	StateFailed:      true,
	StateRollingBack: true,
	StateRolledBack:  true,
}

// ValidOperationTypes contains all valid operation types.
var ValidOperationTypes = map[OperationType]bool{
	OperationTypeInstanceTypeChange: true,
	OperationTypeStorageTypeChange:  true,
	OperationTypeEngineUpgrade:      true,
	OperationTypeInstanceCycle:      true,
}

// ValidStepStates contains all valid step states.
var ValidStepStates = map[StepState]bool{
	StepStatePending:    true,
	StepStateInProgress: true,
	StepStateWaiting:    true,
	StepStateCompleted:  true,
	StepStateFailed:     true,
	StepStateSkipped:    true,
}

// Validate checks if the operation has valid required fields.
// Returns nil if valid, or an error describing the validation failure.
func (op *Operation) Validate() error {
	if op.ID == "" {
		return &ValidationError{Field: "id", Message: "operation ID is required"}
	}
	if !ValidOperationTypes[op.Type] {
		return &ValidationError{Field: "type", Message: "invalid operation type: " + string(op.Type)}
	}
	if !ValidOperationStates[op.State] {
		return &ValidationError{Field: "state", Message: "invalid operation state: " + string(op.State)}
	}
	if op.ClusterID == "" {
		return &ValidationError{Field: "cluster_id", Message: "cluster ID is required"}
	}
	if op.CreatedAt.IsZero() {
		return &ValidationError{Field: "created_at", Message: "created_at is required"}
	}
	if op.CurrentStepIndex < 0 {
		return &ValidationError{Field: "current_step_index", Message: "current_step_index cannot be negative"}
	}
	if op.CurrentStepIndex > len(op.Steps) {
		return &ValidationError{Field: "current_step_index", Message: "current_step_index exceeds number of steps"}
	}
	// Validate each step
	for i, step := range op.Steps {
		if err := step.Validate(); err != nil {
			if ve, ok := err.(*ValidationError); ok {
				ve.Field = "steps[" + strconv.Itoa(i) + "]." + ve.Field
				return ve
			}
			return err
		}
	}
	return nil
}

// Validate checks if the step has valid required fields.
func (s *Step) Validate() error {
	if s.ID == "" {
		return &ValidationError{Field: "id", Message: "step ID is required"}
	}
	if s.Name == "" {
		return &ValidationError{Field: "name", Message: "step name is required"}
	}
	if !ValidStepStates[s.State] {
		return &ValidationError{Field: "state", Message: "invalid step state: " + string(s.State)}
	}
	if s.Action == "" {
		return &ValidationError{Field: "action", Message: "step action is required"}
	}
	return nil
}

// Validate checks if the event has valid required fields.
func (e *Event) Validate() error {
	if e.ID == "" {
		return &ValidationError{Field: "id", Message: "event ID is required"}
	}
	if e.OperationID == "" {
		return &ValidationError{Field: "operation_id", Message: "operation ID is required"}
	}
	if e.Type == "" {
		return &ValidationError{Field: "type", Message: "event type is required"}
	}
	if e.Timestamp.IsZero() {
		return &ValidationError{Field: "timestamp", Message: "timestamp is required"}
	}
	return nil
}

// ValidationError represents a validation error for a specific field.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}
