package mock

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mpz/devops/tools/rds-maint-machine/internal/mock/templates"
)

// Template data types
type (
	regionData struct {
		Regions []string
	}

	clusterMemberData struct {
		ID            string
		IsWriter      string
		PromotionTier int32
	}

	clusterData struct {
		ID             string
		ARN            string
		Engine         string
		EngineVersion  string
		Status         string
		ParameterGroup string
		Members        []clusterMemberData
	}

	clustersData struct {
		Clusters []clusterData
	}

	instanceData struct {
		ID             string
		InstanceType   string
		Status         string
		ARN            string
		StorageType    string
		ClusterID      string
		ParameterGroup string
		IOPS           *int32
	}

	instancesData struct {
		Instances []instanceData
	}

	tagData struct {
		Key   string
		Value string
	}

	tagsData struct {
		Tags []tagData
	}

	parameterGroupData struct {
		Name        string
		Family      string
		Description string
	}

	parameterGroupsData struct {
		ParameterGroups []parameterGroupData
	}

	snapshotData struct {
		ID            string
		ClusterID     string
		Status        string
		Engine        string
		EngineVersion string
	}

	snapshotsData struct {
		Snapshots []snapshotData
	}

	blueGreenData struct {
		Identifier        string
		Name              string
		SourceClusterARN  string
		TargetClusterARN  string
		Status            string
		StatusDetails     string
		Tasks             []blueGreenTaskData
		SwitchoverDetails []blueGreenSwitchoverData
	}

	blueGreenTaskData struct {
		Name   string
		Status string
	}

	blueGreenSwitchoverData struct {
		SourceMember string
		TargetMember string
		Status       string
	}

	blueGreenDeploymentsData struct {
		Deployments []blueGreenData
	}

	engineVersionData struct {
		Engine               string
		EngineVersion        string
		ParameterGroupFamily string
		UpgradeTargets       []upgradeTargetData
	}

	upgradeTargetData struct {
		Engine      string
		Version     string
		Description string
		IsMajor     bool
	}

	orderableInstanceData struct {
		InstanceClass     string
		AvailabilityZones []string
	}

	orderableInstanceOptionsData struct {
		Engine        string
		EngineVersion string
		InstanceTypes []orderableInstanceData
	}

	// RDS Proxy data types
	proxyData struct {
		ProxyName    string
		ProxyARN     string
		Status       string
		EngineFamily string
		Endpoint     string
		VpcID        string
	}

	proxiesData struct {
		Proxies []proxyData
	}

	proxyTargetGroupData struct {
		TargetGroupName string
		DBProxyName     string
		Status          string
		IsDefault       string
	}

	proxyTargetGroupsData struct {
		DBProxyName  string
		TargetGroups []proxyTargetGroupData
	}

	proxyTargetData struct {
		RDSResourceID     string
		Type              string
		Endpoint          string
		Port              int32
		TargetHealthState string
	}

	proxyTargetsData struct {
		DBProxyName     string
		TargetGroupName string
		Targets         []proxyTargetData
	}
)

// Helper to execute templates
func (s *Server) executeTemplate(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(http.StatusOK)
	if err := templates.Execute(w, name, data); err != nil {
		s.logger.Error("failed to execute template", "template", name, "error", err)
	}
}

// simulateAPILatency adds a small delay to simulate real AWS API latency.
// This makes the demo more realistic by showing loading states in the UI.
func (s *Server) simulateAPILatency() {
	timing := s.state.GetTiming()
	if timing.FastMode {
		return // Skip delay in fast mode
	}
	// Simulate 200-500ms API latency (typical for AWS API calls)
	delay := 200 + (timing.BaseWaitMs / 5)
	if delay > 500 {
		delay = 500
	}
	time.Sleep(time.Duration(delay) * time.Millisecond)
}

// ==================== EC2 API Handlers (for regions) ====================

func (s *Server) handleDescribeRegions(w http.ResponseWriter, values url.Values) {
	data := regionData{
		Regions: []string{
			"us-east-1", "us-east-2", "us-west-1", "us-west-2",
			"eu-west-1", "eu-west-2", "eu-central-1",
			"ap-northeast-1", "ap-southeast-1", "ap-southeast-2",
		},
	}
	s.executeTemplate(w, "describe_regions.xml", data)
}

// ==================== RDS API Handlers ====================

func (s *Server) handleDescribeDBClusters(w http.ResponseWriter, values url.Values) {
	clusterID := values.Get("DBClusterIdentifier")

	var clusters []*MockCluster
	if clusterID != "" {
		cluster, ok := s.state.GetCluster(clusterID)
		if !ok {
			s.sendErrorResponse(w, "DBClusterNotFound", fmt.Sprintf("DBCluster %s not found", clusterID), 404)
			return
		}
		clusters = []*MockCluster{cluster}
	} else {
		clusters = s.state.ListClusters()
	}

	data := clustersData{Clusters: make([]clusterData, 0, len(clusters))}
	for _, cluster := range clusters {
		clusterARN := fmt.Sprintf("arn:aws:rds:us-east-1:123456789012:cluster:%s", cluster.ID)
		cd := clusterData{
			ID:             cluster.ID,
			ARN:            clusterARN,
			Engine:         cluster.Engine,
			EngineVersion:  cluster.EngineVersion,
			Status:         cluster.Status,
			ParameterGroup: "default.aurora-postgresql15",
			Members:        make([]clusterMemberData, 0),
		}
		for _, memberID := range cluster.Members {
			if inst, ok := s.state.GetInstance(memberID); ok {
				isWriter := "false"
				if inst.IsWriter {
					isWriter = "true"
				}
				cd.Members = append(cd.Members, clusterMemberData{
					ID:            memberID,
					IsWriter:      isWriter,
					PromotionTier: inst.PromotionTier,
				})
			}
		}
		data.Clusters = append(data.Clusters, cd)
	}
	s.executeTemplate(w, "describe_db_clusters.xml", data)
}

