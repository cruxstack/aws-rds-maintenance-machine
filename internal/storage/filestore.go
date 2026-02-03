package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/cockroachdb/errors"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/types"
)

// FileStore implements Store using the filesystem.
// Structure:
//
//	{dataDir}/
//	└── operations/
//	    └── {operation-id}/
//	        ├── operation.json           # Current operation state
//	        └── events/
//	            ├── 0001-{timestamp}-{type}.json
//	            └── ...
type FileStore struct {
	dataDir       string
	mu            sync.RWMutex
	eventCounters map[string]*atomic.Int64 // operation ID -> event counter
}

// NewFileStore creates a new file-based store.
func NewFileStore(dataDir string) (*FileStore, error) {
	// Create base directories
	opsDir := filepath.Join(dataDir, "operations")
	if err := os.MkdirAll(opsDir, 0755); err != nil {
		return nil, errors.Wrap(err, "create operations directory")
	}

	return &FileStore{
		dataDir:       dataDir,
		eventCounters: make(map[string]*atomic.Int64),
	}, nil
}

// operationDir returns the directory for an operation.
func (s *FileStore) operationDir(operationID string) string {
	return filepath.Join(s.dataDir, "operations", operationID)
}

// operationFile returns the path to an operation's state file.
func (s *FileStore) operationFile(operationID string) string {
	return filepath.Join(s.operationDir(operationID), "operation.json")
}

// eventsDir returns the directory for an operation's events.
func (s *FileStore) eventsDir(operationID string) string {
	return filepath.Join(s.operationDir(operationID), "events")
}

// SaveOperation persists the current state of an operation.
func (s *FileStore) SaveOperation(ctx context.Context, op *types.Operation) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure operation directory exists
	opDir := s.operationDir(op.ID)
	if err := os.MkdirAll(opDir, 0755); err != nil {
		return errors.Wrap(err, "create operation directory")
	}

	// Ensure events directory exists
	eventsDir := s.eventsDir(op.ID)
	if err := os.MkdirAll(eventsDir, 0755); err != nil {
		return errors.Wrap(err, "create events directory")
	}

	// Marshal operation to JSON
	data, err := json.MarshalIndent(op, "", "  ")
	if err != nil {
		return errors.Wrap(err, "marshal operation")
	}

	// Atomic write with fsync for durability
	opFile := s.operationFile(op.ID)
	if err := atomicWriteFile(opFile, data, 0644); err != nil {
		return errors.Wrap(err, "write operation file")
	}

	return nil
}

// GetOperation retrieves an operation by ID.
func (s *FileStore) GetOperation(ctx context.Context, id string) (*types.Operation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	opFile := s.operationFile(id)
	data, err := os.ReadFile(opFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "read operation file")
	}

	var op types.Operation
	if err := json.Unmarshal(data, &op); err != nil {
		return nil, errors.Wrap(err, "unmarshal operation")
	}

	return &op, nil
}

// CorruptedFile represents a file that could not be loaded due to corruption.
type CorruptedFile struct {
	Path  string
	Error error
}

// ListOperations returns all operations.
func (s *FileStore) ListOperations(ctx context.Context) ([]*types.Operation, error) {
	ops, _, err := s.listOperationsWithCorrupted(ctx)
	return ops, err
}

// listOperationsWithCorrupted returns all operations and a list of corrupted operation files.
func (s *FileStore) listOperationsWithCorrupted(ctx context.Context) ([]*types.Operation, []CorruptedFile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	opsDir := filepath.Join(s.dataDir, "operations")
	entries, err := os.ReadDir(opsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, errors.Wrap(err, "read operations directory")
	}

	var operations []*types.Operation
	var corrupted []CorruptedFile

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		opFile := filepath.Join(opsDir, entry.Name(), "operation.json")
		data, err := os.ReadFile(opFile)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			corrupted = append(corrupted, CorruptedFile{Path: opFile, Error: err})
			continue
		}

		var op types.Operation
		if err := json.Unmarshal(data, &op); err != nil {
			corrupted = append(corrupted, CorruptedFile{Path: opFile, Error: err})
			continue
		}
		// Validate operation data integrity
		if err := op.Validate(); err != nil {
			corrupted = append(corrupted, CorruptedFile{Path: opFile, Error: errors.Wrap(err, "validation failed")})
			continue
		}
		operations = append(operations, &op)
	}

	return operations, corrupted, nil
}

