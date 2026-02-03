// Package storage provides persistence for operations and events.
package storage

import (
	"context"

	"github.com/mpz/devops/tools/rds-maint-machine/internal/types"
)

// Store is the interface for persisting operations and events.
// This interface is designed to be compatible with both file-based storage
// and DynamoDB, using an append-only pattern for events.
type Store interface {
	// SaveOperation persists the current state of an operation.
	// This overwrites the previous state (like a DynamoDB PutItem).
	SaveOperation(ctx context.Context, op *types.Operation) error

	// GetOperation retrieves an operation by ID.
	GetOperation(ctx context.Context, id string) (*types.Operation, error)

	// ListOperations returns all operations.
	ListOperations(ctx context.Context) ([]*types.Operation, error)

	// DeleteOperation removes an operation and all its events.
	DeleteOperation(ctx context.Context, id string) error

	// AppendEvent adds an event to an operation's event log.
	// Events are immutable once written (append-only pattern).
	AppendEvent(ctx context.Context, event types.Event) error

	// GetEvents retrieves all events for an operation, ordered by sequence.
	GetEvents(ctx context.Context, operationID string) ([]types.Event, error)

	// LoadAll loads all operations and events from storage.
	// Used for recovery on startup.
	LoadAll(ctx context.Context) (map[string]*types.Operation, map[string][]types.Event, error)
}

// NullStore is a no-op store implementation for when persistence is disabled.
type NullStore struct{}

func (s *NullStore) SaveOperation(ctx context.Context, op *types.Operation) error {
	return nil
}

func (s *NullStore) GetOperation(ctx context.Context, id string) (*types.Operation, error) {
	return nil, nil
}

func (s *NullStore) ListOperations(ctx context.Context) ([]*types.Operation, error) {
	return nil, nil
}

func (s *NullStore) DeleteOperation(ctx context.Context, id string) error {
	return nil
}

func (s *NullStore) AppendEvent(ctx context.Context, event types.Event) error {
	return nil
}

func (s *NullStore) GetEvents(ctx context.Context, operationID string) ([]types.Event, error) {
	return nil, nil
}

func (s *NullStore) LoadAll(ctx context.Context) (map[string]*types.Operation, map[string][]types.Event, error) {
	return make(map[string]*types.Operation), make(map[string][]types.Event), nil
}
