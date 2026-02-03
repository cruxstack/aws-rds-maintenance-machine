package machine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cockroachdb/errors"
	"github.com/google/uuid"
	internalerrors "github.com/mpz/devops/tools/rds-maint-machine/internal/errors"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/types"
)

// validateExcludedInstances checks that excluded instance IDs exist in the cluster
// and that not all instances are excluded. Returns:
// - excludeSet: map of excluded instance IDs for quick lookup
// - error if validation fails
func validateExcludedInstances(excludeIDs []string, instances []types.InstanceInfo) (map[string]bool, error) {
	// Build set of valid instance IDs
	validIDs := make(map[string]bool)
	nonAutoScaledCount := 0
	for _, inst := range instances {
		validIDs[inst.InstanceID] = true
		if !inst.IsAutoScaled {
			nonAutoScaledCount++
		}
	}

	// Build exclude set and validate each ID exists
	excludeSet := make(map[string]bool)
	var unknownIDs []string
	for _, id := range excludeIDs {
		if !validIDs[id] {
			unknownIDs = append(unknownIDs, id)
		}
		excludeSet[id] = true
	}

	if len(unknownIDs) > 0 {
		return nil, errors.Wrapf(internalerrors.ErrInvalidParameter,
			"excluded instance(s) not found in cluster: %v", unknownIDs)
	}

	// Count how many non-autoscaled instances will be modified
	modifiedCount := 0
	for _, inst := range instances {
		if !inst.IsAutoScaled && !excludeSet[inst.InstanceID] {
			modifiedCount++
		}
	}

	if modifiedCount == 0 {
		return nil, errors.Wrap(internalerrors.ErrInvalidParameter,
			"all non-autoscaled instances are excluded; nothing to modify")
	}

	return excludeSet, nil
}

