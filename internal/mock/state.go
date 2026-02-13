// Package mock provides a stateful mock RDS server for local demo and testing.
package mock

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"
)

// TimingConfig controls simulated wait times.
type TimingConfig struct {
	BaseWaitMs    int  // Base wait time in milliseconds
	RandomRangeMs int  // Random additional time (0 to this value)
	FastMode      bool // Skip all waits (instant transitions)
}

// DefaultTimingConfig returns fast demo defaults.
func DefaultTimingConfig() TimingConfig {
	return TimingConfig{
		BaseWaitMs:    500,
		RandomRangeMs: 200,
		FastMode:      false,
	}
}

// State holds the in-memory state for the mock RDS server.
type State struct {
	mu                   sync.RWMutex
	clusters             map[string]*MockCluster
	instances            map[string]*MockInstance
	snapshots            map[string]*MockSnapshot
	blueGreenDeployments map[string]*MockBlueGreenDeployment
	proxies              map[string]*MockDBProxy
	proxyTargetGroups    map[string]*MockDBProxyTargetGroup // key: proxyName/targetGroupName

	// Timing configuration
	timing TimingConfig

	// Fault injection
	faults *FaultInjector

	// For state transitions
	stopCh chan struct{}
}

// MockCluster represents a simulated RDS cluster.
type MockCluster struct {
	ID                        string
	Engine                    string
	EngineVersion             string
	Status                    string   // See rds.ClusterStatus for all possible values
	Members                   []string // Instance IDs (order matters: first is typically writer)
	StatusChangedAt           time.Time
	ParameterGroupName        string // Cluster parameter group name (for PG correlation in mock)
	LogicalReplicationEnabled bool   // Whether rds.logical_replication is enabled (for Blue-Green prereqs)
}

// MockInstance represents a simulated RDS instance.
type MockInstance struct {
	ID              string
	ClusterID       string
	InstanceType    string
	Status          string // See rds.InstanceStatus for all possible values
	IsWriter        bool
	IsAutoScaled    bool
	StorageType     string
	IOPS            *int32
	ARN             string
	PromotionTier   int32
	StatusChangedAt time.Time
	CreatedAt       time.Time

	// PerformanceInsightsEnabled indicates if Performance Insights is enabled on the instance.
	PerformanceInsightsEnabled bool

	// Pending modifications (applied when status becomes available)
	PendingInstanceType string
	PendingStorageType  string
	PendingIOPS         *int32

	// TransitionalStatus is an optional intermediate status before becoming available.
	// When set, instance will transition to this status first, then to available.
	// This simulates real AWS behavior like "configuring-enhanced-monitoring".
	TransitionalStatus string

	// PendingStatusChange simulates AWS async behavior where API returns before status updates.
	// If set, the instance will transition to this status at PendingStatusChangeAt time.
	PendingStatusChange   string
	PendingStatusChangeAt time.Time
}

// MockSnapshot represents a simulated RDS cluster snapshot.
type MockSnapshot struct {
	ID              string
	ClusterID       string
	Status          string // "creating", "available"
	Engine          string
	EngineVersion   string
	StatusChangedAt time.Time
	CreatedAt       time.Time
}

// MockBlueGreenDeployment represents a simulated Blue-Green deployment.
type MockBlueGreenDeployment struct {
	Identifier          string
	Name                string
	SourceClusterARN    string
	TargetClusterARN    string
	TargetEngineVersion string
	Status              string // PROVISIONING, AVAILABLE, SWITCHOVER_IN_PROGRESS, SWITCHOVER_COMPLETED, DELETING
	StatusDetails       string
	Tasks               []MockBlueGreenTask
	SwitchoverDetails   []MockBlueGreenSwitchoverDetail
	StatusChangedAt     time.Time
	CreatedAt           time.Time
}

// MockBlueGreenTask represents a task in the Blue-Green deployment.
type MockBlueGreenTask struct {
	Name   string // CREATING_READ_REPLICA_OF_SOURCE, DB_ENGINE_VERSION_UPGRADE, etc.
	Status string // PENDING, IN_PROGRESS, COMPLETED, FAILED
}

// MockBlueGreenSwitchoverDetail contains switchover details for a member.
type MockBlueGreenSwitchoverDetail struct {
	SourceMember string
	TargetMember string
	Status       string
}

// MockDBProxy represents a simulated RDS Proxy.
type MockDBProxy struct {
	ProxyName    string
	ProxyARN     string
	Status       string // available, creating, modifying, deleting
	EngineFamily string // POSTGRESQL, MYSQL
	Endpoint     string
	VpcID        string
}

// MockDBProxyTargetGroup represents a target group for an RDS Proxy.
type MockDBProxyTargetGroup struct {
	TargetGroupName string
	DBProxyName     string
	DBClusterID     string // Target cluster ID
	Status          string // available, creating
	IsDefault       bool
}

// MockDBProxyTarget represents a target within a proxy target group.
type MockDBProxyTarget struct {
	RDSResourceID string
	Type          string // TRACKED_CLUSTER, RDS_INSTANCE
	Endpoint      string
	Port          int32
	TargetHealth  string // AVAILABLE, UNAVAILABLE, REGISTERING
}

// AllTransitionalStatuses returns all RDS instance transitional statuses
// that will eventually become "available".
func AllTransitionalStatuses() []string {
	return []string{
		"backing-up",
		"configuring-enhanced-monitoring",
		"configuring-iam-database-auth",
		"configuring-log-exports",
		"configuring-performance-insights",
		"converting-to-vpc",
		"creating",
		"maintenance",
		"modifying",
		"moving-to-vpc",
		"rebooting",
		"resetting-master-credentials",
		"renaming",
		"starting",
		"storage-config-upgrade",
		"storage-optimization",
		"upgrading",
	}
}