func (s *Server) handleDescribeDBInstances(w http.ResponseWriter, values url.Values) {
	instanceID := values.Get("DBInstanceIdentifier")

	// Check for db-cluster-id filter (used by optimized GetClusterInfo)
	filterClusterID := ""
	for i := 1; i <= 10; i++ {
		filterName := values.Get(fmt.Sprintf("Filters.Filter.%d.Name", i))
		if filterName == "db-cluster-id" {
			filterClusterID = values.Get(fmt.Sprintf("Filters.Filter.%d.Values.Value.1", i))
			break
		}
	}

	var instances []*MockInstance
	if instanceID != "" {
		inst, ok := s.state.GetInstance(instanceID)
		if !ok {
			s.sendErrorResponse(w, "DBInstanceNotFound", fmt.Sprintf("DBInstance %s not found", instanceID), 404)
			return
		}
		instances = []*MockInstance{inst}
	} else if filterClusterID != "" {
		// Filter by cluster ID
		allInstances := s.state.ListInstances()
		for _, inst := range allInstances {
			if inst.ClusterID == filterClusterID {
				instances = append(instances, inst)
			}
		}
	} else {
		instances = s.state.ListInstances()
	}

	data := instancesData{Instances: make([]instanceData, 0, len(instances))}
	for _, inst := range instances {
		data.Instances = append(data.Instances, instanceData{
			ID:             inst.ID,
			InstanceType:   inst.InstanceType,
			Status:         inst.Status,
			ARN:            inst.ARN,
			StorageType:    inst.StorageType,
			ClusterID:      inst.ClusterID,
			ParameterGroup: "default.aurora-postgresql15",
			IOPS:           inst.IOPS,
		})
	}
	s.executeTemplate(w, "describe_db_instances.xml", data)
}

func (s *Server) handleListTagsForResource(w http.ResponseWriter, values url.Values) {
	resourceName := values.Get("ResourceName")

	data := tagsData{Tags: make([]tagData, 0)}
	if strings.Contains(resourceName, ":db:") {
		parts := strings.Split(resourceName, ":db:")
		if len(parts) == 2 {
			instanceID := parts[1]
			if inst, ok := s.state.GetInstance(instanceID); ok && inst.IsAutoScaled {
				data.Tags = append(data.Tags, tagData{
					Key:   "application-autoscaling:resourceId",
					Value: "cluster:demo-autoscaled:reader",
				})
			}
		}
	}
	s.executeTemplate(w, "list_tags_for_resource.xml", data)
}

func (s *Server) handleCreateDBInstance(w http.ResponseWriter, values url.Values) {
	instanceID := values.Get("DBInstanceIdentifier")
	clusterID := values.Get("DBClusterIdentifier")
	instanceType := values.Get("DBInstanceClass")

	if instanceID == "" || clusterID == "" || instanceType == "" {
		s.sendErrorResponse(w, "InvalidParameterValue", "Missing required parameters", 400)
		return
	}

	faultResult := s.state.Faults().Check("CreateDBInstance", instanceID)
	if faultResult.ShouldFail {
		s.sendErrorResponse(w, faultResult.ErrorCode, faultResult.ErrorMsg, 400)
		return
	}

	promotionTier := int32(0)
	if pt := values.Get("PromotionTier"); pt != "" {
		if v, err := strconv.Atoi(pt); err == nil {
			promotionTier = int32(v)
		}
	}

	inst := &MockInstance{
		ID:            instanceID,
		ClusterID:     clusterID,
		InstanceType:  instanceType,
		StorageType:   "aurora",
		IsWriter:      false,
		IsAutoScaled:  false,
		PromotionTier: promotionTier,
	}

	if err := s.state.CreateInstance(inst); err != nil {
		s.sendErrorResponse(w, "InvalidParameterValue", err.Error(), 400)
		return
	}

	data := instanceData{
		ID:           instanceID,
		InstanceType: instanceType,
		ARN:          fmt.Sprintf("arn:aws:rds:us-east-1:123456789012:db:%s", instanceID),
		ClusterID:    clusterID,
	}
	s.executeTemplate(w, "create_db_instance.xml", data)
}

func (s *Server) handleModifyDBInstance(w http.ResponseWriter, values url.Values) {
	instanceID := values.Get("DBInstanceIdentifier")
	if instanceID == "" {
		s.sendErrorResponse(w, "InvalidParameterValue", "DBInstanceIdentifier is required", 400)
		return
	}

	faultResult := s.state.Faults().Check("ModifyDBInstance", instanceID)
	if faultResult.ShouldFail {
		s.sendErrorResponse(w, faultResult.ErrorCode, faultResult.ErrorMsg, 400)
		return
	}

	instanceType := values.Get("DBInstanceClass")
	storageType := values.Get("StorageType")
	var iops *int32
	if iopsStr := values.Get("Iops"); iopsStr != "" {
		if v, err := strconv.Atoi(iopsStr); err == nil {
			i := int32(v)
			iops = &i
		}
	}

	if err := s.state.ModifyInstance(instanceID, instanceType, storageType, iops); err != nil {
		s.sendErrorResponse(w, "DBInstanceNotFound", err.Error(), 404)
		return
	}

	inst, ok := s.state.GetInstance(instanceID)
	if !ok {
		s.sendErrorResponse(w, "DBInstanceNotFound", fmt.Sprintf("DBInstance %s not found after modification", instanceID), 404)
		return
	}
	data := instanceData{
		ID:           inst.ID,
		InstanceType: inst.InstanceType,
		Status:       inst.Status,
		ARN:          inst.ARN,
	}
	s.executeTemplate(w, "modify_db_instance.xml", data)
}

