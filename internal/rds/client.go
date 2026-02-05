// Package rds provides an RDS client wrapper for cluster maintenance operations.
package rds

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/cockroachdb/errors"
	internalerrors "github.com/mpz/devops/tools/rds-maint-machine/internal/errors"
	internaltypes "github.com/mpz/devops/tools/rds-maint-machine/internal/types"
)

// Client wraps the AWS RDS client with convenience methods.
type Client struct {
	rds     *rds.Client
	baseURL string // for testing with mock servers
}

// ClientConfig contains configuration for the RDS client.
type ClientConfig struct {
	AWSConfig aws.Config
	BaseURL   string // optional, for testing
}

// NewClient creates a new RDS client.
func NewClient(cfg ClientConfig) *Client {
	opts := []func(*rds.Options){}
	if cfg.BaseURL != "" {
		opts = append(opts, func(o *rds.Options) {
			o.BaseEndpoint = aws.String(cfg.BaseURL)
		})
	}

	return &Client{
		rds:     rds.NewFromConfig(cfg.AWSConfig, opts...),
		baseURL: cfg.BaseURL,
	}
}

// NewClientWithRDS creates a client with an existing RDS client (for testing).
func NewClientWithRDS(client *rds.Client) *Client {
	return &Client{rds: client}
}

// ListClusters returns a summary of all Aurora clusters in the region.
func (c *Client) ListClusters(ctx context.Context) ([]internaltypes.ClusterSummary, error) {
	var clusters []internaltypes.ClusterSummary

	paginator := rds.NewDescribeDBClustersPaginator(c.rds, &rds.DescribeDBClustersInput{})
	for paginator.HasMorePages() {
		out, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, errors.Wrap(err, "describe clusters")
		}

		for _, cluster := range out.DBClusters {
			// Only include Aurora clusters
			engine := aws.ToString(cluster.Engine)
			if strings.HasPrefix(engine, "aurora") {
				clusters = append(clusters, internaltypes.ClusterSummary{
					ClusterID:     aws.ToString(cluster.DBClusterIdentifier),
					Engine:        engine,
					EngineVersion: aws.ToString(cluster.EngineVersion),
					Status:        aws.ToString(cluster.Status),
				})
			}
		}
	}

	return clusters, nil
}

// GetClusterInfo retrieves information about an RDS cluster.
// This method is optimized to batch instance lookups and cache tag checks.
func (c *Client) GetClusterInfo(ctx context.Context, clusterID string) (*internaltypes.ClusterInfo, error) {
	out, err := c.rds.DescribeDBClusters(ctx, &rds.DescribeDBClustersInput{
		DBClusterIdentifier: aws.String(clusterID),
	})
	if err != nil {
		if strings.Contains(err.Error(), "DBClusterNotFound") {
			return nil, errors.Wrap(internalerrors.ErrClusterNotFound, clusterID)
		}
		return nil, errors.Wrap(err, "describe clusters")
	}

	if len(out.DBClusters) == 0 {
		return nil, errors.Wrap(internalerrors.ErrClusterNotFound, clusterID)
	}

	cluster := out.DBClusters[0]
	info := &internaltypes.ClusterInfo{
		ClusterID:     aws.ToString(cluster.DBClusterIdentifier),
		Engine:        aws.ToString(cluster.Engine),
		EngineVersion: aws.ToString(cluster.EngineVersion),
		Status:        aws.ToString(cluster.Status),
		Instances:     make([]internaltypes.InstanceInfo, 0, len(cluster.DBClusterMembers)),
	}

	// Build a map of member IDs to their writer status
	memberWriterStatus := make(map[string]bool)
	for _, member := range cluster.DBClusterMembers {
		instanceID := aws.ToString(member.DBInstanceIdentifier)
		memberWriterStatus[instanceID] = member.IsClusterWriter != nil && *member.IsClusterWriter
	}

	// Batch fetch all instances using filter (more efficient than N individual calls)
	// Note: DescribeDBInstances with db-cluster-id filter returns all instances in the cluster
	instancesOut, err := c.rds.DescribeDBInstances(ctx, &rds.DescribeDBInstancesInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("db-cluster-id"),
				Values: []string{clusterID},
			},
		},
	})
	if err != nil {
		return nil, errors.Wrap(err, "describe cluster instances")
	}

	// Build a map of instance ARNs for batch tag lookup
	instanceARNs := make([]string, 0, len(instancesOut.DBInstances))
	for _, inst := range instancesOut.DBInstances {
		instanceARNs = append(instanceARNs, aws.ToString(inst.DBInstanceArn))
	}

	// Batch check autoscaling tags
	autoScaledSet := c.batchCheckAutoScaled(ctx, instanceARNs)

	// Process instances
	for _, instance := range instancesOut.DBInstances {
		instanceID := aws.ToString(instance.DBInstanceIdentifier)
		instanceARN := aws.ToString(instance.DBInstanceArn)

		instInfo := internaltypes.InstanceInfo{
			InstanceID:   instanceID,
			InstanceType: aws.ToString(instance.DBInstanceClass),
			Status:       aws.ToString(instance.DBInstanceStatus),
			StorageType:  aws.ToString(instance.StorageType),
			IsAutoScaled: autoScaledSet[instanceARN],
		}

		if instance.Iops != nil {
			iops := int32(*instance.Iops)
			instInfo.IOPS = &iops
		}

		if memberWriterStatus[instanceID] {
			instInfo.Role = "writer"
		} else {
			instInfo.Role = "reader"
		}

		info.Instances = append(info.Instances, instInfo)
	}

	return info, nil
}

// batchCheckAutoScaled checks multiple instance ARNs for autoscaling tags.
// Returns a map of ARN -> isAutoScaled.
func (c *Client) batchCheckAutoScaled(ctx context.Context, arns []string) map[string]bool {
	result := make(map[string]bool)
	for _, arn := range arns {
		result[arn] = c.isAutoScaledInstance(ctx, arn)
	}
	return result
}

// GetInstanceInfo retrieves information about a specific RDS instance.
func (c *Client) GetInstanceInfo(ctx context.Context, instanceID string) (*internaltypes.InstanceInfo, error) {
	out, err := c.rds.DescribeDBInstances(ctx, &rds.DescribeDBInstancesInput{
		DBInstanceIdentifier: aws.String(instanceID),
	})
	if err != nil {
		if strings.Contains(err.Error(), "DBInstanceNotFound") {
			return nil, errors.Wrap(internalerrors.ErrInstanceNotFound, instanceID)
		}
		return nil, errors.Wrap(err, "describe instance")
	}

	if len(out.DBInstances) == 0 {
		return nil, errors.Wrap(internalerrors.ErrInstanceNotFound, instanceID)
	}

	instance := out.DBInstances[0]
	info := &internaltypes.InstanceInfo{
		InstanceID:   aws.ToString(instance.DBInstanceIdentifier),
		InstanceType: aws.ToString(instance.DBInstanceClass),
		Status:       aws.ToString(instance.DBInstanceStatus),
		StorageType:  aws.ToString(instance.StorageType),
	}

	if instance.Iops != nil {
		iops := int32(*instance.Iops)
		info.IOPS = &iops
	}

	// Check if this is an auto-scaled instance by looking at tags
	info.IsAutoScaled = c.isAutoScaledInstance(ctx, aws.ToString(instance.DBInstanceArn))

	return info, nil
}