// IsTransitionalStatus returns true if the given status is a transitional status
// that will eventually become "available".
func IsTransitionalStatus(status string) bool {
	for _, s := range AllTransitionalStatuses() {
		if s == status {
			return true
		}
	}
	return false
}

// NewState creates a new mock state with the given timing configuration.
func NewState(timing TimingConfig) *State {
	s := &State{
		clusters:             make(map[string]*MockCluster),
		instances:            make(map[string]*MockInstance),
		snapshots:            make(map[string]*MockSnapshot),
		blueGreenDeployments: make(map[string]*MockBlueGreenDeployment),
		proxies:              make(map[string]*MockDBProxy),
		proxyTargetGroups:    make(map[string]*MockDBProxyTargetGroup),
		timing:               timing,
		faults:               NewFaultInjector(),
		stopCh:               make(chan struct{}),
	}
	return s
}

// Start begins the background state transition goroutine.
func (s *State) Start() {
	go s.runStateTransitions()
}

// Stop halts the background state transition goroutine.
func (s *State) Stop() {
	close(s.stopCh)
}

// GetTiming returns the current timing configuration.
func (s *State) GetTiming() TimingConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.timing
}

// SetTiming updates the timing configuration.
func (s *State) SetTiming(timing TimingConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.timing = timing
}

// Faults returns the fault injector.
func (s *State) Faults() *FaultInjector {
	return s.faults
}

// getWaitDurationLocked calculates how long a state transition should take.
// MUST be called with s.mu held.
func (s *State) getWaitDurationLocked() time.Duration {
	if s.timing.FastMode {
		return 50 * time.Millisecond // Small delay even in fast mode for realism
	}
	base := s.timing.BaseWaitMs
	randomVal := 0
	if s.timing.RandomRangeMs > 0 {
		randomVal = rand.Intn(s.timing.RandomRangeMs + 1)
	}
	return time.Duration(base+randomVal) * time.Millisecond
}

// SeedDemoClusters populates the state with demo clusters.
func (s *State) SeedDemoClusters() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seedDemoClustersLocked()
}

