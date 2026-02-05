// Package rds provides an RDS client wrapper for cluster maintenance operations.
package rds

// InstanceStatus represents the status of an RDS DB instance.
// See: https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/accessing-monitoring.html
type InstanceStatus string

// All possible RDS DB instance statuses from AWS documentation.
const (
	// StatusAvailable indicates the DB instance is healthy and available.
	StatusAvailable InstanceStatus = "available"

	// Transitional statuses - instance will eventually become available
	// These statuses indicate the instance is undergoing a configuration change.

	// StatusBackingUp indicates the DB instance is currently being backed up.
	StatusBackingUp InstanceStatus = "backing-up"

	// StatusConfiguringEnhancedMonitoring indicates Enhanced Monitoring is being enabled or disabled.
	StatusConfiguringEnhancedMonitoring InstanceStatus = "configuring-enhanced-monitoring"

	// StatusConfiguringIAMDatabaseAuth indicates IAM database authentication is being enabled or disabled.
	StatusConfiguringIAMDatabaseAuth InstanceStatus = "configuring-iam-database-auth"

	// StatusConfiguringLogExports indicates publishing log files to CloudWatch Logs is being enabled or disabled.
	StatusConfiguringLogExports InstanceStatus = "configuring-log-exports"

	// StatusConfiguringPerformanceInsights indicates Performance Insights is being enabled or disabled.
	StatusConfiguringPerformanceInsights InstanceStatus = "configuring-performance-insights"

	// StatusConvertingToVPC indicates the DB instance is being converted to a VPC.
	StatusConvertingToVPC InstanceStatus = "converting-to-vpc"

	// StatusCreating indicates the DB instance is being created.
	StatusCreating InstanceStatus = "creating"

	// StatusMaintenance indicates Amazon RDS is applying a maintenance update.
	StatusMaintenance InstanceStatus = "maintenance"

	// StatusModifying indicates the DB instance is being modified.
	StatusModifying InstanceStatus = "modifying"

	// StatusMovingToVPC indicates the DB instance is being moved to a new VPC.
	StatusMovingToVPC InstanceStatus = "moving-to-vpc"

	// StatusRebooting indicates the DB instance is being rebooted.
	StatusRebooting InstanceStatus = "rebooting"

	// StatusResettingMasterCredentials indicates the master credentials are being reset.
	StatusResettingMasterCredentials InstanceStatus = "resetting-master-credentials"

	// StatusRenaming indicates the DB instance is being renamed.
	StatusRenaming InstanceStatus = "renaming"

	// StatusStarting indicates the DB instance is starting.
	StatusStarting InstanceStatus = "starting"

	// StatusStorageConfigUpgrade indicates the storage file system configuration is being upgraded.
	StatusStorageConfigUpgrade InstanceStatus = "storage-config-upgrade"

	// StatusStorageOptimization indicates Amazon RDS is optimizing the storage.
	StatusStorageOptimization InstanceStatus = "storage-optimization"

	// StatusUpgrading indicates the database engine or OS version is being upgraded.
	StatusUpgrading InstanceStatus = "upgrading"

	// Stopped/Stopping statuses

	// StatusStopped indicates the DB instance is stopped.
	StatusStopped InstanceStatus = "stopped"

	// StatusStopping indicates the DB instance is being stopped.
	StatusStopping InstanceStatus = "stopping"

	// Deletion statuses

	// StatusDeleting indicates the DB instance is being deleted.
	StatusDeleting InstanceStatus = "deleting"

	// StatusDeletePrecheck indicates Amazon RDS is validating read replicas before deletion.
	StatusDeletePrecheck InstanceStatus = "delete-precheck"

	// Error/Failure statuses

	// StatusFailed indicates the DB instance has failed and RDS can't recover it.
	StatusFailed InstanceStatus = "failed"

	// StatusInaccessibleEncryptionCredentials indicates the KMS key can't be accessed.
	StatusInaccessibleEncryptionCredentials InstanceStatus = "inaccessible-encryption-credentials"

	// StatusInaccessibleEncryptionCredentialsRecoverable indicates the KMS key can't be accessed but may be recoverable.
	StatusInaccessibleEncryptionCredentialsRecoverable InstanceStatus = "inaccessible-encryption-credentials-recoverable"

	// StatusIncompatibleCreate indicates RDS can't create due to incompatible resources.
	StatusIncompatibleCreate InstanceStatus = "incompatible-create"

	// StatusIncompatibleNetwork indicates recovery failed due to VPC issues.
	StatusIncompatibleNetwork InstanceStatus = "incompatible-network"

	// StatusIncompatibleOptionGroup indicates option group change failed.
	StatusIncompatibleOptionGroup InstanceStatus = "incompatible-option-group"

	// StatusIncompatibleParameters indicates parameters are incompatible with the instance.
	StatusIncompatibleParameters InstanceStatus = "incompatible-parameters"

	// StatusIncompatibleRestore indicates point-in-time restore failed.
	StatusIncompatibleRestore InstanceStatus = "incompatible-restore"

	// StatusInsufficientCapacity indicates there isn't enough capacity to create the instance.
	StatusInsufficientCapacity InstanceStatus = "insufficient-capacity"

	// StatusRestoreError indicates an error occurred during restore.
	StatusRestoreError InstanceStatus = "restore-error"

	// StatusStorageFull indicates the DB instance has reached its storage capacity.
	StatusStorageFull InstanceStatus = "storage-full"

	// StatusStorageInitialization indicates the DB instance is loading data from S3.
	StatusStorageInitialization InstanceStatus = "storage-initialization"
)