// buildInstanceTypeChangeSteps builds the steps for an instance type change operation.
// This performs a zero-downtime instance type change by:
// 1. Creating a temp reader with the new instance type (unless SkipTempInstance is true)
// 2. Waiting for it to become available
// 3. Promoting it to writer (failover) - only if writer is being modified
// 4. Modifying all original non-autoscaled instances to the new type
// 5. Waiting for all instances to become available
// 6. Failing over back to original writer - only if writer was modified
// 7. Deleting the temp instance
func (e *Engine) buildInstanceTypeChangeSteps(ctx context.Context, op *types.Operation) error {
	var params types.InstanceTypeChangeParams
	if err := json.Unmarshal(op.Parameters, &params); err != nil {
		return errors.Wrap(err, "unmarshal params")
	}

	if params.TargetInstanceType == "" {
		return errors.New("missing required parameter: target_instance_type")
	}

	// Get RDS client for operation's region
	rdsClient, err := e.getRDSClient(ctx, op)
	if err != nil {
		return errors.Wrap(err, "get rds client")
	}

	// Get current cluster info to know instances
	info, err := rdsClient.GetClusterInfo(ctx, op.ClusterID)
	if err != nil {
		return errors.Wrap(err, "get cluster info")
	}

	// Validate excluded instances exist and not all are excluded
	excludeSet, err := validateExcludedInstances(params.ExcludeInstances, info.Instances)
	if err != nil {
		return err
	}

	// Check if the writer is excluded - if so, we don't need failover steps
	var originalWriter *types.InstanceInfo
	writerExcluded := false
	for _, instance := range info.Instances {
		if instance.Role == "writer" && !instance.IsAutoScaled {
			originalWriter = &instance
			if excludeSet[instance.InstanceID] {
				writerExcluded = true
			}
			break
		}
	}

	// Determine if we should create a temp instance
	// By default, create temp instance for redundancy unless explicitly skipped
	createTempInstance := !params.SkipTempInstance

	steps := []types.Step{}

	// Step 1: Get cluster info (for tracking)
	steps = append(steps, types.Step{
		ID:          uuid.New().String(),
		Name:        "Get cluster info",
		Description: "Retrieve current cluster state",
		State:       types.StepStatePending,
		Action:      "get_cluster_info",
		MaxRetries:  3,
	})

	// Create temp instance if enabled
	if createTempInstance {
		createParams, err := json.Marshal(map[string]string{
			"instance_type": params.TargetInstanceType,
			"engine":        info.Engine,
		})
		if err != nil {
			return errors.Wrap(err, "marshal create_temp_instance params")
		}
		steps = append(steps, types.Step{
			ID:          uuid.New().String(),
			Name:        "Create temp instance",
			Description: "Create temporary reader with new instance type: " + params.TargetInstanceType,
			State:       types.StepStatePending,
			Action:      "create_temp_instance",
			Parameters:  createParams,
			MaxRetries:  1,
		})

		steps = append(steps, types.Step{
			ID:          uuid.New().String(),
			Name:        "Wait for temp instance",
			Description: "Wait for temporary instance to become available",
			State:       types.StepStatePending,
			Action:      "wait_instance_available",
			MaxRetries:  1,
		})
	}

	// Only failover to temp instance if writer is NOT excluded (we need to modify it)
	if createTempInstance && !writerExcluded {
		steps = append(steps, types.Step{
			ID:          uuid.New().String(),
			Name:        "Failover to temp instance",
			Description: "Promote temporary instance to writer",
			State:       types.StepStatePending,
			Action:      "failover_to_instance",
			MaxRetries:  1,
		})

		steps = append(steps, types.Step{
			ID:          uuid.New().String(),
			Name:        "Wait for failover",
			Description: "Wait for cluster to stabilize after failover",
			State:       types.StepStatePending,
			Action:      "wait_cluster_available",
			MaxRetries:  1,
		})
	}

	// Steps: Modify each non-autoscaled instance
	for _, instance := range info.Instances {
		if instance.IsAutoScaled {
			continue // Skip autoscaled instances - they'll get new type from policy
		}
		if excludeSet[instance.InstanceID] {
			continue // Skip explicitly excluded instances
		}

		modifyParams, err := json.Marshal(map[string]string{
			"instance_id":   instance.InstanceID,
			"instance_type": params.TargetInstanceType,
		})
		if err != nil {
			return errors.Wrapf(err, "marshal modify_instance params for %s", instance.InstanceID)
		}
		steps = append(steps, types.Step{
			ID:          uuid.New().String(),
			Name:        "Modify instance: " + instance.InstanceID,
			Description: "Change instance type to " + params.TargetInstanceType,
			State:       types.StepStatePending,
			Action:      "modify_instance",
			Parameters:  modifyParams,
			MaxRetries:  2,
		})

		// Wait for instance to be available after modification
		// CRITICAL: instance_id MUST be set explicitly to ensure we wait for THIS instance,
		// not the temp instance. Without this, all modify operations could run in parallel.
		waitParams, err := json.Marshal(map[string]string{
			"instance_id": instance.InstanceID,
		})
		if err != nil {
			return errors.Wrapf(err, "marshal wait_instance_available params for %s", instance.InstanceID)
		}
		steps = append(steps, types.Step{
			ID:          uuid.New().String(),
			Name:        "Wait for instance: " + instance.InstanceID,
			Description: "Wait for instance modification to complete",
			State:       types.StepStatePending,
			Action:      "wait_instance_available",
			Parameters:  waitParams,
			MaxRetries:  1,
		})
	}

	// Only add failover-back steps if we did a failover (temp instance + writer not excluded)
	if createTempInstance && !writerExcluded && originalWriter != nil {
		failoverParams, err := json.Marshal(map[string]string{
			"instance_id": originalWriter.InstanceID,
		})
		if err != nil {
			return errors.Wrapf(err, "marshal failover params for %s", originalWriter.InstanceID)
		}
		steps = append(steps, types.Step{
			ID:          uuid.New().String(),
			Name:        "Failover back to original writer",
			Description: "Restore original writer: " + originalWriter.InstanceID,
			State:       types.StepStatePending,
			Action:      "failover_to_instance",
			Parameters:  failoverParams,
			MaxRetries:  1,
		})

		steps = append(steps, types.Step{
			ID:          uuid.New().String(),
			Name:        "Wait for final failover",
			Description: "Wait for cluster to stabilize",
			State:       types.StepStatePending,
			Action:      "wait_cluster_available",
			MaxRetries:  1,
		})
	}

	// Delete temp instance if we created one
	if createTempInstance {
		deleteParams, err := json.Marshal(map[string]bool{
			"skip_final_snapshot": true,
		})
		if err != nil {
			return errors.Wrap(err, "marshal delete_instance params")
		}
		steps = append(steps, types.Step{
			ID:          uuid.New().String(),
			Name:        "Delete temp instance",
			Description: "Remove temporary maintenance instance",
			State:       types.StepStatePending,
			Action:      "delete_instance",
			Parameters:  deleteParams,
			MaxRetries:  2,
		})

		steps = append(steps, types.Step{
			ID:          uuid.New().String(),
			Name:        "Wait for temp instance deletion",
			Description: "Wait for temporary instance to be deleted",
			State:       types.StepStatePending,
			Action:      "wait_instance_deleted",
			MaxRetries:  1,
		})
	}

	op.Steps = steps
	return nil
}