// seedDemoClustersLocked populates the state with demo clusters.
// MUST be called with s.mu held.
func (s *State) seedDemoClustersLocked() {
	now := time.Now()

	// Demo 1: Single instance cluster (without logical replication - for testing prereqs alert)
	s.clusters["demo-single"] = &MockCluster{
		ID:                        "demo-single",
		Engine:                    "aurora-postgresql",
		EngineVersion:             "15.4",
		Status:                    "available",
		Members:                   []string{"demo-single-writer"},
		StatusChangedAt:           now,
		ParameterGroupName:        "demo-single-pg",
		LogicalReplicationEnabled: false, // Intentionally disabled for testing
	}
	s.instances["demo-single-writer"] = &MockInstance{
		ID:                         "demo-single-writer",
		ClusterID:                  "demo-single",
		InstanceType:               "db.r6g.large",
		Status:                     "available",
		IsWriter:                   true,
		IsAutoScaled:               false,
		StorageType:                "aurora",
		ARN:                        "arn:aws:rds:us-east-1:123456789012:db:demo-single-writer",
		PromotionTier:              1,
		PerformanceInsightsEnabled: true,
		StatusChangedAt:            now,
		CreatedAt:                  now.Add(-24 * time.Hour),
	}

	// Demo 2: Multi-instance cluster (1 writer + 2 readers)
	s.clusters["demo-multi"] = &MockCluster{
		ID:                        "demo-multi",
		Engine:                    "aurora-postgresql",
		EngineVersion:             "15.4",
		Status:                    "available",
		Members:                   []string{"demo-multi-writer", "demo-multi-reader-1", "demo-multi-reader-2"},
		StatusChangedAt:           now,
		ParameterGroupName:        "demo-multi-pg",
		LogicalReplicationEnabled: true, // Enabled for Blue-Green deployments
	}
	s.instances["demo-multi-writer"] = &MockInstance{
		ID:                         "demo-multi-writer",
		ClusterID:                  "demo-multi",
		InstanceType:               "db.r6g.large",
		Status:                     "available",
		IsWriter:                   true,
		IsAutoScaled:               false,
		StorageType:                "aurora",
		ARN:                        "arn:aws:rds:us-east-1:123456789012:db:demo-multi-writer",
		PromotionTier:              1,
		PerformanceInsightsEnabled: true,
		StatusChangedAt:            now,
		CreatedAt:                  now.Add(-48 * time.Hour),
	}
	s.instances["demo-multi-reader-1"] = &MockInstance{
		ID:                         "demo-multi-reader-1",
		ClusterID:                  "demo-multi",
		InstanceType:               "db.r6g.large",
		Status:                     "available",
		IsWriter:                   false,
		IsAutoScaled:               false,
		StorageType:                "aurora",
		ARN:                        "arn:aws:rds:us-east-1:123456789012:db:demo-multi-reader-1",
		PromotionTier:              2,
		PerformanceInsightsEnabled: true,
		StatusChangedAt:            now,
		CreatedAt:                  now.Add(-48 * time.Hour),
	}
	s.instances["demo-multi-reader-2"] = &MockInstance{
		ID:                         "demo-multi-reader-2",
		ClusterID:                  "demo-multi",
		InstanceType:               "db.r6g.large",
		Status:                     "available",
		IsWriter:                   false,
		IsAutoScaled:               false,
		StorageType:                "aurora",
		ARN:                        "arn:aws:rds:us-east-1:123456789012:db:demo-multi-reader-2",
		PromotionTier:              2,
		PerformanceInsightsEnabled: true,
		StatusChangedAt:            now,
		CreatedAt:                  now.Add(-48 * time.Hour),
	}

	// Demo 3: Mixed autoscaled cluster (1 writer + 1 TF reader + 2 autoscaled readers)
	s.clusters["demo-autoscaled"] = &MockCluster{
		ID:                        "demo-autoscaled",
		Engine:                    "aurora-postgresql",
		EngineVersion:             "15.4",
		Status:                    "available",
		Members:                   []string{"demo-autoscaled-writer", "demo-autoscaled-reader-1", "demo-autoscaled-asg-1", "demo-autoscaled-asg-2"},
		StatusChangedAt:           now,
		ParameterGroupName:        "demo-autoscaled-pg",
		LogicalReplicationEnabled: true, // Enabled for Blue-Green deployments
	}
	s.instances["demo-autoscaled-writer"] = &MockInstance{
		ID:                         "demo-autoscaled-writer",
		ClusterID:                  "demo-autoscaled",
		InstanceType:               "db.r6g.xlarge",
		Status:                     "available",
		IsWriter:                   true,
		IsAutoScaled:               false,
		StorageType:                "aurora",
		ARN:                        "arn:aws:rds:us-east-1:123456789012:db:demo-autoscaled-writer",
		PromotionTier:              1,
		PerformanceInsightsEnabled: true,
		StatusChangedAt:            now,
		CreatedAt:                  now.Add(-72 * time.Hour),
	}
	s.instances["demo-autoscaled-reader-1"] = &MockInstance{
		ID:                         "demo-autoscaled-reader-1",
		ClusterID:                  "demo-autoscaled",
		InstanceType:               "db.r6g.xlarge",
		Status:                     "available",
		IsWriter:                   false,
		IsAutoScaled:               false,
		StorageType:                "aurora",
		ARN:                        "arn:aws:rds:us-east-1:123456789012:db:demo-autoscaled-reader-1",
		PromotionTier:              2,
		PerformanceInsightsEnabled: true,
		StatusChangedAt:            now,
		CreatedAt:                  now.Add(-72 * time.Hour),
	}
	s.instances["demo-autoscaled-asg-1"] = &MockInstance{
		ID:                         "demo-autoscaled-asg-1",
		ClusterID:                  "demo-autoscaled",
		InstanceType:               "db.r6g.large",
		Status:                     "available",
		IsWriter:                   false,
		IsAutoScaled:               true, // Autoscaled!
		StorageType:                "aurora",
		ARN:                        "arn:aws:rds:us-east-1:123456789012:db:demo-autoscaled-asg-1",
		PromotionTier:              15,
		PerformanceInsightsEnabled: true,
		StatusChangedAt:            now,
		CreatedAt:                  now.Add(-1 * time.Hour),
	}
	s.instances["demo-autoscaled-asg-2"] = &MockInstance{
		ID:                         "demo-autoscaled-asg-2",
		ClusterID:                  "demo-autoscaled",
		InstanceType:               "db.r6g.large",
		Status:                     "available",
		IsWriter:                   false,
		IsAutoScaled:               true, // Autoscaled!
		StorageType:                "aurora",
		ARN:                        "arn:aws:rds:us-east-1:123456789012:db:demo-autoscaled-asg-2",
		PromotionTier:              15,
		PerformanceInsightsEnabled: true,
		StatusChangedAt:            now,
		CreatedAt:                  now.Add(-30 * time.Minute),
	}

	// Demo 4: Cluster ready for engine upgrade (no proxy)
	s.clusters["demo-upgrade"] = &MockCluster{
		ID:                        "demo-upgrade",
		Engine:                    "aurora-postgresql",
		EngineVersion:             "15.4",
		Status:                    "available",
		Members:                   []string{"demo-upgrade-writer", "demo-upgrade-reader-1"},
		StatusChangedAt:           now,
		ParameterGroupName:        "demo-upgrade-pg",
		LogicalReplicationEnabled: true, // Enabled for Blue-Green deployments
	}
	s.instances["demo-upgrade-writer"] = &MockInstance{
		ID:                         "demo-upgrade-writer",
		ClusterID:                  "demo-upgrade",
		InstanceType:               "db.r6g.large",
		Status:                     "available",
		IsWriter:                   true,
		IsAutoScaled:               false,
		StorageType:                "aurora",
		ARN:                        "arn:aws:rds:us-east-1:123456789012:db:demo-upgrade-writer",
		PromotionTier:              1,
		PerformanceInsightsEnabled: true,
		StatusChangedAt:            now,
		CreatedAt:                  now.Add(-96 * time.Hour),
	}
	s.instances["demo-upgrade-reader-1"] = &MockInstance{
		ID:                         "demo-upgrade-reader-1",
		ClusterID:                  "demo-upgrade",
		InstanceType:               "db.r6g.large",
		Status:                     "available",
		IsWriter:                   false,
		IsAutoScaled:               false,
		StorageType:                "aurora",
		ARN:                        "arn:aws:rds:us-east-1:123456789012:db:demo-upgrade-reader-1",
		PromotionTier:              2,
		PerformanceInsightsEnabled: true,
		StatusChangedAt:            now,
		CreatedAt:                  now.Add(-96 * time.Hour),
	}

	// Demo 5: Cluster with RDS Proxy (1 writer + 2 readers) - for testing proxy retargeting
	s.clusters["demo-proxy-cluster"] = &MockCluster{
		ID:                        "demo-proxy-cluster",
		Engine:                    "aurora-postgresql",
		EngineVersion:             "15.4",
		Status:                    "available",
		Members:                   []string{"demo-proxy-cluster-writer", "demo-proxy-cluster-reader-1", "demo-proxy-cluster-reader-2"},
		StatusChangedAt:           now,
		ParameterGroupName:        "demo-proxy-cluster-pg",
		LogicalReplicationEnabled: true, // Enabled for Blue-Green deployments
	}
	s.instances["demo-proxy-cluster-writer"] = &MockInstance{
		ID:                         "demo-proxy-cluster-writer",
		ClusterID:                  "demo-proxy-cluster",
		InstanceType:               "db.r6g.xlarge",
		Status:                     "available",
		IsWriter:                   true,
		IsAutoScaled:               false,
		StorageType:                "aurora",
		ARN:                        "arn:aws:rds:us-east-1:123456789012:db:demo-proxy-cluster-writer",
		PromotionTier:              1,
		PerformanceInsightsEnabled: true,
		StatusChangedAt:            now,
		CreatedAt:                  now.Add(-120 * time.Hour),
	}
	s.instances["demo-proxy-cluster-reader-1"] = &MockInstance{
		ID:                         "demo-proxy-cluster-reader-1",
		ClusterID:                  "demo-proxy-cluster",
		InstanceType:               "db.r6g.xlarge",
		Status:                     "available",
		IsWriter:                   false,
		IsAutoScaled:               false,
		StorageType:                "aurora",
		ARN:                        "arn:aws:rds:us-east-1:123456789012:db:demo-proxy-cluster-reader-1",
		PromotionTier:              2,
		PerformanceInsightsEnabled: true,
		StatusChangedAt:            now,
		CreatedAt:                  now.Add(-120 * time.Hour),
	}
	s.instances["demo-proxy-cluster-reader-2"] = &MockInstance{
		ID:                         "demo-proxy-cluster-reader-2",
		ClusterID:                  "demo-proxy-cluster",
		InstanceType:               "db.r6g.xlarge",
		Status:                     "available",
		IsWriter:                   false,
		IsAutoScaled:               false,
		StorageType:                "aurora",
		ARN:                        "arn:aws:rds:us-east-1:123456789012:db:demo-proxy-cluster-reader-2",
		PromotionTier:              2,
		PerformanceInsightsEnabled: true,
		StatusChangedAt:            now,
		CreatedAt:                  now.Add(-120 * time.Hour),
	}

	// Seed demo proxies
	s.seedDemoProxiesLocked()
}

