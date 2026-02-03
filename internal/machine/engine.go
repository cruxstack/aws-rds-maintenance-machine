// Package machine provides the state machine engine for RDS maintenance operations.
package machine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/google/uuid"
	internalerrors "github.com/mpz/devops/tools/rds-maint-machine/internal/errors"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/rds"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/storage"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/types"
)

// Engine is the state machine engine that executes operations.
type Engine struct {
	mu            sync.RWMutex
	operations    map[string]*types.Operation
	events        map[string][]types.Event
	clientManager *rds.ClientManager
	store         storage.Store
	logger        *slog.Logger
	handlers      map[string]StepHandler
	notifier      Notifier

	// Configuration
	defaultRegion       string
	defaultWaitTimeout  time.Duration
	defaultPollInterval time.Duration
}

// StepHandler is a function that executes a single step.
type StepHandler func(ctx context.Context, op *types.Operation, step *types.Step) error

// Notifier sends notifications about operation events.
type Notifier interface {
	NotifyOperationStarted(ctx context.Context, op *types.Operation) error
	NotifyOperationCompleted(ctx context.Context, op *types.Operation) error
	NotifyOperationFailed(ctx context.Context, op *types.Operation) error
	NotifyOperationPaused(ctx context.Context, op *types.Operation, reason string) error
	NotifyStepCompleted(ctx context.Context, op *types.Operation, step *types.Step) error
}

// EngineConfig contains configuration for the engine.
type EngineConfig struct {
	ClientManager       *rds.ClientManager
	Store               storage.Store
	Logger              *slog.Logger
	Notifier            Notifier
	DefaultRegion       string
	DefaultWaitTimeout  time.Duration
	DefaultPollInterval time.Duration
}

// NewEngine creates a new state machine engine.
func NewEngine(cfg EngineConfig) *Engine {
	e := &Engine{
		operations:          make(map[string]*types.Operation),
		events:              make(map[string][]types.Event),
		clientManager:       cfg.ClientManager,
		store:               cfg.Store,
		logger:              cfg.Logger,
		handlers:            make(map[string]StepHandler),
		notifier:            cfg.Notifier,
		defaultRegion:       cfg.DefaultRegion,
		defaultWaitTimeout:  cfg.DefaultWaitTimeout,
		defaultPollInterval: cfg.DefaultPollInterval,
	}

	if e.logger == nil {
		e.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if e.defaultWaitTimeout == 0 {
		e.defaultWaitTimeout = 45 * time.Minute
	}
	if e.defaultPollInterval == 0 {
		e.defaultPollInterval = 30 * time.Second
	}
	if e.defaultRegion == "" {
		e.defaultRegion = "us-east-1"
	}
	if e.store == nil {
		e.store = &storage.NullStore{}
	}

	// Register default step handlers
	e.registerHandlers()

	return e
}

// registerHandlers registers the step handlers.
func (e *Engine) registerHandlers() {
	e.handlers["get_cluster_info"] = e.handleGetClusterInfo
	e.handlers["create_temp_instance"] = e.handleCreateTempInstance
	e.handlers["wait_instance_available"] = e.handleWaitInstanceAvailable
	e.handlers["failover_to_instance"] = e.handleFailoverToInstance
	e.handlers["modify_instance"] = e.handleModifyInstance
	e.handlers["delete_instance"] = e.handleDeleteInstance
	e.handlers["wait_instance_deleted"] = e.handleWaitInstanceDeleted
	e.handlers["create_snapshot"] = e.handleCreateSnapshot
	e.handlers["wait_snapshot_available"] = e.handleWaitSnapshotAvailable
	e.handlers["modify_cluster"] = e.handleModifyCluster
	e.handlers["wait_cluster_available"] = e.handleWaitClusterAvailable
	e.handlers["prepare_parameter_group"] = e.handlePrepareParameterGroup

	// Blue-Green deployment handlers
	e.handlers["create_blue_green_deployment"] = e.handleCreateBlueGreenDeployment
	e.handlers["wait_blue_green_available"] = e.handleWaitBlueGreenAvailable
	e.handlers["switchover_blue_green"] = e.handleSwitchoverBlueGreen
	e.handlers["cleanup_blue_green"] = e.handleCleanupBlueGreen

	// RDS Proxy handlers
	e.handlers["validate_proxy_health"] = e.handleValidateProxyHealth
	e.handlers["deregister_proxy_targets"] = e.handleDeregisterProxyTargets
	e.handlers["register_proxy_targets"] = e.handleRegisterProxyTargets
	e.handlers["retarget_proxies"] = e.handleRetargetProxies // deprecated: kept for backward compatibility

	// Instance cycle handlers
	e.handlers["reboot_instance"] = e.handleRebootInstance
}

// LoadFromStore loads all operations and events from persistent storage.
// Returns a list of operation IDs that were in running state and need to be resumed.
func (e *Engine) LoadFromStore(ctx context.Context) ([]string, error) {
	operations, events, err := e.store.LoadAll(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "load from store")
	}

	e.mu.Lock()
	e.operations = operations
	e.events = events
	e.mu.Unlock()

	// Find operations that need to be resumed
	var runningOps []string
	for id, op := range operations {
		if op.State == types.StateRunning {
			runningOps = append(runningOps, id)
		}
	}

	e.logger.Info("loaded state from storage",
		slog.Int("operations", len(operations)),
		slog.Int("running", len(runningOps)))

	return runningOps, nil
}

