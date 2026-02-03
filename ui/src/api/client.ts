import type {
  Operation,
  OperationEvent,
  ClusterSummary,
  ClusterInfo,
  BlueGreenDeployment,
  UpgradeTarget,
  InstanceTypeOption,
  RegionsResponse,
  CreateOperationRequest,
  ResumeRequest,
  MockState,
  MockTiming,
  MockFault,
  ClusterProxiesResponse,
} from '@/types';

const MOCK_ENDPOINT = '/mock';

class ApiError extends Error {
  statusCode: number;
  isCredentialError: boolean;

  constructor(statusCode: number, message: string) {
    super(message);
    this.name = 'ApiError';
    this.statusCode = statusCode;
    this.isCredentialError = isCredentialError(message);
  }
}

// Detect AWS credential-related errors from error messages
function isCredentialError(message: string): boolean {
  const credentialPatterns = [
    /ExpiredToken/i,
    /InvalidClientTokenId/i,
    /InvalidAccessKeyId/i,
    /SignatureDoesNotMatch/i,
    /UnrecognizedClientException/i,
    /InvalidIdentityToken/i,
    /AccessDenied/i,
    /credentials/i,
    /security token.*invalid/i,
    /security token.*expired/i,
    /unable to locate credentials/i,
    /no credentials found/i,
  ];
  return credentialPatterns.some((pattern) => pattern.test(message));
}

async function handleResponse<T>(response: Response): Promise<T> {
  if (!response.ok) {
    const data = await response.json().catch(() => ({}));
    throw new ApiError(response.status, data.error || response.statusText);
  }
  return response.json();
}

// Normalize operation to ensure arrays are never null
function normalizeOperation(op: Operation): Operation {
  return {
    ...op,
    steps: op.steps ?? [],
  };
}

// Normalize events array
function normalizeEvents(events: OperationEvent[] | null): OperationEvent[] {
  return events ?? [];
}

// Regions
export async function getRegions(): Promise<RegionsResponse> {
  const res = await fetch('/api/regions');
  return handleResponse(res);
}

// Clusters
export async function getClusters(region: string): Promise<ClusterSummary[]> {
  const res = await fetch(`/api/regions/${region}/clusters`);
  const clusters = await handleResponse<ClusterSummary[]>(res);
  return clusters ?? [];
}

export async function getClusterInfo(
  clusterId: string,
  region?: string
): Promise<ClusterInfo> {
  const headers: Record<string, string> = { 'X-Cluster-Id': clusterId };
  if (region) headers['X-Region'] = region;
  const res = await fetch('/api/cluster', { headers });
  return handleResponse(res);
}

export async function getClusterBlueGreen(
  clusterId: string,
  region?: string
): Promise<BlueGreenDeployment[]> {
  const headers: Record<string, string> = { 'X-Cluster-Id': clusterId };
  if (region) headers['X-Region'] = region;
  const res = await fetch('/api/cluster/blue-green', { headers });
  const deployments = await handleResponse<BlueGreenDeployment[]>(res);
  return deployments ?? [];
}

export async function getClusterUpgradeTargets(
  clusterId: string,
  region?: string
): Promise<{ upgrade_targets: UpgradeTarget[] }> {
  const headers: Record<string, string> = { 'X-Cluster-Id': clusterId };
  if (region) headers['X-Region'] = region;
  const res = await fetch('/api/cluster/upgrade-targets', { headers });
  return handleResponse(res);
}

export async function getClusterInstanceTypes(
  clusterId: string,
  region?: string
): Promise<{ instance_types: InstanceTypeOption[] }> {
  const headers: Record<string, string> = { 'X-Cluster-Id': clusterId };
  if (region) headers['X-Region'] = region;
  const res = await fetch('/api/cluster/instance-types', { headers });
  return handleResponse(res);
}

export async function getClusterProxies(
  clusterId: string,
  region?: string
): Promise<ClusterProxiesResponse> {
  const headers: Record<string, string> = { 'X-Cluster-Id': clusterId };
  if (region) headers['X-Region'] = region;
  const res = await fetch('/api/cluster/proxies', { headers });
  const data = await handleResponse<ClusterProxiesResponse>(res);
  return {
    ...data,
    proxies: data.proxies ?? [],
  };
}