// buildStorageTypeChangeSteps builds the steps for a storage type change operation.
// Similar to instance type change but modifies storage type instead.
func (e *Engine) buildStorageTypeChangeSteps(ctx context.Context, op *types.Operation) error {
	var params types.StorageTypeChangeParams
	if err := json.Unmarshal(op.Parameters, &params); err != nil {
		return errors.Wrap(err, "unmarshal params")
	}

	if params.TargetStorageType == "" {
		return errors.New("missing required parameter: target_storage_type")
	}

	// Get RDS client for operation's region
	rdsClient, err := e.getRDSClient(ctx, op)
	if err != nil {
		return errors.Wrap(err, "get rds client")
	}

	// Get current cluster info
	info, err := rdsClient.GetClusterInfo(ctx, op.ClusterID)
	if err != nil {
		return errors.Wrap(err, "get cluster info")
	}

	if len(info.Instances) == 0 {
		return errors.New("cluster has no instances")
	}

	// Validate excluded instances exist and not all are excluded
	excludeSet, err := validateExcludedInstances(params.ExcludeInstances, info.Instances)
	if err != nil {
		return err
	}

	// Check if the writer is excluded - if so, we don't need failover steps
	var originalWriter *types.InstanceInfo
	writerExcluded := false
	for _, instance := range info.Instances {
		if instance.Role == "writer" && !instance.IsAutoScaled {
			originalWriter = &instance
			if excludeSet[instance.InstanceID] {
				writerExcluded = true
			}
			break
		}
	}

	// Determine if we should create a temp instance
	// By default, create temp instance for redundancy unless explicitly skipped
	createTempInstance := !params.SkipTempInstance

	steps := []types.Step{}

	// Step 1: Get cluster info
	steps = append(steps, types.Step{
		ID:          uuid.New().String(),
		Name:        "Get cluster info",
		Description: "Retrieve current cluster state",
		State:       types.StepStatePending,
		Action:      "get_cluster_info",
		MaxRetries:  3,
	})

	// Create temp instance if enabled
	if createTempInstance {
		createParams, err := json.Marshal(map[string]string{
			"instance_type": info.Instances[0].InstanceType, // Use same instance type
			"engine":        info.Engine,
		})
		if err != nil {
			return errors.Wrap(err, "marshal create_temp_instance params")
		}
		steps = append(steps, types.Step{
			ID:          uuid.New().String(),
			Name:        "Create temp instance",
			Description: "Create temporary reader for failover",
			State:       types.StepStatePending,
			Action:      "create_temp_instance",
			Parameters:  createParams,
			MaxRetries:  1,
		})

		steps = append(steps, types.Step{
			ID:          uuid.New().String(),
			Name:        "Wait for temp instance",
			Description: "Wait for temporary instance to become available",
			State:       types.StepStatePending,
			Action:      "wait_instance_available",
			MaxRetries:  1,
		})
	}

	// Only failover to temp instance if writer is NOT excluded (we need to modify it)
	if createTempInstance && !writerExcluded {
		steps = append(steps, types.Step{
			ID:          uuid.New().String(),
			Name:        "Failover to temp instance",
			Description: "Promote temporary instance to writer",
			State:       types.StepStatePending,
			Action:      "failover_to_instance",
			MaxRetries:  1,
		})

		steps = append(steps, types.Step{
			ID:          uuid.New().String(),
			Name:        "Wait for failover",
			Description: "Wait for cluster to stabilize after failover",
			State:       types.StepStatePending,
			Action:      "wait_cluster_available",
			MaxRetries:  1,
		})
	}

	// Steps: Modify storage on each non-autoscaled instance
	for _, instance := range info.Instances {
		if instance.IsAutoScaled {
			continue
		}
		if excludeSet[instance.InstanceID] {
			continue // Skip explicitly excluded instances
		}

		modifyData := map[string]any{
			"instance_id":  instance.InstanceID,
			"storage_type": params.TargetStorageType,
		}
		if params.IOPS != nil {
			modifyData["iops"] = *params.IOPS
		}
		if params.StorageThroughput != nil {
			modifyData["storage_throughput"] = *params.StorageThroughput
		}
		modifyParams, err := json.Marshal(modifyData)
		if err != nil {
			return errors.Wrapf(err, "marshal modify_instance params for %s", instance.InstanceID)
		}

		steps = append(steps, types.Step{
			ID:          uuid.New().String(),
			Name:        "Modify storage: " + instance.InstanceID,
			Description: "Change storage type to " + params.TargetStorageType,
			State:       types.StepStatePending,
			Action:      "modify_instance",
			Parameters:  modifyParams,
			MaxRetries:  2,
		})

		// CRITICAL: instance_id MUST be set explicitly to ensure we wait for THIS instance,
		// not the temp instance. Without this, all modify operations could run in parallel.
		waitParams, err := json.Marshal(map[string]string{
			"instance_id": instance.InstanceID,
		})
		if err != nil {
			return errors.Wrapf(err, "marshal wait_instance_available params for %s", instance.InstanceID)
		}
		steps = append(steps, types.Step{
			ID:          uuid.New().String(),
			Name:        "Wait for instance: " + instance.InstanceID,
			Description: "Wait for storage modification to complete",
			State:       types.StepStatePending,
			Action:      "wait_instance_available",
			Parameters:  waitParams,
			MaxRetries:  1,
		})
	}

	// Only add failover-back steps if we did a failover (temp instance + writer not excluded)
	if createTempInstance && !writerExcluded && originalWriter != nil {
		failoverParams, err := json.Marshal(map[string]string{
			"instance_id": originalWriter.InstanceID,
		})
		if err != nil {
			return errors.Wrapf(err, "marshal failover params for %s", originalWriter.InstanceID)
		}
		steps = append(steps, types.Step{
			ID:          uuid.New().String(),
			Name:        "Failover back to original writer",
			Description: "Restore original writer: " + originalWriter.InstanceID,
			State:       types.StepStatePending,
			Action:      "failover_to_instance",
			Parameters:  failoverParams,
			MaxRetries:  1,
		})

		steps = append(steps, types.Step{
			ID:          uuid.New().String(),
			Name:        "Wait for final failover",
			Description: "Wait for cluster to stabilize",
			State:       types.StepStatePending,
			Action:      "wait_cluster_available",
			MaxRetries:  1,
		})
	}

	// Delete temp instance if we created one
	if createTempInstance {
		deleteParams, err := json.Marshal(map[string]bool{
			"skip_final_snapshot": true,
		})
		if err != nil {
			return errors.Wrap(err, "marshal delete_instance params")
		}
		steps = append(steps, types.Step{
			ID:          uuid.New().String(),
			Name:        "Delete temp instance",
			Description: "Remove temporary maintenance instance",
			State:       types.StepStatePending,
			Action:      "delete_instance",
			Parameters:  deleteParams,
			MaxRetries:  2,
		})

		steps = append(steps, types.Step{
			ID:          uuid.New().String(),
			Name:        "Wait for temp instance deletion",
			Description: "Wait for temporary instance to be deleted",
			State:       types.StepStatePending,
			Action:      "wait_instance_deleted",
			MaxRetries:  1,
		})
	}

	op.Steps = steps
	return nil
}