// Reset clears all state and re-seeds demo clusters.
func (s *State) Reset() {
	// Clear faults first (has its own lock)
	s.faults.ClearAll()

	// Then reset and reseed state atomically
	s.mu.Lock()
	defer s.mu.Unlock()

	s.clusters = make(map[string]*MockCluster)
	s.instances = make(map[string]*MockInstance)
	s.snapshots = make(map[string]*MockSnapshot)
	s.blueGreenDeployments = make(map[string]*MockBlueGreenDeployment)
	s.proxies = make(map[string]*MockDBProxy)
	s.proxyTargetGroups = make(map[string]*MockDBProxyTargetGroup)

	// Seed demo clusters while still holding the lock
	s.seedDemoClustersLocked()
}

// GetCluster returns a cluster by ID.
func (s *State) GetCluster(id string) (*MockCluster, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.clusters[id]
	if !ok {
		return nil, false
	}
	// Return a copy
	clusterCopy := *c
	clusterCopy.Members = make([]string, len(c.Members))
	for i, m := range c.Members {
		clusterCopy.Members[i] = m
	}
	return &clusterCopy, true
}

// GetInstance returns an instance by ID.
func (s *State) GetInstance(id string) (*MockInstance, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	inst, ok := s.instances[id]
	if !ok {
		return nil, false
	}
	// Return a copy
	instCopy := *inst
	return &instCopy, true
}

// GetSnapshot returns a snapshot by ID.
func (s *State) GetSnapshot(id string) (*MockSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap, ok := s.snapshots[id]
	if !ok {
		return nil, false
	}
	snapCopy := *snap
	return &snapCopy, true
}

// ListClusters returns all clusters.
func (s *State) ListClusters() []*MockCluster {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*MockCluster, 0, len(s.clusters))
	for _, c := range s.clusters {
		clusterCopy := *c
		clusterCopy.Members = make([]string, len(c.Members))
		for i, m := range c.Members {
			clusterCopy.Members[i] = m
		}
		result = append(result, &clusterCopy)
	}
	return result
}

// ListInstances returns all instances.
func (s *State) ListInstances() []*MockInstance {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*MockInstance, 0, len(s.instances))
	for _, inst := range s.instances {
		instCopy := *inst
		result = append(result, &instCopy)
	}
	return result
}

// ListSnapshots returns all snapshots.
func (s *State) ListSnapshots() []*MockSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*MockSnapshot, 0, len(s.snapshots))
	for _, snap := range s.snapshots {
		snapCopy := *snap
		result = append(result, &snapCopy)
	}
	return result
}

