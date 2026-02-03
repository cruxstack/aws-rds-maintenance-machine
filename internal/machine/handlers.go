package machine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	internalerrors "github.com/mpz/devops/tools/rds-maint-machine/internal/errors"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/rds"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/types"
)

// handleGetClusterInfo retrieves cluster information.
func (e *Engine) handleGetClusterInfo(ctx context.Context, op *types.Operation, step *types.Step) error {
	rdsClient, err := e.getRDSClient(ctx, op)
	if err != nil {
		return err
	}

	info, err := rdsClient.GetClusterInfo(ctx, op.ClusterID)
	if err != nil {
		return err
	}

	result, _ := json.Marshal(info)
	step.Result = result
	return nil
}

// handleCreateTempInstance creates a temporary instance.
func (e *Engine) handleCreateTempInstance(ctx context.Context, op *types.Operation, step *types.Step) error {
	rdsClient, err := e.getRDSClient(ctx, op)
	if err != nil {
		return err
	}

	var params struct {
		InstanceType string `json:"instance_type"`
		Engine       string `json:"engine"`
	}
	if err := json.Unmarshal(step.Parameters, &params); err != nil {
		return errors.Wrap(err, "unmarshal params")
	}

	instanceID := rds.GenerateTempInstanceID(op.ClusterID, op.ID)

	createParams := rds.CreateInstanceParams{
		ClusterID:     op.ClusterID,
		InstanceID:    instanceID,
		InstanceType:  params.InstanceType,
		Engine:        params.Engine,
		PromotionTier: 0, // Highest priority for failover
		OperationID:   op.ID,
	}

	_, err = rdsClient.CreateClusterInstance(ctx, createParams)
	if err != nil {
		return err
	}

	result, _ := json.Marshal(map[string]string{"instance_id": instanceID})
	step.Result = result
	return nil
}

// handleWaitInstanceAvailable waits for an instance to become available AND reach desired state.
func (e *Engine) handleWaitInstanceAvailable(ctx context.Context, op *types.Operation, step *types.Step) error {
	rdsClient, err := e.getRDSClient(ctx, op)
	if err != nil {
		return err
	}

	var params struct {
		InstanceID string `json:"instance_id"`
	}
	if len(step.Parameters) > 0 {
		if err := json.Unmarshal(step.Parameters, &params); err != nil {
			return errors.Wrap(err, "unmarshal params")
		}
	}

	// If instance_id is empty, this is likely a wait for the temp instance created earlier.
	// We ONLY fall back to the temp instance if this step is specifically for waiting on
	// the temp instance (identified by step name). For any other wait step, we MUST have
	// an explicit instance_id to prevent the bug where all modifications run in parallel.
	if params.InstanceID == "" {
		// Check if this is a temp instance wait step by looking at the step name
		isTempInstanceWait := step.Name == "Wait for temp instance"
		if isTempInstanceWait {
			params.InstanceID = e.findCreatedInstanceID(op)
			if params.InstanceID != "" {
				e.logger.Info("using temp instance ID from previous step",
					"operation_id", op.ID,
					"instance_id", params.InstanceID)
			}
		} else {
			// This is NOT a temp instance wait, but instance_id is missing.
			// This is a critical error that would cause parallel modifications.
			e.logger.Error("CRITICAL: wait_instance_available step missing instance_id parameter",
				"operation_id", op.ID,
				"step_id", step.ID,
				"step_name", step.Name,
				"step_parameters", string(step.Parameters))
			return errors.Wrapf(internalerrors.ErrInvalidParameter,
				"instance_id required for step %q - missing parameter would cause parallel modifications", step.Name)
		}
	}

	if params.InstanceID == "" {
		return errors.Wrap(internalerrors.ErrInvalidParameter, "instance_id required")
	}

	// Determine what we're waiting for based on the operation type and previous step
	var targetInstanceType string
	var targetStorageType string

	// Look for the previous modify step to get the target state
	for i := op.CurrentStepIndex - 1; i >= 0; i-- {
		prevStep := &op.Steps[i]
		if prevStep.Action == "modify_instance" {
			var modifyParams struct {
				InstanceID   string `json:"instance_id"`
				InstanceType string `json:"instance_type,omitempty"`
				StorageType  string `json:"storage_type,omitempty"`
			}
			if err := json.Unmarshal(prevStep.Parameters, &modifyParams); err == nil {
				if modifyParams.InstanceID == params.InstanceID {
					targetInstanceType = modifyParams.InstanceType
					targetStorageType = modifyParams.StorageType
					break
				}
			}
		}
	}

	e.logger.Info("waiting for instance to reach desired state",
		"operation_id", op.ID,
		"instance_id", params.InstanceID,
		"step_name", step.Name,
		"target_instance_type", targetInstanceType,
		"target_storage_type", targetStorageType)

	step.WaitCondition = "waiting for instance to become available and reach desired state"
	step.State = types.StepStateWaiting

	// Poll until instance is available AND has the desired configuration
	timeout := time.After(e.getWaitTimeout(op))
	ticker := time.NewTicker(e.defaultPollInterval)
	defer ticker.Stop()

	pollCount := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return errors.Wrapf(internalerrors.ErrWaitTimeout, "instance %s did not reach desired state", params.InstanceID)
		case <-ticker.C:
			pollCount++

			// Get current instance info
			instanceInfo, err := rdsClient.GetInstanceInfo(ctx, params.InstanceID)
			if err != nil {
				if pollCount%10 == 0 {
					e.logger.Warn("error getting instance info",
						"operation_id", op.ID,
						"instance_id", params.InstanceID,
						"error", err)
				}
				continue
			}

			// Check if instance is available
			instanceStatus := rds.InstanceStatus(instanceInfo.Status)
			if !instanceStatus.IsAvailable() {
				step.WaitCondition = fmt.Sprintf("instance status: %s", instanceInfo.Status)
				if pollCount%10 == 0 {
					e.logger.Info("instance not yet available",
						"operation_id", op.ID,
						"instance_id", params.InstanceID,
						"status", instanceInfo.Status,
						"poll_count", pollCount)
				}
				continue
			}

			// Instance is available, now check if it has the desired configuration
			configMatch := true
			var mismatchReason string

			if targetInstanceType != "" && instanceInfo.InstanceType != targetInstanceType {
				configMatch = false
				mismatchReason = fmt.Sprintf("instance type is %s, waiting for %s", instanceInfo.InstanceType, targetInstanceType)
			}

			if targetStorageType != "" && instanceInfo.StorageType != targetStorageType {
				configMatch = false
				if mismatchReason != "" {
					mismatchReason += "; "
				}
				mismatchReason += fmt.Sprintf("storage type is %s, waiting for %s", instanceInfo.StorageType, targetStorageType)
			}

			if !configMatch {
				step.WaitCondition = mismatchReason
				if pollCount%10 == 0 {
					e.logger.Info("instance available but configuration not yet applied",
						"operation_id", op.ID,
						"instance_id", params.InstanceID,
						"current_instance_type", instanceInfo.InstanceType,
						"target_instance_type", targetInstanceType,
						"current_storage_type", instanceInfo.StorageType,
						"target_storage_type", targetStorageType,
						"poll_count", pollCount)
				}
				continue
			}

			// Instance is available AND has the desired configuration
			e.logger.Info("instance has reached desired state",
				"operation_id", op.ID,
				"instance_id", params.InstanceID,
				"instance_type", instanceInfo.InstanceType,
				"storage_type", instanceInfo.StorageType,
				"poll_count", pollCount)

			return nil
		}
	}
}

// handleFailoverToInstance initiates a failover to a specific instance and verifies it completes.
func (e *Engine) handleFailoverToInstance(ctx context.Context, op *types.Operation, step *types.Step) error {
	rdsClient, err := e.getRDSClient(ctx, op)
	if err != nil {
		return err
	}

	var params struct {
		InstanceID string `json:"instance_id"`
	}
	if len(step.Parameters) > 0 {
		if err := json.Unmarshal(step.Parameters, &params); err != nil {
			return errors.Wrap(err, "unmarshal params")
		}
	}

	// If instance_id is empty, look for it in previous step results
	if params.InstanceID == "" {
		params.InstanceID = e.findCreatedInstanceID(op)
	}

	if params.InstanceID == "" {
		return errors.Wrap(internalerrors.ErrInvalidParameter, "instance_id required")
	}

	// Check current cluster state to see if target is already the writer
	clusterInfo, err := rdsClient.GetClusterInfo(ctx, op.ClusterID)
	if err != nil {
		return errors.Wrap(err, "get cluster info")
	}

	// Find the target instance in the cluster and check if it's already the writer
	var targetInstance *types.InstanceInfo
	for i := range clusterInfo.Instances {
		if clusterInfo.Instances[i].InstanceID == params.InstanceID {
			targetInstance = &clusterInfo.Instances[i]
			break
		}
	}

	if targetInstance == nil {
		return errors.Wrapf(internalerrors.ErrInstanceNotFound,
			"instance %s not found in cluster %s", params.InstanceID, op.ClusterID)
	}

	// If target is already the writer, no failover needed
	if targetInstance.Role == "writer" {
		step.Result, _ = json.Marshal(map[string]string{
			"status":  "skipped",
			"message": "instance is already the writer",
		})
		return nil
	}

	// Validate that the target instance is in a state that can accept failover.
	// AWS requires the instance to be "available" for FailoverDBCluster to succeed.
	instanceStatus := rds.InstanceStatus(targetInstance.Status)
	if !instanceStatus.CanFailover() {
		return errors.Wrapf(internalerrors.ErrInvalidState,
			"instance %s is in state %q and cannot be failover target (must be %q)",
			params.InstanceID, targetInstance.Status, rds.StatusAvailable)
	}

	// Initiate the failover
	if err := rdsClient.FailoverCluster(ctx, op.ClusterID, params.InstanceID); err != nil {
		return err
	}

	// Poll until the target becomes the writer or we timeout
	step.WaitCondition = "waiting for failover to complete"
	step.State = types.StepStateWaiting

	timeout := time.After(e.getWaitTimeout(op))
	ticker := time.NewTicker(e.defaultPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return errors.Wrapf(internalerrors.ErrWaitTimeout,
				"failover to %s did not complete in time", params.InstanceID)
		case <-ticker.C:
			info, err := rdsClient.GetClusterInfo(ctx, op.ClusterID)
			if err != nil {
				// Transient errors during failover are expected, continue polling
				continue
			}

			// Check if the target is now the writer
			for _, inst := range info.Instances {
				if inst.InstanceID == params.InstanceID {
					if inst.Role == "writer" {
						step.Result, _ = json.Marshal(map[string]string{
							"status":  "completed",
							"message": "failover completed successfully",
						})
						return nil
					}
					step.WaitCondition = "failover in progress, instance role: " + inst.Role
					break
				}
			}
		}
	}
}

