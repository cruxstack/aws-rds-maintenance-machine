// Package machine provides step function execution types and methods.
package machine

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cockroachdb/errors"
	internalerrors "github.com/mpz/devops/tools/rds-maint-machine/internal/errors"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/types"
)

// StepExecutionResult contains the result of executing a single step.
// This is designed for Step Functions integration where each Lambda invocation
// executes only one step and returns immediately.
type StepExecutionResult struct {
	// OperationID is the operation identifier.
	OperationID string `json:"operation_id"`
	// OperationState is the current state of the operation.
	OperationState types.OperationState `json:"operation_state"`

	// StepIndex is the index of the step that was executed.
	StepIndex int `json:"step_index"`
	// StepName is the human-readable name of the step.
	StepName string `json:"step_name"`
	// StepAction is the action type of the step.
	StepAction string `json:"step_action"`
	// StepState is the state of the step after execution.
	StepState types.StepState `json:"step_state"`

	// WaitCondition describes what the step is waiting for (if waiting).
	WaitCondition string `json:"wait_condition,omitempty"`
	// PauseReason explains why the operation is paused (if paused).
	PauseReason string `json:"pause_reason,omitempty"`

	// Continue indicates whether Step Functions should continue execution.
	// True if there are more steps to execute or the current step is still in progress.
	Continue bool `json:"continue"`
	// NeedsWait indicates the current step is waiting for a condition.
	// Step Functions should use Wait state then call poll action.
	NeedsWait bool `json:"needs_wait"`
	// NeedsIntervention indicates human intervention is required.
	// Step Functions should use callback pattern to wait for external input.
	NeedsIntervention bool `json:"needs_intervention"`
	// Completed indicates the operation has completed successfully.
	Completed bool `json:"completed"`
	// Failed indicates the operation has failed.
	Failed bool `json:"failed"`

	// Error contains any error message from step execution.
	Error string `json:"error,omitempty"`
	// StepResult contains the result data from step execution.
	StepResult json.RawMessage `json:"step_result,omitempty"`
}

// PollResult contains the result of polling a wait condition.
type PollResult struct {
	// OperationID is the operation identifier.
	OperationID string `json:"operation_id"`
	// OperationState is the current state of the operation.
	OperationState types.OperationState `json:"operation_state"`

	// StepIndex is the index of the step being polled.
	StepIndex int `json:"step_index"`
	// StepName is the human-readable name of the step.
	StepName string `json:"step_name"`
	// StepAction is the action type of the step.
	StepAction string `json:"step_action"`
	// StepState is the current state of the step.
	StepState types.StepState `json:"step_state"`

	// WaitCondition describes what the step is waiting for.
	WaitCondition string `json:"wait_condition,omitempty"`

	// Ready indicates the wait condition is satisfied and the step can complete.
	Ready bool `json:"ready"`
	// Continue indicates Step Functions should continue to next step.
	// True when the step has completed after polling.
	Continue bool `json:"continue"`
	// NeedsIntervention indicates human intervention is required.
	NeedsIntervention bool `json:"needs_intervention"`

	// Error contains any error message from polling.
	Error string `json:"error,omitempty"`
}