func (s *Server) handleDeleteDBInstance(w http.ResponseWriter, values url.Values) {
	instanceID := values.Get("DBInstanceIdentifier")
	if instanceID == "" {
		s.sendErrorResponse(w, "InvalidParameterValue", "DBInstanceIdentifier is required", 400)
		return
	}

	faultResult := s.state.Faults().Check("DeleteDBInstance", instanceID)
	if faultResult.ShouldFail {
		s.sendErrorResponse(w, faultResult.ErrorCode, faultResult.ErrorMsg, 400)
		return
	}

	inst, ok := s.state.GetInstance(instanceID)
	if !ok {
		s.sendErrorResponse(w, "DBInstanceNotFound", fmt.Sprintf("DBInstance %s not found", instanceID), 404)
		return
	}

	if err := s.state.DeleteInstance(instanceID); err != nil {
		s.sendErrorResponse(w, "InvalidParameterValue", err.Error(), 400)
		return
	}

	data := instanceData{ID: instanceID, ARN: inst.ARN}
	s.executeTemplate(w, "delete_db_instance.xml", data)
}

func (s *Server) handleFailoverDBCluster(w http.ResponseWriter, values url.Values) {
	clusterID := values.Get("DBClusterIdentifier")
	targetInstanceID := values.Get("TargetDBInstanceIdentifier")

	if clusterID == "" {
		s.sendErrorResponse(w, "InvalidParameterValue", "DBClusterIdentifier is required", 400)
		return
	}

	faultResult := s.state.Faults().Check("FailoverDBCluster", clusterID)
	if faultResult.ShouldFail {
		s.sendErrorResponse(w, faultResult.ErrorCode, faultResult.ErrorMsg, 400)
		return
	}

	if err := s.state.FailoverCluster(clusterID, targetInstanceID); err != nil {
		s.sendErrorResponse(w, "InvalidParameterValue", err.Error(), 400)
		return
	}

	cluster, ok := s.state.GetCluster(clusterID)
	if !ok {
		s.sendErrorResponse(w, "DBClusterNotFound", fmt.Sprintf("DBCluster %s not found after failover", clusterID), 404)
		return
	}
	data := clusterData{ID: cluster.ID, Status: cluster.Status}
	s.executeTemplate(w, "failover_db_cluster.xml", data)
}

func (s *Server) handleModifyDBCluster(w http.ResponseWriter, values url.Values) {
	clusterID := values.Get("DBClusterIdentifier")
	if clusterID == "" {
		s.sendErrorResponse(w, "InvalidParameterValue", "DBClusterIdentifier is required", 400)
		return
	}

	faultResult := s.state.Faults().Check("ModifyDBCluster", clusterID)
	if faultResult.ShouldFail {
		s.sendErrorResponse(w, faultResult.ErrorCode, faultResult.ErrorMsg, 400)
		return
	}

	engineVersion := values.Get("EngineVersion")
	if engineVersion != "" {
		if err := s.state.ModifyCluster(clusterID, engineVersion); err != nil {
			s.sendErrorResponse(w, "DBClusterNotFound", err.Error(), 404)
			return
		}
	}

	cluster, ok := s.state.GetCluster(clusterID)
	if !ok {
		s.sendErrorResponse(w, "DBClusterNotFound", fmt.Sprintf("DBCluster %s not found after modification", clusterID), 404)
		return
	}
	data := clusterData{
		ID:            cluster.ID,
		Engine:        cluster.Engine,
		EngineVersion: cluster.EngineVersion,
		Status:        cluster.Status,
	}
	s.executeTemplate(w, "modify_db_cluster.xml", data)
}

func (s *Server) handleCreateDBClusterSnapshot(w http.ResponseWriter, values url.Values) {
	clusterID := values.Get("DBClusterIdentifier")
	snapshotID := values.Get("DBClusterSnapshotIdentifier")

	if clusterID == "" || snapshotID == "" {
		s.sendErrorResponse(w, "InvalidParameterValue", "DBClusterIdentifier and DBClusterSnapshotIdentifier are required", 400)
		return
	}

	faultResult := s.state.Faults().Check("CreateDBClusterSnapshot", snapshotID)
	if faultResult.ShouldFail {
		s.sendErrorResponse(w, faultResult.ErrorCode, faultResult.ErrorMsg, 400)
		return
	}

	if err := s.state.CreateSnapshot(clusterID, snapshotID); err != nil {
		s.sendErrorResponse(w, "InvalidParameterValue", err.Error(), 400)
		return
	}

	snap, ok := s.state.GetSnapshot(snapshotID)
	if !ok {
		s.sendErrorResponse(w, "DBClusterSnapshotNotFound", fmt.Sprintf("Snapshot %s not found after creation", snapshotID), 404)
		return
	}
	data := snapshotData{
		ID:            snap.ID,
		ClusterID:     snap.ClusterID,
		Status:        snap.Status,
		Engine:        snap.Engine,
		EngineVersion: snap.EngineVersion,
	}
	s.executeTemplate(w, "create_db_cluster_snapshot.xml", data)
}