// buildEngineUpgradeSteps builds the steps for an engine version upgrade using Blue-Green deployment.
// This operation uses AWS Blue-Green deployment for near-zero-downtime upgrades.
//
// NOTE: AWS Blue-Green deployments do not support clusters with RDS Proxy targets.
// If a proxy is detected, the cluster must be deregistered from the proxy before creating
// the Blue-Green deployment, and re-registered after switchover completes.
func (e *Engine) buildEngineUpgradeSteps(ctx context.Context, op *types.Operation) error {
	var params types.EngineUpgradeParams
	if err := json.Unmarshal(op.Parameters, &params); err != nil {
		return errors.Wrap(err, "unmarshal params")
	}

	if params.TargetEngineVersion == "" {
		return errors.New("missing required parameter: target_engine_version")
	}

	skipProxySteps := params.SkipProxyRetarget != nil && *params.SkipProxyRetarget
	steps := []types.Step{}

	// Step 1: Get cluster info and ARN
	steps = append(steps, types.Step{
		ID:          uuid.New().String(),
		Name:        "Get cluster info",
		Description: "Retrieve current cluster state and ARN",
		State:       types.StepStatePending,
		Action:      "get_cluster_info",
		MaxRetries:  3,
	})

	// Step 2: Prepare parameter groups for the target version
	// This step creates parameter groups for the green environment with migrated custom settings
	prepareParamsMap := map[string]any{
		"target_engine_version": params.TargetEngineVersion,
	}
	if params.DBClusterParameterGroupName != "" {
		prepareParamsMap["target_cluster_parameter_group_name"] = params.DBClusterParameterGroupName
	}
	if params.DBInstanceParameterGroupName != "" {
		prepareParamsMap["target_instance_parameter_group_name"] = params.DBInstanceParameterGroupName
	}
	prepareParams, err := json.Marshal(prepareParamsMap)
	if err != nil {
		return errors.Wrap(err, "marshal prepare_parameter_group params")
	}
	steps = append(steps, types.Step{
		ID:          uuid.New().String(),
		Name:        "Prepare parameter groups",
		Description: "Create parameter groups for target version with custom settings",
		State:       types.StepStatePending,
		Action:      "prepare_parameter_group",
		Parameters:  prepareParams,
		MaxRetries:  1,
	})

	// Step 3: Wait for cluster to be available
	// Blue-Green deployment creation requires the cluster to be in 'available' state
	steps = append(steps, types.Step{
		ID:          uuid.New().String(),
		Name:        "Wait for cluster available",
		Description: "Ensure cluster is in available state before creating Blue-Green deployment",
		State:       types.StepStatePending,
		Action:      "wait_cluster_available",
		MaxRetries:  1,
	})

	// Step 4: Validate RDS Proxy health (if not skipped)
	// This discovers proxies pointing at the cluster and validates they are healthy.
	// Must run BEFORE Blue-Green deployment creation because we need to deregister proxies first.
	if !skipProxySteps {
		steps = append(steps, types.Step{
			ID:          uuid.New().String(),
			Name:        "Validate proxy health",
			Description: "Discover and validate RDS Proxies targeting this cluster",
			State:       types.StepStatePending,
			Action:      "validate_proxy_health",
			MaxRetries:  2,
		})
	}

	// Step 5: Deregister cluster from RDS Proxy targets (if not skipped)
	// AWS Blue-Green deployments don't support clusters with RDS Proxy targets.
	// We must deregister before creating the Blue-Green deployment.
	// WARNING: This will cause proxy connections to fail until re-registered after switchover.
	if !skipProxySteps {
		steps = append(steps, types.Step{
			ID:          uuid.New().String(),
			Name:        "Deregister proxy targets",
			Description: "Deregister cluster from RDS Proxy (required for Blue-Green deployment)",
			State:       types.StepStatePending,
			Action:      "deregister_proxy_targets",
			MaxRetries:  2,
		})
	}

	// Step 6: Create Blue-Green deployment
	bgParamsMap := map[string]any{
		"target_engine_version": params.TargetEngineVersion,
	}
	// Parameter group names will be populated from prepare_parameter_group step results
	bgParams, err := json.Marshal(bgParamsMap)
	if err != nil {
		return errors.Wrap(err, "marshal create_blue_green_deployment params")
	}
	steps = append(steps, types.Step{
		ID:          uuid.New().String(),
		Name:        "Create Blue-Green deployment",
		Description: "Create Blue-Green deployment for engine upgrade to " + params.TargetEngineVersion,
		State:       types.StepStatePending,
		Action:      "create_blue_green_deployment",
		Parameters:  bgParams,
		MaxRetries:  1,
	})

	// Step 7: Wait for Blue-Green deployment to be available
	steps = append(steps, types.Step{
		ID:          uuid.New().String(),
		Name:        "Wait for green environment",
		Description: "Wait for green environment to be ready",
		State:       types.StepStatePending,
		Action:      "wait_blue_green_available",
		MaxRetries:  1,
	})

	// Step 8: Switchover Blue-Green deployment
	switchoverParamsMap := map[string]any{}
	if params.SwitchoverTimeout > 0 {
		switchoverParamsMap["switchover_timeout"] = params.SwitchoverTimeout
	}
	switchoverParams, err := json.Marshal(switchoverParamsMap)
	if err != nil {
		return errors.Wrap(err, "marshal switchover_blue_green params")
	}
	steps = append(steps, types.Step{
		ID:          uuid.New().String(),
		Name:        "Switchover",
		Description: "Perform Blue-Green switchover to upgraded cluster",
		State:       types.StepStatePending,
		Action:      "switchover_blue_green",
		Parameters:  switchoverParams,
		MaxRetries:  1,
	})

	// Step 9: Register cluster to RDS Proxy targets (if not skipped)
	// Re-register the new cluster to the proxy after switchover completes.
	if !skipProxySteps {
		steps = append(steps, types.Step{
			ID:          uuid.New().String(),
			Name:        "Register proxy targets",
			Description: "Register upgraded cluster to RDS Proxy",
			State:       types.StepStatePending,
			Action:      "register_proxy_targets",
			MaxRetries:  3,
		})
	}

	// Step 10: Cleanup Blue-Green deployment and old cluster
	steps = append(steps, types.Step{
		ID:          uuid.New().String(),
		Name:        "Cleanup",
		Description: "Delete Blue-Green deployment and old cluster instances",
		State:       types.StepStatePending,
		Action:      "cleanup_blue_green",
		MaxRetries:  1,
	})

	// Step 11: Verify final cluster state
	steps = append(steps, types.Step{
		ID:          uuid.New().String(),
		Name:        "Verify upgrade",
		Description: "Verify cluster is running new engine version",
		State:       types.StepStatePending,
		Action:      "get_cluster_info",
		MaxRetries:  3,
	})

	op.Steps = steps

	// Set auto-pause before proxy deregister step by default (unless explicitly disabled)
	// PauseBeforeProxyDeregister defaults to true when nil
	// WARNING: Deregistering will cause proxy connections to fail until re-registered
	if !skipProxySteps && (params.PauseBeforeProxyDeregister == nil || *params.PauseBeforeProxyDeregister) {
		for i, step := range op.Steps {
			if step.Action == "deregister_proxy_targets" {
				op.PauseBeforeSteps = append(op.PauseBeforeSteps, i)
				break
			}
		}
	}

	// Set auto-pause before switchover step by default (unless explicitly disabled)
	// PauseBeforeSwitchover defaults to true when nil
	if params.PauseBeforeSwitchover == nil || *params.PauseBeforeSwitchover {
		for i, step := range op.Steps {
			if step.Action == "switchover_blue_green" {
				op.PauseBeforeSteps = append(op.PauseBeforeSteps, i)
				break
			}
		}
	}

	// Set auto-pause before cleanup step by default (unless explicitly disabled)
	// PauseBeforeCleanup defaults to true when nil
	// This allows verification that the upgrade was successful before deleting old resources
	if params.PauseBeforeCleanup == nil || *params.PauseBeforeCleanup {
		for i, step := range op.Steps {
			if step.Action == "cleanup_blue_green" {
				op.PauseBeforeSteps = append(op.PauseBeforeSteps, i)
				break
			}
		}
	}

	return nil
}