// handleModifyInstance modifies an instance.
func (e *Engine) handleModifyInstance(ctx context.Context, op *types.Operation, step *types.Step) error {
	rdsClient, err := e.getRDSClient(ctx, op)
	if err != nil {
		return err
	}

	var params struct {
		InstanceID        string `json:"instance_id"`
		InstanceType      string `json:"instance_type,omitempty"`
		StorageType       string `json:"storage_type,omitempty"`
		IOPS              *int32 `json:"iops,omitempty"`
		StorageThroughput *int32 `json:"storage_throughput,omitempty"`
	}
	if err := json.Unmarshal(step.Parameters, &params); err != nil {
		return errors.Wrap(err, "unmarshal params")
	}

	if params.InstanceID == "" {
		return errors.Wrap(internalerrors.ErrInvalidParameter, "instance_id required")
	}

	// Check instance status before modifying
	instanceInfo, err := rdsClient.GetInstanceInfo(ctx, params.InstanceID)
	if err != nil {
		return errors.Wrapf(err, "get instance info for %s", params.InstanceID)
	}

	e.logger.Info("MODIFY: instance current status",
		"operation_id", op.ID,
		"instance_id", params.InstanceID,
		"current_status", instanceInfo.Status,
		"current_type", instanceInfo.InstanceType,
		"target_type", params.InstanceType)

	// If instance is not available, it might already be modifying
	if instanceInfo.Status != "available" {
		e.logger.Warn("MODIFY: instance not available, may already be modifying",
			"operation_id", op.ID,
			"instance_id", params.InstanceID,
			"status", instanceInfo.Status)
	}

	e.logger.Info("MODIFY: starting instance modification",
		"operation_id", op.ID,
		"instance_id", params.InstanceID,
		"instance_type", params.InstanceType,
		"storage_type", params.StorageType)

	modifyParams := rds.ModifyInstanceParams{
		InstanceID:        params.InstanceID,
		InstanceType:      params.InstanceType,
		StorageType:       params.StorageType,
		IOPS:              params.IOPS,
		StorageThroughput: params.StorageThroughput,
		ApplyImmediately:  true,
	}

	err = rdsClient.ModifyInstance(ctx, modifyParams)

	e.logger.Info("MODIFY: completed instance modification call",
		"operation_id", op.ID,
		"instance_id", params.InstanceID,
		"error", err)

	return err
}

// handleDeleteInstance deletes an instance.
func (e *Engine) handleDeleteInstance(ctx context.Context, op *types.Operation, step *types.Step) error {
	rdsClient, err := e.getRDSClient(ctx, op)
	if err != nil {
		return err
	}

	var params struct {
		InstanceID        string `json:"instance_id"`
		SkipFinalSnapshot bool   `json:"skip_final_snapshot"`
	}
	if len(step.Parameters) > 0 {
		if err := json.Unmarshal(step.Parameters, &params); err != nil {
			return errors.Wrap(err, "unmarshal params")
		}
	}

	// If instance_id is empty, look for temp instance
	if params.InstanceID == "" {
		params.InstanceID = e.findCreatedInstanceID(op)
	}

	if params.InstanceID == "" {
		return errors.Wrap(internalerrors.ErrInvalidParameter, "instance_id required")
	}

	// Safety check: never delete the current writer instance.
	// This prevents data loss if a failover didn't complete as expected.
	clusterInfo, err := rdsClient.GetClusterInfo(ctx, op.ClusterID)
	if err != nil {
		return errors.Wrap(err, "get cluster info for safety check")
	}

	for _, inst := range clusterInfo.Instances {
		if inst.InstanceID == params.InstanceID && inst.Role == "writer" {
			return errors.Wrapf(internalerrors.ErrInvalidState,
				"refusing to delete instance %s because it is the current writer; "+
					"failover may not have completed successfully", params.InstanceID)
		}
	}

	return rdsClient.DeleteInstance(ctx, params.InstanceID, params.SkipFinalSnapshot)
}

// handleWaitInstanceDeleted waits for an instance to be deleted.
func (e *Engine) handleWaitInstanceDeleted(ctx context.Context, op *types.Operation, step *types.Step) error {
	rdsClient, err := e.getRDSClient(ctx, op)
	if err != nil {
		return err
	}

	var params struct {
		InstanceID string `json:"instance_id"`
	}
	if len(step.Parameters) > 0 {
		if err := json.Unmarshal(step.Parameters, &params); err != nil {
			return errors.Wrap(err, "unmarshal params")
		}
	}

	if params.InstanceID == "" {
		params.InstanceID = e.findCreatedInstanceID(op)
	}

	if params.InstanceID == "" {
		return errors.Wrap(internalerrors.ErrInvalidParameter, "instance_id required")
	}

	step.WaitCondition = "waiting for instance to be deleted"
	step.State = types.StepStateWaiting

	err = rdsClient.WaitForInstanceDeleted(ctx, params.InstanceID, e.getWaitTimeout(op))
	if err != nil {
		return errors.Wrapf(internalerrors.ErrWaitTimeout, "instance %s: %v", params.InstanceID, err)
	}

	return nil
}

// handleCreateSnapshot creates a cluster snapshot.
func (e *Engine) handleCreateSnapshot(ctx context.Context, op *types.Operation, step *types.Step) error {
	rdsClient, err := e.getRDSClient(ctx, op)
	if err != nil {
		return err
	}

	var params struct {
		SnapshotID string `json:"snapshot_id"`
	}
	if len(step.Parameters) > 0 {
		if err := json.Unmarshal(step.Parameters, &params); err != nil {
			return errors.Wrap(err, "unmarshal params")
		}
	}

	if params.SnapshotID == "" {
		params.SnapshotID = op.ClusterID + "-pre-upgrade-" + time.Now().Format("20060102-150405")
	}

	err = rdsClient.CreateClusterSnapshot(ctx, op.ClusterID, params.SnapshotID)
	if err != nil {
		return err
	}

	result, _ := json.Marshal(map[string]string{"snapshot_id": params.SnapshotID})
	step.Result = result
	return nil
}

// handleWaitSnapshotAvailable waits for a snapshot to become available.
func (e *Engine) handleWaitSnapshotAvailable(ctx context.Context, op *types.Operation, step *types.Step) error {
	rdsClient, err := e.getRDSClient(ctx, op)
	if err != nil {
		return err
	}

	var params struct {
		SnapshotID string `json:"snapshot_id"`
	}
	if len(step.Parameters) > 0 {
		if err := json.Unmarshal(step.Parameters, &params); err != nil {
			return errors.Wrap(err, "unmarshal params")
		}
	}

	if params.SnapshotID == "" {
		params.SnapshotID = e.findCreatedSnapshotID(op)
	}

	if params.SnapshotID == "" {
		return errors.Wrap(internalerrors.ErrInvalidParameter, "snapshot_id required")
	}

	step.WaitCondition = "waiting for snapshot to become available"
	step.State = types.StepStateWaiting

	err = rdsClient.WaitForSnapshotAvailable(ctx, params.SnapshotID, e.getWaitTimeout(op))
	if err != nil {
		return errors.Wrapf(internalerrors.ErrWaitTimeout, "snapshot %s: %v", params.SnapshotID, err)
	}

	return nil
}

// handleModifyCluster modifies cluster settings.
func (e *Engine) handleModifyCluster(ctx context.Context, op *types.Operation, step *types.Step) error {
	rdsClient, err := e.getRDSClient(ctx, op)
	if err != nil {
		return err
	}

	var params struct {
		EngineVersion                string `json:"engine_version,omitempty"`
		AllowMajorVersionUpgrade     bool   `json:"allow_major_version_upgrade,omitempty"`
		DBClusterParameterGroupName  string `json:"db_cluster_parameter_group_name,omitempty"`
		DBInstanceParameterGroupName string `json:"db_instance_parameter_group_name,omitempty"`
	}
	if err := json.Unmarshal(step.Parameters, &params); err != nil {
		return errors.Wrap(err, "unmarshal params")
	}

	modifyParams := rds.ModifyClusterParams{
		ClusterID:                    op.ClusterID,
		EngineVersion:                params.EngineVersion,
		AllowMajorVersionUpgrade:     params.AllowMajorVersionUpgrade,
		DBClusterParameterGroupName:  params.DBClusterParameterGroupName,
		DBInstanceParameterGroupName: params.DBInstanceParameterGroupName,
		ApplyImmediately:             true,
	}

	return rdsClient.ModifyCluster(ctx, modifyParams)
}