// GetClusterInstances returns all instances for a cluster.
func (s *State) GetClusterInstances(clusterID string) []*MockInstance {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cluster, ok := s.clusters[clusterID]
	if !ok {
		return nil
	}

	result := make([]*MockInstance, 0, len(cluster.Members))
	for _, memberID := range cluster.Members {
		if inst, ok := s.instances[memberID]; ok {
			instCopy := *inst
			result = append(result, &instCopy)
		}
	}
	return result
}

// clusterHasPerformanceInsightsLocked checks if any instance in the cluster has Performance Insights enabled.
// MUST be called with s.mu held.
func (s *State) clusterHasPerformanceInsightsLocked(clusterID string) bool {
	cluster, ok := s.clusters[clusterID]
	if !ok {
		return false
	}
	for _, memberID := range cluster.Members {
		if inst, ok := s.instances[memberID]; ok && inst.PerformanceInsightsEnabled {
			return true
		}
	}
	return false
}

// isDemoClusterLocked checks if a cluster is one of the demo clusters.
// MUST be called with s.mu held.
func (s *State) isDemoClusterLocked(clusterID string) bool {
	switch clusterID {
	case "demo-single", "demo-multi", "demo-autoscaled", "demo-upgrade":
		return true
	default:
		return false
	}
}

// CreateInstance adds a new instance to the state.
func (s *State) CreateInstance(inst *MockInstance) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.instances[inst.ID]; exists {
		return fmt.Errorf("instance already exists: %s", inst.ID)
	}

	cluster, ok := s.clusters[inst.ClusterID]
	if !ok {
		return fmt.Errorf("cluster not found: %s", inst.ClusterID)
	}

	now := time.Now()
	inst.StatusChangedAt = now
	inst.CreatedAt = now
	inst.ARN = fmt.Sprintf("arn:aws:rds:us-east-1:123456789012:db:%s", inst.ID)

	// If the cluster has Performance Insights enabled, the new instance will also get it
	// and will need to go through configuring-performance-insights after creating
	if s.clusterHasPerformanceInsightsLocked(inst.ClusterID) {
		inst.TransitionalStatus = "configuring-performance-insights"
		// PI will be enabled after the transitional status completes
	}

	// For create, we'll just use "creating" immediately since the instance is new
	// The async behavior for create is that the instance might not show up
	// in DescribeDBInstances immediately, but that's harder to simulate
	inst.Status = "creating"

	s.instances[inst.ID] = inst
	cluster.Members = append(cluster.Members, inst.ID)

	return nil
}

// ModifyInstance updates an instance's configuration.
func (s *State) ModifyInstance(id string, instanceType, storageType string, iops *int32) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	inst, ok := s.instances[id]
	if !ok {
		return fmt.Errorf("instance not found: %s", id)
	}

	// Store pending changes
	if instanceType != "" {
		inst.PendingInstanceType = instanceType
	}
	if storageType != "" {
		inst.PendingStorageType = storageType
	}
	if iops != nil {
		inst.PendingIOPS = iops
	}

	// Simulate AWS async behavior: status change is delayed
	// The instance remains "available" briefly before transitioning to "modifying"
	// This mimics the race condition where modify API returns before status updates

	// If already modifying or has a pending status change, apply immediately
	// (this simulates AWS rejecting concurrent modifications)
	if inst.Status == "modifying" || inst.PendingStatusChange != "" {
		inst.Status = "modifying"
		inst.StatusChangedAt = time.Now()
		inst.PendingStatusChange = ""
		inst.PendingStatusChangeAt = time.Time{}
	} else {
		// Instance stays "available" for a short time before changing to "modifying"
		// This delay simulates the AWS API propagation delay (1-3 seconds typically)
		delay := time.Duration(1+rand.Intn(2)) * time.Second
		if s.timing.FastMode {
			delay = 50 * time.Millisecond // Faster for testing
		}
		inst.PendingStatusChange = "modifying"
		inst.PendingStatusChangeAt = time.Now().Add(delay)
	}

	return nil
}

// DeleteInstance marks an instance for deletion.
func (s *State) DeleteInstance(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	inst, ok := s.instances[id]
	if !ok {
		return fmt.Errorf("instance not found: %s", id)
	}

	// Simulate async behavior: DeleteDBInstance returns immediately
	// Instance remains available briefly before transitioning to deleting
	delay := time.Duration(500+rand.Intn(1000)) * time.Millisecond
	if s.timing.FastMode {
		delay = 30 * time.Millisecond
	}
	inst.PendingStatusChange = "deleting"
	inst.PendingStatusChangeAt = time.Now().Add(delay)

	return nil
}

// DeleteCluster marks a cluster for deletion.
func (s *State) DeleteCluster(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cluster, ok := s.clusters[id]
	if !ok {
		return fmt.Errorf("cluster not found: %s", id)
	}

	// Mark cluster as deleting
	cluster.Status = "deleting"
	cluster.StatusChangedAt = time.Now()

	return nil
}

// FailoverCluster performs a failover to the target instance.
func (s *State) FailoverCluster(clusterID, targetInstanceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cluster, ok := s.clusters[clusterID]
	if !ok {
		return fmt.Errorf("cluster not found: %s", clusterID)
	}

	targetInst, ok := s.instances[targetInstanceID]
	if !ok {
		return fmt.Errorf("instance not found: %s", targetInstanceID)
	}

	if targetInst.ClusterID != clusterID {
		return fmt.Errorf("instance %s does not belong to cluster %s", targetInstanceID, clusterID)
	}

	// Find current writer and demote
	for _, memberID := range cluster.Members {
		if inst, ok := s.instances[memberID]; ok {
			if inst.IsWriter {
				inst.IsWriter = false
				inst.Status = "modifying"
				inst.StatusChangedAt = time.Now()
			}
		}
	}

	// Promote target
	targetInst.IsWriter = true
	targetInst.Status = "modifying"
	targetInst.StatusChangedAt = time.Now()

	cluster.Status = "modifying"
	cluster.StatusChangedAt = time.Now()

	return nil
}