// isAutoScaledInstance checks if an instance was created by autoscaling.
func (c *Client) isAutoScaledInstance(ctx context.Context, arn string) bool {
	out, err := c.rds.ListTagsForResource(ctx, &rds.ListTagsForResourceInput{
		ResourceName: aws.String(arn),
	})
	if err != nil {
		return false
	}

	for _, tag := range out.TagList {
		if aws.ToString(tag.Key) == "application-autoscaling:resourceId" {
			return true
		}
	}
	return false
}

// CreateClusterInstance creates a new instance in the cluster.
func (c *Client) CreateClusterInstance(ctx context.Context, params CreateInstanceParams) (string, error) {
	input := &rds.CreateDBInstanceInput{
		DBClusterIdentifier:  aws.String(params.ClusterID),
		DBInstanceIdentifier: aws.String(params.InstanceID),
		DBInstanceClass:      aws.String(params.InstanceType),
		Engine:               aws.String(params.Engine),
		PromotionTier:        aws.Int32(params.PromotionTier),
	}

	if params.AvailabilityZone != "" {
		input.AvailabilityZone = aws.String(params.AvailabilityZone)
	}

	// Add tags to identify this as a temp maintenance instance
	input.Tags = []types.Tag{
		{Key: aws.String("rds-maint-machine"), Value: aws.String("temp-instance")},
		{Key: aws.String("rds-maint-operation-id"), Value: aws.String(params.OperationID)},
	}

	_, err := c.rds.CreateDBInstance(ctx, input)
	if err != nil {
		return "", errors.Wrap(err, "create instance")
	}

	return params.InstanceID, nil
}

// CreateInstanceParams contains parameters for creating an instance.
type CreateInstanceParams struct {
	ClusterID        string
	InstanceID       string
	InstanceType     string
	Engine           string
	PromotionTier    int32
	AvailabilityZone string
	OperationID      string
}

// ModifyInstance modifies an existing RDS instance.
func (c *Client) ModifyInstance(ctx context.Context, params ModifyInstanceParams) error {
	input := &rds.ModifyDBInstanceInput{
		DBInstanceIdentifier: aws.String(params.InstanceID),
		ApplyImmediately:     aws.Bool(params.ApplyImmediately),
	}

	if params.InstanceType != "" {
		input.DBInstanceClass = aws.String(params.InstanceType)
	}

	if params.StorageType != "" {
		input.StorageType = aws.String(params.StorageType)
	}

	if params.IOPS != nil {
		input.Iops = aws.Int32(*params.IOPS)
	}

	if params.StorageThroughput != nil {
		input.StorageThroughput = aws.Int32(*params.StorageThroughput)
	}

	_, err := c.rds.ModifyDBInstance(ctx, input)
	if err != nil {
		return errors.Wrap(err, "modify instance")
	}

	return nil
}

// ModifyInstanceParams contains parameters for modifying an instance.
type ModifyInstanceParams struct {
	InstanceID        string
	InstanceType      string
	StorageType       string
	IOPS              *int32
	StorageThroughput *int32
	ApplyImmediately  bool
}

// DeleteInstance deletes an RDS instance.
func (c *Client) DeleteInstance(ctx context.Context, instanceID string, skipFinalSnapshot bool) error {
	input := &rds.DeleteDBInstanceInput{
		DBInstanceIdentifier: aws.String(instanceID),
		SkipFinalSnapshot:    aws.Bool(skipFinalSnapshot),
	}

	_, err := c.rds.DeleteDBInstance(ctx, input)
	if err != nil {
		return errors.Wrap(err, "delete instance")
	}

	return nil
}

// RebootInstance reboots an RDS instance.
func (c *Client) RebootInstance(ctx context.Context, instanceID string) error {
	input := &rds.RebootDBInstanceInput{
		DBInstanceIdentifier: aws.String(instanceID),
	}

	_, err := c.rds.RebootDBInstance(ctx, input)
	if err != nil {
		return errors.Wrap(err, "reboot instance")
	}

	return nil
}

// FailoverCluster initiates a failover to a specific instance.
func (c *Client) FailoverCluster(ctx context.Context, clusterID, targetInstanceID string) error {
	input := &rds.FailoverDBClusterInput{
		DBClusterIdentifier:        aws.String(clusterID),
		TargetDBInstanceIdentifier: aws.String(targetInstanceID),
	}

	_, err := c.rds.FailoverDBCluster(ctx, input)
	if err != nil {
		return errors.Wrap(err, "failover cluster")
	}

	return nil
}

// ModifyCluster modifies cluster-level settings.
func (c *Client) ModifyCluster(ctx context.Context, params ModifyClusterParams) error {
	input := &rds.ModifyDBClusterInput{
		DBClusterIdentifier: aws.String(params.ClusterID),
		ApplyImmediately:    aws.Bool(params.ApplyImmediately),
	}

	if params.EngineVersion != "" {
		input.EngineVersion = aws.String(params.EngineVersion)
		input.AllowMajorVersionUpgrade = aws.Bool(params.AllowMajorVersionUpgrade)
	}

	if params.DBClusterParameterGroupName != "" {
		input.DBClusterParameterGroupName = aws.String(params.DBClusterParameterGroupName)
	}

	if params.DBInstanceParameterGroupName != "" {
		input.DBInstanceParameterGroupName = aws.String(params.DBInstanceParameterGroupName)
	}

	_, err := c.rds.ModifyDBCluster(ctx, input)
	if err != nil {
		return errors.Wrap(err, "modify cluster")
	}

	return nil
}

// ModifyClusterParams contains parameters for modifying a cluster.
type ModifyClusterParams struct {
	ClusterID                    string
	EngineVersion                string
	AllowMajorVersionUpgrade     bool
	ApplyImmediately             bool
	DBClusterParameterGroupName  string
	DBInstanceParameterGroupName string // Required for engine upgrades when instances use custom PG
}

// CreateClusterSnapshot creates a manual snapshot of the cluster.
func (c *Client) CreateClusterSnapshot(ctx context.Context, clusterID, snapshotID string) error {
	input := &rds.CreateDBClusterSnapshotInput{
		DBClusterIdentifier:         aws.String(clusterID),
		DBClusterSnapshotIdentifier: aws.String(snapshotID),
		Tags: []types.Tag{
			{Key: aws.String("rds-maint-machine"), Value: aws.String("pre-upgrade-snapshot")},
		},
	}

	_, err := c.rds.CreateDBClusterSnapshot(ctx, input)
	if err != nil {
		return errors.Wrap(err, "create snapshot")
	}

	return nil
}

// WaitForInstanceAvailable waits for an instance to become available.
func (c *Client) WaitForInstanceAvailable(ctx context.Context, instanceID string, timeout time.Duration) error {
	waiter := rds.NewDBInstanceAvailableWaiter(c.rds, func(o *rds.DBInstanceAvailableWaiterOptions) {
		o.MinDelay = 1 * time.Second // Faster polling for demo mode
		o.MaxDelay = 5 * time.Second
	})
	return waiter.Wait(ctx, &rds.DescribeDBInstancesInput{
		DBInstanceIdentifier: aws.String(instanceID),
	}, timeout)
}

// IsInstanceAvailable checks if an instance is currently in the "available" state.
// This is a single-check version that returns immediately without blocking.
func (c *Client) IsInstanceAvailable(ctx context.Context, instanceID string) (bool, error) {
	out, err := c.rds.DescribeDBInstances(ctx, &rds.DescribeDBInstancesInput{
		DBInstanceIdentifier: aws.String(instanceID),
	})
	if err != nil {
		if strings.Contains(err.Error(), "DBInstanceNotFound") {
			return false, errors.Wrap(internalerrors.ErrInstanceNotFound, instanceID)
		}
		return false, err
	}
	if len(out.DBInstances) == 0 {
		return false, errors.Wrap(internalerrors.ErrInstanceNotFound, instanceID)
	}
	return aws.ToString(out.DBInstances[0].DBInstanceStatus) == "available", nil
}