// handleWaitClusterAvailable waits for the cluster to become available.
func (e *Engine) handleWaitClusterAvailable(ctx context.Context, op *types.Operation, step *types.Step) error {
	rdsClient, err := e.getRDSClient(ctx, op)
	if err != nil {
		return err
	}

	step.WaitCondition = "waiting for cluster to become available"
	step.State = types.StepStateWaiting

	e.logger.Info("starting wait for cluster available",
		"operation_id", op.ID,
		"cluster_id", op.ClusterID,
		"step_name", step.Name)

	// Poll until cluster and all instances are available
	timeout := time.After(e.getWaitTimeout(op))
	ticker := time.NewTicker(e.defaultPollInterval)
	defer ticker.Stop()

	pollCount := 0
	for {
		select {
		case <-ctx.Done():
			e.logger.Warn("context cancelled while waiting for cluster",
				"operation_id", op.ID,
				"cluster_id", op.ClusterID)
			return ctx.Err()
		case <-timeout:
			e.logger.Error("timeout waiting for cluster available",
				"operation_id", op.ID,
				"cluster_id", op.ClusterID,
				"last_condition", step.WaitCondition)
			return errors.Wrapf(internalerrors.ErrWaitTimeout, "cluster %s", op.ClusterID)
		case <-ticker.C:
			pollCount++
			info, err := rdsClient.GetClusterInfo(ctx, op.ClusterID)
			if err != nil {
				e.logger.Warn("transient error getting cluster info",
					"operation_id", op.ID,
					"cluster_id", op.ClusterID,
					"error", err,
					"poll_count", pollCount)
				continue
			}

			// Check cluster status using the status helper
			clusterStatus := rds.ClusterStatus(info.Status)
			if !clusterStatus.IsAvailable() {
				// Update wait condition to show current cluster status
				if clusterStatus.IsTransitional() {
					step.WaitCondition = "cluster status: " + info.Status
				}
				if pollCount%10 == 0 {
					e.logger.Info("waiting for cluster",
						"operation_id", op.ID,
						"cluster_id", op.ClusterID,
						"cluster_status", info.Status,
						"poll_count", pollCount)
				}
				continue
			}

			// Check all instance statuses
			allAvailable := true
			var blockingInstance string
			var blockingStatus string
			for _, instance := range info.Instances {
				instanceStatus := rds.InstanceStatus(instance.Status)

				// Skip stopped or deleting instances - they don't block cluster availability
				if instanceStatus.IsStopped() || instanceStatus.IsDeleting() {
					if pollCount == 1 {
						e.logger.Info("skipping stopped/deleting instance in availability check",
							"operation_id", op.ID,
							"instance_id", instance.InstanceID,
							"instance_status", instance.Status)
					}
					continue
				}

				if !instanceStatus.IsAvailable() {
					allAvailable = false
					blockingInstance = instance.InstanceID
					blockingStatus = instance.Status

					if instanceStatus.IsError() {
						e.logger.Error("instance in error state",
							"operation_id", op.ID,
							"instance_id", instance.InstanceID,
							"instance_status", instance.Status)
						return errors.Wrapf(internalerrors.ErrWaitTimeout,
							"instance %s is in error state: %s", instance.InstanceID, instance.Status)
					}
					break
				}
			}

			if !allAvailable {
				step.WaitCondition = "instance " + blockingInstance + " status: " + blockingStatus
				if pollCount%10 == 0 {
					e.logger.Info("waiting for instance",
						"operation_id", op.ID,
						"instance_id", blockingInstance,
						"instance_status", blockingStatus,
						"poll_count", pollCount)
				}
			}

			if allAvailable {
				e.logger.Info("cluster and all instances available",
					"operation_id", op.ID,
					"cluster_id", op.ClusterID,
					"poll_count", pollCount)
				return nil
			}
		}
	}
}

// findCreatedInstanceID finds the instance ID created by a previous step.
func (e *Engine) findCreatedInstanceID(op *types.Operation) string {
	for _, step := range op.Steps {
		if step.Action == "create_temp_instance" && step.State == types.StepStateCompleted {
			var result struct {
				InstanceID string `json:"instance_id"`
			}
			if err := json.Unmarshal(step.Result, &result); err == nil {
				return result.InstanceID
			}
		}
	}
	return ""
}

// findCreatedSnapshotID finds the snapshot ID created by a previous step.
func (e *Engine) findCreatedSnapshotID(op *types.Operation) string {
	for _, step := range op.Steps {
		if step.Action == "create_snapshot" && step.State == types.StepStateCompleted {
			var result struct {
				SnapshotID string `json:"snapshot_id"`
			}
			if err := json.Unmarshal(step.Result, &result); err == nil {
				return result.SnapshotID
			}
		}
	}
	return ""
}

// handlePrepareParameterGroup prepares parameter groups for engine upgrade.
// If the cluster or instances use custom parameter groups, this creates new parameter groups
// for the target engine version and migrates the custom settings.
func (e *Engine) handlePrepareParameterGroup(ctx context.Context, op *types.Operation, step *types.Step) error {
	rdsClient, err := e.getRDSClient(ctx, op)
	if err != nil {
		return err
	}

	var params struct {
		TargetEngineVersion      string `json:"target_engine_version"`
		TargetParameterGroupName string `json:"target_parameter_group_name,omitempty"`
	}
	if err := json.Unmarshal(step.Parameters, &params); err != nil {
		return errors.Wrap(err, "unmarshal params")
	}

	// Get cluster info for engine type
	clusterInfo, err := rdsClient.GetClusterInfo(ctx, op.ClusterID)
	if err != nil {
		return errors.Wrap(err, "get cluster info")
	}

	targetFamily := rds.GetDefaultParameterGroupFamily(clusterInfo.Engine, params.TargetEngineVersion)

	// ==================== CLUSTER PARAMETER GROUP ====================
	currentClusterPG, err := rdsClient.GetClusterParameterGroup(ctx, op.ClusterID)
	if err != nil {
		return errors.Wrap(err, "get current cluster parameter group")
	}

	e.addEvent(op.ID, "info", fmt.Sprintf("Current cluster parameter group: %s (family: %s)", currentClusterPG.Name, currentClusterPG.Family), nil)

	var targetClusterPGName string
	var clusterPGAction string
	var clusterMigratedCount int
	var clusterSkippedParams []string

	isDefaultClusterPG := strings.HasPrefix(currentClusterPG.Name, "default.")
	if isDefaultClusterPG {
		// Using default parameter group, just use the default for the new version
		targetClusterPGName = rds.GetDefaultParameterGroupName(targetFamily)
		clusterPGAction = "using_default"
		e.addEvent(op.ID, "info", fmt.Sprintf("Using default cluster parameter group for target version: %s", targetClusterPGName), nil)
	} else {
		// Custom cluster parameter group - need to migrate settings
		e.addEvent(op.ID, "info", "Custom cluster parameter group detected, migrating settings...", nil)

		customParams, err := rdsClient.GetClusterParameterGroupCustomParameters(ctx, currentClusterPG.Name)
		if err != nil {
			return errors.Wrap(err, "get custom cluster parameters")
		}

		e.addEvent(op.ID, "info", fmt.Sprintf("Found %d custom cluster parameter(s) to migrate", len(customParams)), nil)

		targetClusterPGName = params.TargetParameterGroupName
		if targetClusterPGName == "" {
			targetClusterPGName = fmt.Sprintf("%s-%s-upgraded", op.ClusterID, strings.ReplaceAll(params.TargetEngineVersion, ".", "-"))
		}

		exists, err := rdsClient.ParameterGroupExists(ctx, targetClusterPGName)
		if err != nil {
			return errors.Wrap(err, "check cluster parameter group exists")
		}

		if !exists {
			description := fmt.Sprintf("Migrated from %s for engine upgrade to %s", currentClusterPG.Name, params.TargetEngineVersion)
			if err := rdsClient.CreateClusterParameterGroup(ctx, targetClusterPGName, targetFamily, description); err != nil {
				return errors.Wrap(err, "create cluster parameter group")
			}
			e.addEvent(op.ID, "info", fmt.Sprintf("Created cluster parameter group: %s (family: %s)", targetClusterPGName, targetFamily), nil)
		} else {
			e.addEvent(op.ID, "info", fmt.Sprintf("Cluster parameter group %s already exists, reusing", targetClusterPGName), nil)
		}

		// Apply custom parameters
		clusterMigratedCount = len(customParams)
		clusterSkippedParams = e.applyParametersToClusterPG(ctx, rdsClient, op, targetClusterPGName, customParams)
		clusterPGAction = "migrated"
	}

	// ==================== INSTANCE PARAMETER GROUP ====================
	// Get instance parameter group from the first instance (they typically all use the same one)
	var targetInstancePGName string
	var instancePGAction string
	var instanceMigratedCount int
	var instanceSkippedParams []string
	var sourceInstancePGName string

	if len(clusterInfo.Instances) > 0 {
		writerInstanceID := ""
		for _, inst := range clusterInfo.Instances {
			if inst.Role == "writer" {
				writerInstanceID = inst.InstanceID
				break
			}
		}
		if writerInstanceID == "" {
			writerInstanceID = clusterInfo.Instances[0].InstanceID
		}

		currentInstancePG, err := rdsClient.GetInstanceParameterGroup(ctx, writerInstanceID)
		if err != nil {
			return errors.Wrap(err, "get current instance parameter group")
		}

		sourceInstancePGName = currentInstancePG.Name
		e.addEvent(op.ID, "info", fmt.Sprintf("Current instance parameter group: %s (family: %s)", currentInstancePG.Name, currentInstancePG.Family), nil)

		isDefaultInstancePG := strings.HasPrefix(currentInstancePG.Name, "default.")
		if isDefaultInstancePG {
			// Using default instance parameter group
			targetInstancePGName = rds.GetDefaultParameterGroupName(targetFamily)
			instancePGAction = "using_default"
			e.addEvent(op.ID, "info", fmt.Sprintf("Using default instance parameter group for target version: %s", targetInstancePGName), nil)
		} else {
			// Custom instance parameter group - need to migrate settings
			e.addEvent(op.ID, "info", "Custom instance parameter group detected, migrating settings...", nil)

			customParams, err := rdsClient.GetInstanceParameterGroupCustomParameters(ctx, currentInstancePG.Name)
			if err != nil {
				return errors.Wrap(err, "get custom instance parameters")
			}

			e.addEvent(op.ID, "info", fmt.Sprintf("Found %d custom instance parameter(s) to migrate", len(customParams)), nil)

			// Generate instance PG name based on cluster PG name pattern
			targetInstancePGName = fmt.Sprintf("%s-%s-instance-upgraded", op.ClusterID, strings.ReplaceAll(params.TargetEngineVersion, ".", "-"))

			exists, err := rdsClient.InstanceParameterGroupExists(ctx, targetInstancePGName)
			if err != nil {
				return errors.Wrap(err, "check instance parameter group exists")
			}

			if !exists {
				description := fmt.Sprintf("Migrated from %s for engine upgrade to %s", currentInstancePG.Name, params.TargetEngineVersion)
				if err := rdsClient.CreateInstanceParameterGroup(ctx, targetInstancePGName, targetFamily, description); err != nil {
					return errors.Wrap(err, "create instance parameter group")
				}
				e.addEvent(op.ID, "info", fmt.Sprintf("Created instance parameter group: %s (family: %s)", targetInstancePGName, targetFamily), nil)
			} else {
				e.addEvent(op.ID, "info", fmt.Sprintf("Instance parameter group %s already exists, reusing", targetInstancePGName), nil)
			}

			// Apply custom parameters
			instanceMigratedCount = len(customParams)
			instanceSkippedParams = e.applyParametersToInstancePG(ctx, rdsClient, op, targetInstancePGName, customParams)
			instancePGAction = "migrated"
		}
	}

	result, _ := json.Marshal(map[string]any{
		"cluster_parameter_group": map[string]any{
			"name":           targetClusterPGName,
			"family":         targetFamily,
			"migrated_count": clusterMigratedCount,
			"skipped_params": clusterSkippedParams,
			"source":         currentClusterPG.Name,
			"action":         clusterPGAction,
		},
		"instance_parameter_group": map[string]any{
			"name":           targetInstancePGName,
			"family":         targetFamily,
			"migrated_count": instanceMigratedCount,
			"skipped_params": instanceSkippedParams,
			"source":         sourceInstancePGName,
			"action":         instancePGAction,
		},
	})
	step.Result = result

	// Update the next step with both parameter groups
	// For Blue-Green deployments, update create_blue_green_deployment step
	// For legacy upgrades, update modify_cluster step
	e.updateModifyClusterStepWithPGs(op, targetClusterPGName, targetInstancePGName)
	e.updateBlueGreenDeploymentStepWithPGs(op, targetClusterPGName, targetInstancePGName)

	return nil
}