func (s *Server) handleDescribeDBClusterSnapshots(w http.ResponseWriter, values url.Values) {
	snapshotID := values.Get("DBClusterSnapshotIdentifier")

	var snapshots []*MockSnapshot
	if snapshotID != "" {
		snap, ok := s.state.GetSnapshot(snapshotID)
		if !ok {
			s.sendErrorResponse(w, "DBClusterSnapshotNotFound", fmt.Sprintf("Snapshot %s not found", snapshotID), 404)
			return
		}
		snapshots = []*MockSnapshot{snap}
	} else {
		snapshots = s.state.ListSnapshots()
	}

	data := snapshotsData{Snapshots: make([]snapshotData, 0, len(snapshots))}
	for _, snap := range snapshots {
		data.Snapshots = append(data.Snapshots, snapshotData{
			ID:            snap.ID,
			ClusterID:     snap.ClusterID,
			Status:        snap.Status,
			Engine:        snap.Engine,
			EngineVersion: snap.EngineVersion,
		})
	}
	s.executeTemplate(w, "describe_db_cluster_snapshots.xml", data)
}

// ==================== Parameter Group Handlers ====================

func (s *Server) handleDescribeDBClusterParameterGroups(w http.ResponseWriter, values url.Values) {
	pgName := values.Get("DBClusterParameterGroupName")

	data := parameterGroupsData{ParameterGroups: make([]parameterGroupData, 0)}
	if pgName != "" {
		if strings.HasPrefix(pgName, "default.") {
			family := strings.TrimPrefix(pgName, "default.")
			data.ParameterGroups = append(data.ParameterGroups, parameterGroupData{
				Name:        pgName,
				Family:      family,
				Description: fmt.Sprintf("Default cluster parameter group for %s", family),
			})
		} else {
			data.ParameterGroups = append(data.ParameterGroups, parameterGroupData{
				Name:        pgName,
				Family:      "aurora-postgresql15",
				Description: "Custom cluster parameter group",
			})
		}
	}
	s.executeTemplate(w, "describe_db_cluster_parameter_groups.xml", data)
}

func (s *Server) handleDescribeDBClusterParameters(w http.ResponseWriter, values url.Values) {
	s.executeTemplate(w, "describe_db_cluster_parameters.xml", nil)
}

func (s *Server) handleCreateDBClusterParameterGroup(w http.ResponseWriter, values url.Values) {
	pgName := values.Get("DBClusterParameterGroupName")
	family := values.Get("DBParameterGroupFamily")
	description := values.Get("Description")

	if pgName == "" || family == "" {
		s.sendErrorResponse(w, "InvalidParameterValue", "DBClusterParameterGroupName and DBParameterGroupFamily are required", 400)
		return
	}

	data := parameterGroupData{Name: pgName, Family: family, Description: description}
	s.executeTemplate(w, "create_db_cluster_parameter_group.xml", data)
}

func (s *Server) handleModifyDBClusterParameterGroup(w http.ResponseWriter, values url.Values) {
	pgName := values.Get("DBClusterParameterGroupName")
	if pgName == "" {
		s.sendErrorResponse(w, "InvalidParameterValue", "DBClusterParameterGroupName is required", 400)
		return
	}

	data := parameterGroupData{Name: pgName}
	s.executeTemplate(w, "modify_db_cluster_parameter_group.xml", data)
}

// DB Instance Parameter Group Handlers

func (s *Server) handleDescribeDBParameterGroups(w http.ResponseWriter, values url.Values) {
	pgName := values.Get("DBParameterGroupName")

	data := parameterGroupsData{ParameterGroups: make([]parameterGroupData, 0)}
	if pgName != "" {
		if strings.HasPrefix(pgName, "default.") {
			family := strings.TrimPrefix(pgName, "default.")
			data.ParameterGroups = append(data.ParameterGroups, parameterGroupData{
				Name:        pgName,
				Family:      family,
				Description: fmt.Sprintf("Default parameter group for %s", family),
			})
		} else {
			data.ParameterGroups = append(data.ParameterGroups, parameterGroupData{
				Name:        pgName,
				Family:      "aurora-postgresql15",
				Description: "Custom instance parameter group",
			})
		}
	}
	s.executeTemplate(w, "describe_db_parameter_groups.xml", data)
}

func (s *Server) handleDescribeDBParameters(w http.ResponseWriter, values url.Values) {
	s.executeTemplate(w, "describe_db_parameters.xml", nil)
}

func (s *Server) handleCreateDBParameterGroup(w http.ResponseWriter, values url.Values) {
	pgName := values.Get("DBParameterGroupName")
	family := values.Get("DBParameterGroupFamily")
	description := values.Get("Description")

	if pgName == "" || family == "" {
		s.sendErrorResponse(w, "InvalidParameterValue", "DBParameterGroupName and DBParameterGroupFamily are required", 400)
		return
	}

	data := parameterGroupData{Name: pgName, Family: family, Description: description}
	s.executeTemplate(w, "create_db_parameter_group.xml", data)
}

func (s *Server) handleModifyDBParameterGroup(w http.ResponseWriter, values url.Values) {
	pgName := values.Get("DBParameterGroupName")
	if pgName == "" {
		s.sendErrorResponse(w, "InvalidParameterValue", "DBParameterGroupName is required", 400)
		return
	}

	data := parameterGroupData{Name: pgName}
	s.executeTemplate(w, "modify_db_parameter_group.xml", data)
}

// ==================== Blue-Green Deployment Handlers ====================