// WaitForInstanceDeleted waits for an instance to be deleted.
func (c *Client) WaitForInstanceDeleted(ctx context.Context, instanceID string, timeout time.Duration) error {
	waiter := rds.NewDBInstanceDeletedWaiter(c.rds, func(o *rds.DBInstanceDeletedWaiterOptions) {
		o.MinDelay = 1 * time.Second
		o.MaxDelay = 5 * time.Second
	})
	return waiter.Wait(ctx, &rds.DescribeDBInstancesInput{
		DBInstanceIdentifier: aws.String(instanceID),
	}, timeout)
}

// IsInstanceDeleted checks if an instance has been deleted (no longer exists).
// This is a single-check version that returns immediately without blocking.
func (c *Client) IsInstanceDeleted(ctx context.Context, instanceID string) (bool, error) {
	_, err := c.rds.DescribeDBInstances(ctx, &rds.DescribeDBInstancesInput{
		DBInstanceIdentifier: aws.String(instanceID),
	})
	if err != nil {
		if strings.Contains(err.Error(), "DBInstanceNotFound") {
			return true, nil // Instance is deleted
		}
		return false, err
	}
	return false, nil // Instance still exists
}

// WaitForSnapshotAvailable waits for a snapshot to become available.
func (c *Client) WaitForSnapshotAvailable(ctx context.Context, snapshotID string, timeout time.Duration) error {
	waiter := rds.NewDBClusterSnapshotAvailableWaiter(c.rds, func(o *rds.DBClusterSnapshotAvailableWaiterOptions) {
		o.MinDelay = 1 * time.Second
		o.MaxDelay = 5 * time.Second
	})
	return waiter.Wait(ctx, &rds.DescribeDBClusterSnapshotsInput{
		DBClusterSnapshotIdentifier: aws.String(snapshotID),
	}, timeout)
}

// IsSnapshotAvailable checks if a snapshot is currently in the "available" state.
// This is a single-check version that returns immediately without blocking.
func (c *Client) IsSnapshotAvailable(ctx context.Context, snapshotID string) (bool, error) {
	out, err := c.rds.DescribeDBClusterSnapshots(ctx, &rds.DescribeDBClusterSnapshotsInput{
		DBClusterSnapshotIdentifier: aws.String(snapshotID),
	})
	if err != nil {
		return false, err
	}
	if len(out.DBClusterSnapshots) == 0 {
		return false, errors.Errorf("snapshot %s not found", snapshotID)
	}
	return aws.ToString(out.DBClusterSnapshots[0].Status) == "available", nil
}

// GetWriterInstance returns the current writer instance for the cluster.
func (c *Client) GetWriterInstance(ctx context.Context, clusterID string) (*internaltypes.InstanceInfo, error) {
	info, err := c.GetClusterInfo(ctx, clusterID)
	if err != nil {
		return nil, err
	}

	for _, instance := range info.Instances {
		if instance.Role == "writer" {
			return &instance, nil
		}
	}

	return nil, errors.New("cluster has no writer instance")
}

// GenerateTempInstanceID generates a unique instance ID for temporary instances.
func GenerateTempInstanceID(clusterID, operationID string) string {
	// Use last 8 chars of operation ID to keep it short
	suffix := operationID
	if len(suffix) > 8 {
		suffix = suffix[len(suffix)-8:]
	}
	return fmt.Sprintf("%s-maint-%s", clusterID, suffix)
}

// ClusterParameterGroupInfo contains information about a cluster parameter group.
type ClusterParameterGroupInfo struct {
	Name        string
	Family      string
	Description string
}

// ParameterInfo contains information about a single parameter.
type ParameterInfo struct {
	Name         string
	Value        string
	ApplyType    string // "static" or "dynamic"
	IsModifiable bool
	Source       string // "user" for custom values, "system" or "engine-default" for defaults
}

// GetClusterParameterGroup returns information about the cluster's parameter group.
func (c *Client) GetClusterParameterGroup(ctx context.Context, clusterID string) (*ClusterParameterGroupInfo, error) {
	out, err := c.rds.DescribeDBClusters(ctx, &rds.DescribeDBClustersInput{
		DBClusterIdentifier: aws.String(clusterID),
	})
	if err != nil {
		return nil, errors.Wrap(err, "describe cluster")
	}
	if len(out.DBClusters) == 0 {
		return nil, errors.Wrap(internalerrors.ErrClusterNotFound, clusterID)
	}

	pgName := aws.ToString(out.DBClusters[0].DBClusterParameterGroup)
	if pgName == "" {
		return nil, errors.New("cluster has no associated parameter group")
	}

	// Get parameter group details
	pgOut, err := c.rds.DescribeDBClusterParameterGroups(ctx, &rds.DescribeDBClusterParameterGroupsInput{
		DBClusterParameterGroupName: aws.String(pgName),
	})
	if err != nil {
		return nil, errors.Wrap(err, "describe parameter group")
	}
	if len(pgOut.DBClusterParameterGroups) == 0 {
		return nil, errors.Errorf("parameter group %s not found", pgName)
	}

	pg := pgOut.DBClusterParameterGroups[0]
	return &ClusterParameterGroupInfo{
		Name:        aws.ToString(pg.DBClusterParameterGroupName),
		Family:      aws.ToString(pg.DBParameterGroupFamily),
		Description: aws.ToString(pg.Description),
	}, nil
}

// GetClusterParameterGroupCustomParameters returns only the user-modified (non-default) parameters.
func (c *Client) GetClusterParameterGroupCustomParameters(ctx context.Context, parameterGroupName string) ([]ParameterInfo, error) {
	var customParams []ParameterInfo

	paginator := rds.NewDescribeDBClusterParametersPaginator(c.rds, &rds.DescribeDBClusterParametersInput{
		DBClusterParameterGroupName: aws.String(parameterGroupName),
		Source:                      aws.String("user"), // Only get user-modified parameters
	})

	for paginator.HasMorePages() {
		out, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, errors.Wrap(err, "describe parameters")
		}

		for _, param := range out.Parameters {
			if param.ParameterValue != nil {
				customParams = append(customParams, ParameterInfo{
					Name:         aws.ToString(param.ParameterName),
					Value:        aws.ToString(param.ParameterValue),
					ApplyType:    aws.ToString(param.ApplyType),
					IsModifiable: aws.ToBool(param.IsModifiable),
					Source:       aws.ToString(param.Source),
				})
			}
		}
	}

	return customParams, nil
}

// CreateClusterParameterGroup creates a new cluster parameter group.
func (c *Client) CreateClusterParameterGroup(ctx context.Context, name, family, description string) error {
	_, err := c.rds.CreateDBClusterParameterGroup(ctx, &rds.CreateDBClusterParameterGroupInput{
		DBClusterParameterGroupName: aws.String(name),
		DBParameterGroupFamily:      aws.String(family),
		Description:                 aws.String(description),
		Tags: []types.Tag{
			{Key: aws.String("created-by"), Value: aws.String("rds-maint-machine")},
		},
	})
	if err != nil {
		// Check if it already exists
		if strings.Contains(err.Error(), "DBParameterGroupAlreadyExists") {
			return nil // Already exists, that's fine
		}
		return errors.Wrap(err, "create parameter group")
	}
	return nil
}