// ExecuteCurrentStep executes only the current step synchronously.
// This method is designed for Step Functions integration where each Lambda
// invocation should execute a single step and return immediately.
//
// Unlike executeSteps which runs in a background goroutine, this method:
// - Executes synchronously (blocks until step completes or reaches wait state)
// - Returns after executing one step (or attempting to)
// - Does not automatically advance to next step
// - Returns detailed status for Step Functions to determine next action
func (e *Engine) ExecuteCurrentStep(ctx context.Context, id string) (*StepExecutionResult, error) {
	e.mu.Lock()
	op, ok := e.operations[id]
	if !ok {
		e.mu.Unlock()
		return nil, internalerrors.ErrOperationNotFound
	}

	result := &StepExecutionResult{
		OperationID:    op.ID,
		OperationState: op.State,
		StepIndex:      op.CurrentStepIndex,
	}

	// Check if operation is in a state where we can execute steps
	switch op.State {
	case types.StateCompleted:
		result.Completed = true
		e.mu.Unlock()
		return result, nil

	case types.StateFailed, types.StateRolledBack:
		result.Failed = true
		result.Error = op.Error
		e.mu.Unlock()
		return result, nil

	case types.StatePaused:
		result.NeedsIntervention = true
		result.PauseReason = op.PauseReason
		e.mu.Unlock()
		return result, nil

	case types.StateCreated:
		// Need to start the operation first
		e.mu.Unlock()
		return nil, errors.Wrap(internalerrors.ErrInvalidState, "operation not started; call 'start' action first")

	case types.StateRollingBack:
		e.mu.Unlock()
		return nil, errors.Wrap(internalerrors.ErrInvalidState, "operation is rolling back")
	}

	// Operation is running - check if we have more steps
	if op.CurrentStepIndex >= len(op.Steps) {
		// All steps complete - mark operation as completed
		op.State = types.StateCompleted
		now := time.Now()
		op.CompletedAt = &now
		op.UpdatedAt = now
		e.mu.Unlock()

		e.persistOperation(ctx, op)
		e.addEvent(op.ID, "operation_completed", "Operation completed successfully", nil)
		if e.notifier != nil {
			e.notifier.NotifyOperationCompleted(ctx, op)
		}

		result.OperationState = types.StateCompleted
		result.Completed = true
		return result, nil
	}

	step := &op.Steps[op.CurrentStepIndex]
	result.StepName = step.Name
	result.StepAction = step.Action
	result.StepState = step.State

	// If step is already waiting, return that status (caller should use poll)
	if step.State == types.StepStateWaiting {
		result.WaitCondition = step.WaitCondition
		result.NeedsWait = true
		result.Continue = true
		e.mu.Unlock()
		return result, nil
	}

	// If step is already completed, advance to next step
	if step.State == types.StepStateCompleted {
		op.CurrentStepIndex++
		op.UpdatedAt = time.Now()
		e.mu.Unlock()
		e.persistOperation(ctx, op)

		// Recursive call to execute the next step
		return e.ExecuteCurrentStep(ctx, id)
	}

	e.mu.Unlock()

	// Execute the step
	err := e.executeStepForStepFn(ctx, op, step)

	e.mu.Lock()

	// Update result with current state
	result.StepState = step.State
	result.OperationState = op.State
	result.StepResult = step.Result

	if err != nil {
		if errors.Is(err, internalerrors.ErrInterventionRequired) {
			result.NeedsIntervention = true
			result.PauseReason = op.PauseReason
			e.mu.Unlock()
			return result, nil
		}

		// Check if we should retry
		if step.RetryCount < step.MaxRetries {
			step.RetryCount++
			step.State = types.StepStatePending
			step.Error = ""
			e.mu.Unlock()
			e.persistOperation(ctx, op)
			e.addEvent(op.ID, "step_retry", "Step will be retried: "+step.Name, nil)
			result.StepState = types.StepStatePending
			result.Continue = true
			return result, nil
		}

		// Step failed - pause operation for intervention
		step.State = types.StepStateFailed
		step.Error = err.Error()
		now := time.Now()
		step.CompletedAt = &now
		op.State = types.StatePaused
		op.PauseReason = "Step failed: " + step.Name + " - " + err.Error()
		op.UpdatedAt = time.Now()
		e.mu.Unlock()

		e.persistOperation(ctx, op)
		e.addEvent(op.ID, "step_failed", step.Error, nil)
		if e.notifier != nil {
			e.notifier.NotifyOperationPaused(ctx, op, op.PauseReason)
		}

		result.StepState = types.StepStateFailed
		result.OperationState = types.StatePaused
		result.NeedsIntervention = true
		result.PauseReason = op.PauseReason
		result.Error = err.Error()
		return result, nil
	}

	// Check if step is now waiting
	if step.State == types.StepStateWaiting {
		e.mu.Unlock()
		e.persistOperation(ctx, op)
		result.WaitCondition = step.WaitCondition
		result.NeedsWait = true
		result.Continue = true
		return result, nil
	}

	// Step completed successfully
	step.State = types.StepStateCompleted
	now := time.Now()
	step.CompletedAt = &now
	op.CurrentStepIndex++
	op.UpdatedAt = time.Now()

	// Check if this was the last step
	if op.CurrentStepIndex >= len(op.Steps) {
		op.State = types.StateCompleted
		op.CompletedAt = &now
		result.OperationState = types.StateCompleted
		result.Completed = true
		result.Continue = false
	} else {
		result.Continue = true
	}
	e.mu.Unlock()

	e.persistOperation(ctx, op)
	e.addEvent(op.ID, "step_completed", "Completed: "+step.Name, nil)
	if e.notifier != nil {
		e.notifier.NotifyStepCompleted(ctx, op, step)
	}

	if result.Completed {
		e.addEvent(op.ID, "operation_completed", "Operation completed successfully", nil)
		if e.notifier != nil {
			e.notifier.NotifyOperationCompleted(ctx, op)
		}
	}

	result.StepState = types.StepStateCompleted
	return result, nil
}