func (s *Server) handleCreateBlueGreenDeployment(w http.ResponseWriter, values url.Values) {
	deploymentName := values.Get("BlueGreenDeploymentName")
	source := values.Get("Source")
	targetEngineVersion := values.Get("TargetEngineVersion")

	if deploymentName == "" || source == "" {
		s.sendErrorResponse(w, "InvalidParameterValue", "BlueGreenDeploymentName and Source are required", 400)
		return
	}

	faultResult := s.state.Faults().Check("CreateBlueGreenDeployment", deploymentName)
	if faultResult.ShouldFail {
		s.sendErrorResponse(w, faultResult.ErrorCode, faultResult.ErrorMsg, 400)
		return
	}

	bg, err := s.state.CreateBlueGreenDeployment(deploymentName, source, targetEngineVersion)
	if err != nil {
		s.sendErrorResponse(w, "InvalidParameterValue", err.Error(), 400)
		return
	}

	data := s.convertBlueGreenToData(bg)
	s.executeTemplate(w, "create_blue_green_deployment.xml", data)
}

func (s *Server) handleDescribeBlueGreenDeployments(w http.ResponseWriter, values url.Values) {
	identifier := values.Get("BlueGreenDeploymentIdentifier")

	var deployments []*MockBlueGreenDeployment
	if identifier != "" {
		bg, ok := s.state.GetBlueGreenDeployment(identifier)
		if !ok {
			s.sendErrorResponse(w, "BlueGreenDeploymentNotFound", fmt.Sprintf("Blue-Green deployment %s not found", identifier), 404)
			return
		}
		deployments = []*MockBlueGreenDeployment{bg}
	} else {
		deployments = s.state.ListBlueGreenDeployments()
	}

	data := blueGreenDeploymentsData{Deployments: make([]blueGreenData, 0, len(deployments))}
	for _, bg := range deployments {
		data.Deployments = append(data.Deployments, s.convertBlueGreenToData(bg))
	}
	s.executeTemplate(w, "describe_blue_green_deployments.xml", data)
}

func (s *Server) handleSwitchoverBlueGreenDeployment(w http.ResponseWriter, values url.Values) {
	identifier := values.Get("BlueGreenDeploymentIdentifier")
	if identifier == "" {
		s.sendErrorResponse(w, "InvalidParameterValue", "BlueGreenDeploymentIdentifier is required", 400)
		return
	}

	faultResult := s.state.Faults().Check("SwitchoverBlueGreenDeployment", identifier)
	if faultResult.ShouldFail {
		s.sendErrorResponse(w, faultResult.ErrorCode, faultResult.ErrorMsg, 400)
		return
	}

	if err := s.state.SwitchoverBlueGreenDeployment(identifier); err != nil {
		s.sendErrorResponse(w, "InvalidParameterValue", err.Error(), 400)
		return
	}

	bg, ok := s.state.GetBlueGreenDeployment(identifier)
	if !ok {
		s.sendErrorResponse(w, "BlueGreenDeploymentNotFound", fmt.Sprintf("Blue-Green deployment %s not found after switchover", identifier), 404)
		return
	}
	data := s.convertBlueGreenToData(bg)
	s.executeTemplate(w, "switchover_blue_green_deployment.xml", data)
}

func (s *Server) handleDeleteBlueGreenDeployment(w http.ResponseWriter, values url.Values) {
	identifier := values.Get("BlueGreenDeploymentIdentifier")
	if identifier == "" {
		s.sendErrorResponse(w, "InvalidParameterValue", "BlueGreenDeploymentIdentifier is required", 400)
		return
	}

	faultResult := s.state.Faults().Check("DeleteBlueGreenDeployment", identifier)
	if faultResult.ShouldFail {
		s.sendErrorResponse(w, faultResult.ErrorCode, faultResult.ErrorMsg, 400)
		return
	}

	if err := s.state.DeleteBlueGreenDeployment(identifier); err != nil {
		s.sendErrorResponse(w, "InvalidParameterValue", err.Error(), 400)
		return
	}

	bg, ok := s.state.GetBlueGreenDeployment(identifier)
	if !ok {
		s.sendErrorResponse(w, "BlueGreenDeploymentNotFound", fmt.Sprintf("Blue-Green deployment %s not found after deletion", identifier), 404)
		return
	}
	data := blueGreenData{Identifier: bg.Identifier, Status: bg.Status}
	s.executeTemplate(w, "delete_blue_green_deployment.xml", data)
}

func (s *Server) convertBlueGreenToData(bg *MockBlueGreenDeployment) blueGreenData {
	data := blueGreenData{
		Identifier:        bg.Identifier,
		Name:              bg.Name,
		SourceClusterARN:  bg.SourceClusterARN,
		TargetClusterARN:  bg.TargetClusterARN,
		Status:            bg.Status,
		StatusDetails:     bg.StatusDetails,
		Tasks:             make([]blueGreenTaskData, 0, len(bg.Tasks)),
		SwitchoverDetails: make([]blueGreenSwitchoverData, 0, len(bg.SwitchoverDetails)),
	}
	for _, task := range bg.Tasks {
		data.Tasks = append(data.Tasks, blueGreenTaskData{Name: task.Name, Status: task.Status})
	}
	for _, detail := range bg.SwitchoverDetails {
		data.SwitchoverDetails = append(data.SwitchoverDetails, blueGreenSwitchoverData{
			SourceMember: detail.SourceMember,
			TargetMember: detail.TargetMember,
			Status:       detail.Status,
		})
	}
	return data
}

// ==================== Cluster and Engine Version Handlers ====================

