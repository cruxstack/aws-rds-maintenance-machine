// Package main provides the AWS Lambda entry point for the RDS maintenance machine.
// This is designed to work with AWS Step Functions for orchestration.
package main

import (
	"context"
	"encoding/json"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/app"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/config"
)

var appInst *app.App

func init() {
	cfg, err := config.NewConfig()
	if err != nil {
		panic("config init failed: " + err.Error())
	}

	appInst, err = app.New(context.Background(), cfg)
	if err != nil {
		panic("app init failed: " + err.Error())
	}
}

// StepFunctionEvent represents an event from AWS Step Functions.
type StepFunctionEvent struct {
	// Action is the operation to perform (create, start, status, resume)
	Action string `json:"action"`
	// OperationID is the ID of an existing operation (for status, resume)
	OperationID string `json:"operation_id,omitempty"`
	// Params contains operation-specific parameters
	Params json.RawMessage `json:"params,omitempty"`
}

// StepFunctionResponse is the response back to Step Functions.
// This structure is designed for Step Functions to determine the next action.
type StepFunctionResponse struct {
	// Status indicates success, error, waiting, paused, or completed
	Status string `json:"status"`
	// OperationID is the ID of the operation
	OperationID string `json:"operation_id,omitempty"`
	// OperationState is the current operation state (created, running, paused, completed, failed, etc.)
	OperationState string `json:"operation_state,omitempty"`

	// Step execution details (populated by step/poll actions)
	StepIndex     int    `json:"step_index,omitempty"`
	StepName      string `json:"step_name,omitempty"`
	StepAction    string `json:"step_action,omitempty"`
	StepState     string `json:"step_state,omitempty"`
	WaitCondition string `json:"wait_condition,omitempty"`
	PauseReason   string `json:"pause_reason,omitempty"`

	// Flow control flags for Step Functions
	Continue          bool `json:"continue"`           // More steps to execute
	NeedsWait         bool `json:"needs_wait"`         // Current step is waiting for a condition
	NeedsIntervention bool `json:"needs_intervention"` // Human intervention required
	Completed         bool `json:"completed"`          // Operation completed successfully
	Failed            bool `json:"failed"`             // Operation failed

	// Error contains any error message
	Error string `json:"error,omitempty"`
	// Data contains additional response data
	Data json.RawMessage `json:"data,omitempty"`
}

func handler(ctx context.Context, event StepFunctionEvent) (StepFunctionResponse, error) {
	input, _ := json.Marshal(event)

	req := app.Request{
		Type:  app.RequestTypeStepFunction,
		Input: input,
	}

	resp := appInst.HandleRequest(ctx, req)

	// Parse response
	var response StepFunctionResponse
	if resp.StatusCode >= 400 {
		var errResp map[string]string
		json.Unmarshal(resp.Body, &errResp)
		response.Status = "error"
		response.Error = errResp["error"]
	} else {
		response.Status = "success"
		response.Data = resp.Body

		// Try to extract operation info (for create/status/update actions)
		var opInfo struct {
			ID    string `json:"id"`
			State string `json:"state"`
		}
		if err := json.Unmarshal(resp.Body, &opInfo); err == nil && opInfo.ID != "" {
			response.OperationID = opInfo.ID
			response.OperationState = opInfo.State
		}

		// Try to extract step execution result (for step/poll actions)
		var stepResult struct {
			OperationID       string `json:"operation_id"`
			OperationState    string `json:"operation_state"`
			StepIndex         int    `json:"step_index"`
			StepName          string `json:"step_name"`
			StepAction        string `json:"step_action"`
			StepState         string `json:"step_state"`
			WaitCondition     string `json:"wait_condition"`
			PauseReason       string `json:"pause_reason"`
			Continue          bool   `json:"continue"`
			NeedsWait         bool   `json:"needs_wait"`
			NeedsIntervention bool   `json:"needs_intervention"`
			Completed         bool   `json:"completed"`
			Failed            bool   `json:"failed"`
			Error             string `json:"error"`
		}
		if err := json.Unmarshal(resp.Body, &stepResult); err == nil && stepResult.OperationID != "" {
			response.OperationID = stepResult.OperationID
			response.OperationState = stepResult.OperationState
			response.StepIndex = stepResult.StepIndex
			response.StepName = stepResult.StepName
			response.StepAction = stepResult.StepAction
			response.StepState = stepResult.StepState
			response.WaitCondition = stepResult.WaitCondition
			response.PauseReason = stepResult.PauseReason
			response.Continue = stepResult.Continue
			response.NeedsWait = stepResult.NeedsWait
			response.NeedsIntervention = stepResult.NeedsIntervention
			response.Completed = stepResult.Completed
			response.Failed = stepResult.Failed
			if stepResult.Error != "" {
				response.Error = stepResult.Error
			}

			// Update status based on step result
			if stepResult.Completed {
				response.Status = "completed"
			} else if stepResult.Failed {
				response.Status = "failed"
			} else if stepResult.NeedsIntervention {
				response.Status = "paused"
			} else if stepResult.NeedsWait {
				response.Status = "waiting"
			}
		}

		// Try to extract poll result (for poll action)
		var pollResult struct {
			OperationID       string `json:"operation_id"`
			OperationState    string `json:"operation_state"`
			StepIndex         int    `json:"step_index"`
			StepName          string `json:"step_name"`
			StepAction        string `json:"step_action"`
			StepState         string `json:"step_state"`
			WaitCondition     string `json:"wait_condition"`
			Ready             bool   `json:"ready"`
			Continue          bool   `json:"continue"`
			NeedsIntervention bool   `json:"needs_intervention"`
			Error             string `json:"error"`
		}
		if err := json.Unmarshal(resp.Body, &pollResult); err == nil && pollResult.OperationID != "" && pollResult.StepName != "" {
			response.OperationID = pollResult.OperationID
			response.OperationState = pollResult.OperationState
			response.StepIndex = pollResult.StepIndex
			response.StepName = pollResult.StepName
			response.StepAction = pollResult.StepAction
			response.StepState = pollResult.StepState
			response.WaitCondition = pollResult.WaitCondition
			response.Continue = pollResult.Continue
			response.NeedsIntervention = pollResult.NeedsIntervention
			if pollResult.Error != "" {
				response.Error = pollResult.Error
			}

			// For poll results, "ready" means the wait is complete
			if pollResult.Ready {
				response.Status = "ready"
				response.NeedsWait = false
			} else if pollResult.NeedsIntervention {
				response.Status = "paused"
			} else {
				response.Status = "waiting"
				response.NeedsWait = true
			}
		}
	}

	return response, nil
}

func main() {
	lambda.Start(handler)
}