// DeleteOperation removes an operation and all its events.
func (s *FileStore) DeleteOperation(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	opDir := s.operationDir(id)
	if err := os.RemoveAll(opDir); err != nil && !os.IsNotExist(err) {
		return errors.Wrap(err, "remove operation directory")
	}

	delete(s.eventCounters, id)
	return nil
}

// getEventCounter returns the event counter for an operation, initializing if needed.
func (s *FileStore) getEventCounter(operationID string) *atomic.Int64 {
	counter, ok := s.eventCounters[operationID]
	if !ok {
		counter = &atomic.Int64{}
		s.eventCounters[operationID] = counter
	}
	return counter
}

// AppendEvent adds an event to an operation's event log.
func (s *FileStore) AppendEvent(ctx context.Context, event types.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	eventsDir := s.eventsDir(event.OperationID)
	if err := os.MkdirAll(eventsDir, 0755); err != nil {
		return errors.Wrap(err, "create events directory")
	}

	// Get next sequence number
	counter := s.getEventCounter(event.OperationID)
	seq := counter.Add(1)

	// Format: NNNN-timestamp-type.json
	// Using sequence ensures ordering even if timestamps collide
	filename := fmt.Sprintf("%04d-%s-%s.json",
		seq,
		event.Timestamp.Format("20060102T150405"),
		sanitizeFilename(event.Type),
	)

	data, err := json.MarshalIndent(event, "", "  ")
	if err != nil {
		return errors.Wrap(err, "marshal event")
	}

	// Atomic write with fsync for durability
	eventFile := filepath.Join(eventsDir, filename)
	if err := atomicWriteFile(eventFile, data, 0644); err != nil {
		return errors.Wrap(err, "write event file")
	}

	return nil
}

// GetEvents retrieves all events for an operation, ordered by sequence.
func (s *FileStore) GetEvents(ctx context.Context, operationID string) ([]types.Event, error) {
	events, _, err := s.getEventsWithCorrupted(ctx, operationID)
	return events, err
}

// getEventsWithCorrupted retrieves all events and returns a list of corrupted event files.
func (s *FileStore) getEventsWithCorrupted(ctx context.Context, operationID string) ([]types.Event, []CorruptedFile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	eventsDir := s.eventsDir(operationID)
	entries, err := os.ReadDir(eventsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, errors.Wrap(err, "read events directory")
	}

	// Sort entries by name (which starts with sequence number)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	var events []types.Event
	var corrupted []CorruptedFile

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		eventFile := filepath.Join(eventsDir, entry.Name())
		data, err := os.ReadFile(eventFile)
		if err != nil {
			corrupted = append(corrupted, CorruptedFile{Path: eventFile, Error: err})
			continue
		}

		var event types.Event
		if err := json.Unmarshal(data, &event); err != nil {
			corrupted = append(corrupted, CorruptedFile{Path: eventFile, Error: err})
			continue
		}
		// Validate event data integrity
		if err := event.Validate(); err != nil {
			corrupted = append(corrupted, CorruptedFile{Path: eventFile, Error: errors.Wrap(err, "validation failed")})
			continue
		}
		events = append(events, event)
	}

	return events, corrupted, nil
}