// ModifyClusterParameterGroupParams sets parameters on a cluster parameter group.
func (c *Client) ModifyClusterParameterGroupParams(ctx context.Context, parameterGroupName string, params []ParameterInfo) error {
	if len(params) == 0 {
		return nil
	}

	// AWS allows max 20 parameters per call
	const batchSize = 20
	for i := 0; i < len(params); i += batchSize {
		end := i + batchSize
		if end > len(params) {
			end = len(params)
		}
		batch := params[i:end]

		var awsParams []types.Parameter
		for _, p := range batch {
			applyMethod := types.ApplyMethodImmediate
			if p.ApplyType == "static" {
				applyMethod = types.ApplyMethodPendingReboot
			}
			awsParams = append(awsParams, types.Parameter{
				ParameterName:  aws.String(p.Name),
				ParameterValue: aws.String(p.Value),
				ApplyMethod:    applyMethod,
			})
		}

		_, err := c.rds.ModifyDBClusterParameterGroup(ctx, &rds.ModifyDBClusterParameterGroupInput{
			DBClusterParameterGroupName: aws.String(parameterGroupName),
			Parameters:                  awsParams,
		})
		if err != nil {
			return errors.Wrapf(err, "modify parameter group (batch starting at %d)", i)
		}
	}

	return nil
}

// ParameterGroupExists checks if a parameter group exists.
func (c *Client) ParameterGroupExists(ctx context.Context, name string) (bool, error) {
	_, err := c.rds.DescribeDBClusterParameterGroups(ctx, &rds.DescribeDBClusterParameterGroupsInput{
		DBClusterParameterGroupName: aws.String(name),
	})
	if err != nil {
		if strings.Contains(err.Error(), "DBParameterGroupNotFound") {
			return false, nil
		}
		return false, errors.Wrap(err, "describe parameter group")
	}
	return true, nil
}

// GetDefaultParameterGroupFamily returns the default parameter group family for a given engine version.
// For example: aurora-postgresql15 -> default.aurora-postgresql15
func GetDefaultParameterGroupFamily(engine, version string) string {
	// Extract major version
	majorVersion := version
	if idx := strings.Index(version, "."); idx > 0 {
		majorVersion = version[:idx]
	}

	// Handle aurora-postgresql and aurora-mysql
	if strings.HasPrefix(engine, "aurora-postgresql") {
		return fmt.Sprintf("aurora-postgresql%s", majorVersion)
	} else if strings.HasPrefix(engine, "aurora-mysql") {
		// Aurora MySQL uses different versioning
		return fmt.Sprintf("aurora-mysql%s", majorVersion)
	}
	return fmt.Sprintf("%s%s", engine, majorVersion)
}

// GetDefaultParameterGroupName returns the default parameter group name for a family.
func GetDefaultParameterGroupName(family string) string {
	return fmt.Sprintf("default.%s", family)
}

// InstanceParameterGroupInfo contains information about a DB instance parameter group.
type InstanceParameterGroupInfo struct {
	Name        string
	Family      string
	Description string
}

// GetInstanceParameterGroup returns information about an instance's parameter group.
func (c *Client) GetInstanceParameterGroup(ctx context.Context, instanceID string) (*InstanceParameterGroupInfo, error) {
	out, err := c.rds.DescribeDBInstances(ctx, &rds.DescribeDBInstancesInput{
		DBInstanceIdentifier: aws.String(instanceID),
	})
	if err != nil {
		return nil, errors.Wrap(err, "describe instance")
	}
	if len(out.DBInstances) == 0 {
		return nil, errors.Wrap(internalerrors.ErrInstanceNotFound, instanceID)
	}

	instance := out.DBInstances[0]
	if len(instance.DBParameterGroups) == 0 {
		return nil, errors.New("instance has no associated parameter group")
	}

	pgName := aws.ToString(instance.DBParameterGroups[0].DBParameterGroupName)

	// Get parameter group details
	pgOut, err := c.rds.DescribeDBParameterGroups(ctx, &rds.DescribeDBParameterGroupsInput{
		DBParameterGroupName: aws.String(pgName),
	})
	if err != nil {
		return nil, errors.Wrap(err, "describe parameter group")
	}
	if len(pgOut.DBParameterGroups) == 0 {
		return nil, errors.Errorf("parameter group %s not found", pgName)
	}

	pg := pgOut.DBParameterGroups[0]
	return &InstanceParameterGroupInfo{
		Name:        aws.ToString(pg.DBParameterGroupName),
		Family:      aws.ToString(pg.DBParameterGroupFamily),
		Description: aws.ToString(pg.Description),
	}, nil
}

// GetInstanceParameterGroupCustomParameters returns only the user-modified (non-default) parameters
// for a DB instance parameter group.
func (c *Client) GetInstanceParameterGroupCustomParameters(ctx context.Context, parameterGroupName string) ([]ParameterInfo, error) {
	var customParams []ParameterInfo

	paginator := rds.NewDescribeDBParametersPaginator(c.rds, &rds.DescribeDBParametersInput{
		DBParameterGroupName: aws.String(parameterGroupName),
		Source:               aws.String("user"), // Only get user-modified parameters
	})

	for paginator.HasMorePages() {
		out, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, errors.Wrap(err, "describe parameters")
		}

		for _, param := range out.Parameters {
			if param.ParameterValue != nil {
				customParams = append(customParams, ParameterInfo{
					Name:         aws.ToString(param.ParameterName),
					Value:        aws.ToString(param.ParameterValue),
					ApplyType:    aws.ToString(param.ApplyType),
					IsModifiable: aws.ToBool(param.IsModifiable),
					Source:       aws.ToString(param.Source),
				})
			}
		}
	}

	return customParams, nil
}

// CreateInstanceParameterGroup creates a new DB instance parameter group.
func (c *Client) CreateInstanceParameterGroup(ctx context.Context, name, family, description string) error {
	_, err := c.rds.CreateDBParameterGroup(ctx, &rds.CreateDBParameterGroupInput{
		DBParameterGroupName:   aws.String(name),
		DBParameterGroupFamily: aws.String(family),
		Description:            aws.String(description),
		Tags: []types.Tag{
			{Key: aws.String("created-by"), Value: aws.String("rds-maint-machine")},
		},
	})
	if err != nil {
		// Check if it already exists
		if strings.Contains(err.Error(), "DBParameterGroupAlreadyExists") {
			return nil // Already exists, that's fine
		}
		return errors.Wrap(err, "create parameter group")
	}
	return nil
}

// ModifyInstanceParameterGroupParams sets parameters on a DB instance parameter group.
func (c *Client) ModifyInstanceParameterGroupParams(ctx context.Context, parameterGroupName string, params []ParameterInfo) error {
	if len(params) == 0 {
		return nil
	}

	// AWS allows max 20 parameters per call
	const batchSize = 20
	for i := 0; i < len(params); i += batchSize {
		end := i + batchSize
		if end > len(params) {
			end = len(params)
		}
		batch := params[i:end]

		var awsParams []types.Parameter
		for _, p := range batch {
			applyMethod := types.ApplyMethodImmediate
			if p.ApplyType == "static" {
				applyMethod = types.ApplyMethodPendingReboot
			}
			awsParams = append(awsParams, types.Parameter{
				ParameterName:  aws.String(p.Name),
				ParameterValue: aws.String(p.Value),
				ApplyMethod:    applyMethod,
			})
		}

		_, err := c.rds.ModifyDBParameterGroup(ctx, &rds.ModifyDBParameterGroupInput{
			DBParameterGroupName: aws.String(parameterGroupName),
			Parameters:           awsParams,
		})
		if err != nil {
			return errors.Wrapf(err, "modify parameter group (batch starting at %d)", i)
		}
	}

	return nil
}