// ModifyCluster updates cluster settings (e.g., engine version).
func (s *State) ModifyCluster(clusterID, engineVersion string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cluster, ok := s.clusters[clusterID]
	if !ok {
		return fmt.Errorf("cluster not found: %s", clusterID)
	}

	cluster.Status = "upgrading"
	cluster.StatusChangedAt = time.Now()

	// Store the target version - will be applied when status becomes available
	// For simplicity, we'll apply it immediately but keep status as upgrading
	cluster.EngineVersion = engineVersion

	// Mark all instances as modifying too
	for _, memberID := range cluster.Members {
		if inst, ok := s.instances[memberID]; ok {
			inst.Status = "modifying"
			inst.StatusChangedAt = time.Now()
		}
	}

	return nil
}

// CreateSnapshot creates a new snapshot.
func (s *State) CreateSnapshot(clusterID, snapshotID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cluster, ok := s.clusters[clusterID]
	if !ok {
		return fmt.Errorf("cluster not found: %s", clusterID)
	}

	if _, exists := s.snapshots[snapshotID]; exists {
		return fmt.Errorf("snapshot already exists: %s", snapshotID)
	}

	now := time.Now()
	s.snapshots[snapshotID] = &MockSnapshot{
		ID:              snapshotID,
		ClusterID:       clusterID,
		Status:          "creating",
		Engine:          cluster.Engine,
		EngineVersion:   cluster.EngineVersion,
		StatusChangedAt: now,
		CreatedAt:       now,
	}

	return nil
}

// SetInstanceTransitionalStatus sets a transitional status that the instance
// will transition through before becoming available. This is useful for simulating
// scenarios like "configuring-enhanced-monitoring" or "configuring-iam-database-auth".
func (s *State) SetInstanceTransitionalStatus(instanceID, transitionalStatus string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	inst, ok := s.instances[instanceID]
	if !ok {
		return fmt.Errorf("instance not found: %s", instanceID)
	}

	if !IsTransitionalStatus(transitionalStatus) {
		return fmt.Errorf("unsupported transitional status: %s", transitionalStatus)
	}

	inst.TransitionalStatus = transitionalStatus
	return nil
}

// SetInstanceStatus directly sets an instance's status for testing purposes.
// This allows simulating various AWS scenarios like instances in error states.
func (s *State) SetInstanceStatus(instanceID, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	inst, ok := s.instances[instanceID]
	if !ok {
		return fmt.Errorf("instance not found: %s", instanceID)
	}

	inst.Status = status
	inst.StatusChangedAt = time.Now()
	return nil
}

// RebootInstance initiates a reboot of an instance.
func (s *State) RebootInstance(instanceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	inst, ok := s.instances[instanceID]
	if !ok {
		return fmt.Errorf("instance not found: %s", instanceID)
	}

	// Simulate async behavior: RebootDBInstance returns immediately
	// Instance remains available briefly before transitioning to rebooting
	delay := time.Duration(300+rand.Intn(700)) * time.Millisecond
	if s.timing.FastMode {
		delay = 20 * time.Millisecond
	}
	inst.PendingStatusChange = "rebooting"
	inst.PendingStatusChangeAt = time.Now().Add(delay)

	// For demo clusters, writer instances go through storage-optimization after reboot
	// This simulates real AWS behavior where storage optimization can occur during restarts
	if inst.IsWriter && s.isDemoClusterLocked(inst.ClusterID) {
		inst.TransitionalStatus = "storage-optimization"
	} else if s.clusterHasPerformanceInsightsLocked(inst.ClusterID) {
		// If the cluster has Performance Insights enabled, the instance needs to configure PI after reboot
		inst.TransitionalStatus = "configuring-performance-insights"
		// Temporarily disable PI - it will be re-enabled after configuring-performance-insights
		inst.PerformanceInsightsEnabled = false
	}
	return nil
}

// StartInstance starts a stopped instance.
func (s *State) StartInstance(instanceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	inst, ok := s.instances[instanceID]
	if !ok {
		return fmt.Errorf("instance not found: %s", instanceID)
	}

	if inst.Status != "stopped" {
		return fmt.Errorf("instance %s cannot be started: current status is %s", instanceID, inst.Status)
	}

	inst.Status = "starting"
	inst.StatusChangedAt = time.Now()

	// If the cluster has Performance Insights enabled, the instance needs to configure PI after starting
	if s.clusterHasPerformanceInsightsLocked(inst.ClusterID) {
		inst.TransitionalStatus = "configuring-performance-insights"
		// Temporarily disable PI - it will be re-enabled after configuring-performance-insights
		inst.PerformanceInsightsEnabled = false
	}
	return nil
}

// StopInstance stops an instance.
func (s *State) StopInstance(instanceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	inst, ok := s.instances[instanceID]
	if !ok {
		return fmt.Errorf("instance not found: %s", instanceID)
	}

	if inst.Status != "available" {
		return fmt.Errorf("instance %s cannot be stopped: current status is %s", instanceID, inst.Status)
	}

	inst.Status = "stopping"
	inst.StatusChangedAt = time.Now()
	return nil
}

// ==================== Blue-Green Deployment Methods ====================

