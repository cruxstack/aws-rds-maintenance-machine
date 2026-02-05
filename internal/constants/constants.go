// Package constants provides shared constant values used throughout the application.
package constants

import "time"

// Default timeouts and intervals
const (
	// DefaultWaitTimeout is the default timeout for wait operations (45 minutes).
	DefaultWaitTimeout = 45 * time.Minute

	// DefaultPollInterval is the default interval for polling operations (30 seconds).
	DefaultPollInterval = 30 * time.Second

	// DefaultWaitTimeoutSeconds is the default timeout in seconds (2700 = 45 minutes).
	DefaultWaitTimeoutSeconds = 2700

	// DefaultPollIntervalSeconds is the default poll interval in seconds.
	DefaultPollIntervalSeconds = 30
)

// Default region
const (
	// DefaultAWSRegion is the default AWS region when not specified.
	DefaultAWSRegion = "us-east-1"
)

// HTTP server defaults
const (
	// DefaultHTTPPort is the default HTTP server port.
	DefaultHTTPPort = "8080"

	// DefaultReadTimeout is the default HTTP read timeout.
	DefaultReadTimeout = 15 * time.Second

	// DefaultWriteTimeout is the default HTTP write timeout (longer for SSE).
	DefaultWriteTimeout = 120 * time.Second

	// DefaultIdleTimeout is the default HTTP idle timeout.
	DefaultIdleTimeout = 60 * time.Second

	// DefaultShutdownTimeout is the default graceful shutdown timeout.
	DefaultShutdownTimeout = 30 * time.Second

	// DemoShutdownTimeout is the shutdown timeout for demo mode.
	DemoShutdownTimeout = 5 * time.Second
)

// Cache durations
const (
	// StaticFileCacheDuration is the cache duration for CSS/JS files (1 hour).
	StaticFileCacheDuration = 3600

	// FaviconCacheDuration is the cache duration for favicon (24 hours).
	FaviconCacheDuration = 86400
)

// RDS instance roles
const (
	// RoleWriter is the writer/primary instance role.
	RoleWriter = "writer"

	// RoleReader is the reader/replica instance role.
	RoleReader = "reader"
)

// AWS tag keys used by the application
const (
	// TagApplicationAutoscaling is the tag key for autoscaling-managed instances.
	TagApplicationAutoscaling = "application-autoscaling:resourceId"

	// TagCreatedBy is the tag key indicating who created a resource.
	TagCreatedBy = "created-by"

	// TagCreatedByValue is the value for the created-by tag.
	TagCreatedByValue = "rds-maint-machine"

	// TagOperationID is the tag key for the operation ID.
	TagOperationID = "rds-maint-operation-id"

	// TagPreUpgradeSnapshot is the tag value indicating a pre-upgrade snapshot.
	TagPreUpgradeSnapshot = "pre-upgrade-snapshot"
)

// Blue-Green deployment statuses
const (
	// BGStatusProvisioning indicates the deployment is being provisioned.
	BGStatusProvisioning = "PROVISIONING"

	// BGStatusAvailable indicates the deployment is available.
	BGStatusAvailable = "AVAILABLE"

	// BGStatusSwitchoverInProgress indicates switchover is in progress.
	BGStatusSwitchoverInProgress = "SWITCHOVER_IN_PROGRESS"

	// BGStatusSwitchoverCompleted indicates switchover completed successfully.
	BGStatusSwitchoverCompleted = "SWITCHOVER_COMPLETED"

	// BGStatusSwitchoverFailed indicates switchover failed.
	BGStatusSwitchoverFailed = "SWITCHOVER_FAILED"

	// BGStatusDeleting indicates the deployment is being deleted.
	BGStatusDeleting = "DELETING"

	// BGStatusInvalidConfiguration indicates invalid configuration.
	BGStatusInvalidConfiguration = "INVALID_CONFIGURATION"

	// BGStatusProvisioningFailed indicates provisioning failed.
	BGStatusProvisioningFailed = "PROVISIONING_FAILED"
)

// Blue-Green deployment task statuses
const (
	// BGTaskStatusPending indicates the task is pending.
	BGTaskStatusPending = "PENDING"

	// BGTaskStatusInProgress indicates the task is in progress.
	BGTaskStatusInProgress = "IN_PROGRESS"

	// BGTaskStatusCompleted indicates the task completed.
	BGTaskStatusCompleted = "COMPLETED"

	// BGTaskStatusFailed indicates the task failed.
	BGTaskStatusFailed = "FAILED"
)

// Switchover defaults
const (
	// DefaultSwitchoverTimeout is the default switchover timeout in seconds.
	DefaultSwitchoverTimeout = 300
)

// File permissions
const (
	// DefaultDirMode is the default permission mode for directories.
	DefaultDirMode = 0755

	// DefaultFileMode is the default permission mode for files.
	DefaultFileMode = 0644
)

// Demo mode constants
const (
	// DemoModeRetryAttempts is the number of retry attempts in demo mode.
	DemoModeRetryAttempts = 1

	// DemoFastModePollInterval is the polling interval in fast mode.
	DemoFastModePollInterval = 50 * time.Millisecond
)

// Operation ID suffix length for parameter group names
const (
	// OperationIDSuffixLength is the number of characters to use from the operation ID.
	OperationIDSuffixLength = 8
)