// InstanceParameterGroupExists checks if a DB instance parameter group exists.
func (c *Client) InstanceParameterGroupExists(ctx context.Context, name string) (bool, error) {
	_, err := c.rds.DescribeDBParameterGroups(ctx, &rds.DescribeDBParameterGroupsInput{
		DBParameterGroupName: aws.String(name),
	})
	if err != nil {
		if strings.Contains(err.Error(), "DBParameterGroupNotFound") {
			return false, nil
		}
		return false, errors.Wrap(err, "describe parameter group")
	}
	return true, nil
}

// ==================== Blue-Green Deployment Methods ====================

// BlueGreenDeploymentInfo contains information about a Blue-Green deployment.
type BlueGreenDeploymentInfo struct {
	Identifier          string                      `json:"identifier"`
	Name                string                      `json:"name"`
	Source              string                      `json:"source"`                // Source cluster ARN
	Target              string                      `json:"target"`                // Target (green) cluster ARN
	TargetEngineVersion string                      `json:"target_engine_version"` // Engine version of the target cluster
	Status              string                      `json:"status"`                // PROVISIONING, AVAILABLE, SWITCHOVER_IN_PROGRESS, SWITCHOVER_COMPLETED, DELETING, etc.
	StatusDetails       string                      `json:"status_details"`
	Tasks               []BlueGreenTask             `json:"tasks"`
	SwitchoverDetails   []BlueGreenSwitchoverDetail `json:"switchover_details"`
}

// BlueGreenTask represents a task in the Blue-Green deployment process.
type BlueGreenTask struct {
	Name   string `json:"name"`   // e.g., CREATING_READ_REPLICA_OF_SOURCE, DB_ENGINE_VERSION_UPGRADE
	Status string `json:"status"` // PENDING, IN_PROGRESS, COMPLETED, FAILED
}

// BlueGreenSwitchoverDetail contains details about a member's switchover status.
type BlueGreenSwitchoverDetail struct {
	SourceMember string `json:"source_member"`
	TargetMember string `json:"target_member"`
	Status       string `json:"status"`
}

// CreateBlueGreenDeploymentParams contains parameters for creating a Blue-Green deployment.
type CreateBlueGreenDeploymentParams struct {
	DeploymentName                     string
	SourceClusterARN                   string
	TargetEngineVersion                string
	TargetDBClusterParameterGroupName  string
	TargetDBInstanceParameterGroupName string
}

// CreateBlueGreenDeployment creates a new Blue-Green deployment for engine upgrade.
func (c *Client) CreateBlueGreenDeployment(ctx context.Context, params CreateBlueGreenDeploymentParams) (*BlueGreenDeploymentInfo, error) {
	input := &rds.CreateBlueGreenDeploymentInput{
		BlueGreenDeploymentName: aws.String(params.DeploymentName),
		Source:                  aws.String(params.SourceClusterARN),
		TargetEngineVersion:     aws.String(params.TargetEngineVersion),
	}

	if params.TargetDBClusterParameterGroupName != "" {
		input.TargetDBClusterParameterGroupName = aws.String(params.TargetDBClusterParameterGroupName)
	}
	if params.TargetDBInstanceParameterGroupName != "" {
		input.TargetDBParameterGroupName = aws.String(params.TargetDBInstanceParameterGroupName)
	}

	out, err := c.rds.CreateBlueGreenDeployment(ctx, input)
	if err != nil {
		return nil, errors.Wrap(err, "create blue-green deployment")
	}

	return convertBlueGreenDeployment(out.BlueGreenDeployment), nil
}

// DescribeBlueGreenDeployment retrieves information about a Blue-Green deployment.
func (c *Client) DescribeBlueGreenDeployment(ctx context.Context, identifier string) (*BlueGreenDeploymentInfo, error) {
	out, err := c.rds.DescribeBlueGreenDeployments(ctx, &rds.DescribeBlueGreenDeploymentsInput{
		BlueGreenDeploymentIdentifier: aws.String(identifier),
	})
	if err != nil {
		if strings.Contains(err.Error(), "BlueGreenDeploymentNotFound") {
			return nil, errors.Wrap(internalerrors.ErrBlueGreenDeploymentNotFound, identifier)
		}
		return nil, errors.Wrap(err, "describe blue-green deployment")
	}

	if len(out.BlueGreenDeployments) == 0 {
		return nil, errors.Wrap(internalerrors.ErrBlueGreenDeploymentNotFound, identifier)
	}

	return convertBlueGreenDeployment(&out.BlueGreenDeployments[0]), nil
}

// ListBlueGreenDeploymentsForCluster returns all Blue-Green deployments for a cluster.
func (c *Client) ListBlueGreenDeploymentsForCluster(ctx context.Context, clusterARN string) ([]*BlueGreenDeploymentInfo, error) {
	out, err := c.rds.DescribeBlueGreenDeployments(ctx, &rds.DescribeBlueGreenDeploymentsInput{})
	if err != nil {
		return nil, errors.Wrap(err, "describe blue-green deployments")
	}

	var result []*BlueGreenDeploymentInfo
	for i := range out.BlueGreenDeployments {
		bg := &out.BlueGreenDeployments[i]
		// Filter by source or target cluster ARN
		if aws.ToString(bg.Source) == clusterARN || aws.ToString(bg.Target) == clusterARN {
			info := convertBlueGreenDeployment(bg)

			// Fetch target engine version from the target cluster if available
			if info.Target != "" && (info.Status == "PROVISIONING" || info.Status == "AVAILABLE") {
				// Extract cluster ID from ARN (format: arn:aws:rds:region:account:cluster:cluster-id)
				targetClusterID := extractClusterIDFromARN(info.Target)
				if targetClusterID != "" {
					if clusterInfo, err := c.GetClusterInfo(ctx, targetClusterID); err == nil {
						info.TargetEngineVersion = clusterInfo.EngineVersion
					}
				}
			}

			result = append(result, info)
		}
	}

	return result, nil
}

// extractClusterIDFromARN extracts the cluster identifier from an ARN.
// ARN format: arn:aws:rds:region:account:cluster:cluster-id
func extractClusterIDFromARN(arn string) string {
	parts := strings.Split(arn, ":")
	if len(parts) >= 7 && parts[5] == "cluster" {
		return parts[6]
	}
	return ""
}

// SwitchoverBlueGreenDeployment performs the switchover from blue to green environment.
func (c *Client) SwitchoverBlueGreenDeployment(ctx context.Context, identifier string, timeoutSeconds int) error {
	input := &rds.SwitchoverBlueGreenDeploymentInput{
		BlueGreenDeploymentIdentifier: aws.String(identifier),
	}
	if timeoutSeconds > 0 {
		input.SwitchoverTimeout = aws.Int32(int32(timeoutSeconds))
	}

	_, err := c.rds.SwitchoverBlueGreenDeployment(ctx, input)
	if err != nil {
		return errors.Wrap(err, "switchover blue-green deployment")
	}

	return nil
}

// DeleteBlueGreenDeployment deletes a Blue-Green deployment.
// After switchover, deleteTarget should be false (old instances are deleted separately).
func (c *Client) DeleteBlueGreenDeployment(ctx context.Context, identifier string, deleteTarget bool) error {
	input := &rds.DeleteBlueGreenDeploymentInput{
		BlueGreenDeploymentIdentifier: aws.String(identifier),
	}
	// Note: DeleteTarget cannot be true after switchover is completed
	if deleteTarget {
		input.DeleteTarget = aws.Bool(true)
	}

	_, err := c.rds.DeleteBlueGreenDeployment(ctx, input)
	if err != nil {
		return errors.Wrap(err, "delete blue-green deployment")
	}

	return nil
}