// LoadAll loads all operations and events from storage.
// It tolerates corrupted files by skipping them and logging warnings.
// This ensures the system can recover even with some data corruption.
func (s *FileStore) LoadAll(ctx context.Context) (map[string]*types.Operation, map[string][]types.Event, error) {
	// First, clean up any orphaned temp files from previous crashes
	if err := s.cleanupTempFiles(); err != nil {
		slog.Warn("failed to cleanup temp files", "error", err)
	}

	operations := make(map[string]*types.Operation)
	events := make(map[string][]types.Event)

	ops, corruptedOps, err := s.listOperationsWithCorrupted(ctx)
	if err != nil {
		return nil, nil, errors.Wrap(err, "list operations")
	}

	// Log warnings for corrupted operations
	for _, cf := range corruptedOps {
		slog.Error("skipping corrupted operation file",
			"path", cf.Path,
			"error", cf.Error)
	}

	for _, op := range ops {
		operations[op.ID] = op

		opEvents, corruptedEvents, err := s.getEventsWithCorrupted(ctx, op.ID)
		if err != nil {
			slog.Error("failed to load events for operation, skipping",
				"operation_id", op.ID,
				"error", err)
			continue
		}

		// Log warnings for corrupted events
		for _, cf := range corruptedEvents {
			slog.Warn("skipping corrupted event file",
				"operation_id", op.ID,
				"path", cf.Path,
				"error", cf.Error)
		}

		events[op.ID] = opEvents

		// Initialize event counter based on loaded events
		// Use total file count (including corrupted) to avoid sequence collisions
		s.mu.Lock()
		counter := s.getEventCounter(op.ID)
		totalEvents := len(opEvents) + len(corruptedEvents)
		counter.Store(int64(totalEvents))
		s.mu.Unlock()
	}

	// Summary log if there were corrupted files
	if len(corruptedOps) > 0 {
		slog.Warn("some operation files were corrupted and skipped",
			"count", len(corruptedOps))
	}

	return operations, events, nil
}

// sanitizeFilename removes characters that are problematic in filenames.
func sanitizeFilename(s string) string {
	// Replace problematic characters with underscore
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
		" ", "_",
	)
	return replacer.Replace(s)
}

// atomicWriteFile writes data to a file atomically using write-to-temp-then-rename pattern.
// It also fsyncs the file to ensure durability.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	// Write to temp file in same directory (ensures same filesystem for atomic rename)
	dir := filepath.Dir(path)
	tmpFile, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return errors.Wrap(err, "create temp file")
	}
	tmpPath := tmpFile.Name()

	// Clean up temp file on any error
	success := false
	defer func() {
		if !success {
			tmpFile.Close()
			os.Remove(tmpPath)
		}
	}()

	// Write data
	if _, err := tmpFile.Write(data); err != nil {
		return errors.Wrap(err, "write to temp file")
	}

	// Fsync to ensure data is on disk before rename
	if err := tmpFile.Sync(); err != nil {
		return errors.Wrap(err, "sync temp file")
	}

	// Close before rename (required on Windows)
	if err := tmpFile.Close(); err != nil {
		return errors.Wrap(err, "close temp file")
	}

	// Set permissions
	if err := os.Chmod(tmpPath, perm); err != nil {
		return errors.Wrap(err, "chmod temp file")
	}

	// Atomic rename
	if err := os.Rename(tmpPath, path); err != nil {
		return errors.Wrap(err, "rename temp file")
	}

	success = true
	return nil
}

// cleanupTempFiles removes orphaned .tmp files in the data directory.
// These can be left behind after crashes during writes.
func (s *FileStore) cleanupTempFiles() error {
	opsDir := filepath.Join(s.dataDir, "operations")

	return filepath.Walk(opsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip inaccessible paths
		}
		if info.IsDir() {
			return nil
		}
		// Remove any .tmp files or files starting with .tmp-
		name := info.Name()
		if strings.HasSuffix(name, ".tmp") || strings.HasPrefix(name, ".tmp-") {
			slog.Warn("removing orphaned temp file", "path", path)
			os.Remove(path)
		}
		return nil
	})
}
