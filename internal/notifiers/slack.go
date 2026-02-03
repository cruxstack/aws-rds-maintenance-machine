// Package notifiers provides notification integrations.
package notifiers

import (
	"context"
	"fmt"
	"time"

	"github.com/mpz/devops/tools/rds-maint-machine/internal/types"
	"github.com/slack-go/slack"
)

// SlackNotifier sends notifications to Slack.
type SlackNotifier struct {
	client  *slack.Client
	channel string
	apiURL  string
}

// NewSlackNotifier creates a new Slack notifier.
func NewSlackNotifier(token, channel string) *SlackNotifier {
	return &SlackNotifier{
		client:  slack.New(token),
		channel: channel,
	}
}

// NewSlackNotifierWithAPIURL creates a Slack notifier with a custom API URL (for testing).
func NewSlackNotifierWithAPIURL(token, channel, apiURL string) *SlackNotifier {
	opts := []slack.Option{}
	if apiURL != "" {
		opts = append(opts, slack.OptionAPIURL(apiURL))
	}
	return &SlackNotifier{
		client:  slack.New(token, opts...),
		channel: channel,
		apiURL:  apiURL,
	}
}

// NotifyOperationStarted sends a notification when an operation starts.
func (n *SlackNotifier) NotifyOperationStarted(ctx context.Context, op *types.Operation) error {
	text := fmt.Sprintf(":rocket: *RDS Maintenance Started*\n"+
		"• *Operation*: %s\n"+
		"• *Cluster*: `%s`\n"+
		"• *ID*: `%s`\n"+
		"• *Steps*: %d total",
		operationTypeName(op.Type), op.ClusterID, op.ID, len(op.Steps))

	_, _, err := n.client.PostMessageContext(ctx, n.channel, slack.MsgOptionText(text, false))
	return err
}

// NotifyOperationCompleted sends a notification when an operation completes.
func (n *SlackNotifier) NotifyOperationCompleted(ctx context.Context, op *types.Operation) error {
	duration := "unknown"
	if op.StartedAt != nil && op.CompletedAt != nil {
		duration = op.CompletedAt.Sub(*op.StartedAt).Round(time.Second).String()
	}
	text := fmt.Sprintf(":white_check_mark: *RDS Maintenance Completed*\n"+
		"• *Operation*: %s\n"+
		"• *Cluster*: `%s`\n"+
		"• *ID*: `%s`\n"+
		"• *Duration*: %s",
		operationTypeName(op.Type), op.ClusterID, op.ID, duration)

	_, _, err := n.client.PostMessageContext(ctx, n.channel, slack.MsgOptionText(text, false))
	return err
}

// NotifyOperationFailed sends a notification when an operation fails.
func (n *SlackNotifier) NotifyOperationFailed(ctx context.Context, op *types.Operation) error {
	text := fmt.Sprintf(":x: *RDS Maintenance Failed*\n"+
		"• *Operation*: %s\n"+
		"• *Cluster*: `%s`\n"+
		"• *ID*: `%s`\n"+
		"• *Error*: %s",
		operationTypeName(op.Type), op.ClusterID, op.ID, op.Error)

	_, _, err := n.client.PostMessageContext(ctx, n.channel, slack.MsgOptionText(text, false))
	return err
}

// NotifyOperationPaused sends a notification when an operation is paused.
func (n *SlackNotifier) NotifyOperationPaused(ctx context.Context, op *types.Operation, reason string) error {
	text := fmt.Sprintf(":warning: *RDS Maintenance Paused - Intervention Required*\n"+
		"• *Operation*: %s\n"+
		"• *Cluster*: `%s`\n"+
		"• *ID*: `%s`\n"+
		"• *Reason*: %s\n"+
		"• *Current Step*: %d/%d (%s)\n\n"+
		"Use the UI or API to continue, rollback, or abort.",
		operationTypeName(op.Type), op.ClusterID, op.ID, reason,
		op.CurrentStepIndex+1, len(op.Steps), getCurrentStepName(op))

	_, _, err := n.client.PostMessageContext(ctx, n.channel, slack.MsgOptionText(text, false))
	return err
}

// NotifyStepCompleted sends a notification when a step completes.
func (n *SlackNotifier) NotifyStepCompleted(ctx context.Context, op *types.Operation, step *types.Step) error {
	// Only notify for significant steps to reduce noise
	if !isSignificantStep(step.Action) {
		return nil
	}

	text := fmt.Sprintf(":heavy_check_mark: *Step Completed*\n"+
		"• *Cluster*: `%s`\n"+
		"• *Step*: %s\n"+
		"• *Progress*: %d/%d",
		op.ClusterID, step.Name, op.CurrentStepIndex, len(op.Steps))

	_, _, err := n.client.PostMessageContext(ctx, n.channel, slack.MsgOptionText(text, false))
	return err
}

func operationTypeName(t types.OperationType) string {
	switch t {
	case types.OperationTypeInstanceTypeChange:
		return "Instance Type Change"
	case types.OperationTypeStorageTypeChange:
		return "Storage Type Change"
	case types.OperationTypeEngineUpgrade:
		return "Engine Upgrade"
	case types.OperationTypeInstanceCycle:
		return "Instance Cycle"
	default:
		return string(t)
	}
}

func getCurrentStepName(op *types.Operation) string {
	if op.CurrentStepIndex < len(op.Steps) {
		return op.Steps[op.CurrentStepIndex].Name
	}
	return "unknown"
}

func isSignificantStep(action string) bool {
	significantActions := map[string]bool{
		"create_temp_instance": true,
		"failover_to_instance": true,
		"create_snapshot":      true,
		"modify_cluster":       true,
		"delete_instance":      true,
	}
	return significantActions[action]
}

// NullNotifier is a no-op notifier for testing.
type NullNotifier struct{}

func (n *NullNotifier) NotifyOperationStarted(ctx context.Context, op *types.Operation) error {
	return nil
}

func (n *NullNotifier) NotifyOperationCompleted(ctx context.Context, op *types.Operation) error {
	return nil
}

func (n *NullNotifier) NotifyOperationFailed(ctx context.Context, op *types.Operation) error {
	return nil
}

func (n *NullNotifier) NotifyOperationPaused(ctx context.Context, op *types.Operation, reason string) error {
	return nil
}

func (n *NullNotifier) NotifyStepCompleted(ctx context.Context, op *types.Operation, step *types.Step) error {
	return nil
}