// applyParametersToClusterPG applies custom parameters to a cluster parameter group.
func (e *Engine) applyParametersToClusterPG(ctx context.Context, rdsClient *rds.Client, op *types.Operation, pgName string, customParams []rds.ParameterInfo) []string {
	var skippedParams []string
	if len(customParams) == 0 {
		return skippedParams
	}

	if err := rdsClient.ModifyClusterParameterGroupParams(ctx, pgName, customParams); err != nil {
		e.addEvent(op.ID, "warning", fmt.Sprintf("Some cluster parameters could not be applied: %v", err), nil)
		// Try applying parameters one by one
		for _, p := range customParams {
			if err := rdsClient.ModifyClusterParameterGroupParams(ctx, pgName, []rds.ParameterInfo{p}); err != nil {
				skippedParams = append(skippedParams, p.Name)
				e.addEvent(op.ID, "warning", fmt.Sprintf("Skipped incompatible cluster parameter: %s=%s (%v)", p.Name, p.Value, err), nil)
			} else {
				e.addEvent(op.ID, "info", fmt.Sprintf("Applied cluster parameter: %s=%s", p.Name, p.Value), nil)
			}
		}
	} else {
		e.addEvent(op.ID, "info", fmt.Sprintf("Applied %d custom cluster parameter(s) to %s", len(customParams), pgName), nil)
	}

	if len(skippedParams) > 0 {
		e.addEvent(op.ID, "warning", fmt.Sprintf("Skipped %d incompatible cluster parameter(s): %v", len(skippedParams), skippedParams), nil)
	}
	return skippedParams
}

// applyParametersToInstancePG applies custom parameters to an instance parameter group.
func (e *Engine) applyParametersToInstancePG(ctx context.Context, rdsClient *rds.Client, op *types.Operation, pgName string, customParams []rds.ParameterInfo) []string {
	var skippedParams []string
	if len(customParams) == 0 {
		return skippedParams
	}

	if err := rdsClient.ModifyInstanceParameterGroupParams(ctx, pgName, customParams); err != nil {
		e.addEvent(op.ID, "warning", fmt.Sprintf("Some instance parameters could not be applied: %v", err), nil)
		// Try applying parameters one by one
		for _, p := range customParams {
			if err := rdsClient.ModifyInstanceParameterGroupParams(ctx, pgName, []rds.ParameterInfo{p}); err != nil {
				skippedParams = append(skippedParams, p.Name)
				e.addEvent(op.ID, "warning", fmt.Sprintf("Skipped incompatible instance parameter: %s=%s (%v)", p.Name, p.Value, err), nil)
			} else {
				e.addEvent(op.ID, "info", fmt.Sprintf("Applied instance parameter: %s=%s", p.Name, p.Value), nil)
			}
		}
	} else {
		e.addEvent(op.ID, "info", fmt.Sprintf("Applied %d custom instance parameter(s) to %s", len(customParams), pgName), nil)
	}

	if len(skippedParams) > 0 {
		e.addEvent(op.ID, "warning", fmt.Sprintf("Skipped %d incompatible instance parameter(s): %v", len(skippedParams), skippedParams), nil)
	}
	return skippedParams
}

// updateModifyClusterStepWithPGs updates the modify_cluster step to use the specified parameter groups.
func (e *Engine) updateModifyClusterStepWithPGs(op *types.Operation, clusterPGName, instancePGName string) {
	for i := range op.Steps {
		if op.Steps[i].Action == "modify_cluster" && op.Steps[i].State == types.StepStatePending {
			var params map[string]any
			if err := json.Unmarshal(op.Steps[i].Parameters, &params); err == nil {
				if clusterPGName != "" {
					params["db_cluster_parameter_group_name"] = clusterPGName
				}
				if instancePGName != "" {
					params["db_instance_parameter_group_name"] = instancePGName
				}
				newParams, _ := json.Marshal(params)
				op.Steps[i].Parameters = newParams
			}
			break
		}
	}
}

// updateBlueGreenDeploymentStepWithPGs updates the create_blue_green_deployment step to use the specified parameter groups.
func (e *Engine) updateBlueGreenDeploymentStepWithPGs(op *types.Operation, clusterPGName, instancePGName string) {
	for i := range op.Steps {
		if op.Steps[i].Action == "create_blue_green_deployment" && op.Steps[i].State == types.StepStatePending {
			var params map[string]any
			if err := json.Unmarshal(op.Steps[i].Parameters, &params); err == nil {
				if clusterPGName != "" {
					params["target_cluster_parameter_group_name"] = clusterPGName
				}
				if instancePGName != "" {
					params["target_instance_parameter_group_name"] = instancePGName
				}
				newParams, _ := json.Marshal(params)
				op.Steps[i].Parameters = newParams
			}
			break
		}
	}
}

// ==================== Blue-Green Deployment Handlers ====================