// CreateBlueGreenDeployment creates a new Blue-Green deployment.
func (s *State) CreateBlueGreenDeployment(name, sourceClusterARN, targetEngineVersion string) (*MockBlueGreenDeployment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Extract cluster ID from ARN
	sourceClusterID := extractClusterIDFromARN(sourceClusterARN)
	cluster, ok := s.clusters[sourceClusterID]
	if !ok {
		return nil, fmt.Errorf("source cluster not found: %s", sourceClusterID)
	}

	// Generate deployment identifier
	identifier := fmt.Sprintf("bgd-%s-%d", name, time.Now().UnixNano()%1000000)

	now := time.Now()
	bg := &MockBlueGreenDeployment{
		Identifier:          identifier,
		Name:                name,
		SourceClusterARN:    sourceClusterARN,
		TargetClusterARN:    "", // Will be set during provisioning
		TargetEngineVersion: targetEngineVersion,
		Status:              "PROVISIONING",
		StatusDetails:       "Creating green environment",
		Tasks: []MockBlueGreenTask{
			{Name: "CREATING_READ_REPLICA_OF_SOURCE", Status: "IN_PROGRESS"},
			{Name: "DB_ENGINE_VERSION_UPGRADE", Status: "PENDING"},
			{Name: "CREATE_DB_INSTANCES_FOR_CLUSTER", Status: "PENDING"},
		},
		SwitchoverDetails: []MockBlueGreenSwitchoverDetail{
			{
				SourceMember: sourceClusterARN,
				TargetMember: "", // Will be set during provisioning
				Status:       "PROVISIONING",
			},
		},
		StatusChangedAt: now,
		CreatedAt:       now,
	}

	// Add switchover details for each instance
	for _, memberID := range cluster.Members {
		if inst, ok := s.instances[memberID]; ok {
			bg.SwitchoverDetails = append(bg.SwitchoverDetails, MockBlueGreenSwitchoverDetail{
				SourceMember: inst.ARN,
				TargetMember: "", // Will be set during provisioning
				Status:       "PROVISIONING",
			})
		}
	}

	s.blueGreenDeployments[identifier] = bg
	return bg, nil
}

// GetBlueGreenDeployment returns a Blue-Green deployment by identifier.
func (s *State) GetBlueGreenDeployment(identifier string) (*MockBlueGreenDeployment, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	bg, ok := s.blueGreenDeployments[identifier]
	if !ok {
		return nil, false
	}
	// Return a copy
	bgCopy := *bg
	bgCopy.Tasks = make([]MockBlueGreenTask, len(bg.Tasks))
	for i, t := range bg.Tasks {
		bgCopy.Tasks[i] = t
	}
	bgCopy.SwitchoverDetails = make([]MockBlueGreenSwitchoverDetail, len(bg.SwitchoverDetails))
	for i, d := range bg.SwitchoverDetails {
		bgCopy.SwitchoverDetails[i] = d
	}
	return &bgCopy, true
}

// SwitchoverBlueGreenDeployment initiates a switchover.
func (s *State) SwitchoverBlueGreenDeployment(identifier string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	bg, ok := s.blueGreenDeployments[identifier]
	if !ok {
		return fmt.Errorf("blue-green deployment not found: %s", identifier)
	}

	if bg.Status != "AVAILABLE" {
		return fmt.Errorf("blue-green deployment is not available for switchover: %s", bg.Status)
	}

	bg.Status = "SWITCHOVER_IN_PROGRESS"
	bg.StatusDetails = "Performing switchover"
	bg.StatusChangedAt = time.Now()

	// Update switchover details
	for i := range bg.SwitchoverDetails {
		bg.SwitchoverDetails[i].Status = "SWITCHING_OVER"
	}

	return nil
}

// DeleteBlueGreenDeployment deletes a Blue-Green deployment.
func (s *State) DeleteBlueGreenDeployment(identifier string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	bg, ok := s.blueGreenDeployments[identifier]
	if !ok {
		return fmt.Errorf("blue-green deployment not found: %s", identifier)
	}

	bg.Status = "DELETING"
	bg.StatusChangedAt = time.Now()

	return nil
}

// extractClusterIDFromARN extracts the cluster ID from an ARN.
func extractClusterIDFromARN(arn string) string {
	// ARN format: arn:aws:rds:region:account:cluster:cluster-id
	parts := strings.Split(arn, ":cluster:")
	if len(parts) == 2 {
		return parts[1]
	}
	// Fallback: just return the ARN if it's not in ARN format (for local testing)
	return arn
}

// ListBlueGreenDeployments returns all Blue-Green deployments.
func (s *State) ListBlueGreenDeployments() []*MockBlueGreenDeployment {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*MockBlueGreenDeployment, 0, len(s.blueGreenDeployments))
	for _, bg := range s.blueGreenDeployments {
		bgCopy := *bg
		bgCopy.Tasks = make([]MockBlueGreenTask, len(bg.Tasks))
		for i, t := range bg.Tasks {
			bgCopy.Tasks[i] = t
		}
		bgCopy.SwitchoverDetails = make([]MockBlueGreenSwitchoverDetail, len(bg.SwitchoverDetails))
		for i, d := range bg.SwitchoverDetails {
			bgCopy.SwitchoverDetails[i] = d
		}
		result = append(result, &bgCopy)
	}
	return result
}

