// Package jsonutil provides JSON encoding/decoding utilities with proper error handling.
package jsonutil

import (
	"encoding/json"
	"fmt"
	"log/slog"
)

// MustMarshal marshals the given value to JSON, logging any errors.
// This is useful for cases where marshaling is expected to succeed
// (e.g., marshaling well-known struct types) but we want to be aware
// if it ever fails.
//
// Returns nil if marshaling fails (after logging the error).
func MustMarshal(v any) []byte {
	result, err := json.Marshal(v)
	if err != nil {
		slog.Error("json marshal failed", slog.String("error", err.Error()), slog.Any("value_type", typeName(v)))
		return nil
	}
	return result
}

// MustMarshalWithLogger marshals the given value to JSON using the provided logger.
// Returns nil if marshaling fails (after logging the error).
func MustMarshalWithLogger(logger *slog.Logger, v any) []byte {
	result, err := json.Marshal(v)
	if err != nil {
		if logger != nil {
			logger.Error("json marshal failed", slog.String("error", err.Error()), slog.Any("value_type", typeName(v)))
		}
		return nil
	}
	return result
}

// MarshalOrEmpty marshals the given value to JSON, returning an empty JSON object "{}""
// if marshaling fails. This is useful when you need a valid JSON value regardless.
func MarshalOrEmpty(v any) []byte {
	result, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return result
}

// typeName returns a string representation of the value's type for logging.
func typeName(v any) string {
	if v == nil {
		return "nil"
	}
	return fmt.Sprintf("%T", v)
}