// handleCreateBlueGreenDeployment creates a Blue-Green deployment for engine upgrade.
// If an existing Blue-Green deployment is found for the cluster in a compatible state
// (PROVISIONING or AVAILABLE), it will be adopted instead of creating a new one.
func (e *Engine) handleCreateBlueGreenDeployment(ctx context.Context, op *types.Operation, step *types.Step) error {
	rdsClient, err := e.getRDSClient(ctx, op)
	if err != nil {
		return err
	}

	var params struct {
		TargetEngineVersion              string `json:"target_engine_version"`
		TargetClusterParameterGroupName  string `json:"target_cluster_parameter_group_name,omitempty"`
		TargetInstanceParameterGroupName string `json:"target_instance_parameter_group_name,omitempty"`
	}
	if err := json.Unmarshal(step.Parameters, &params); err != nil {
		return errors.Wrap(err, "unmarshal params")
	}

	// Get cluster ARN
	clusterARN, err := rdsClient.GetClusterARN(ctx, op.ClusterID)
	if err != nil {
		return errors.Wrap(err, "get cluster ARN")
	}

	// Check for existing Blue-Green deployments that can be adopted
	existingDeployments, err := rdsClient.ListBlueGreenDeploymentsForCluster(ctx, clusterARN)
	if err != nil {
		e.addEvent(op.ID, "warning", fmt.Sprintf("Failed to check for existing Blue-Green deployments: %v", err), nil)
		// Continue with creation attempt - it will fail if one already exists
	} else {
		for _, bgInfo := range existingDeployments {
			// Only adopt deployments that are in progress or ready for switchover
			// Skip deployments that are already switched over, deleting, or failed
			switch bgInfo.Status {
			case "PROVISIONING", "AVAILABLE":
				e.addEvent(op.ID, "info", fmt.Sprintf("Adopting existing Blue-Green deployment: %s (status: %s)", bgInfo.Identifier, bgInfo.Status), nil)

				result, _ := json.Marshal(map[string]any{
					"deployment_identifier": bgInfo.Identifier,
					"deployment_name":       bgInfo.Name,
					"source_arn":            bgInfo.Source,
					"target_arn":            bgInfo.Target,
					"status":                bgInfo.Status,
					"adopted":               true,
				})
				step.Result = result
				return nil
			}
		}
	}

	// No existing deployment found, create a new one
	// Use parameter group names from step params (set by prepare_parameter_group step)
	// or fall back to finding them from the prepare step result
	clusterPGName := params.TargetClusterParameterGroupName
	instancePGName := params.TargetInstanceParameterGroupName
	if clusterPGName == "" || instancePGName == "" {
		prepClusterPG, prepInstancePG := e.findPreparedParameterGroupNames(op)
		if clusterPGName == "" {
			clusterPGName = prepClusterPG
		}
		if instancePGName == "" {
			instancePGName = prepInstancePG
		}
	}

	// Generate deployment name (max 60 chars per AWS limit)
	versionSuffix := "-upgrade-" + strings.ReplaceAll(params.TargetEngineVersion, ".", "-")
	maxClusterLen := 60 - len(versionSuffix)
	clusterPrefix := op.ClusterID
	if len(clusterPrefix) > maxClusterLen {
		clusterPrefix = clusterPrefix[:maxClusterLen]
	}
	deploymentName := clusterPrefix + versionSuffix

	e.addEvent(op.ID, "info", fmt.Sprintf("Creating Blue-Green deployment: %s", deploymentName), nil)

	bgParams := rds.CreateBlueGreenDeploymentParams{
		DeploymentName:                     deploymentName,
		SourceClusterARN:                   clusterARN,
		TargetEngineVersion:                params.TargetEngineVersion,
		TargetDBClusterParameterGroupName:  clusterPGName,
		TargetDBInstanceParameterGroupName: instancePGName,
	}

	bgInfo, err := rdsClient.CreateBlueGreenDeployment(ctx, bgParams)
	if err != nil {
		return err
	}

	e.addEvent(op.ID, "info", fmt.Sprintf("Blue-Green deployment created: %s (status: %s)", bgInfo.Identifier, bgInfo.Status), nil)

	result, _ := json.Marshal(map[string]any{
		"deployment_identifier": bgInfo.Identifier,
		"deployment_name":       bgInfo.Name,
		"source_arn":            bgInfo.Source,
		"target_arn":            bgInfo.Target,
		"status":                bgInfo.Status,
	})
	step.Result = result
	return nil
}

// handleWaitBlueGreenAvailable waits for the Blue-Green deployment to be available.
func (e *Engine) handleWaitBlueGreenAvailable(ctx context.Context, op *types.Operation, step *types.Step) error {
	rdsClient, err := e.getRDSClient(ctx, op)
	if err != nil {
		return err
	}

	// Get deployment identifier from previous step
	deploymentID := e.findBlueGreenDeploymentID(op)
	if deploymentID == "" {
		return errors.Wrap(internalerrors.ErrInvalidParameter, "blue-green deployment identifier not found")
	}

	step.WaitCondition = "waiting for Blue-Green deployment to be available"
	step.State = types.StepStateWaiting

	timeout := time.After(e.getWaitTimeout(op))
	ticker := time.NewTicker(e.defaultPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return errors.Wrapf(internalerrors.ErrWaitTimeout, "Blue-Green deployment %s", deploymentID)
		case <-ticker.C:
			bgInfo, err := rdsClient.DescribeBlueGreenDeployment(ctx, deploymentID)
			if err != nil {
				// Transient errors are expected, continue polling
				continue
			}

			// Update wait condition with current status
			step.WaitCondition = fmt.Sprintf("Blue-Green status: %s", bgInfo.Status)

			// Log task progress
			for _, task := range bgInfo.Tasks {
				if task.Status == "IN_PROGRESS" {
					step.WaitCondition = fmt.Sprintf("Blue-Green: %s (%s)", task.Name, task.Status)
				}
			}

			switch bgInfo.Status {
			case "AVAILABLE":
				// Check that all tasks are complete before allowing switchover
				// Even when status is AVAILABLE, tasks might still be IN_PROGRESS
				allTasksComplete := true
				for _, task := range bgInfo.Tasks {
					if task.Status == "IN_PROGRESS" || task.Status == "PENDING" {
						allTasksComplete = false
						step.WaitCondition = fmt.Sprintf("Blue-Green: waiting for task %s (%s)", task.Name, task.Status)
						break
					}
					if task.Status == "FAILED" {
						return errors.Errorf("Blue-Green deployment task %s failed", task.Name)
					}
				}

				if !allTasksComplete {
					continue // Keep polling until all tasks complete
				}

				e.addEvent(op.ID, "info", "Blue-Green deployment is available and all tasks complete, ready for switchover", nil)
				result, _ := json.Marshal(map[string]any{
					"deployment_identifier": bgInfo.Identifier,
					"status":                bgInfo.Status,
					"target_arn":            bgInfo.Target,
				})
				step.Result = result
				return nil
			case "INVALID_CONFIGURATION", "PROVISIONING_FAILED":
				return errors.Errorf("Blue-Green deployment failed with status: %s (%s)", bgInfo.Status, bgInfo.StatusDetails)
			}
		}
	}
}

// handleSwitchoverBlueGreen performs the Blue-Green switchover.
func (e *Engine) handleSwitchoverBlueGreen(ctx context.Context, op *types.Operation, step *types.Step) error {
	rdsClient, err := e.getRDSClient(ctx, op)
	if err != nil {
		return err
	}

	var params struct {
		SwitchoverTimeout int `json:"switchover_timeout,omitempty"`
	}
	if len(step.Parameters) > 0 {
		if err := json.Unmarshal(step.Parameters, &params); err != nil {
			return errors.Wrap(err, "unmarshal params")
		}
	}

	// Default switchover timeout
	if params.SwitchoverTimeout == 0 {
		params.SwitchoverTimeout = 300 // 5 minutes
	}

	// Get deployment identifier from previous step
	deploymentID := e.findBlueGreenDeploymentID(op)
	if deploymentID == "" {
		return errors.Wrap(internalerrors.ErrInvalidParameter, "blue-green deployment identifier not found")
	}

	// Check if switchover has already completed (idempotency check)
	// This handles cases where the step is retried after switchover already succeeded
	bgInfo, err := rdsClient.DescribeBlueGreenDeployment(ctx, deploymentID)
	if err == nil {
		switch bgInfo.Status {
		case "SWITCHOVER_COMPLETED":
			e.addEvent(op.ID, "info", "Blue-Green switchover already completed", nil)
			result, _ := json.Marshal(map[string]any{
				"deployment_identifier": bgInfo.Identifier,
				"status":                bgInfo.Status,
				"switchover_details":    bgInfo.SwitchoverDetails,
			})
			step.Result = result
			return nil
		case "SWITCHOVER_IN_PROGRESS":
			e.addEvent(op.ID, "info", "Blue-Green switchover already in progress, waiting for completion", nil)
			// Skip initiating switchover, just wait for completion
			goto waitForCompletion
		case "SWITCHOVER_FAILED":
			return errors.Errorf("switchover previously failed: %s", bgInfo.StatusDetails)
		}
	}

	e.addEvent(op.ID, "info", fmt.Sprintf("Initiating Blue-Green switchover for deployment %s", deploymentID), nil)

	// Initiate switchover
	if err := rdsClient.SwitchoverBlueGreenDeployment(ctx, deploymentID, params.SwitchoverTimeout); err != nil {
		return err
	}

waitForCompletion:

	// Wait for switchover to complete
	step.WaitCondition = "waiting for switchover to complete"
	step.State = types.StepStateWaiting

	timeout := time.After(e.getWaitTimeout(op))
	ticker := time.NewTicker(e.defaultPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return errors.Wrapf(internalerrors.ErrWaitTimeout, "switchover for deployment %s", deploymentID)
		case <-ticker.C:
			bgInfo, err := rdsClient.DescribeBlueGreenDeployment(ctx, deploymentID)
			if err != nil {
				continue
			}

			step.WaitCondition = fmt.Sprintf("switchover status: %s", bgInfo.Status)

			switch bgInfo.Status {
			case "SWITCHOVER_COMPLETED":
				e.addEvent(op.ID, "info", "Blue-Green switchover completed successfully", nil)
				result, _ := json.Marshal(map[string]any{
					"deployment_identifier": bgInfo.Identifier,
					"status":                bgInfo.Status,
					"switchover_details":    bgInfo.SwitchoverDetails,
				})
				step.Result = result
				return nil
			case "SWITCHOVER_FAILED":
				return errors.Errorf("switchover failed: %s", bgInfo.StatusDetails)
			}
		}
	}
}

