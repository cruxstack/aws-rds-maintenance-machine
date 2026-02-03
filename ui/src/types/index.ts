// Operation types
export type OperationType =
  | 'instance_type_change'
  | 'storage_type_change'
  | 'engine_upgrade'
  | 'instance_cycle';

export type OperationState =
  | 'created'
  | 'running'
  | 'paused'
  | 'completed'
  | 'failed'
  | 'rolling_back'
  | 'rolled_back';

export type StepState =
  | 'pending'
  | 'in_progress'
  | 'waiting'
  | 'completed'
  | 'failed'
  | 'skipped';

export interface Step {
  id: string;
  name: string;
  description: string;
  state: StepState;
  action: string;
  parameters?: Record<string, unknown>;
  result?: Record<string, unknown>;
  error?: string;
  started_at?: string;
  completed_at?: string;
  wait_condition?: string;
  retry_count: number;
  max_retries: number;
}

export interface Operation {
  id: string;
  type: OperationType;
  state: OperationState;
  cluster_id: string;
  region: string;
  parameters: Record<string, unknown>;
  steps: Step[];
  current_step_index: number;
  error?: string;
  pause_reason?: string;
  wait_timeout?: number;
  pause_before_steps?: number[];
  created_at: string;
  updated_at: string;
  started_at?: string;
  completed_at?: string;
}

export interface OperationEvent {
  id: string;
  operation_id: string;
  type: string;
  message: string;
  data?: Record<string, unknown>;
  timestamp: string;
}

export interface ClusterSummary {
  cluster_id: string;
  engine: string;
  engine_version: string;
  status: string;
}

export interface InstanceInfo {
  instance_id: string;
  instance_type: string;
  role: 'writer' | 'reader';
  status: string;
  is_auto_scaled: boolean;
  storage_type?: string;
  iops?: number;
}

export interface ClusterInfo {
  cluster_id: string;
  engine: string;
  engine_version: string;
  status: string;
  instances: InstanceInfo[];
}

export interface BlueGreenDeployment {
  identifier: string;
  name?: string;
  status: string;
  source?: string;
  target?: string;
  target_engine_version?: string;
  tasks?: Array<{
    name: string;
    status: string;
  }>;
}

export interface UpgradeTarget {
  engine_version: string;
  is_major_version_upgrade: boolean;
}

export interface InstanceTypeOption {
  instance_class: string;
}

export interface RegionsResponse {
  regions: string[];
  default_region: string;
}

// Demo mode types
export interface MockTiming {
  BaseWaitMs: number;
  RandomRangeMs: number;
  FastMode: boolean;
}

export interface MockFault {
  id: string;
  type: 'api_error' | 'delay' | 'stuck';
  action?: string;
  target?: string;
  probability: number;
  enabled: boolean;
  params?: Record<string, unknown>;
}

export interface MockState {
  clusters: Array<{
    ID: string;
    Status: string;
    Engine: string;
    EngineVersion: string;
  }>;
  instances: Array<{
    ID: string;
    ClusterID: string;
    Status: string;
    InstanceType: string;
    IsWriter: boolean;
    IsAutoScaled: boolean;
    PerformanceInsightsEnabled?: boolean;
  }>;
  faults: MockFault[];
  timing: MockTiming;
}

// Create operation request
export interface CreateOperationRequest {
  type: OperationType;
  cluster_id: string;
  region?: string;
  params: Record<string, unknown>;
}

// Resume action
export type ResumeAction = 'continue' | 'rollback' | 'abort' | 'mark_complete';

export interface ResumeRequest {
  action: ResumeAction;
  comment?: string;
}

// RDS Proxy types
export interface ProxyTargetGroup {
  target_group_name: string;
  db_proxy_name: string;
  db_cluster_id?: string;
  status: string;
  is_default: boolean;
}

export interface ProxyTarget {
  target_arn?: string;
  type: string;
  rds_resource_id: string;
  endpoint?: string;
  port?: number;
  target_health: string;
}

export interface ProxyInfo {
  proxy_name: string;
  proxy_arn: string;
  status: string;
  engine_family: string;
  endpoint: string;
  vpc_id: string;
}

export interface ProxyWithTargets {
  proxy: ProxyInfo;
  target_groups: ProxyTargetGroup[];
  targets: ProxyTarget[];
}

export interface ClusterProxiesResponse {
  cluster_id: string;
  proxies: ProxyWithTargets[];
}