// Operations
export async function getOperations(): Promise<Operation[]> {
  const res = await fetch('/api/operations');
  const ops = await handleResponse<Operation[]>(res);
  return (ops ?? []).map(normalizeOperation);
}

export async function getOperation(id: string): Promise<Operation> {
  const res = await fetch(`/api/operations/${id}`);
  const op = await handleResponse<Operation>(res);
  return normalizeOperation(op);
}

export async function getOperationEvents(id: string): Promise<OperationEvent[]> {
  const res = await fetch(`/api/operations/${id}/events`);
  const events = await handleResponse<OperationEvent[]>(res);
  return normalizeEvents(events);
}

export async function createOperation(
  req: CreateOperationRequest
): Promise<Operation> {
  const res = await fetch('/api/operations', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  });
  const op = await handleResponse<Operation>(res);
  return normalizeOperation(op);
}

export async function deleteOperation(id: string): Promise<void> {
  const res = await fetch(`/api/operations/${id}`, { method: 'DELETE' });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new ApiError(res.status, data.error || res.statusText);
  }
}

export async function startOperation(id: string): Promise<Operation> {
  const res = await fetch(`/api/operations/${id}/start`, { method: 'POST' });
  const op = await handleResponse<Operation>(res);
  return normalizeOperation(op);
}

export async function pauseOperation(
  id: string,
  reason: string
): Promise<Operation> {
  const res = await fetch(`/api/operations/${id}/pause`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ reason }),
  });
  const op = await handleResponse<Operation>(res);
  return normalizeOperation(op);
}

export async function resumeOperation(
  id: string,
  req: ResumeRequest
): Promise<Operation> {
  const res = await fetch(`/api/operations/${id}/resume`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  });
  const op = await handleResponse<Operation>(res);
  return normalizeOperation(op);
}

export async function updateOperationTimeout(
  id: string,
  waitTimeout: number
): Promise<Operation> {
  const res = await fetch(`/api/operations/${id}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ wait_timeout: waitTimeout }),
  });
  const op = await handleResponse<Operation>(res);
  return normalizeOperation(op);
}

export async function updateOperationPauseSteps(
  id: string,
  pauseBeforeSteps: number[]
): Promise<Operation> {
  const res = await fetch(`/api/operations/${id}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ pause_before_steps: pauseBeforeSteps }),
  });
  const op = await handleResponse<Operation>(res);
  return normalizeOperation(op);
}

export async function deleteAllOperations(): Promise<void> {
  const res = await fetch('/api/operations', { method: 'DELETE' });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new ApiError(res.status, data.error || res.statusText);
  }
}

// Mock server endpoints (demo mode only)
export async function getMockState(): Promise<MockState> {
  const res = await fetch(`${MOCK_ENDPOINT}/state`);
  const state = await handleResponse<MockState>(res);
  // Normalize arrays that might be null from Go
  return {
    ...state,
    clusters: state.clusters ?? [],
    instances: state.instances ?? [],
    faults: state.faults ?? [],
  };
}

export async function updateMockTiming(timing: MockTiming): Promise<void> {
  const res = await fetch(`${MOCK_ENDPOINT}/timing`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(timing),
  });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new ApiError(res.status, data.error || res.statusText);
  }
}

export async function resetMockState(): Promise<void> {
  const res = await fetch(`${MOCK_ENDPOINT}/reset`, { method: 'POST' });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new ApiError(res.status, data.error || res.statusText);
  }
}

export async function addMockFault(
  fault: Omit<MockFault, 'id'>
): Promise<MockFault> {
  const res = await fetch(`${MOCK_ENDPOINT}/faults`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(fault),
  });
  return handleResponse(res);
}

export async function toggleMockFault(
  id: string,
  enabled: boolean
): Promise<void> {
  const res = await fetch(`${MOCK_ENDPOINT}/faults/${id}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ enabled }),
  });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new ApiError(res.status, data.error || res.statusText);
  }
}

export async function deleteMockFault(id: string): Promise<void> {
  const res = await fetch(`${MOCK_ENDPOINT}/faults/${id}`, { method: 'DELETE' });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new ApiError(res.status, data.error || res.statusText);
  }
}

export async function clearMockFaults(): Promise<void> {
  const res = await fetch(`${MOCK_ENDPOINT}/faults`, { method: 'DELETE' });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new ApiError(res.status, data.error || res.statusText);
  }
}

export { ApiError };