// handleCleanupBlueGreen cleans up the Blue-Green deployment and old cluster resources.
func (e *Engine) handleCleanupBlueGreen(ctx context.Context, op *types.Operation, step *types.Step) error {
	rdsClient, err := e.getRDSClient(ctx, op)
	if err != nil {
		return err
	}

	// Get deployment identifier
	deploymentID := e.findBlueGreenDeploymentID(op)
	if deploymentID == "" {
		return errors.Wrap(internalerrors.ErrInvalidParameter, "blue-green deployment identifier not found")
	}

	// Get the switchover details from the switchover step result (most reliable source)
	oldInstances, oldClusterID := e.findSwitchoverDetails(op)

	// Track if deployment still exists (needed for cleanup decisions)
	deploymentExists := true

	// If we couldn't get switchover details from the step, try describing the deployment
	if len(oldInstances) == 0 || oldClusterID == "" {
		bgInfo, err := rdsClient.DescribeBlueGreenDeployment(ctx, deploymentID)
		if err != nil {
			if errors.Is(err, internalerrors.ErrBlueGreenDeploymentNotFound) {
				deploymentExists = false
				// Deployment is gone but we don't have switchover details - we can't identify old resources
				// Try to infer from the original cluster ID (old resources get -old1 suffix)
				e.addEvent(op.ID, "warning", "Blue-Green deployment already deleted but switchover details not found in operation state", nil)
				oldClusterID = op.ClusterID // The original cluster becomes the "old" one after switchover
			} else {
				return errors.Wrap(err, "describe blue-green deployment for cleanup")
			}
		} else {
			// Extract switchover details from the deployment info
			for _, detail := range bgInfo.SwitchoverDetails {
				if strings.Contains(detail.SourceMember, ":db:") {
					parts := strings.Split(detail.SourceMember, ":db:")
					if len(parts) == 2 {
						oldInstances = append(oldInstances, parts[1])
					}
				} else if strings.Contains(detail.SourceMember, ":cluster:") {
					parts := strings.Split(detail.SourceMember, ":cluster:")
					if len(parts) == 2 {
						oldClusterID = parts[1]
					}
				}
			}
		}
	}

	e.addEvent(op.ID, "info", fmt.Sprintf("Cleaning up Blue-Green deployment %s (old cluster: %s, old instances: %v)", deploymentID, oldClusterID, oldInstances), nil)

	// Step 1: Delete the Blue-Green deployment record (if it still exists)
	// After switchover, we cannot use --delete-target, so just delete the deployment record
	if deploymentExists {
		if err := rdsClient.DeleteBlueGreenDeployment(ctx, deploymentID, false); err != nil {
			// If not found, that's fine - it was already deleted
			if !errors.Is(err, internalerrors.ErrBlueGreenDeploymentNotFound) && !strings.Contains(err.Error(), "NotFound") {
				// If cleanup fails, we should pause and let user decide
				e.addEvent(op.ID, "warning", fmt.Sprintf("Failed to delete Blue-Green deployment: %v", err), nil)
				op.State = types.StatePaused
				op.PauseReason = fmt.Sprintf("Cleanup failed: could not delete Blue-Green deployment %s: %v. The upgrade was successful but old resources may still exist. Select 'mark_complete' to complete the operation anyway, or 'abort' to stop.", deploymentID, err)
				return errors.Wrap(internalerrors.ErrInterventionRequired, "cleanup failed")
			}
			e.addEvent(op.ID, "info", "Blue-Green deployment record already deleted", nil)
		} else {
			e.addEvent(op.ID, "info", "Blue-Green deployment record deleted", nil)
		}
	} else {
		e.addEvent(op.ID, "info", "Blue-Green deployment record already deleted", nil)
	}

	// Step 2: Delete old instances (they have -old1 suffix after switchover)
	var deletedInstances []string
	var failedDeletes []string

	for _, instID := range oldInstances {
		// The old instances are renamed with -old1 suffix after switchover
		// Check if already has the suffix (in case we got it from post-switchover data)
		oldInstID := instID
		if !strings.HasSuffix(instID, "-old1") {
			oldInstID = instID + "-old1"
		}
		e.addEvent(op.ID, "info", fmt.Sprintf("Deleting old instance: %s", oldInstID), nil)

		if err := rdsClient.DeleteInstance(ctx, oldInstID, true); err != nil {
			// Treat "not found" as success - resource is already gone
			if strings.Contains(err.Error(), "NotFound") || strings.Contains(err.Error(), "not found") {
				e.addEvent(op.ID, "info", fmt.Sprintf("Old instance %s already deleted or does not exist", oldInstID), nil)
				deletedInstances = append(deletedInstances, oldInstID)
			} else {
				e.addEvent(op.ID, "warning", fmt.Sprintf("Failed to delete old instance %s: %v", oldInstID, err), nil)
				failedDeletes = append(failedDeletes, oldInstID)
			}
		} else {
			deletedInstances = append(deletedInstances, oldInstID)
		}
	}

	// Step 3: Delete old cluster (it has -old1 suffix after switchover)
	if oldClusterID != "" {
		// Check if already has the suffix (in case we got it from post-switchover data)
		oldClusterIDRenamed := oldClusterID
		if !strings.HasSuffix(oldClusterID, "-old1") {
			oldClusterIDRenamed = oldClusterID + "-old1"
		}
		e.addEvent(op.ID, "info", fmt.Sprintf("Deleting old cluster: %s", oldClusterIDRenamed), nil)

		// Wait a bit for instances to start deleting before trying to delete cluster
		time.Sleep(2 * time.Second)

		if err := rdsClient.DeleteCluster(ctx, oldClusterIDRenamed, true); err != nil {
			// Treat "not found" as success - resource is already gone
			if strings.Contains(err.Error(), "NotFound") || strings.Contains(err.Error(), "not found") {
				e.addEvent(op.ID, "info", fmt.Sprintf("Old cluster %s already deleted or does not exist", oldClusterIDRenamed), nil)
			} else {
				e.addEvent(op.ID, "warning", fmt.Sprintf("Failed to delete old cluster %s: %v", oldClusterIDRenamed, err), nil)
				failedDeletes = append(failedDeletes, oldClusterIDRenamed)
			}
		} else {
			e.addEvent(op.ID, "info", fmt.Sprintf("Old cluster %s deletion initiated", oldClusterIDRenamed), nil)
		}
	}

	// If any deletes failed (excluding "not found" errors), pause for intervention
	if len(failedDeletes) > 0 {
		op.State = types.StatePaused
		op.PauseReason = fmt.Sprintf("Cleanup partially failed: could not delete %v. The upgrade was successful but these resources may still exist and incur charges. Select 'mark_complete' to complete the operation anyway, or 'abort' to stop.", failedDeletes)
		return errors.Wrap(internalerrors.ErrInterventionRequired, "cleanup partially failed")
	}

	result, _ := json.Marshal(map[string]any{
		"deleted_deployment": deploymentID,
		"deleted_instances":  deletedInstances,
		"old_cluster":        oldClusterID,
	})
	step.Result = result

	e.addEvent(op.ID, "info", "Blue-Green cleanup completed successfully", nil)
	return nil
}

// findBlueGreenDeploymentID finds the Blue-Green deployment identifier from previous steps.
func (e *Engine) findBlueGreenDeploymentID(op *types.Operation) string {
	for _, step := range op.Steps {
		if step.Action == "create_blue_green_deployment" && step.State == types.StepStateCompleted {
			var result struct {
				DeploymentIdentifier string `json:"deployment_identifier"`
			}
			if err := json.Unmarshal(step.Result, &result); err == nil {
				return result.DeploymentIdentifier
			}
		}
	}
	return ""
}

// findPreparedParameterGroupNames finds both cluster and instance parameter group names from the prepare step.
func (e *Engine) findPreparedParameterGroupNames(op *types.Operation) (clusterPG, instancePG string) {
	for _, step := range op.Steps {
		if step.Action == "prepare_parameter_group" && step.State == types.StepStateCompleted {
			var result struct {
				ClusterParameterGroup struct {
					Name string `json:"name"`
				} `json:"cluster_parameter_group"`
				InstanceParameterGroup struct {
					Name string `json:"name"`
				} `json:"instance_parameter_group"`
			}
			if err := json.Unmarshal(step.Result, &result); err == nil {
				return result.ClusterParameterGroup.Name, result.InstanceParameterGroup.Name
			}
		}
	}
	return "", ""
}

// findSwitchoverDetails extracts old instance IDs and cluster ID from the switchover step result.
func (e *Engine) findSwitchoverDetails(op *types.Operation) (oldInstances []string, oldClusterID string) {
	for _, step := range op.Steps {
		if step.Action == "switchover_blue_green" && step.State == types.StepStateCompleted {
			var result struct {
				SwitchoverDetails []struct {
					SourceMember string `json:"source_member"`
				} `json:"switchover_details"`
			}
			if err := json.Unmarshal(step.Result, &result); err == nil {
				for _, detail := range result.SwitchoverDetails {
					if strings.Contains(detail.SourceMember, ":db:") {
						parts := strings.Split(detail.SourceMember, ":db:")
						if len(parts) == 2 {
							oldInstances = append(oldInstances, parts[1])
						}
					} else if strings.Contains(detail.SourceMember, ":cluster:") {
						parts := strings.Split(detail.SourceMember, ":cluster:")
						if len(parts) == 2 {
							oldClusterID = parts[1]
						}
					}
				}
			}
		}
	}
	return oldInstances, oldClusterID
}