func (s *Server) handleDeleteDBCluster(w http.ResponseWriter, values url.Values) {
	clusterID := values.Get("DBClusterIdentifier")
	if clusterID == "" {
		s.sendErrorResponse(w, "InvalidParameterValue", "DBClusterIdentifier is required", 400)
		return
	}

	faultResult := s.state.Faults().Check("DeleteDBCluster", clusterID)
	if faultResult.ShouldFail {
		s.sendErrorResponse(w, faultResult.ErrorCode, faultResult.ErrorMsg, 400)
		return
	}

	cluster, ok := s.state.GetCluster(clusterID)
	if !ok {
		s.sendErrorResponse(w, "DBClusterNotFoundFault", fmt.Sprintf("DBCluster %s not found", clusterID), 404)
		return
	}

	if err := s.state.DeleteCluster(clusterID); err != nil {
		s.sendErrorResponse(w, "InvalidParameterValue", err.Error(), 400)
		return
	}

	data := clusterData{ID: clusterID, Engine: cluster.Engine, EngineVersion: cluster.EngineVersion}
	s.executeTemplate(w, "delete_db_cluster.xml", data)
}

func (s *Server) handleDescribeDBEngineVersions(w http.ResponseWriter, values url.Values) {
	engine := values.Get("Engine")
	engineVersion := values.Get("EngineVersion")

	if engine == "" {
		s.sendErrorResponse(w, "InvalidParameterValue", "Engine is required", 400)
		return
	}

	// Generate mock upgrade targets based on the current version
	var upgradeTargets []upgradeTargetData
	if engineVersion != "" {
		parts := strings.Split(engineVersion, ".")
		if len(parts) >= 2 {
			major, _ := strconv.Atoi(parts[0])
			minor, _ := strconv.Atoi(parts[1])

			// Add minor upgrades (next 2-3 patch versions)
			for i := 1; i <= 3; i++ {
				upgradeTargets = append(upgradeTargets, upgradeTargetData{
					Engine:      engine,
					Version:     fmt.Sprintf("%d.%d", major, minor+i),
					Description: fmt.Sprintf("PostgreSQL %d.%d", major, minor+i),
					IsMajor:     false,
				})
			}

			// Add major upgrades (next major version)
			upgradeTargets = append(upgradeTargets, upgradeTargetData{
				Engine:      engine,
				Version:     fmt.Sprintf("%d.1", major+1),
				Description: fmt.Sprintf("PostgreSQL %d.1", major+1),
				IsMajor:     true,
			})
			upgradeTargets = append(upgradeTargets, upgradeTargetData{
				Engine:      engine,
				Version:     fmt.Sprintf("%d.2", major+1),
				Description: fmt.Sprintf("PostgreSQL %d.2", major+1),
				IsMajor:     true,
			})
		}
	}

	paramGroupFamily := "aurora-postgresql15"
	if engineVersion != "" {
		parts := strings.Split(engineVersion, ".")
		if len(parts) > 0 {
			paramGroupFamily = fmt.Sprintf("aurora-postgresql%s", parts[0])
		}
	}

	data := engineVersionData{
		Engine:               engine,
		EngineVersion:        engineVersion,
		ParameterGroupFamily: paramGroupFamily,
		UpgradeTargets:       upgradeTargets,
	}
	s.executeTemplate(w, "describe_db_engine_versions.xml", data)
}

func (s *Server) handleRebootDBInstance(w http.ResponseWriter, values url.Values) {
	instanceID := values.Get("DBInstanceIdentifier")
	if instanceID == "" {
		s.sendErrorResponse(w, "MissingParameter", "DBInstanceIdentifier is required", 400)
		return
	}

	faultResult := s.state.Faults().Check("RebootDBInstance", instanceID)
	if faultResult.ShouldFail {
		s.sendErrorResponse(w, faultResult.ErrorCode, faultResult.ErrorMsg, 400)
		return
	}

	if err := s.state.RebootInstance(instanceID); err != nil {
		s.sendErrorResponse(w, "DBInstanceNotFound", err.Error(), 404)
		return
	}

	inst, ok := s.state.GetInstance(instanceID)
	if !ok {
		s.sendErrorResponse(w, "DBInstanceNotFound", fmt.Sprintf("DBInstance %s not found after reboot", instanceID), 404)
		return
	}
	data := instanceData{
		ID:           inst.ID,
		InstanceType: inst.InstanceType,
		Status:       inst.Status,
		ClusterID:    inst.ClusterID,
	}
	s.executeTemplate(w, "reboot_db_instance.xml", data)
}