// GetClusterARN retrieves the ARN for a cluster.
func (c *Client) GetClusterARN(ctx context.Context, clusterID string) (string, error) {
	out, err := c.rds.DescribeDBClusters(ctx, &rds.DescribeDBClustersInput{
		DBClusterIdentifier: aws.String(clusterID),
	})
	if err != nil {
		if strings.Contains(err.Error(), "DBClusterNotFound") {
			return "", errors.Wrap(internalerrors.ErrClusterNotFound, clusterID)
		}
		return "", errors.Wrap(err, "describe cluster")
	}

	if len(out.DBClusters) == 0 {
		return "", errors.Wrap(internalerrors.ErrClusterNotFound, clusterID)
	}

	return aws.ToString(out.DBClusters[0].DBClusterArn), nil
}

// DeleteCluster deletes an RDS cluster (used for cleanup after Blue-Green switchover).
func (c *Client) DeleteCluster(ctx context.Context, clusterID string, skipFinalSnapshot bool) error {
	input := &rds.DeleteDBClusterInput{
		DBClusterIdentifier: aws.String(clusterID),
		SkipFinalSnapshot:   aws.Bool(skipFinalSnapshot),
	}

	_, err := c.rds.DeleteDBCluster(ctx, input)
	if err != nil {
		return errors.Wrap(err, "delete cluster")
	}

	return nil
}

// convertBlueGreenDeployment converts AWS SDK type to our internal type.
func convertBlueGreenDeployment(bg *types.BlueGreenDeployment) *BlueGreenDeploymentInfo {
	if bg == nil {
		return nil
	}

	info := &BlueGreenDeploymentInfo{
		Identifier:    aws.ToString(bg.BlueGreenDeploymentIdentifier),
		Name:          aws.ToString(bg.BlueGreenDeploymentName),
		Source:        aws.ToString(bg.Source),
		Target:        aws.ToString(bg.Target),
		Status:        aws.ToString(bg.Status),
		StatusDetails: aws.ToString(bg.StatusDetails),
	}

	for _, task := range bg.Tasks {
		info.Tasks = append(info.Tasks, BlueGreenTask{
			Name:   aws.ToString(task.Name),
			Status: aws.ToString(task.Status),
		})
	}

	for _, detail := range bg.SwitchoverDetails {
		info.SwitchoverDetails = append(info.SwitchoverDetails, BlueGreenSwitchoverDetail{
			SourceMember: aws.ToString(detail.SourceMember),
			TargetMember: aws.ToString(detail.TargetMember),
			Status:       aws.ToString(detail.Status),
		})
	}

	return info
}

// UpgradeTarget represents a valid upgrade target for an engine version.
type UpgradeTarget struct {
	EngineVersion         string `json:"engine_version"`
	Description           string `json:"description"`
	IsMajorVersionUpgrade bool   `json:"is_major_version_upgrade"`
	SupportsBlueGreen     bool   `json:"supports_blue_green"`
}

// OrderableInstanceType represents an available instance type for a given engine.
type OrderableInstanceType struct {
	InstanceClass       string   `json:"instance_class"`
	StorageType         string   `json:"storage_type"`
	AvailabilityZones   []string `json:"availability_zones"`
	SupportsClusterMode bool     `json:"supports_cluster_mode"`
}

// GetValidUpgradeTargets returns valid upgrade targets for a given engine and version.
// It filters to only include targets that support Blue-Green deployments.
func (c *Client) GetValidUpgradeTargets(ctx context.Context, engine, engineVersion string) ([]UpgradeTarget, error) {
	out, err := c.rds.DescribeDBEngineVersions(ctx, &rds.DescribeDBEngineVersionsInput{
		Engine:        aws.String(engine),
		EngineVersion: aws.String(engineVersion),
	})
	if err != nil {
		return nil, errors.Wrap(err, "describe db engine versions")
	}

	if len(out.DBEngineVersions) == 0 {
		return nil, errors.Errorf("engine version %s not found for engine %s", engineVersion, engine)
	}

	var targets []UpgradeTarget
	for _, target := range out.DBEngineVersions[0].ValidUpgradeTarget {
		// Check if Blue-Green deployment is supported for this upgrade target
		supportsBlueGreen := false
		for _, mechanism := range target.SupportedEngineModes {
			// Aurora PostgreSQL in provisioned mode supports Blue-Green
			if mechanism == "provisioned" {
				supportsBlueGreen = true
				break
			}
		}

		// For Aurora, Blue-Green is generally supported for the upgrade targets
		// The SupportedEngineModes indicates what modes the target version supports
		// We'll include targets and mark whether they support Blue-Green
		targets = append(targets, UpgradeTarget{
			EngineVersion:         aws.ToString(target.EngineVersion),
			Description:           aws.ToString(target.Description),
			IsMajorVersionUpgrade: aws.ToBool(target.IsMajorVersionUpgrade),
			SupportsBlueGreen:     supportsBlueGreen || len(target.SupportedEngineModes) == 0,
		})
	}

	return targets, nil
}

// GetOrderableInstanceTypes returns available instance types for a given engine and version.
// It filters to only include instance types that support Aurora cluster mode.
func (c *Client) GetOrderableInstanceTypes(ctx context.Context, engine, engineVersion string) ([]OrderableInstanceType, error) {
	// Use a map to deduplicate instance classes (same class can appear for different storage types)
	instanceMap := make(map[string]*OrderableInstanceType)

	paginator := rds.NewDescribeOrderableDBInstanceOptionsPaginator(c.rds, &rds.DescribeOrderableDBInstanceOptionsInput{
		Engine:        aws.String(engine),
		EngineVersion: aws.String(engineVersion),
	})

	for paginator.HasMorePages() {
		out, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, errors.Wrap(err, "describe orderable db instance options")
		}

		for _, opt := range out.OrderableDBInstanceOptions {
			// Only include options that support Aurora cluster mode
			supportsCluster := false
			for _, mode := range opt.SupportedEngineModes {
				if mode == "provisioned" {
					supportsCluster = true
					break
				}
			}
			if !supportsCluster {
				continue
			}

			instanceClass := aws.ToString(opt.DBInstanceClass)
			storageType := aws.ToString(opt.StorageType)

			// Skip serverless instance types - these require different configuration
			if strings.Contains(instanceClass, "serverless") {
				continue
			}

			// Add to map or update existing entry
			if existing, ok := instanceMap[instanceClass]; ok {
				// Add availability zones if not already present
				for _, az := range opt.AvailabilityZones {
					azName := aws.ToString(az.Name)
					found := false
					for _, existingAZ := range existing.AvailabilityZones {
						if existingAZ == azName {
							found = true
							break
						}
					}
					if !found {
						existing.AvailabilityZones = append(existing.AvailabilityZones, azName)
					}
				}
			} else {
				azs := make([]string, 0, len(opt.AvailabilityZones))
				for _, az := range opt.AvailabilityZones {
					azs = append(azs, aws.ToString(az.Name))
				}
				instanceMap[instanceClass] = &OrderableInstanceType{
					InstanceClass:       instanceClass,
					StorageType:         storageType,
					AvailabilityZones:   azs,
					SupportsClusterMode: true,
				}
			}
		}
	}

	// Convert map to slice and sort by instance class
	result := make([]OrderableInstanceType, 0, len(instanceMap))
	for _, inst := range instanceMap {
		result = append(result, *inst)
	}

	// Sort by instance class for consistent ordering
	// Instance classes follow pattern: db.<family>.<size>
	// We want to sort by family, then by size (logical ordering)
	sortInstanceTypes(result)

	return result, nil
}