// transitionalStatuses contains all statuses that indicate the instance
// is undergoing a change and will eventually become available.
var transitionalStatuses = map[InstanceStatus]bool{
	StatusBackingUp:                      true,
	StatusConfiguringEnhancedMonitoring:  true,
	StatusConfiguringIAMDatabaseAuth:     true,
	StatusConfiguringLogExports:          true,
	StatusConfiguringPerformanceInsights: true,
	StatusConvertingToVPC:                true,
	StatusCreating:                       true,
	StatusMaintenance:                    true,
	StatusModifying:                      true,
	StatusMovingToVPC:                    true,
	StatusRebooting:                      true,
	StatusResettingMasterCredentials:     true,
	StatusRenaming:                       true,
	StatusStarting:                       true,
	StatusStorageConfigUpgrade:           true,
	StatusStorageOptimization:            true,
	StatusUpgrading:                      true,
}

// errorStatuses contains statuses that indicate a problem with the instance.
var errorStatuses = map[InstanceStatus]bool{
	StatusFailed:                                       true,
	StatusInaccessibleEncryptionCredentials:            true,
	StatusInaccessibleEncryptionCredentialsRecoverable: true,
	StatusIncompatibleCreate:                           true,
	StatusIncompatibleNetwork:                          true,
	StatusIncompatibleOptionGroup:                      true,
	StatusIncompatibleParameters:                       true,
	StatusIncompatibleRestore:                          true,
	StatusInsufficientCapacity:                         true,
	StatusRestoreError:                                 true,
	StatusStorageFull:                                  true,
}

// IsTransitional returns true if the status indicates the instance is
// undergoing a change and will eventually become available.
func (s InstanceStatus) IsTransitional() bool {
	return transitionalStatuses[s]
}

// IsAvailable returns true if the status indicates the instance is ready.
func (s InstanceStatus) IsAvailable() bool {
	return s == StatusAvailable
}

// IsError returns true if the status indicates a problem with the instance.
func (s InstanceStatus) IsError() bool {
	return errorStatuses[s]
}

// IsDeleting returns true if the instance is being deleted.
func (s InstanceStatus) IsDeleting() bool {
	return s == StatusDeleting || s == StatusDeletePrecheck
}

// IsStopped returns true if the instance is stopped or stopping.
func (s InstanceStatus) IsStopped() bool {
	return s == StatusStopped || s == StatusStopping
}