func (s *Server) handleDescribeOrderableDBInstanceOptions(w http.ResponseWriter, values url.Values) {
	engine := values.Get("Engine")
	engineVersion := values.Get("EngineVersion")

	if engine == "" {
		s.sendErrorResponse(w, "InvalidParameterValue", "Engine is required", 400)
		return
	}

	// Generate mock instance types for Aurora PostgreSQL
	// Grouped by family in a logical order
	instanceTypes := []orderableInstanceData{
		// r6g family (Graviton2 - recommended)
		{InstanceClass: "db.r6g.large", AvailabilityZones: []string{"us-east-1a", "us-east-1b", "us-east-1c"}},
		{InstanceClass: "db.r6g.xlarge", AvailabilityZones: []string{"us-east-1a", "us-east-1b", "us-east-1c"}},
		{InstanceClass: "db.r6g.2xlarge", AvailabilityZones: []string{"us-east-1a", "us-east-1b", "us-east-1c"}},
		{InstanceClass: "db.r6g.4xlarge", AvailabilityZones: []string{"us-east-1a", "us-east-1b", "us-east-1c"}},
		{InstanceClass: "db.r6g.8xlarge", AvailabilityZones: []string{"us-east-1a", "us-east-1b", "us-east-1c"}},
		{InstanceClass: "db.r6g.12xlarge", AvailabilityZones: []string{"us-east-1a", "us-east-1b", "us-east-1c"}},
		{InstanceClass: "db.r6g.16xlarge", AvailabilityZones: []string{"us-east-1a", "us-east-1b", "us-east-1c"}},
		// r5 family (Intel)
		{InstanceClass: "db.r5.large", AvailabilityZones: []string{"us-east-1a", "us-east-1b", "us-east-1c"}},
		{InstanceClass: "db.r5.xlarge", AvailabilityZones: []string{"us-east-1a", "us-east-1b", "us-east-1c"}},
		{InstanceClass: "db.r5.2xlarge", AvailabilityZones: []string{"us-east-1a", "us-east-1b", "us-east-1c"}},
		{InstanceClass: "db.r5.4xlarge", AvailabilityZones: []string{"us-east-1a", "us-east-1b", "us-east-1c"}},
		{InstanceClass: "db.r5.8xlarge", AvailabilityZones: []string{"us-east-1a", "us-east-1b", "us-east-1c"}},
		{InstanceClass: "db.r5.12xlarge", AvailabilityZones: []string{"us-east-1a", "us-east-1b", "us-east-1c"}},
		{InstanceClass: "db.r5.16xlarge", AvailabilityZones: []string{"us-east-1a", "us-east-1b", "us-east-1c"}},
		{InstanceClass: "db.r5.24xlarge", AvailabilityZones: []string{"us-east-1a", "us-east-1b", "us-east-1c"}},
		// t4g family (burstable, Graviton)
		{InstanceClass: "db.t4g.micro", AvailabilityZones: []string{"us-east-1a", "us-east-1b", "us-east-1c"}},
		{InstanceClass: "db.t4g.small", AvailabilityZones: []string{"us-east-1a", "us-east-1b", "us-east-1c"}},
		{InstanceClass: "db.t4g.medium", AvailabilityZones: []string{"us-east-1a", "us-east-1b", "us-east-1c"}},
		{InstanceClass: "db.t4g.large", AvailabilityZones: []string{"us-east-1a", "us-east-1b", "us-east-1c"}},
		// t3 family (burstable, Intel)
		{InstanceClass: "db.t3.micro", AvailabilityZones: []string{"us-east-1a", "us-east-1b", "us-east-1c"}},
		{InstanceClass: "db.t3.small", AvailabilityZones: []string{"us-east-1a", "us-east-1b", "us-east-1c"}},
		{InstanceClass: "db.t3.medium", AvailabilityZones: []string{"us-east-1a", "us-east-1b", "us-east-1c"}},
		{InstanceClass: "db.t3.large", AvailabilityZones: []string{"us-east-1a", "us-east-1b", "us-east-1c"}},
	}

	data := orderableInstanceOptionsData{
		Engine:        engine,
		EngineVersion: engineVersion,
		InstanceTypes: instanceTypes,
	}
	s.executeTemplate(w, "describe_orderable_db_instance_options.xml", data)
}

// ==================== RDS Proxy Handlers ====================

func (s *Server) handleDescribeDBProxies(w http.ResponseWriter, values url.Values) {
	// Simulate API latency for realistic demo experience
	s.simulateAPILatency()

	proxyName := values.Get("DBProxyName")

	var proxies []*MockDBProxy
	if proxyName != "" {
		proxy, ok := s.state.GetProxy(proxyName)
		if !ok {
			s.sendErrorResponse(w, "DBProxyNotFoundFault", fmt.Sprintf("DBProxy %s not found", proxyName), 404)
			return
		}
		proxies = []*MockDBProxy{proxy}
	} else {
		proxies = s.state.ListProxies()
	}

	data := proxiesData{Proxies: make([]proxyData, 0, len(proxies))}
	for _, p := range proxies {
		data.Proxies = append(data.Proxies, proxyData{
			ProxyName:    p.ProxyName,
			ProxyARN:     p.ProxyARN,
			Status:       p.Status,
			EngineFamily: p.EngineFamily,
			Endpoint:     p.Endpoint,
			VpcID:        p.VpcID,
		})
	}
	s.executeTemplate(w, "describe_db_proxies.xml", data)
}

func (s *Server) handleDescribeDBProxyTargetGroups(w http.ResponseWriter, values url.Values) {
	// Simulate API latency for realistic demo experience
	s.simulateAPILatency()

	proxyName := values.Get("DBProxyName")
	if proxyName == "" {
		s.sendErrorResponse(w, "MissingParameter", "DBProxyName is required", 400)
		return
	}

	targetGroups := s.state.GetProxyTargetGroups(proxyName)
	data := proxyTargetGroupsData{
		DBProxyName:  proxyName,
		TargetGroups: make([]proxyTargetGroupData, 0, len(targetGroups)),
	}
	for _, tg := range targetGroups {
		isDefault := "false"
		if tg.IsDefault {
			isDefault = "true"
		}
		data.TargetGroups = append(data.TargetGroups, proxyTargetGroupData{
			TargetGroupName: tg.TargetGroupName,
			DBProxyName:     tg.DBProxyName,
			Status:          tg.Status,
			IsDefault:       isDefault,
		})
	}
	s.executeTemplate(w, "describe_db_proxy_target_groups.xml", data)
}