// executeStepForStepFn executes a single step for Step Functions mode.
// Unlike the regular executeStep, this version:
// - Does NOT start wait loops (returns early with StepStateWaiting)
// - Is designed for synchronous execution within Lambda timeout
func (e *Engine) executeStepForStepFn(ctx context.Context, op *types.Operation, step *types.Step) error {
	e.mu.Lock()
	step.State = types.StepStateInProgress
	now := time.Now()
	step.StartedAt = &now
	e.mu.Unlock()

	e.persistOperation(ctx, op)
	e.addEvent(op.ID, "step_started", "Starting: "+step.Name, nil)

	handler, ok := e.handlers[step.Action]
	if !ok {
		return errors.Wrapf(internalerrors.ErrInvalidParameter, "unknown action: %s", step.Action)
	}

	// Execute the handler
	// For wait handlers, they will set StepStateWaiting and return nil
	return handler(ctx, op, step)
}

// PollCurrentStep polls the current step's wait condition without advancing.
// This is used by Step Functions to check if a waiting step is ready to complete.
//
// For steps in StepStateWaiting, this method:
// - Checks if the wait condition is satisfied
// - If satisfied, completes the step and advances to next
// - If not satisfied, returns Ready=false so Step Functions can wait and retry
func (e *Engine) PollCurrentStep(ctx context.Context, id string) (*PollResult, error) {
	e.mu.RLock()
	op, ok := e.operations[id]
	if !ok {
		e.mu.RUnlock()
		return nil, internalerrors.ErrOperationNotFound
	}

	result := &PollResult{
		OperationID:    op.ID,
		OperationState: op.State,
		StepIndex:      op.CurrentStepIndex,
	}

	// Check operation state
	if op.State == types.StateCompleted {
		result.Ready = true
		e.mu.RUnlock()
		return result, nil
	}

	if op.State == types.StatePaused {
		result.NeedsIntervention = true
		e.mu.RUnlock()
		return result, nil
	}

	if op.State != types.StateRunning {
		e.mu.RUnlock()
		return nil, errors.Wrapf(internalerrors.ErrInvalidState, "operation is in state %s", op.State)
	}

	if op.CurrentStepIndex >= len(op.Steps) {
		result.Ready = true
		result.Continue = true
		e.mu.RUnlock()
		return result, nil
	}

	step := &op.Steps[op.CurrentStepIndex]
	result.StepName = step.Name
	result.StepAction = step.Action
	result.StepState = step.State
	result.WaitCondition = step.WaitCondition

	// If step is not waiting, it's either pending, in progress, or completed
	if step.State != types.StepStateWaiting {
		if step.State == types.StepStateCompleted {
			result.Ready = true
			result.Continue = true
		}
		e.mu.RUnlock()
		return result, nil
	}

	e.mu.RUnlock()

	// Poll the wait condition based on step action
	ready, err := e.pollWaitCondition(ctx, op, step)
	if err != nil {
		// Check if this is a retriable error or a fatal one
		if errors.Is(err, internalerrors.ErrWaitTimeout) {
			result.Error = err.Error()
			return result, nil
		}
		return nil, err
	}

	if !ready {
		return result, nil
	}

	// Wait condition satisfied - complete the step
	e.mu.Lock()
	step.State = types.StepStateCompleted
	now := time.Now()
	step.CompletedAt = &now
	op.CurrentStepIndex++
	op.UpdatedAt = time.Now()
	e.mu.Unlock()

	e.persistOperation(ctx, op)
	e.addEvent(op.ID, "step_completed", "Completed: "+step.Name, nil)
	if e.notifier != nil {
		e.notifier.NotifyStepCompleted(ctx, op, step)
	}

	result.Ready = true
	result.Continue = op.CurrentStepIndex < len(op.Steps)
	result.StepState = types.StepStateCompleted
	return result, nil
}