// buildInstanceCycleSteps builds the steps for an instance cycle (reboot) operation.
// This operation creates a temp instance for failover (unless SkipTempInstance is true),
// then reboots all non-autoscaled instances one at a time.
// If the writer is excluded, we skip the failover steps and only reboot the readers.
func (e *Engine) buildInstanceCycleSteps(ctx context.Context, op *types.Operation) error {
	// Parse params to get excluded instances
	var params types.InstanceCycleParams
	if op.Parameters != nil && len(op.Parameters) > 0 {
		if err := json.Unmarshal(op.Parameters, &params); err != nil {
			return errors.Wrap(err, "unmarshal params")
		}
	}

	client, err := e.clientManager.GetClient(ctx, op.Region)
	if err != nil {
		return errors.Wrap(err, "get RDS client")
	}

	// Get cluster info to find all instances
	info, err := client.GetClusterInfo(ctx, op.ClusterID)
	if err != nil {
		return errors.Wrap(err, "get cluster info")
	}

	// Validate excluded instances exist and not all are excluded
	excludeSet, err := validateExcludedInstances(params.ExcludeInstances, info.Instances)
	if err != nil {
		return err
	}

	// Separate writer and readers, exclude autoscaled instances
	var writer *types.InstanceInfo
	var readers []*types.InstanceInfo
	writerExcluded := false

	for i := range info.Instances {
		inst := &info.Instances[i]
		if inst.IsAutoScaled {
			e.logger.Info("skipping autoscaled instance", "instance", inst.InstanceID)
			continue
		}
		if inst.Role == "writer" {
			writer = inst
			if excludeSet[inst.InstanceID] {
				writerExcluded = true
			}
		} else {
			// Only add readers that are not excluded
			if !excludeSet[inst.InstanceID] {
				readers = append(readers, inst)
			}
		}
	}

	if writer == nil {
		return errors.New("no writer instance found in cluster")
	}

	// Determine if we should create a temp instance
	// By default, create temp instance for redundancy unless explicitly skipped
	createTempInstance := !params.SkipTempInstance

	var steps []types.Step

	// Step 1: Get initial cluster state
	steps = append(steps, types.Step{
		ID:          uuid.New().String(),
		Name:        "Get cluster info",
		Description: "Get current cluster state before cycling instances",
		State:       types.StepStatePending,
		Action:      "get_cluster_info",
		MaxRetries:  3,
	})

	// Create temp instance if enabled
	if createTempInstance {
		createParams, err := json.Marshal(map[string]string{
			"instance_type": writer.InstanceType,
			"engine":        info.Engine,
		})
		if err != nil {
			return errors.Wrap(err, "marshal create_temp_instance params")
		}
		steps = append(steps, types.Step{
			ID:          uuid.New().String(),
			Name:        "Create temp instance",
			Description: "Create temporary instance for failover during reboot",
			State:       types.StepStatePending,
			Action:      "create_temp_instance",
			Parameters:  createParams,
			MaxRetries:  1,
		})

		steps = append(steps, types.Step{
			ID:          uuid.New().String(),
			Name:        "Wait for temp instance",
			Description: "Wait for temporary instance to become available",
			State:       types.StepStatePending,
			Action:      "wait_instance_available",
			MaxRetries:  1,
		})
	}

	// Only failover to temp instance if writer is NOT excluded (we need to reboot it)
	if createTempInstance && !writerExcluded {
		steps = append(steps, types.Step{
			ID:          uuid.New().String(),
			Name:        "Failover to temp instance",
			Description: "Promote temporary instance to writer",
			State:       types.StepStatePending,
			Action:      "failover_to_instance",
			MaxRetries:  1,
		})

		steps = append(steps, types.Step{
			ID:          uuid.New().String(),
			Name:        "Wait for failover",
			Description: "Wait for cluster to stabilize after failover",
			State:       types.StepStatePending,
			Action:      "wait_cluster_available",
			MaxRetries:  1,
		})
	}

	// Reboot original writer if not excluded (now a reader after failover)
	if !writerExcluded {
		writerRebootParams, err := json.Marshal(map[string]string{
			"instance_id": writer.InstanceID,
		})
		if err != nil {
			return errors.Wrapf(err, "marshal reboot_instance params for %s", writer.InstanceID)
		}
		steps = append(steps, types.Step{
			ID:          uuid.New().String(),
			Name:        "Reboot original writer",
			Description: fmt.Sprintf("Reboot instance %s (original writer)", writer.InstanceID),
			State:       types.StepStatePending,
			Action:      "reboot_instance",
			Parameters:  writerRebootParams,
			MaxRetries:  1,
		})

		// CRITICAL: instance_id MUST be set explicitly to ensure we wait for THIS instance,
		// not the temp instance. Without this, all reboot operations could run in parallel.
		writerWaitParams, err := json.Marshal(map[string]string{
			"instance_id": writer.InstanceID,
		})
		if err != nil {
			return errors.Wrapf(err, "marshal wait_instance_available params for %s", writer.InstanceID)
		}
		steps = append(steps, types.Step{
			ID:          uuid.New().String(),
			Name:        "Wait for original writer",
			Description: fmt.Sprintf("Wait for instance %s to be available", writer.InstanceID),
			State:       types.StepStatePending,
			Action:      "wait_instance_available",
			Parameters:  writerWaitParams,
			MaxRetries:  1,
		})
	}

	// Reboot each reader instance (one at a time, wait for available between each)
	for i, reader := range readers {
		rebootParams, err := json.Marshal(map[string]string{
			"instance_id": reader.InstanceID,
		})
		if err != nil {
			return errors.Wrapf(err, "marshal reboot_instance params for %s", reader.InstanceID)
		}

		steps = append(steps, types.Step{
			ID:          uuid.New().String(),
			Name:        fmt.Sprintf("Reboot reader %d", i+1),
			Description: fmt.Sprintf("Reboot reader instance %s", reader.InstanceID),
			State:       types.StepStatePending,
			Action:      "reboot_instance",
			Parameters:  rebootParams,
			MaxRetries:  1,
		})

		// CRITICAL: instance_id MUST be set explicitly to ensure we wait for THIS instance,
		// not the temp instance. Without this, all reboot operations could run in parallel.
		waitParams, err := json.Marshal(map[string]string{
			"instance_id": reader.InstanceID,
		})
		if err != nil {
			return errors.Wrapf(err, "marshal wait_instance_available params for %s", reader.InstanceID)
		}
		steps = append(steps, types.Step{
			ID:          uuid.New().String(),
			Name:        fmt.Sprintf("Wait for reader %d", i+1),
			Description: fmt.Sprintf("Wait for instance %s to be available", reader.InstanceID),
			State:       types.StepStatePending,
			Action:      "wait_instance_available",
			Parameters:  waitParams,
			MaxRetries:  1,
		})
	}

	// Only add failover-back steps if we did a failover (temp instance + writer not excluded)
	if createTempInstance && !writerExcluded {
		failoverParams, err := json.Marshal(map[string]string{
			"instance_id": writer.InstanceID,
		})
		if err != nil {
			return errors.Wrapf(err, "marshal failover params for %s", writer.InstanceID)
		}
		steps = append(steps, types.Step{
			ID:          uuid.New().String(),
			Name:        "Failover back to original writer",
			Description: "Restore original writer: " + writer.InstanceID,
			State:       types.StepStatePending,
			Action:      "failover_to_instance",
			Parameters:  failoverParams,
			MaxRetries:  1,
		})

		steps = append(steps, types.Step{
			ID:          uuid.New().String(),
			Name:        "Wait for final failover",
			Description: "Wait for cluster to stabilize",
			State:       types.StepStatePending,
			Action:      "wait_cluster_available",
			MaxRetries:  1,
		})
	}

	// Delete temp instance if we created one
	if createTempInstance {
		deleteParams, err := json.Marshal(map[string]bool{
			"skip_final_snapshot": true,
		})
		if err != nil {
			return errors.Wrap(err, "marshal delete_instance params")
		}
		steps = append(steps, types.Step{
			ID:          uuid.New().String(),
			Name:        "Delete temp instance",
			Description: "Remove temporary maintenance instance",
			State:       types.StepStatePending,
			Action:      "delete_instance",
			Parameters:  deleteParams,
			MaxRetries:  2,
		})

		steps = append(steps, types.Step{
			ID:          uuid.New().String(),
			Name:        "Wait for temp instance deletion",
			Description: "Wait for temporary instance to be deleted",
			State:       types.StepStatePending,
			Action:      "wait_instance_deleted",
			MaxRetries:  1,
		})
	}

	// Final step: Verify cluster state
	steps = append(steps, types.Step{
		ID:          uuid.New().String(),
		Name:        "Verify cluster",
		Description: "Verify all instances are available",
		State:       types.StepStatePending,
		Action:      "get_cluster_info",
		MaxRetries:  3,
	})

	op.Steps = steps
	return nil
}