func (s *Server) handleDescribeDBProxyTargets(w http.ResponseWriter, values url.Values) {
	// Simulate API latency for realistic demo experience
	s.simulateAPILatency()

	proxyName := values.Get("DBProxyName")
	targetGroupName := values.Get("TargetGroupName")

	if proxyName == "" {
		s.sendErrorResponse(w, "MissingParameter", "DBProxyName is required", 400)
		return
	}

	// Default to "default" target group if not specified
	if targetGroupName == "" {
		targetGroupName = "default"
	}

	tg, ok := s.state.GetProxyTargetGroup(proxyName, targetGroupName)
	if !ok {
		s.sendErrorResponse(w, "DBProxyTargetGroupNotFoundFault",
			fmt.Sprintf("Target group %s not found for proxy %s", targetGroupName, proxyName), 404)
		return
	}

	data := proxyTargetsData{
		DBProxyName:     proxyName,
		TargetGroupName: targetGroupName,
		Targets:         make([]proxyTargetData, 0),
	}

	// If there's a cluster target, add it
	if tg.DBClusterID != "" {
		cluster, ok := s.state.GetCluster(tg.DBClusterID)
		if ok {
			// Add the cluster itself as a target
			data.Targets = append(data.Targets, proxyTargetData{
				RDSResourceID:     tg.DBClusterID,
				Type:              "TRACKED_CLUSTER",
				Endpoint:          tg.DBClusterID + ".cluster-123456789012.us-east-1.rds.amazonaws.com",
				Port:              5432,
				TargetHealthState: "AVAILABLE",
			})

			// Also add individual instances as RDS_INSTANCE targets
			for _, memberID := range cluster.Members {
				if inst, ok := s.state.GetInstance(memberID); ok {
					targetHealth := "AVAILABLE"
					if inst.Status != "available" {
						targetHealth = "UNAVAILABLE"
					}
					data.Targets = append(data.Targets, proxyTargetData{
						RDSResourceID:     memberID,
						Type:              "RDS_INSTANCE",
						Endpoint:          memberID + ".123456789012.us-east-1.rds.amazonaws.com",
						Port:              5432,
						TargetHealthState: targetHealth,
					})
				}
			}
		}
	}

	s.executeTemplate(w, "describe_db_proxy_targets.xml", data)
}

func (s *Server) handleRegisterDBProxyTargets(w http.ResponseWriter, values url.Values) {
	proxyName := values.Get("DBProxyName")
	targetGroupName := values.Get("TargetGroupName")

	if proxyName == "" {
		s.sendErrorResponse(w, "MissingParameter", "DBProxyName is required", 400)
		return
	}
	if targetGroupName == "" {
		targetGroupName = "default"
	}

	// Get cluster identifiers from the request
	clusterID := values.Get("DBClusterIdentifiers.member.1")
	if clusterID == "" {
		s.sendErrorResponse(w, "MissingParameter", "DBClusterIdentifiers is required", 400)
		return
	}

	if err := s.state.RegisterProxyTarget(proxyName, targetGroupName, clusterID); err != nil {
		s.sendErrorResponse(w, "InvalidParameterValue", err.Error(), 400)
		return
	}

	// Build targets response
	tg, ok := s.state.GetProxyTargetGroup(proxyName, targetGroupName)
	if !ok {
		s.sendErrorResponse(w, "DBProxyTargetGroupNotFoundFault",
			fmt.Sprintf("Target group %s not found for proxy %s", targetGroupName, proxyName), 404)
		return
	}

	data := proxyTargetsData{
		DBProxyName:     proxyName,
		TargetGroupName: targetGroupName,
		Targets:         make([]proxyTargetData, 0),
	}

	// If there's a cluster target, add it
	if tg.DBClusterID != "" {
		cluster, ok := s.state.GetCluster(tg.DBClusterID)
		if ok {
			// Add the cluster itself as a target
			data.Targets = append(data.Targets, proxyTargetData{
				RDSResourceID:     tg.DBClusterID,
				Type:              "TRACKED_CLUSTER",
				Endpoint:          tg.DBClusterID + ".cluster-123456789012.us-east-1.rds.amazonaws.com",
				Port:              5432,
				TargetHealthState: "AVAILABLE",
			})

			// Also add individual instances as RDS_INSTANCE targets
			for _, memberID := range cluster.Members {
				if inst, ok := s.state.GetInstance(memberID); ok {
					targetHealth := "AVAILABLE"
					if inst.Status != "available" {
						targetHealth = "UNAVAILABLE"
					}
					data.Targets = append(data.Targets, proxyTargetData{
						RDSResourceID:     memberID,
						Type:              "RDS_INSTANCE",
						Endpoint:          memberID + ".123456789012.us-east-1.rds.amazonaws.com",
						Port:              5432,
						TargetHealthState: targetHealth,
					})
				}
			}
		}
	}

	s.executeTemplate(w, "register_db_proxy_targets.xml", data)
}

func (s *Server) handleDeregisterDBProxyTargets(w http.ResponseWriter, values url.Values) {
	proxyName := values.Get("DBProxyName")
	targetGroupName := values.Get("TargetGroupName")

	if proxyName == "" {
		s.sendErrorResponse(w, "MissingParameter", "DBProxyName is required", 400)
		return
	}
	if targetGroupName == "" {
		targetGroupName = "default"
	}

	// Get cluster identifiers from the request
	clusterID := values.Get("DBClusterIdentifiers.member.1")
	if clusterID == "" {
		s.sendErrorResponse(w, "MissingParameter", "DBClusterIdentifiers is required", 400)
		return
	}

	if err := s.state.DeregisterProxyTarget(proxyName, targetGroupName, clusterID); err != nil {
		s.sendErrorResponse(w, "InvalidParameterValue", err.Error(), 400)
		return
	}

	// Return empty success response
	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`<DeregisterDBProxyTargetsResponse xmlns="http://rds.amazonaws.com/doc/2014-10-31/"><DeregisterDBProxyTargetsResult></DeregisterDBProxyTargetsResult></DeregisterDBProxyTargetsResponse>`))
}