// pollWaitCondition checks if the wait condition for a step is satisfied.
// Returns true if the condition is met and the step can complete.
func (e *Engine) pollWaitCondition(ctx context.Context, op *types.Operation, step *types.Step) (bool, error) {
	rdsClient, err := e.getRDSClient(ctx, op)
	if err != nil {
		return false, err
	}

	switch step.Action {
	case "wait_instance_available":
		var params struct {
			InstanceID string `json:"instance_id"`
		}
		if len(step.Parameters) > 0 {
			json.Unmarshal(step.Parameters, &params)
		}
		// Only fall back to temp instance ID if this is specifically the temp instance wait step
		if params.InstanceID == "" {
			isTempInstanceWait := step.Name == "Wait for temp instance"
			if isTempInstanceWait {
				params.InstanceID = e.findCreatedInstanceID(op)
			} else {
				// This is NOT a temp instance wait, but instance_id is missing.
				// This is a critical error that would cause parallel modifications.
				e.logger.Error("CRITICAL: wait_instance_available step missing instance_id parameter (stepfn)",
					"operation_id", op.ID,
					"step_id", step.ID,
					"step_name", step.Name)
				return false, errors.Wrapf(internalerrors.ErrInvalidParameter,
					"instance_id required for step %q - missing parameter would cause parallel modifications", step.Name)
			}
		}
		if params.InstanceID == "" {
			return false, errors.Wrap(internalerrors.ErrInvalidParameter, "instance_id required")
		}

		// Get instance info
		instanceInfo, err := rdsClient.GetInstanceInfo(ctx, params.InstanceID)
		if err != nil {
			return false, nil // Transient error, keep waiting
		}

		// Check if instance is available
		if instanceInfo.Status != "available" {
			step.WaitCondition = fmt.Sprintf("instance status: %s", instanceInfo.Status)
			return false, nil
		}

		// Determine target configuration from previous modify step
		var targetInstanceType string
		var targetStorageType string
		for i := op.CurrentStepIndex - 1; i >= 0; i-- {
			prevStep := &op.Steps[i]
			if prevStep.Action == "modify_instance" {
				var modifyParams struct {
					InstanceID   string `json:"instance_id"`
					InstanceType string `json:"instance_type,omitempty"`
					StorageType  string `json:"storage_type,omitempty"`
				}
				if json.Unmarshal(prevStep.Parameters, &modifyParams) == nil {
					if modifyParams.InstanceID == params.InstanceID {
						targetInstanceType = modifyParams.InstanceType
						targetStorageType = modifyParams.StorageType
						break
					}
				}
			}
		}

		// Check if configuration matches
		if targetInstanceType != "" && instanceInfo.InstanceType != targetInstanceType {
			step.WaitCondition = fmt.Sprintf("instance type is %s, waiting for %s", instanceInfo.InstanceType, targetInstanceType)
			return false, nil
		}

		if targetStorageType != "" && instanceInfo.StorageType != targetStorageType {
			step.WaitCondition = fmt.Sprintf("storage type is %s, waiting for %s", instanceInfo.StorageType, targetStorageType)
			return false, nil
		}

		// Instance is available AND has desired configuration
		return true, nil

	case "wait_instance_deleted":
		var params struct {
			InstanceID string `json:"instance_id"`
		}
		if len(step.Parameters) > 0 {
			json.Unmarshal(step.Parameters, &params)
		}
		if params.InstanceID == "" {
			params.InstanceID = e.findCreatedInstanceID(op)
		}
		if params.InstanceID == "" {
			return false, errors.Wrap(internalerrors.ErrInvalidParameter, "instance_id required")
		}

		deleted, err := rdsClient.IsInstanceDeleted(ctx, params.InstanceID)
		if err != nil {
			return false, nil // Transient error, keep waiting
		}
		return deleted, nil

	case "wait_cluster_available":
		info, err := rdsClient.GetClusterInfo(ctx, op.ClusterID)
		if err != nil {
			e.logger.Warn("transient error getting cluster info",
				"operation_id", op.ID,
				"cluster_id", op.ClusterID,
				"error", err)
			return false, nil // Transient error, keep waiting
		}

		// Check cluster and all instances are available
		if info.Status != "available" {
			step.WaitCondition = "cluster status: " + info.Status
			return false, nil
		}
		for _, inst := range info.Instances {
			if inst.Status != "available" {
				step.WaitCondition = "instance " + inst.InstanceID + " status: " + inst.Status
				e.logger.Info("waiting for instance to become available",
					"operation_id", op.ID,
					"instance_id", inst.InstanceID,
					"status", inst.Status)
				return false, nil
			}
		}
		return true, nil

	case "wait_snapshot_available":
		var params struct {
			SnapshotID string `json:"snapshot_id"`
		}
		if len(step.Parameters) > 0 {
			json.Unmarshal(step.Parameters, &params)
		}
		if params.SnapshotID == "" {
			params.SnapshotID = e.findCreatedSnapshotID(op)
		}
		if params.SnapshotID == "" {
			return false, errors.Wrap(internalerrors.ErrInvalidParameter, "snapshot_id required")
		}

		available, err := rdsClient.IsSnapshotAvailable(ctx, params.SnapshotID)
		if err != nil {
			return false, nil
		}
		return available, nil

	case "wait_blue_green_available":
		deploymentID := e.findBlueGreenDeploymentID(op)
		if deploymentID == "" {
			return false, errors.Wrap(internalerrors.ErrInvalidParameter, "blue-green deployment identifier not found")
		}

		bgInfo, err := rdsClient.DescribeBlueGreenDeployment(ctx, deploymentID)
		if err != nil {
			return false, nil
		}

		switch bgInfo.Status {
		case "AVAILABLE":
			return true, nil
		case "INVALID_CONFIGURATION", "PROVISIONING_FAILED":
			return false, errors.Errorf("Blue-Green deployment failed: %s", bgInfo.Status)
		default:
			return false, nil
		}

	case "failover_to_instance":
		// For failover, check if target instance is now the writer
		var params struct {
			InstanceID string `json:"instance_id"`
		}
		if len(step.Parameters) > 0 {
			json.Unmarshal(step.Parameters, &params)
		}
		if params.InstanceID == "" {
			params.InstanceID = e.findCreatedInstanceID(op)
		}

		info, err := rdsClient.GetClusterInfo(ctx, op.ClusterID)
		if err != nil {
			return false, nil
		}

		for _, inst := range info.Instances {
			if inst.InstanceID == params.InstanceID && inst.Role == "writer" {
				return true, nil
			}
		}
		return false, nil

	case "switchover_blue_green":
		deploymentID := e.findBlueGreenDeploymentID(op)
		if deploymentID == "" {
			return false, errors.Wrap(internalerrors.ErrInvalidParameter, "blue-green deployment identifier not found")
		}

		bgInfo, err := rdsClient.DescribeBlueGreenDeployment(ctx, deploymentID)
		if err != nil {
			return false, nil
		}

		switch bgInfo.Status {
		case "SWITCHOVER_COMPLETED":
			return true, nil
		case "SWITCHOVER_FAILED":
			return false, errors.Errorf("switchover failed: %s", bgInfo.StatusDetails)
		default:
			return false, nil
		}

	default:
		// Non-wait actions are considered ready immediately
		return true, nil
	}
}