// ResumeRunningOperations resumes operations that were running when the server stopped.
// If autoResume is false, it pauses them instead with a "server restarted" reason.
func (e *Engine) ResumeRunningOperations(ctx context.Context, operationIDs []string, autoResume bool) {
	for _, id := range operationIDs {
		e.mu.Lock()
		op, ok := e.operations[id]
		if !ok {
			e.mu.Unlock()
			continue
		}

		if autoResume {
			e.logger.Info("auto-resuming operation", slog.String("operation_id", id))
			e.mu.Unlock()
			e.addEvent(id, "operation_resumed", "Operation auto-resumed after server restart", nil)
			go e.executeSteps(context.Background(), op)
		} else {
			e.logger.Info("pausing operation after restart", slog.String("operation_id", id))
			op.State = types.StatePaused
			op.PauseReason = "Server restarted - manual resume required"
			op.UpdatedAt = time.Now()
			e.mu.Unlock()
			e.addEvent(id, "operation_paused", "Server restarted - manual resume required", nil)
			e.persistOperation(ctx, op)
		}
	}
}

// CreateOperation creates a new operation.
func (e *Engine) CreateOperation(ctx context.Context, opType types.OperationType, clusterID, region string, params json.RawMessage, waitTimeout int) (*types.Operation, error) {
	// Use default region if not specified
	if region == "" {
		region = e.defaultRegion
	}

	// Check if there's already a running operation for this cluster
	e.mu.RLock()
	for _, op := range e.operations {
		if op.ClusterID == clusterID && op.Region == region && (op.State == types.StateRunning || op.State == types.StatePaused) {
			e.mu.RUnlock()
			return nil, errors.Wrapf(internalerrors.ErrOperationAlreadyRunning, "cluster %s in region %s", clusterID, region)
		}
	}
	e.mu.RUnlock()

	now := time.Now()
	op := &types.Operation{
		ID:          uuid.New().String(),
		Type:        opType,
		State:       types.StateCreated,
		ClusterID:   clusterID,
		Region:      region,
		Parameters:  params,
		WaitTimeout: waitTimeout,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// Build steps based on operation type (outside of lock since it makes RDS calls)
	var err error
	switch opType {
	case types.OperationTypeInstanceTypeChange:
		err = e.buildInstanceTypeChangeSteps(ctx, op)
	case types.OperationTypeStorageTypeChange:
		err = e.buildStorageTypeChangeSteps(ctx, op)
	case types.OperationTypeEngineUpgrade:
		err = e.buildEngineUpgradeSteps(ctx, op)
	case types.OperationTypeInstanceCycle:
		err = e.buildInstanceCycleSteps(ctx, op)
	default:
		return nil, errors.Wrapf(internalerrors.ErrInvalidParameter, "unknown operation type: %s", opType)
	}

	if err != nil {
		return nil, errors.Wrap(err, "build steps")
	}

	// Now acquire lock to store the operation
	e.mu.Lock()
	e.operations[op.ID] = op
	e.events[op.ID] = []types.Event{}
	e.addEventLocked(op.ID, "operation_created", "Operation created", nil)
	e.mu.Unlock()

	// Persist to storage
	e.persistOperation(ctx, op)

	return op, nil
}

// GetOperation returns an operation by ID.
func (e *Engine) GetOperation(id string) (*types.Operation, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	op, ok := e.operations[id]
	if !ok {
		return nil, internalerrors.ErrOperationNotFound
	}

	return op, nil
}

// ListOperations returns all operations.
func (e *Engine) ListOperations() []*types.Operation {
	e.mu.RLock()
	defer e.mu.RUnlock()

	ops := make([]*types.Operation, 0, len(e.operations))
	for _, op := range e.operations {
		ops = append(ops, op)
	}

	return ops
}

// GetEvents returns events for an operation.
func (e *Engine) GetEvents(operationID string) ([]types.Event, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	events, ok := e.events[operationID]
	if !ok {
		return nil, internalerrors.ErrOperationNotFound
	}

	return events, nil
}

// DeleteOperation deletes an operation that was created but never started.
// Only operations in the "created" state can be deleted.
func (e *Engine) DeleteOperation(ctx context.Context, id string) error {
	return e.deleteOperation(ctx, id, false)
}

// ForceDeleteOperation deletes an operation regardless of its state.
// This is intended for demo mode reset functionality.
func (e *Engine) ForceDeleteOperation(ctx context.Context, id string) error {
	return e.deleteOperation(ctx, id, true)
}

// deleteOperation is the internal implementation for deleting operations.
func (e *Engine) deleteOperation(ctx context.Context, id string, force bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	op, ok := e.operations[id]
	if !ok {
		return internalerrors.ErrOperationNotFound
	}

	// Only allow deletion of operations that were never started (unless forced)
	if !force && op.State != types.StateCreated {
		return errors.Wrapf(internalerrors.ErrInvalidState,
			"cannot delete operation in state %q; only operations in %q state can be deleted",
			op.State, types.StateCreated)
	}

	// Remove from in-memory maps
	delete(e.operations, id)
	delete(e.events, id)

	// Remove from persistent storage
	if err := e.store.DeleteOperation(ctx, id); err != nil {
		e.logger.Error("failed to delete operation from store", "operation_id", id, "error", err)
		// Continue anyway since we've already removed it from memory
	}

	e.logger.Info("operation deleted", "operation_id", id, "force", force)
	return nil
}

// UpdateOperationTimeout updates the wait timeout for an operation.
func (e *Engine) UpdateOperationTimeout(ctx context.Context, id string, timeout int) error {
	e.mu.Lock()
	op, ok := e.operations[id]
	if !ok {
		e.mu.Unlock()
		return internalerrors.ErrOperationNotFound
	}

	op.WaitTimeout = timeout
	op.UpdatedAt = time.Now()
	e.mu.Unlock()

	e.persistOperation(ctx, op)
	e.addEvent(id, "timeout_updated", "Wait timeout updated to "+(time.Duration(timeout)*time.Second).String(), nil)

	return nil
}

// ResetOperationToStep resets an operation to a specific step in paused state.
// This is intended for debugging and testing purposes.
func (e *Engine) ResetOperationToStep(ctx context.Context, id string, stepIndex int) error {
	e.mu.Lock()
	op, ok := e.operations[id]
	if !ok {
		e.mu.Unlock()
		return internalerrors.ErrOperationNotFound
	}

	if stepIndex < 0 || stepIndex >= len(op.Steps) {
		e.mu.Unlock()
		return errors.Wrapf(internalerrors.ErrInvalidParameter, "step index %d out of range (0-%d)", stepIndex, len(op.Steps)-1)
	}

	// Reset operation state
	op.State = types.StatePaused
	op.PauseReason = "Reset to step for retry"
	op.CurrentStepIndex = stepIndex
	op.CompletedAt = nil
	op.UpdatedAt = time.Now()

	// Reset the target step and all subsequent steps
	for i := stepIndex; i < len(op.Steps); i++ {
		op.Steps[i].State = types.StepStatePending
		op.Steps[i].Result = nil
		op.Steps[i].Error = ""
		op.Steps[i].StartedAt = nil
		op.Steps[i].CompletedAt = nil
		op.Steps[i].RetryCount = 0
		op.Steps[i].WaitCondition = ""
	}

	e.mu.Unlock()

	e.persistOperation(ctx, op)
	e.addEvent(id, "operation_reset", fmt.Sprintf("Operation reset to step %d (%s)", stepIndex, op.Steps[stepIndex].Name), nil)

	return nil
}

// StartOperation starts executing an operation.
func (e *Engine) StartOperation(ctx context.Context, id string) error {
	return e.startOperation(ctx, id, false)
}

// StartOperationForStepFn starts an operation for Step Functions mode.
// Unlike StartOperation, this does NOT spawn a background goroutine to execute steps.
// Instead, the caller (Step Functions) is expected to drive execution by calling
// ExecuteCurrentStep and PollCurrentStep.
func (e *Engine) StartOperationForStepFn(ctx context.Context, id string) error {
	return e.startOperation(ctx, id, true)
}

// startOperation is the internal implementation for starting operations.
// If sfMode is true, the background step execution goroutine is NOT started.
func (e *Engine) startOperation(ctx context.Context, id string, sfMode bool) error {
	e.mu.Lock()
	op, ok := e.operations[id]
	if !ok {
		e.mu.Unlock()
		return internalerrors.ErrOperationNotFound
	}

	if op.State != types.StateCreated && op.State != types.StatePaused {
		e.mu.Unlock()
		return errors.Wrapf(internalerrors.ErrInvalidState, "cannot start from state %s", op.State)
	}

	now := time.Now()
	op.State = types.StateRunning
	op.UpdatedAt = now
	if op.StartedAt == nil {
		op.StartedAt = &now
	}
	e.mu.Unlock()

	e.persistOperation(ctx, op)
	e.addEvent(id, "operation_started", "Operation started", nil)

	if e.notifier != nil {
		e.notifier.NotifyOperationStarted(ctx, op)
	}

	// Execute steps in background with a new context that won't be canceled
	// when the HTTP request completes.
	// In Step Functions mode, skip the background execution - the SF executor
	// will drive step execution via ExecuteCurrentStep/PollCurrentStep calls.
	if !sfMode {
		go e.executeSteps(context.Background(), op)
	}

	return nil
}

// ResumeOperation resumes a paused operation.
func (e *Engine) ResumeOperation(ctx context.Context, id string, response types.InterventionResponse) error {
	e.mu.Lock()
	op, ok := e.operations[id]
	if !ok {
		e.mu.Unlock()
		return internalerrors.ErrOperationNotFound
	}

	if op.State != types.StatePaused {
		e.mu.Unlock()
		return internalerrors.ErrOperationNotPaused
	}

	switch response.Action {
	case "continue":
		op.State = types.StateRunning
		op.PauseReason = ""
		op.UpdatedAt = time.Now()
		e.mu.Unlock()
		e.persistOperation(ctx, op)
		e.addEvent(id, "operation_resumed", "Operation resumed: "+response.Comment, nil)
		go e.executeSteps(context.Background(), op)

	case "rollback":
		op.State = types.StateRollingBack
		op.UpdatedAt = time.Now()
		e.mu.Unlock()
		e.persistOperation(ctx, op)
		e.addEvent(id, "rollback_started", "Rollback initiated: "+response.Comment, nil)
		go e.executeRollback(context.Background(), op)

	case "abort":
		op.State = types.StateFailed
		op.Error = "Aborted by operator: " + response.Comment
		op.UpdatedAt = time.Now()
		now := time.Now()
		op.CompletedAt = &now
		e.mu.Unlock()
		e.persistOperation(ctx, op)
		e.addEvent(id, "operation_aborted", "Operation aborted: "+response.Comment, nil)
		if e.notifier != nil {
			e.notifier.NotifyOperationFailed(ctx, op)
		}

	case "mark_complete":
		// Allow user to manually mark operation as complete despite failures
		// This is useful when cleanup fails but the main operation succeeded
		op.State = types.StateCompleted
		op.PauseReason = ""
		op.UpdatedAt = time.Now()
		now := time.Now()
		op.CompletedAt = &now
		e.mu.Unlock()
		e.persistOperation(ctx, op)
		e.addEvent(id, "operation_marked_complete", "Operation manually marked complete: "+response.Comment, nil)
		if e.notifier != nil {
			e.notifier.NotifyOperationCompleted(ctx, op)
		}

	default:
		e.mu.Unlock()
		return errors.Wrapf(internalerrors.ErrInvalidParameter, "unknown action: %s", response.Action)
	}

	return nil
}

// PauseOperation pauses a running operation.
// Returns an error if the operation is on its last step and that step is in progress,
// since pausing at that point would have no effect.
func (e *Engine) PauseOperation(ctx context.Context, id string, reason string) error {
	e.mu.Lock()
	op, ok := e.operations[id]
	if !ok {
		e.mu.Unlock()
		return internalerrors.ErrOperationNotFound
	}

	if op.State != types.StateRunning {
		e.mu.Unlock()
		return internalerrors.ErrOperationNotRunning
	}

	// Check if we're on the last step and it's in progress
	if op.CurrentStepIndex == len(op.Steps)-1 && len(op.Steps) > 0 {
		currentStep := &op.Steps[op.CurrentStepIndex]
		if currentStep.State == types.StepStateInProgress || currentStep.State == types.StepStateWaiting {
			e.mu.Unlock()
			return errors.Wrapf(internalerrors.ErrInvalidState, "cannot pause during last step execution")
		}
	}

	op.State = types.StatePaused
	op.PauseReason = reason
	op.UpdatedAt = time.Now()
	e.mu.Unlock()

	e.persistOperation(ctx, op)
	e.addEvent(id, "operation_paused", reason, nil)

	if e.notifier != nil {
		go e.notifier.NotifyOperationPaused(ctx, op, reason)
	}

	return nil
}

// getWaitTimeout returns the timeout for wait operations.
// Uses operation-specific timeout if set, otherwise the default.
func (e *Engine) getWaitTimeout(op *types.Operation) time.Duration {
	if op.WaitTimeout > 0 {
		return time.Duration(op.WaitTimeout) * time.Second
	}
	return e.defaultWaitTimeout
}

// getRDSClient returns the RDS client for an operation's region.
func (e *Engine) getRDSClient(ctx context.Context, op *types.Operation) (*rds.Client, error) {
	region := op.Region
	if region == "" {
		region = e.defaultRegion
	}
	return e.clientManager.GetClient(ctx, region)
}

// executeSteps executes the steps of an operation.
func (e *Engine) executeSteps(ctx context.Context, op *types.Operation) {
	for op.CurrentStepIndex < len(op.Steps) {
		e.mu.RLock()
		if op.State != types.StateRunning {
			e.mu.RUnlock()
			return
		}
		step := &op.Steps[op.CurrentStepIndex]

		// Check if we should auto-pause before this step
		if e.shouldAutoPause(op, op.CurrentStepIndex) {
			e.mu.RUnlock()
			e.mu.Lock()
			op.State = types.StatePaused
			op.PauseReason = fmt.Sprintf("Auto-pause before step %d: %s", op.CurrentStepIndex+1, step.Name)
			op.UpdatedAt = time.Now()
			// Remove this step from the auto-pause list since we've now paused
			op.PauseBeforeSteps = removeFromSlice(op.PauseBeforeSteps, op.CurrentStepIndex)
			e.mu.Unlock()
			e.persistOperation(ctx, op)
			e.addEvent(op.ID, "operation_paused", op.PauseReason, nil)
			if e.notifier != nil {
				e.notifier.NotifyOperationPaused(ctx, op, op.PauseReason)
			}
			return
		}
		e.mu.RUnlock()

		// Execute step
		err := e.executeStep(ctx, op, step)

		e.mu.Lock()
		if err != nil {
			if errors.Is(err, internalerrors.ErrInterventionRequired) {
				op.State = types.StatePaused
				op.PauseReason = step.Error
				op.UpdatedAt = time.Now()
				e.mu.Unlock()
				e.persistOperation(ctx, op)
				e.addEvent(op.ID, "intervention_required", step.Error, nil)
				if e.notifier != nil {
					e.notifier.NotifyOperationPaused(ctx, op, step.Error)
				}
				return
			}

			// Check if we can retry
			if step.RetryCount < step.MaxRetries {
				step.RetryCount++
				step.State = types.StepStatePending
				step.Error = ""
				e.mu.Unlock()
				e.persistOperation(ctx, op)
				e.addEvent(op.ID, "step_retry", "Retrying step: "+step.Name, nil)
				time.Sleep(e.defaultPollInterval)
				continue
			}

			// Step failed
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
			return
		}

		// Step completed
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
	}

	// All steps completed
	e.mu.Lock()
	op.State = types.StateCompleted
	now := time.Now()
	op.CompletedAt = &now
	op.UpdatedAt = time.Now()
	e.mu.Unlock()

	e.persistOperation(ctx, op)
	e.addEvent(op.ID, "operation_completed", "Operation completed successfully", nil)
	if e.notifier != nil {
		e.notifier.NotifyOperationCompleted(ctx, op)
	}
}

// executeStep executes a single step.
func (e *Engine) executeStep(ctx context.Context, op *types.Operation, step *types.Step) error {
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

	return handler(ctx, op, step)
}

// executeRollback executes rollback for an operation.
func (e *Engine) executeRollback(ctx context.Context, op *types.Operation) {
	e.logger.Info("executing rollback", slog.String("operation_id", op.ID))

	rdsClient, err := e.getRDSClient(ctx, op)
	if err != nil {
		e.logger.Error("failed to get RDS client for rollback",
			slog.String("operation_id", op.ID),
			slog.String("error", err.Error()))
	} else {
		// For now, we only clean up temp instances
		// More sophisticated rollback could be added per operation type
		for _, step := range op.Steps {
			if step.Action == "create_temp_instance" && step.State == types.StepStateCompleted {
				// Extract instance ID from step result
				var result struct {
					InstanceID string `json:"instance_id"`
				}
				if err := json.Unmarshal(step.Result, &result); err == nil && result.InstanceID != "" {
					e.logger.Info("deleting temp instance", slog.String("instance_id", result.InstanceID))
					if err := rdsClient.DeleteInstance(ctx, result.InstanceID, true); err != nil {
						e.logger.Error("failed to delete temp instance",
							slog.String("instance_id", result.InstanceID),
							slog.String("error", err.Error()))
					}
				}
			}
		}
	}

	e.mu.Lock()
	op.State = types.StateRolledBack
	now := time.Now()
	op.CompletedAt = &now
	op.UpdatedAt = time.Now()
	e.mu.Unlock()

	e.persistOperation(ctx, op)
	e.addEvent(op.ID, "rollback_completed", "Rollback completed", nil)
}

// addEvent adds an event to the operation's event log and persists it.
func (e *Engine) addEvent(operationID, eventType, message string, data json.RawMessage) {
	e.mu.Lock()
	event := e.addEventLocked(operationID, eventType, message, data)
	e.mu.Unlock()

	// Persist event to storage
	if err := e.store.AppendEvent(context.Background(), event); err != nil {
		e.logger.Error("failed to persist event",
			slog.String("operation_id", operationID),
			slog.String("event_type", eventType),
			slog.String("error", err.Error()))
	}
}

func (e *Engine) addEventLocked(operationID, eventType, message string, data json.RawMessage) types.Event {
	event := types.Event{
		ID:          uuid.New().String(),
		OperationID: operationID,
		Type:        eventType,
		Message:     message,
		Data:        data,
		Timestamp:   time.Now(),
	}
	e.events[operationID] = append(e.events[operationID], event)
	return event
}

// persistOperation saves an operation to storage.
func (e *Engine) persistOperation(ctx context.Context, op *types.Operation) {
	if err := e.store.SaveOperation(ctx, op); err != nil {
		e.logger.Error("failed to persist operation",
			slog.String("operation_id", op.ID),
			slog.String("error", err.Error()))
	}
}

// ClientManager returns the RDS client manager.
func (e *Engine) ClientManager() *rds.ClientManager {
	return e.clientManager
}

// DefaultRegion returns the default AWS region.
func (e *Engine) DefaultRegion() string {
	return e.defaultRegion
}

// SetPauseBeforeSteps sets the list of step indices where the operation should auto-pause.
// Can be called when operation is in created, paused, or running state.
// Validates that step indices are valid (0 to len(steps)-1) and not already completed.
func (e *Engine) SetPauseBeforeSteps(ctx context.Context, id string, stepIndices []int) error {
	e.mu.Lock()
	op, ok := e.operations[id]
	if !ok {
		e.mu.Unlock()
		return internalerrors.ErrOperationNotFound
	}

	// Allow modification in created, paused, or running states
	if op.State != types.StateCreated && op.State != types.StatePaused && op.State != types.StateRunning {
		e.mu.Unlock()
		return errors.Wrapf(internalerrors.ErrInvalidState, "cannot set pause steps in state %s", op.State)
	}

	// Validate step indices
	validIndices := make([]int, 0, len(stepIndices))
	for _, idx := range stepIndices {
		if idx < 0 || idx >= len(op.Steps) {
			e.mu.Unlock()
			return errors.Wrapf(internalerrors.ErrInvalidParameter, "step index %d out of range (0-%d)", idx, len(op.Steps)-1)
		}
		// Skip already completed steps
		if op.Steps[idx].State == types.StepStateCompleted {
			continue
		}
		validIndices = append(validIndices, idx)
	}

	op.PauseBeforeSteps = validIndices
	op.UpdatedAt = time.Now()
	e.mu.Unlock()

	e.persistOperation(ctx, op)
	e.addEvent(id, "pause_steps_updated", fmt.Sprintf("Auto-pause set for steps: %v", validIndices), nil)

	return nil
}

// shouldAutoPause checks if the operation should auto-pause before the given step index.
// Must be called with at least a read lock held.
func (e *Engine) shouldAutoPause(op *types.Operation, stepIndex int) bool {
	for _, idx := range op.PauseBeforeSteps {
		if idx == stepIndex {
			return true
		}
	}
	return false
}

// removeFromSlice removes a value from an int slice and returns the new slice.
func removeFromSlice(slice []int, value int) []int {
	result := make([]int, 0, len(slice))
	for _, v := range slice {
		if v != value {
			result = append(result, v)
		}
	}
	return result
}