// handleRebootInstance reboots an RDS instance.
func (e *Engine) handleRebootInstance(ctx context.Context, op *types.Operation, step *types.Step) error {
	rdsClient, err := e.getRDSClient(ctx, op)
	if err != nil {
		return err
	}

	var params struct {
		InstanceID string `json:"instance_id"`
	}
	if err := json.Unmarshal(step.Parameters, &params); err != nil {
		return errors.Wrap(err, "unmarshal params")
	}

	if params.InstanceID == "" {
		return errors.New("instance_id is required")
	}

	e.logger.Info("rebooting instance",
		"operation_id", op.ID,
		"instance_id", params.InstanceID)

	err = rdsClient.RebootInstance(ctx, params.InstanceID)
	if err != nil {
		return errors.Wrap(err, "reboot instance")
	}

	result, _ := json.Marshal(map[string]string{"instance_id": params.InstanceID})
	step.Result = result
	return nil
}

// ==================== RDS Proxy Handlers ====================

// handleValidateProxyHealth discovers and validates RDS Proxies targeting this cluster.
// This step runs before switchover to ensure proxies are healthy.
// If no proxies are found, the step succeeds with an empty result.
// If proxies are found but unhealthy, the step fails.
func (e *Engine) handleValidateProxyHealth(ctx context.Context, op *types.Operation, step *types.Step) error {
	rdsClient, err := e.getRDSClient(ctx, op)
	if err != nil {
		return err
	}

	e.logger.Info("discovering RDS Proxies for cluster",
		"operation_id", op.ID,
		"cluster_id", op.ClusterID)

	// Discover proxies pointing at this cluster
	proxies, err := rdsClient.FindProxiesForCluster(ctx, op.ClusterID)
	if err != nil {
		return errors.Wrap(err, "find proxies for cluster")
	}

	if len(proxies) == 0 {
		e.logger.Info("no RDS Proxies found targeting this cluster",
			"operation_id", op.ID,
			"cluster_id", op.ClusterID)
		e.addEvent(op.ID, "info", "No RDS Proxies found targeting this cluster", nil)

		// Store empty result indicating no proxies
		result, _ := json.Marshal(map[string]any{
			"proxies_found": 0,
			"proxies":       []any{},
		})
		step.Result = result
		return nil
	}

	e.logger.Info("found RDS Proxies targeting cluster",
		"operation_id", op.ID,
		"cluster_id", op.ClusterID,
		"proxy_count", len(proxies))

	// Validate health of each proxy
	var healthyProxies []rds.ProxyWithTargets
	for _, proxy := range proxies {
		if err := rdsClient.ValidateProxyHealth(ctx, proxy); err != nil {
			e.addEvent(op.ID, "error", fmt.Sprintf("RDS Proxy %s is unhealthy: %v", proxy.Proxy.ProxyName, err), nil)
			return errors.Wrapf(err, "proxy %s health validation failed", proxy.Proxy.ProxyName)
		}
		healthyProxies = append(healthyProxies, proxy)
		e.logger.Info("RDS Proxy is healthy",
			"operation_id", op.ID,
			"proxy_name", proxy.Proxy.ProxyName,
			"status", proxy.Proxy.Status)
	}

	e.addEvent(op.ID, "info", fmt.Sprintf("Validated %d RDS Proxy(ies) as healthy", len(healthyProxies)), nil)

	// Store proxy info for use by retarget step
	result, _ := json.Marshal(map[string]any{
		"proxies_found": len(healthyProxies),
		"proxies":       healthyProxies,
	})
	step.Result = result
	return nil
}

// handleRetargetProxies retargets RDS Proxies to the new cluster after switchover.
// It reads proxy info from the validate_proxy_health step result.
// If no proxies were found, the step succeeds immediately.
// DEPRECATED: Use handleRegisterProxyTargets for new operations.
func (e *Engine) handleRetargetProxies(ctx context.Context, op *types.Operation, step *types.Step) error {
	rdsClient, err := e.getRDSClient(ctx, op)
	if err != nil {
		return err
	}

	// Get proxy info from the validation step
	proxies := e.findDiscoveredProxies(op)

	if len(proxies) == 0 {
		e.logger.Info("no proxies to retarget",
			"operation_id", op.ID)
		e.addEvent(op.ID, "info", "No RDS Proxies to retarget", nil)

		result, _ := json.Marshal(map[string]any{
			"proxies_retargeted": 0,
		})
		step.Result = result
		return nil
	}

	// The new cluster ID is the same as the original - after Blue-Green switchover,
	// the green cluster takes the name of the original blue cluster
	newClusterID := op.ClusterID

	// Check if targets are already registered and available (idempotency check)
	// This handles cases where the step is retried after retargeting already succeeded
	allAlreadyAvailable := true
	for _, proxy := range proxies {
		for _, tg := range proxy.TargetGroups {
			targets, err := rdsClient.GetProxyTargets(ctx, proxy.Proxy.ProxyName, tg.TargetGroupName)
			if err != nil {
				e.logger.Info("failed to get proxy targets for idempotency check",
					"operation_id", op.ID,
					"proxy_name", proxy.Proxy.ProxyName,
					"error", err)
				allAlreadyAvailable = false
				break
			}

			// Check if our cluster is registered (TRACKED_CLUSTER target exists)
			// and all instance targets are healthy
			// NOTE: TRACKED_CLUSTER targets don't have TargetHealth - only RDS_INSTANCE targets do
			clusterRegistered := false
			hasInstanceTargets := false
			allInstancesAvailable := true

			for _, target := range targets {
				// For TRACKED_CLUSTER targets, check TrackedClusterID (preferred) or fall back to RDSResourceID
				if target.Type == "TRACKED_CLUSTER" {
					if target.TrackedClusterID == newClusterID || target.RDSResourceID == newClusterID {
						clusterRegistered = true
					}
				}
				if target.Type == "RDS_INSTANCE" {
					hasInstanceTargets = true
					if target.TargetHealth != "AVAILABLE" {
						allInstancesAvailable = false
					}
				}
			}

			// Consider it ready if cluster is registered AND either:
			// - There are instance targets and they're all available, OR
			// - There are no instance targets yet (just tracked cluster)
			if !clusterRegistered || (hasInstanceTargets && !allInstancesAvailable) {
				allAlreadyAvailable = false
			}
		}
		if !allAlreadyAvailable {
			break
		}
	}

	if allAlreadyAvailable {
		e.addEvent(op.ID, "info", fmt.Sprintf("Cluster already registered to %d RDS Proxy(ies) and targets are available", len(proxies)), nil)
		var proxyNames []string
		for _, proxy := range proxies {
			proxyNames = append(proxyNames, proxy.Proxy.ProxyName)
		}
		result, _ := json.Marshal(map[string]any{
			"proxies_retargeted": len(proxies),
			"proxy_names":        proxyNames,
			"new_cluster_id":     newClusterID,
			"already_retargeted": true,
		})
		step.Result = result
		return nil
	}

	e.logger.Info("retargeting RDS Proxies to new cluster",
		"operation_id", op.ID,
		"new_cluster_id", newClusterID,
		"proxy_count", len(proxies))

	var retargetedProxies []string
	for _, proxy := range proxies {
		for _, tg := range proxy.TargetGroups {
			e.logger.Info("retargeting proxy target group",
				"operation_id", op.ID,
				"proxy_name", proxy.Proxy.ProxyName,
				"target_group", tg.TargetGroupName,
				"new_cluster_id", newClusterID)

			if err := rdsClient.RetargetProxyToCluster(ctx, proxy.Proxy.ProxyName, tg.TargetGroupName, newClusterID); err != nil {
				// Ignore "already registered" errors
				if !strings.Contains(err.Error(), "already registered") && !strings.Contains(err.Error(), "DBProxyTargetAlreadyRegisteredFault") {
					e.addEvent(op.ID, "error", fmt.Sprintf("Failed to retarget proxy %s: %v", proxy.Proxy.ProxyName, err), nil)
					return errors.Wrapf(err, "retarget proxy %s target group %s", proxy.Proxy.ProxyName, tg.TargetGroupName)
				}
				e.addEvent(op.ID, "info", fmt.Sprintf("Cluster already registered to proxy %s target group %s",
					proxy.Proxy.ProxyName, tg.TargetGroupName), nil)
			} else {
				e.addEvent(op.ID, "info", fmt.Sprintf("Retargeted proxy %s target group %s to cluster %s",
					proxy.Proxy.ProxyName, tg.TargetGroupName, newClusterID), nil)
			}
		}
		retargetedProxies = append(retargetedProxies, proxy.Proxy.ProxyName)
	}

	// Wait for all proxy targets to become available
	e.logger.Info("waiting for proxy targets to become available",
		"operation_id", op.ID)
	step.WaitCondition = "waiting for proxy targets to become available"
	step.State = types.StepStateWaiting

	for _, proxy := range proxies {
		for _, tg := range proxy.TargetGroups {
			if err := rdsClient.WaitForProxyTargetsAvailable(ctx, proxy.Proxy.ProxyName, tg.TargetGroupName, 5*time.Minute); err != nil {
				e.addEvent(op.ID, "error", fmt.Sprintf("Proxy %s targets did not become available: %v", proxy.Proxy.ProxyName, err), nil)
				return errors.Wrapf(err, "wait for proxy %s targets", proxy.Proxy.ProxyName)
			}
		}
	}

	e.addEvent(op.ID, "info", fmt.Sprintf("Successfully retargeted %d RDS Proxy(ies)", len(retargetedProxies)), nil)

	result, _ := json.Marshal(map[string]any{
		"proxies_retargeted": len(retargetedProxies),
		"proxy_names":        retargetedProxies,
		"new_cluster_id":     newClusterID,
	})
	step.Result = result
	return nil
}

