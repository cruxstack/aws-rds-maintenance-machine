// Package errors defines error types for the RDS maintenance state machine.
package errors

import "errors"

var (
	// ErrOperationNotFound indicates the requested operation does not exist.
	ErrOperationNotFound = errors.New("operation not found")
	// ErrOperationAlreadyRunning indicates an operation is already in progress.
	ErrOperationAlreadyRunning = errors.New("operation already running")
	// ErrOperationNotPaused indicates the operation is not paused.
	ErrOperationNotPaused = errors.New("operation is not paused")
	// ErrOperationNotRunning indicates the operation is not running.
	ErrOperationNotRunning = errors.New("operation is not running")
	// ErrInvalidState indicates an invalid state transition was attempted.
	ErrInvalidState = errors.New("invalid state transition")
	// ErrStepFailed indicates a step in the operation failed.
	ErrStepFailed = errors.New("step failed")
	// ErrClusterNotFound indicates the RDS cluster was not found.
	ErrClusterNotFound = errors.New("cluster not found")
	// ErrInstanceNotFound indicates an RDS instance was not found.
	ErrInstanceNotFound = errors.New("instance not found")
	// ErrInvalidParameter indicates an invalid parameter was provided.
	ErrInvalidParameter = errors.New("invalid parameter")
	// ErrWaitTimeout indicates a wait condition timed out.
	ErrWaitTimeout = errors.New("wait timeout")
	// ErrInterventionRequired indicates human intervention is required.
	ErrInterventionRequired = errors.New("intervention required")
	// ErrRollbackFailed indicates the rollback operation failed.
	ErrRollbackFailed = errors.New("rollback failed")
	// ErrNotFound is a generic not found error.
	ErrNotFound = errors.New("not found")
	// ErrBlueGreenDeploymentNotFound indicates the Blue-Green deployment was not found.
	ErrBlueGreenDeploymentNotFound = errors.New("blue-green deployment not found")
	// ErrCannotDelete indicates the resource cannot be deleted in its current state.
	ErrCannotDelete = errors.New("cannot delete")
)

// IsNotFound returns true if the error is any kind of "not found" error.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound) ||
		errors.Is(err, ErrOperationNotFound) ||
		errors.Is(err, ErrClusterNotFound) ||
		errors.Is(err, ErrInstanceNotFound) ||
		errors.Is(err, ErrBlueGreenDeploymentNotFound)
}

// IsCannotDelete returns true if the error indicates a resource cannot be deleted.
func IsCannotDelete(err error) bool {
	return errors.Is(err, ErrCannotDelete) || errors.Is(err, ErrInvalidState)
}