// CanPerformOperations returns true if the instance is in a state
// where maintenance operations can be performed (available or storage-full).
func (s InstanceStatus) CanPerformOperations() bool {
	return s == StatusAvailable || s == StatusStorageFull
}

// ShouldWaitForAvailable returns true if we should wait for this status
// to transition to available before proceeding with operations.
func (s InstanceStatus) ShouldWaitForAvailable() bool {
	return s.IsTransitional()
}

// CanFailover returns true if the instance is in a valid state to be
// the target of a failover operation. AWS requires the target instance
// to be in the "available" state for FailoverDBCluster to succeed.
func (s InstanceStatus) CanFailover() bool {
	return s == StatusAvailable
}

// ClusterStatus represents the status of an RDS DB cluster.
type ClusterStatus string

// All possible RDS DB cluster statuses.
const (
	// ClusterStatusAvailable indicates the cluster is healthy and available.
	ClusterStatusAvailable ClusterStatus = "available"

	// ClusterStatusBackingUp indicates the cluster is being backed up.
	ClusterStatusBackingUp ClusterStatus = "backing-up"

	// ClusterStatusCreating indicates the cluster is being created.
	ClusterStatusCreating ClusterStatus = "creating"

	// ClusterStatusDeleting indicates the cluster is being deleted.
	ClusterStatusDeleting ClusterStatus = "deleting"

	// ClusterStatusFailed indicates the cluster has failed.
	ClusterStatusFailed ClusterStatus = "failed"

	// ClusterStatusFailingOver indicates the cluster is failing over.
	ClusterStatusFailingOver ClusterStatus = "failing-over"

	// ClusterStatusMaintenance indicates maintenance is being applied.
	ClusterStatusMaintenance ClusterStatus = "maintenance"

	// ClusterStatusMigrating indicates the cluster is migrating.
	ClusterStatusMigrating ClusterStatus = "migrating"

	// ClusterStatusModifying indicates the cluster is being modified.
	ClusterStatusModifying ClusterStatus = "modifying"

	// ClusterStatusRebooting indicates the cluster is rebooting.
	ClusterStatusRebooting ClusterStatus = "rebooting"

	// ClusterStatusRenaming indicates the cluster is being renamed.
	ClusterStatusRenaming ClusterStatus = "renaming"

	// ClusterStatusResettingMasterCredentials indicates credentials are being reset.
	ClusterStatusResettingMasterCredentials ClusterStatus = "resetting-master-credentials"

	// ClusterStatusStarting indicates the cluster is starting.
	ClusterStatusStarting ClusterStatus = "starting"

	// ClusterStatusStopped indicates the cluster is stopped.
	ClusterStatusStopped ClusterStatus = "stopped"

	// ClusterStatusStopping indicates the cluster is stopping.
	ClusterStatusStopping ClusterStatus = "stopping"

	// ClusterStatusUpgrading indicates the cluster is being upgraded.
	ClusterStatusUpgrading ClusterStatus = "upgrading"
)

// clusterTransitionalStatuses contains all cluster statuses that indicate
// the cluster is undergoing a change.
var clusterTransitionalStatuses = map[ClusterStatus]bool{
	ClusterStatusBackingUp:                  true,
	ClusterStatusCreating:                   true,
	ClusterStatusFailingOver:                true,
	ClusterStatusMaintenance:                true,
	ClusterStatusMigrating:                  true,
	ClusterStatusModifying:                  true,
	ClusterStatusRebooting:                  true,
	ClusterStatusRenaming:                   true,
	ClusterStatusResettingMasterCredentials: true,
	ClusterStatusStarting:                   true,
	ClusterStatusUpgrading:                  true,
}

// IsTransitional returns true if the cluster status indicates it's
// undergoing a change and will eventually become available.
func (s ClusterStatus) IsTransitional() bool {
	return clusterTransitionalStatuses[s]
}

// IsAvailable returns true if the cluster is available.
func (s ClusterStatus) IsAvailable() bool {
	return s == ClusterStatusAvailable
}