// performSwitchoverLocked performs the actual switchover operations for a Blue-Green deployment.
// This simulates AWS behavior: the source cluster/instances are renamed to -old1 suffix,
// and the green (upgraded) cluster takes over the original names.
// MUST be called with s.mu held (from processTransitions).
func (s *State) performSwitchoverLocked(sourceClusterID, targetEngineVersion string, now time.Time) {
	sourceCluster, ok := s.clusters[sourceClusterID]
	if !ok {
		return
	}

	oldClusterID := sourceClusterID + "-old1"

	// Step 1: Rename source instances to -old1 suffix
	var oldMemberIDs []string
	for _, memberID := range sourceCluster.Members {
		if inst, ok := s.instances[memberID]; ok {
			oldInstID := memberID + "-old1"
			oldMemberIDs = append(oldMemberIDs, oldInstID)

			// Create the "old" instance (renamed source)
			oldInst := &MockInstance{
				ID:                         oldInstID,
				ClusterID:                  oldClusterID,
				InstanceType:               inst.InstanceType,
				Status:                     "available",
				IsWriter:                   inst.IsWriter,
				IsAutoScaled:               inst.IsAutoScaled,
				StorageType:                inst.StorageType,
				IOPS:                       inst.IOPS,
				ARN:                        fmt.Sprintf("arn:aws:rds:us-east-1:123456789012:db:%s", oldInstID),
				PromotionTier:              inst.PromotionTier,
				PerformanceInsightsEnabled: inst.PerformanceInsightsEnabled,
				StatusChangedAt:            now,
				CreatedAt:                  inst.CreatedAt,
			}
			s.instances[oldInstID] = oldInst

			// The original instance stays with the same name - it represents the promoted green
			inst.StatusChangedAt = now
		}
	}

	// Step 2: Create the "old" cluster (renamed source)
	oldCluster := &MockCluster{
		ID:              oldClusterID,
		Engine:          sourceCluster.Engine,
		EngineVersion:   sourceCluster.EngineVersion, // Old engine version
		Status:          "available",
		Members:         oldMemberIDs,
		StatusChangedAt: now,
	}
	s.clusters[oldClusterID] = oldCluster

	// Step 3: Update the source cluster to the new engine version
	// The source cluster keeps its original name but now has the upgraded engine
	sourceCluster.EngineVersion = targetEngineVersion
	sourceCluster.StatusChangedAt = now
}

// ==================== RDS Proxy Methods ====================

// ListProxies returns all RDS Proxies.
func (s *State) ListProxies() []*MockDBProxy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*MockDBProxy, 0, len(s.proxies))
	for _, p := range s.proxies {
		proxyCopy := *p
		result = append(result, &proxyCopy)
	}
	return result
}

// GetProxy returns a proxy by name.
func (s *State) GetProxy(name string) (*MockDBProxy, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.proxies[name]
	if !ok {
		return nil, false
	}
	proxyCopy := *p
	return &proxyCopy, true
}

// GetProxyTargetGroups returns target groups for a proxy.
func (s *State) GetProxyTargetGroups(proxyName string) []*MockDBProxyTargetGroup {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*MockDBProxyTargetGroup
	for key, tg := range s.proxyTargetGroups {
		if strings.HasPrefix(key, proxyName+"/") {
			tgCopy := *tg
			result = append(result, &tgCopy)
		}
	}
	return result
}

// GetProxyTargetGroup returns a specific target group.
func (s *State) GetProxyTargetGroup(proxyName, targetGroupName string) (*MockDBProxyTargetGroup, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key := proxyName + "/" + targetGroupName
	tg, ok := s.proxyTargetGroups[key]
	if !ok {
		return nil, false
	}
	tgCopy := *tg
	return &tgCopy, true
}

// RegisterProxyTarget registers a cluster as a target for a proxy.
func (s *State) RegisterProxyTarget(proxyName, targetGroupName, clusterID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, ok := s.proxies[proxyName]
	if !ok {
		return fmt.Errorf("proxy not found: %s", proxyName)
	}

	key := proxyName + "/" + targetGroupName
	tg, ok := s.proxyTargetGroups[key]
	if !ok {
		return fmt.Errorf("target group not found: %s", targetGroupName)
	}

	tg.DBClusterID = clusterID
	return nil
}

// DeregisterProxyTarget removes a cluster from a proxy target group.
func (s *State) DeregisterProxyTarget(proxyName, targetGroupName, clusterID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := proxyName + "/" + targetGroupName
	tg, ok := s.proxyTargetGroups[key]
	if !ok {
		return fmt.Errorf("target group not found: %s", targetGroupName)
	}

	if tg.DBClusterID == clusterID {
		tg.DBClusterID = ""
	}
	return nil
}

// seedDemoProxiesLocked seeds demo RDS proxies.
// MUST be called with s.mu held.
func (s *State) seedDemoProxiesLocked() {
	// Create a demo proxy pointing at demo-proxy-cluster
	// This cluster has 3 instances and is designed for testing proxy retargeting
	s.proxies["demo-proxy"] = &MockDBProxy{
		ProxyName:    "demo-proxy",
		ProxyARN:     "arn:aws:rds:us-east-1:123456789012:db-proxy:prx-demo",
		Status:       "available",
		EngineFamily: "POSTGRESQL",
		Endpoint:     "demo-proxy.proxy-123456789012.us-east-1.rds.amazonaws.com",
		VpcID:        "vpc-12345678",
	}

	// Create default target group pointing at demo-proxy-cluster (3 instance cluster)
	s.proxyTargetGroups["demo-proxy/default"] = &MockDBProxyTargetGroup{
		TargetGroupName: "default",
		DBProxyName:     "demo-proxy",
		DBClusterID:     "demo-proxy-cluster",
		Status:          "available",
		IsDefault:       true,
	}
}