// sortInstanceTypes sorts instance types in a logical order (by family then size).
func sortInstanceTypes(types []OrderableInstanceType) {
	// Define size order within a family
	sizeOrder := map[string]int{
		"micro": 1, "small": 2, "medium": 3, "large": 4,
		"xlarge": 5, "2xlarge": 6, "4xlarge": 7, "8xlarge": 8,
		"12xlarge": 9, "16xlarge": 10, "24xlarge": 11,
	}

	// Define family generation order (newer is better, list first)
	familyOrder := map[string]int{
		"r7g": 1, "r6g": 2, "r6i": 3, "r5": 4, "r4": 5,
		"x2g": 1, "x2i": 2,
		"m7g": 1, "m6g": 2, "m6i": 3, "m5": 4,
		"t4g": 1, "t3": 2,
	}

	// Sort using a custom comparator
	for i := 0; i < len(types)-1; i++ {
		for j := i + 1; j < len(types); j++ {
			if compareInstanceClass(types[i].InstanceClass, types[j].InstanceClass, familyOrder, sizeOrder) > 0 {
				types[i], types[j] = types[j], types[i]
			}
		}
	}
}

// compareInstanceClass compares two instance class strings.
// Returns negative if a < b, positive if a > b, 0 if equal.
func compareInstanceClass(a, b string, familyOrder, sizeOrder map[string]int) int {
	// Parse instance class: db.<family>.<size>
	partsA := strings.Split(strings.TrimPrefix(a, "db."), ".")
	partsB := strings.Split(strings.TrimPrefix(b, "db."), ".")

	if len(partsA) < 2 || len(partsB) < 2 {
		return strings.Compare(a, b)
	}

	familyA, sizeA := partsA[0], partsA[1]
	familyB, sizeB := partsB[0], partsB[1]

	// Compare by family first
	orderA := familyOrder[familyA]
	orderB := familyOrder[familyB]
	if orderA == 0 {
		orderA = 100 // Unknown families go last
	}
	if orderB == 0 {
		orderB = 100
	}

	if orderA != orderB {
		return orderA - orderB
	}

	// Same family generation, compare alphabetically by family name
	if familyA != familyB {
		return strings.Compare(familyA, familyB)
	}

	// Same family, compare by size
	sizeOrderA := sizeOrder[sizeA]
	sizeOrderB := sizeOrder[sizeB]
	if sizeOrderA == 0 {
		sizeOrderA = 50
	}
	if sizeOrderB == 0 {
		sizeOrderB = 50
	}

	return sizeOrderA - sizeOrderB
}

// ==================== RDS Proxy Methods ====================

// ProxyInfo contains information about an RDS Proxy.
type ProxyInfo struct {
	ProxyName    string `json:"proxy_name"`
	ProxyARN     string `json:"proxy_arn"`
	Status       string `json:"status"`        // available, creating, modifying, deleting, etc.
	EngineFamily string `json:"engine_family"` // MYSQL, POSTGRESQL
	Endpoint     string `json:"endpoint"`
	VpcID        string `json:"vpc_id"`
}

// ProxyTargetGroupInfo contains information about an RDS Proxy target group.
type ProxyTargetGroupInfo struct {
	TargetGroupName string `json:"target_group_name"`
	DBProxyName     string `json:"db_proxy_name"`
	DBClusterID     string `json:"db_cluster_id,omitempty"`
	DBInstanceID    string `json:"db_instance_id,omitempty"`
	Status          string `json:"status"`
	IsDefault       bool   `json:"is_default"`
}

// ProxyTargetInfo contains information about an RDS Proxy target.
type ProxyTargetInfo struct {
	TargetARN        string `json:"target_arn,omitempty"`
	Type             string `json:"type"` // TRACKED_CLUSTER, RDS_INSTANCE, RDS_SERVERLESS_ENDPOINT
	RDSResourceID    string `json:"rds_resource_id"`
	TrackedClusterID string `json:"tracked_cluster_id,omitempty"` // For TRACKED_CLUSTER targets, the cluster identifier
	Endpoint         string `json:"endpoint,omitempty"`
	Port             int32  `json:"port,omitempty"`
	TargetHealth     string `json:"target_health"` // AVAILABLE, UNAVAILABLE, REGISTERING, etc.
}

// ProxyWithTargets combines proxy info with its targets for convenience.
type ProxyWithTargets struct {
	Proxy        ProxyInfo              `json:"proxy"`
	TargetGroups []ProxyTargetGroupInfo `json:"target_groups"`
	Targets      []ProxyTargetInfo      `json:"targets"`
}

// FindProxiesForCluster discovers all RDS Proxies that have targets pointing at this cluster.
// It returns proxy information including target groups and targets.
func (c *Client) FindProxiesForCluster(ctx context.Context, clusterID string) ([]ProxyWithTargets, error) {
	var result []ProxyWithTargets

	// List all proxies (use direct call instead of paginator for mock compatibility)
	proxiesOut, err := c.rds.DescribeDBProxies(ctx, &rds.DescribeDBProxiesInput{})
	if err != nil {
		return nil, errors.Wrap(err, "describe db proxies")
	}

	for _, proxy := range proxiesOut.DBProxies {
		proxyName := aws.ToString(proxy.DBProxyName)

		// Get target groups for this proxy
		targetGroupsOut, err := c.rds.DescribeDBProxyTargetGroups(ctx, &rds.DescribeDBProxyTargetGroupsInput{
			DBProxyName: aws.String(proxyName),
		})
		if err != nil {
			// Skip proxies we can't read target groups for
			continue
		}

		// Check if any target group points to our cluster
		var matchingTargetGroups []ProxyTargetGroupInfo
		var allTargets []ProxyTargetInfo
		pointsToOurCluster := false

		for _, tg := range targetGroupsOut.TargetGroups {
			targetGroupName := aws.ToString(tg.TargetGroupName)

			// Get targets for this target group
			targetsOut, err := c.rds.DescribeDBProxyTargets(ctx, &rds.DescribeDBProxyTargetsInput{
				DBProxyName:     aws.String(proxyName),
				TargetGroupName: aws.String(targetGroupName),
			})
			if err != nil {
				continue
			}

			for _, target := range targetsOut.Targets {
				// Check if this target points to our cluster
				trackedClusterID := aws.ToString(target.TrackedClusterId)
				rdsResourceID := aws.ToString(target.RdsResourceId)

				if trackedClusterID == clusterID || rdsResourceID == clusterID {
					pointsToOurCluster = true
				}

				targetHealth := "UNKNOWN"
				if target.TargetHealth != nil {
					targetHealth = string(target.TargetHealth.State)
				}

				allTargets = append(allTargets, ProxyTargetInfo{
					TargetARN:        aws.ToString(target.TargetArn),
					Type:             string(target.Type),
					RDSResourceID:    rdsResourceID,
					TrackedClusterID: trackedClusterID,
					Endpoint:         aws.ToString(target.Endpoint),
					Port:             aws.ToInt32(target.Port),
					TargetHealth:     targetHealth,
				})
			}

			matchingTargetGroups = append(matchingTargetGroups, ProxyTargetGroupInfo{
				TargetGroupName: targetGroupName,
				DBProxyName:     proxyName,
				Status:          aws.ToString(tg.Status),
				IsDefault:       aws.ToBool(tg.IsDefault),
			})
		}

		if pointsToOurCluster {
			result = append(result, ProxyWithTargets{
				Proxy: ProxyInfo{
					ProxyName:    proxyName,
					ProxyARN:     aws.ToString(proxy.DBProxyArn),
					Status:       string(proxy.Status),
					EngineFamily: aws.ToString(proxy.EngineFamily),
					Endpoint:     aws.ToString(proxy.Endpoint),
					VpcID:        aws.ToString(proxy.VpcId),
				},
				TargetGroups: matchingTargetGroups,
				Targets:      allTargets,
			})
		}
	}

	return result, nil
}