// findDiscoveredProxies extracts proxy info from the validate_proxy_health step result.
func (e *Engine) findDiscoveredProxies(op *types.Operation) []rds.ProxyWithTargets {
	for _, step := range op.Steps {
		if step.Action == "validate_proxy_health" && step.State == types.StepStateCompleted {
			var result struct {
				Proxies []rds.ProxyWithTargets `json:"proxies"`
			}
			if err := json.Unmarshal(step.Result, &result); err == nil {
				return result.Proxies
			}
		}
	}
	return nil
}

// handleDeregisterProxyTargets deregisters the cluster from all discovered proxy target groups.
// This step runs BEFORE Blue-Green deployment creation to work around the AWS limitation
// that Blue-Green deployments don't support clusters with RDS Proxy targets.
// If no proxies were found, the step succeeds immediately.
func (e *Engine) handleDeregisterProxyTargets(ctx context.Context, op *types.Operation, step *types.Step) error {
	rdsClient, err := e.getRDSClient(ctx, op)
	if err != nil {
		return err
	}

	// Get proxy info from the validation step
	proxies := e.findDiscoveredProxies(op)

	if len(proxies) == 0 {
		e.logger.Info("no proxies to deregister",
			"operation_id", op.ID)
		e.addEvent(op.ID, "info", "No RDS Proxies to deregister", nil)

		result, _ := json.Marshal(map[string]any{
			"proxies_deregistered": 0,
		})
		step.Result = result
		return nil
	}

	e.logger.Info("deregistering cluster from RDS Proxies",
		"operation_id", op.ID,
		"cluster_id", op.ClusterID,
		"proxy_count", len(proxies))

	e.addEvent(op.ID, "warning", fmt.Sprintf("Deregistering cluster from %d RDS Proxy(ies) - proxy connections will fail until re-registered after upgrade", len(proxies)), nil)

	var deregisteredProxies []string
	for _, proxy := range proxies {
		for _, tg := range proxy.TargetGroups {
			e.logger.Info("deregistering cluster from proxy target group",
				"operation_id", op.ID,
				"proxy_name", proxy.Proxy.ProxyName,
				"target_group", tg.TargetGroupName,
				"cluster_id", op.ClusterID)

			if err := rdsClient.DeregisterProxyTargets(ctx, proxy.Proxy.ProxyName, tg.TargetGroupName, op.ClusterID); err != nil {
				e.addEvent(op.ID, "error", fmt.Sprintf("Failed to deregister from proxy %s: %v", proxy.Proxy.ProxyName, err), nil)
				return errors.Wrapf(err, "deregister from proxy %s target group %s", proxy.Proxy.ProxyName, tg.TargetGroupName)
			}

			e.addEvent(op.ID, "info", fmt.Sprintf("Deregistered cluster from proxy %s target group %s",
				proxy.Proxy.ProxyName, tg.TargetGroupName), nil)
		}
		deregisteredProxies = append(deregisteredProxies, proxy.Proxy.ProxyName)
	}

	e.addEvent(op.ID, "info", fmt.Sprintf("Successfully deregistered cluster from %d RDS Proxy(ies)", len(deregisteredProxies)), nil)

	result, _ := json.Marshal(map[string]any{
		"proxies_deregistered": len(deregisteredProxies),
		"proxy_names":          deregisteredProxies,
		"cluster_id":           op.ClusterID,
	})
	step.Result = result
	return nil
}

// handleRegisterProxyTargets registers the cluster to all discovered proxy target groups.
// This step runs AFTER Blue-Green switchover to restore proxy connectivity.
// If no proxies were found, the step succeeds immediately.
func (e *Engine) handleRegisterProxyTargets(ctx context.Context, op *types.Operation, step *types.Step) error {
	rdsClient, err := e.getRDSClient(ctx, op)
	if err != nil {
		return err
	}

	// Get proxy info from the validation step
	proxies := e.findDiscoveredProxies(op)

	if len(proxies) == 0 {
		e.logger.Info("no proxies to register",
			"operation_id", op.ID)
		e.addEvent(op.ID, "info", "No RDS Proxies to register", nil)

		result, _ := json.Marshal(map[string]any{
			"proxies_registered": 0,
		})
		step.Result = result
		return nil
	}

	// After Blue-Green switchover, the new cluster has the original name
	clusterID := op.ClusterID

	// Check if targets are already registered and available (idempotency check)
	// This handles cases where the step is retried after registration already succeeded
	allAlreadyAvailable := true
	for _, proxy := range proxies {
		for _, tg := range proxy.TargetGroups {
			targets, err := rdsClient.GetProxyTargets(ctx, proxy.Proxy.ProxyName, tg.TargetGroupName)
			if err != nil {
				allAlreadyAvailable = false
				break
			}

			// Check if our cluster is registered (TRACKED_CLUSTER target exists)
			// and all instance targets are healthy
			// NOTE: TRACKED_CLUSTER targets don't have TargetHealth - only RDS_INSTANCE targets do
			clusterRegistered := false
			hasInstanceTargets := false
			allInstancesAvailable := true

			for _, target := range targets {
				// For TRACKED_CLUSTER targets, check TrackedClusterID (preferred) or fall back to RDSResourceID
				if target.Type == "TRACKED_CLUSTER" {
					if target.TrackedClusterID == clusterID || target.RDSResourceID == clusterID {
						clusterRegistered = true
					}
				}
				if target.Type == "RDS_INSTANCE" {
					hasInstanceTargets = true
					if target.TargetHealth != "AVAILABLE" {
						allInstancesAvailable = false
					}
				}
			}

			// Consider it ready if cluster is registered AND either:
			// - There are instance targets and they're all available, OR
			// - There are no instance targets yet (just tracked cluster)
			if !clusterRegistered || (hasInstanceTargets && !allInstancesAvailable) {
				allAlreadyAvailable = false
			}
		}
		if !allAlreadyAvailable {
			break
		}
	}

	if allAlreadyAvailable {
		e.addEvent(op.ID, "info", fmt.Sprintf("Cluster already registered to %d RDS Proxy(ies) and targets are available", len(proxies)), nil)
		var proxyNames []string
		for _, proxy := range proxies {
			proxyNames = append(proxyNames, proxy.Proxy.ProxyName)
		}
		result, _ := json.Marshal(map[string]any{
			"proxies_registered": len(proxies),
			"proxy_names":        proxyNames,
			"cluster_id":         clusterID,
			"already_registered": true,
		})
		step.Result = result
		return nil
	}

	e.logger.Info("registering cluster to RDS Proxies",
		"operation_id", op.ID,
		"cluster_id", clusterID,
		"proxy_count", len(proxies))

	var registeredProxies []string
	for _, proxy := range proxies {
		for _, tg := range proxy.TargetGroups {
			e.logger.Info("registering cluster to proxy target group",
				"operation_id", op.ID,
				"proxy_name", proxy.Proxy.ProxyName,
				"target_group", tg.TargetGroupName,
				"cluster_id", clusterID)

			if err := rdsClient.RegisterProxyTargets(ctx, proxy.Proxy.ProxyName, tg.TargetGroupName, clusterID); err != nil {
				// Ignore "already registered" errors
				if !strings.Contains(err.Error(), "already registered") && !strings.Contains(err.Error(), "DBProxyTargetAlreadyRegisteredFault") {
					e.addEvent(op.ID, "error", fmt.Sprintf("Failed to register to proxy %s: %v", proxy.Proxy.ProxyName, err), nil)
					return errors.Wrapf(err, "register to proxy %s target group %s", proxy.Proxy.ProxyName, tg.TargetGroupName)
				}
				e.addEvent(op.ID, "info", fmt.Sprintf("Cluster already registered to proxy %s target group %s",
					proxy.Proxy.ProxyName, tg.TargetGroupName), nil)
			} else {
				e.addEvent(op.ID, "info", fmt.Sprintf("Registered cluster to proxy %s target group %s",
					proxy.Proxy.ProxyName, tg.TargetGroupName), nil)
			}
		}
		registeredProxies = append(registeredProxies, proxy.Proxy.ProxyName)
	}

	// Wait for all proxy targets to become available
	e.logger.Info("waiting for proxy targets to become available",
		"operation_id", op.ID)
	step.WaitCondition = "waiting for proxy targets to become available"
	step.State = types.StepStateWaiting

	for _, proxy := range proxies {
		for _, tg := range proxy.TargetGroups {
			if err := rdsClient.WaitForProxyTargetsAvailable(ctx, proxy.Proxy.ProxyName, tg.TargetGroupName, 5*time.Minute); err != nil {
				e.addEvent(op.ID, "error", fmt.Sprintf("Proxy %s targets did not become available: %v", proxy.Proxy.ProxyName, err), nil)
				return errors.Wrapf(err, "wait for proxy %s targets", proxy.Proxy.ProxyName)
			}
		}
	}

	e.addEvent(op.ID, "info", fmt.Sprintf("Successfully registered cluster to %d RDS Proxy(ies) and targets are available", len(registeredProxies)), nil)

	result, _ := json.Marshal(map[string]any{
		"proxies_registered": len(registeredProxies),
		"proxy_names":        registeredProxies,
		"cluster_id":         clusterID,
	})
	step.Result = result
	return nil
}
