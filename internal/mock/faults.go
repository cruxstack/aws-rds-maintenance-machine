package mock

import (
	"fmt"
	"math/rand"
	"sync"

	"github.com/google/uuid"
)

// FaultType identifies the type of fault to inject.
type FaultType string

const (
	// FaultTypeAPIError returns an error for a specific API action.
	FaultTypeAPIError FaultType = "api_error"
	// FaultTypeDelay adds extra delay to an action.
	FaultTypeDelay FaultType = "delay"
	// FaultTypeStuck makes an instance stay in a transitional state forever.
	FaultTypeStuck FaultType = "stuck"
	// FaultTypePartialFail fails after N successful calls.
	FaultTypePartialFail FaultType = "partial_fail"
)

// Fault represents a fault injection rule.
type Fault struct {
	ID          string    `json:"id"`
	Type        FaultType `json:"type"`
	Action      string    `json:"action"`        // RDS action to target (e.g., "CreateDBInstance")
	Target      string    `json:"target"`        // Optional: specific resource ID to target
	Probability float64   `json:"probability"`   // 0.0-1.0, chance the fault triggers
	ErrorCode   string    `json:"error_code"`    // For api_error type
	ErrorMsg    string    `json:"error_message"` // For api_error type
	DelayMs     int       `json:"delay_ms"`      // For delay type
	FailAfterN  int       `json:"fail_after_n"`  // For partial_fail type
	Enabled     bool      `json:"enabled"`

	// Internal counter for partial_fail
	callCount int
}

// FaultInjector manages fault injection rules.
type FaultInjector struct {
	mu     sync.RWMutex
	faults map[string]*Fault
}

// NewFaultInjector creates a new fault injector.
func NewFaultInjector() *FaultInjector {
	return &FaultInjector{
		faults: make(map[string]*Fault),
	}
}

// AddFault adds a new fault rule.
func (fi *FaultInjector) AddFault(f Fault) string {
	fi.mu.Lock()
	defer fi.mu.Unlock()

	if f.ID == "" {
		f.ID = uuid.New().String()[:8]
	}
	f.callCount = 0
	fi.faults[f.ID] = &f
	return f.ID
}

// RemoveFault removes a fault by ID.
func (fi *FaultInjector) RemoveFault(id string) bool {
	fi.mu.Lock()
	defer fi.mu.Unlock()

	if _, ok := fi.faults[id]; ok {
		delete(fi.faults, id)
		return true
	}
	return false
}

// EnableFault enables or disables a fault.
func (fi *FaultInjector) EnableFault(id string, enabled bool) bool {
	fi.mu.Lock()
	defer fi.mu.Unlock()

	if f, ok := fi.faults[id]; ok {
		f.Enabled = enabled
		return true
	}
	return false
}

// ListFaults returns all faults.
func (fi *FaultInjector) ListFaults() []Fault {
	fi.mu.RLock()
	defer fi.mu.RUnlock()

	result := make([]Fault, 0, len(fi.faults))
	for _, f := range fi.faults {
		result = append(result, *f)
	}
	return result
}

// ClearAll removes all faults.
func (fi *FaultInjector) ClearAll() {
	fi.mu.Lock()
	defer fi.mu.Unlock()
	fi.faults = make(map[string]*Fault)
}

// FaultCheckResult contains the result of checking for a fault.
type FaultCheckResult struct {
	ShouldFail  bool
	ErrorCode   string
	ErrorMsg    string
	ExtraDelay  int  // milliseconds
	ShouldStick bool // for stuck type - don't transition state
}

// Check checks if a fault should be triggered for the given action and target.
func (fi *FaultInjector) Check(action, target string) FaultCheckResult {
	fi.mu.Lock()
	defer fi.mu.Unlock()

	result := FaultCheckResult{}

	for _, f := range fi.faults {
		if !f.Enabled {
			continue
		}

		// Check if this fault matches
		if f.Action != "" && f.Action != action {
			continue
		}
		if f.Target != "" && f.Target != target {
			continue
		}

		// Check probability
		if f.Probability < 1.0 && rand.Float64() > f.Probability {
			continue
		}

		// Apply the fault
		switch f.Type {
		case FaultTypeAPIError:
			result.ShouldFail = true
			result.ErrorCode = f.ErrorCode
			if result.ErrorCode == "" {
				result.ErrorCode = "InternalFailure"
			}
			result.ErrorMsg = f.ErrorMsg
			if result.ErrorMsg == "" {
				result.ErrorMsg = fmt.Sprintf("Injected fault for action %s", action)
			}

		case FaultTypeDelay:
			result.ExtraDelay += f.DelayMs

		case FaultTypeStuck:
			result.ShouldStick = true

		case FaultTypePartialFail:
			f.callCount++
			if f.callCount > f.FailAfterN {
				result.ShouldFail = true
				result.ErrorCode = f.ErrorCode
				if result.ErrorCode == "" {
					result.ErrorCode = "InternalFailure"
				}
				result.ErrorMsg = f.ErrorMsg
				if result.ErrorMsg == "" {
					result.ErrorMsg = fmt.Sprintf("Partial fail triggered after %d calls", f.FailAfterN)
				}
			}
		}
	}

	return result
}

// CheckStateTransition checks if a state transition should be blocked (for stuck faults).
func (fi *FaultInjector) CheckStateTransition(resourceID string) bool {
	fi.mu.RLock()
	defer fi.mu.RUnlock()

	for _, f := range fi.faults {
		if !f.Enabled {
			continue
		}
		if f.Type != FaultTypeStuck {
			continue
		}
		if f.Target != "" && f.Target != resourceID {
			continue
		}
		// Check probability
		if f.Probability < 1.0 && rand.Float64() > f.Probability {
			continue
		}
		return true // Block the transition
	}
	return false
}