// GetProxyTargets returns targets for a specific proxy and target group.
func (c *Client) GetProxyTargets(ctx context.Context, proxyName, targetGroupName string) ([]ProxyTargetInfo, error) {
	out, err := c.rds.DescribeDBProxyTargets(ctx, &rds.DescribeDBProxyTargetsInput{
		DBProxyName:     aws.String(proxyName),
		TargetGroupName: aws.String(targetGroupName),
	})
	if err != nil {
		return nil, errors.Wrap(err, "describe db proxy targets")
	}

	var targets []ProxyTargetInfo
	for _, target := range out.Targets {
		targetHealth := "UNKNOWN"
		if target.TargetHealth != nil {
			targetHealth = string(target.TargetHealth.State)
		}

		targets = append(targets, ProxyTargetInfo{
			TargetARN:        aws.ToString(target.TargetArn),
			Type:             string(target.Type),
			RDSResourceID:    aws.ToString(target.RdsResourceId),
			TrackedClusterID: aws.ToString(target.TrackedClusterId),
			Endpoint:         aws.ToString(target.Endpoint),
			Port:             aws.ToInt32(target.Port),
			TargetHealth:     targetHealth,
		})
	}

	return targets, nil
}

// RetargetProxyToCluster updates the proxy target group to point to a new cluster.
// This is done by deregistering old targets and registering the new cluster.
func (c *Client) RetargetProxyToCluster(ctx context.Context, proxyName, targetGroupName, newClusterID string) error {
	// First, get current targets so we know what to deregister
	currentTargets, err := c.GetProxyTargets(ctx, proxyName, targetGroupName)
	if err != nil {
		return errors.Wrap(err, "get current proxy targets")
	}

	// Deregister existing cluster targets (we only deregister cluster targets, not instance targets)
	for _, target := range currentTargets {
		if target.Type == "TRACKED_CLUSTER" && target.RDSResourceID != "" {
			_, err := c.rds.DeregisterDBProxyTargets(ctx, &rds.DeregisterDBProxyTargetsInput{
				DBProxyName:          aws.String(proxyName),
				TargetGroupName:      aws.String(targetGroupName),
				DBClusterIdentifiers: []string{target.RDSResourceID},
			})
			if err != nil {
				// Log but continue - the cluster might already be deregistered
				// This is common after Blue-Green switchover where the old cluster is renamed
				if !strings.Contains(err.Error(), "NotFound") {
					return errors.Wrapf(err, "deregister old cluster target %s", target.RDSResourceID)
				}
			}
		}
	}

	// Register the new cluster
	_, err = c.rds.RegisterDBProxyTargets(ctx, &rds.RegisterDBProxyTargetsInput{
		DBProxyName:          aws.String(proxyName),
		TargetGroupName:      aws.String(targetGroupName),
		DBClusterIdentifiers: []string{newClusterID},
	})
	if err != nil {
		return errors.Wrapf(err, "register new cluster target %s", newClusterID)
	}

	return nil
}

// DeregisterProxyTargets deregisters a cluster from a proxy target group.
// This is used before Blue-Green deployment creation since AWS doesn't support
// Blue-Green deployments for clusters with RDS Proxy targets.
func (c *Client) DeregisterProxyTargets(ctx context.Context, proxyName, targetGroupName, clusterID string) error {
	_, err := c.rds.DeregisterDBProxyTargets(ctx, &rds.DeregisterDBProxyTargetsInput{
		DBProxyName:          aws.String(proxyName),
		TargetGroupName:      aws.String(targetGroupName),
		DBClusterIdentifiers: []string{clusterID},
	})
	if err != nil {
		// Ignore NotFound errors - cluster might already be deregistered
		if !strings.Contains(err.Error(), "NotFound") {
			return errors.Wrapf(err, "deregister cluster %s from proxy %s", clusterID, proxyName)
		}
	}
	return nil
}

// RegisterProxyTargets registers a cluster to a proxy target group.
// This is used after Blue-Green switchover to restore proxy connectivity.
func (c *Client) RegisterProxyTargets(ctx context.Context, proxyName, targetGroupName, clusterID string) error {
	_, err := c.rds.RegisterDBProxyTargets(ctx, &rds.RegisterDBProxyTargetsInput{
		DBProxyName:          aws.String(proxyName),
		TargetGroupName:      aws.String(targetGroupName),
		DBClusterIdentifiers: []string{clusterID},
	})
	if err != nil {
		return errors.Wrapf(err, "register cluster %s to proxy %s", clusterID, proxyName)
	}
	return nil
}

// WaitForProxyTargetsAvailable waits for proxy targets to become available.
func (c *Client) WaitForProxyTargetsAvailable(ctx context.Context, proxyName, targetGroupName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return errors.Errorf("timeout waiting for proxy %s targets to become available", proxyName)
			}

			targets, err := c.GetProxyTargets(ctx, proxyName, targetGroupName)
			if err != nil {
				continue // Retry on error
			}

			if len(targets) == 0 {
				continue // No targets yet, wait
			}

			// Check if all RDS_INSTANCE targets are available
			// NOTE: TRACKED_CLUSTER targets don't have TargetHealth - only RDS_INSTANCE targets do
			// So we only check instance targets for availability
			hasInstanceTargets := false
			allInstancesAvailable := true
			for _, target := range targets {
				if target.Type == "RDS_INSTANCE" {
					hasInstanceTargets = true
					if target.TargetHealth != "AVAILABLE" {
						allInstancesAvailable = false
						break
					}
				}
			}

			// Consider ready if we have instance targets and they're all available,
			// OR if we only have a TRACKED_CLUSTER target (instances will be added by RDS)
			if (hasInstanceTargets && allInstancesAvailable) || !hasInstanceTargets {
				return nil
			}
		}
	}
}

// ValidateProxyHealth checks if a proxy and its targets are healthy.
// Returns nil if healthy, or an error describing the health issue.
func (c *Client) ValidateProxyHealth(ctx context.Context, proxyWithTargets ProxyWithTargets) error {
	// Check proxy status
	if proxyWithTargets.Proxy.Status != "available" {
		return errors.Errorf("proxy %s is not available (status: %s)",
			proxyWithTargets.Proxy.ProxyName, proxyWithTargets.Proxy.Status)
	}

	// Check target groups have at least one
	if len(proxyWithTargets.TargetGroups) == 0 {
		return errors.Errorf("proxy %s has no target groups", proxyWithTargets.Proxy.ProxyName)
	}

	// Check targets are healthy
	hasHealthyTarget := false
	var unhealthyTargets []string

	for _, target := range proxyWithTargets.Targets {
		if target.TargetHealth == "AVAILABLE" {
			hasHealthyTarget = true
		} else {
			unhealthyTargets = append(unhealthyTargets, fmt.Sprintf("%s(%s)", target.RDSResourceID, target.TargetHealth))
		}
	}

	if !hasHealthyTarget {
		return errors.Errorf("proxy %s has no healthy targets: %v",
			proxyWithTargets.Proxy.ProxyName, unhealthyTargets)
	}

	return nil
}
