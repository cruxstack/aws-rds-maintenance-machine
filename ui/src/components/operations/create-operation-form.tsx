import { useState, useEffect, useMemo, memo } from 'react';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Separator } from '@/components/ui/separator';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Info, AlertTriangle, Network } from 'lucide-react';
import * as api from '@/api/client';
import type {
  ClusterSummary,
  ClusterInfo,
  BlueGreenDeployment,
  UpgradeTarget,
  InstanceTypeOption,
  InstanceInfo,
  OperationType,
  CreateOperationRequest,
  ProxyWithTargets,
} from '@/types';

interface CreateOperationFormProps {
  onSubmit: (req: CreateOperationRequest) => Promise<void>;
  onError: (error: string, originalError?: unknown) => void;
}

export const CreateOperationForm = memo(function CreateOperationForm({
  onSubmit,
  onError,
}: CreateOperationFormProps) {
  // Region and cluster state
  const [regionsData, setRegionsData] = useState<{ regions: string[]; defaultRegion: string } | null>(null);
  const [selectedRegion, setSelectedRegion] = useState<string>('');
  const [clusters, setClusters] = useState<ClusterSummary[]>([]);
  const [selectedCluster, setSelectedCluster] = useState<string>('');
  const [isLoadingClusters, setIsLoadingClusters] = useState(false);

  // Cluster info for parameter population
  const [clusterInfo, setClusterInfo] = useState<ClusterInfo | null>(null);
  const [blueGreenDeployment, setBlueGreenDeployment] =
    useState<BlueGreenDeployment | null>(null);
  const [upgradeTargets, setUpgradeTargets] = useState<UpgradeTarget[]>([]);
  const [instanceTypes, setInstanceTypes] = useState<InstanceTypeOption[]>([]);
  const [clusterProxies, setClusterProxies] = useState<ProxyWithTargets[]>([]);
  const [isLoadingProxies, setIsLoadingProxies] = useState(false);

  // Form state
  const [operationType, setOperationType] = useState<OperationType | ''>('');
  const [targetInstanceType, setTargetInstanceType] = useState<string>('');
  const [targetEngineVersion, setTargetEngineVersion] = useState<string>('');
  const [parameterGroup, setParameterGroup] = useState<string>('');
  const [excludeInstances, setExcludeInstances] = useState<Set<string>>(
    new Set()
  );
  const [skipTempInstance, setSkipTempInstance] = useState(false);
  const [pauseBeforeProxyDeregister, setPauseBeforeProxyDeregister] = useState(true);
  const [pauseBeforeSwitchover, setPauseBeforeSwitchover] = useState(true);
  const [pauseBeforeCleanup, setPauseBeforeCleanup] = useState(true);
  const [isSubmitting, setIsSubmitting] = useState(false);

  // Derived state
  const regions = regionsData?.regions ?? [];
  const defaultRegion = regionsData?.defaultRegion ?? '';

  // Load regions on mount (only once)
  useEffect(() => {
    let mounted = true;
    api
      .getRegions()
      .then((data) => {
        if (!mounted) return;
        setRegionsData({
          regions: data.regions ?? [],
          defaultRegion: data.default_region ?? '',
        });
      })
      .catch((err) => {
        if (mounted) onError(`Failed to load regions: ${err.message}`, err);
      });
    return () => { mounted = false; };
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // Auto-select default region when regions data loads
  useEffect(() => {
    if (regionsData?.defaultRegion && !selectedRegion) {
      setSelectedRegion(regionsData.defaultRegion);
    }
  }, [regionsData, selectedRegion]);

  // Load clusters when region changes
  useEffect(() => {
    if (!selectedRegion) {
      setClusters([]);
      return;
    }
    let mounted = true;
    setIsLoadingClusters(true);
    api
      .getClusters(selectedRegion)
      .then((data) => {
        if (mounted) setClusters(data);
      })
      .catch((err) => {
        if (mounted) onError(`Failed to load clusters: ${err.message}`, err);
      })
      .finally(() => {
        if (mounted) setIsLoadingClusters(false);
      });
    return () => { mounted = false; };
  }, [selectedRegion]); // eslint-disable-line react-hooks/exhaustive-deps

  // Fetch cluster info when cluster changes
  useEffect(() => {
    if (!selectedCluster || !selectedRegion) {
      setClusterInfo(null);
      setBlueGreenDeployment(null);
      setUpgradeTargets([]);
      setInstanceTypes([]);
      return;
    }

    let mounted = true;

    const fetchData = async () => {
      try {
        const [info, bgDeployments, upgradeTargetsRes, instanceTypesRes] =
          await Promise.all([
            api.getClusterInfo(selectedCluster, selectedRegion),
            api.getClusterBlueGreen(selectedCluster, selectedRegion).catch(() => []),
            api
              .getClusterUpgradeTargets(selectedCluster, selectedRegion)
              .catch(() => ({ upgrade_targets: [] })),
            api
              .getClusterInstanceTypes(selectedCluster, selectedRegion)
              .catch(() => ({ instance_types: [] })),
          ]);

        if (!mounted) return;

        setClusterInfo(info);

        // Find active Blue-Green deployment
        const activeBg =
          (bgDeployments as BlueGreenDeployment[]).find(
            (bg) => bg.status === 'PROVISIONING' || bg.status === 'AVAILABLE'
          ) || null;
        setBlueGreenDeployment(activeBg);

        setUpgradeTargets(upgradeTargetsRes.upgrade_targets || []);
        setInstanceTypes(instanceTypesRes.instance_types || []);
      } catch (err) {
        if (mounted) onError(`Failed to load cluster info: ${(err as Error).message}`, err);
      }
    };

    fetchData();
    return () => { mounted = false; };
  }, [selectedCluster, selectedRegion]); // eslint-disable-line react-hooks/exhaustive-deps

  // Reset operation-specific fields when type changes
  useEffect(() => {
    setTargetInstanceType('');
    setTargetEngineVersion('');
    setParameterGroup('');
    setExcludeInstances(new Set());
    setSkipTempInstance(false);
    setPauseBeforeProxyDeregister(true);
    setPauseBeforeSwitchover(true);
    setPauseBeforeCleanup(true);
    setClusterProxies([]);
  }, [operationType]);

  // Fetch proxies when engine_upgrade is selected
  useEffect(() => {
    if (operationType !== 'engine_upgrade' || !selectedCluster || !selectedRegion) {
      setClusterProxies([]);
      return;
    }

    let mounted = true;
    setIsLoadingProxies(true);
    api
      .getClusterProxies(selectedCluster, selectedRegion)
      .then((data) => {
        if (mounted) setClusterProxies(data.proxies || []);
      })
      .catch((err) => {
        if (mounted) console.warn('Failed to load cluster proxies:', err.message);
      })
      .finally(() => {
        if (mounted) setIsLoadingProxies(false);
      });

    return () => { mounted = false; };
  }, [operationType, selectedCluster, selectedRegion]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();

    if (!selectedCluster || !operationType) {
      onError('Please select a cluster and operation type');
      return;
    }

    const params: Record<string, unknown> = {};

    if (operationType === 'instance_type_change') {
      if (!targetInstanceType) {
        onError('Please select a target instance type');
        return;
      }
      params.target_instance_type = targetInstanceType;
      if (excludeInstances.size > 0) {
        params.exclude_instances = Array.from(excludeInstances);
      }
      if (skipTempInstance) {
        params.skip_temp_instance = true;
      }
    } else if (operationType === 'engine_upgrade') {
      if (!targetEngineVersion) {
        onError('Please select a target engine version');
        return;
      }
      params.target_engine_version = targetEngineVersion;
      if (parameterGroup) {
        params.db_cluster_parameter_group_name = parameterGroup;
      }
      // Only send pause options if explicitly disabled (defaults are all true)
      if (!pauseBeforeProxyDeregister) {
        params.pause_before_proxy_deregister = false;
      }
      if (!pauseBeforeSwitchover) {
        params.pause_before_switchover = false;
      }
      if (!pauseBeforeCleanup) {
        params.pause_before_cleanup = false;
      }
    } else if (operationType === 'instance_cycle') {
      if (excludeInstances.size > 0) {
        params.exclude_instances = Array.from(excludeInstances);
      }
      if (skipTempInstance) {
        params.skip_temp_instance = true;
      }
    }

    setIsSubmitting(true);
    try {
      await onSubmit({
        type: operationType,
        cluster_id: selectedCluster,
        region: selectedRegion,
        params,
      });
      // Reset form on success
      setOperationType('');
      setTargetInstanceType('');
      setTargetEngineVersion('');
      setParameterGroup('');
      setExcludeInstances(new Set());
      setSkipTempInstance(false);
      setPauseBeforeProxyDeregister(true);
      setPauseBeforeSwitchover(true);
      setPauseBeforeCleanup(true);
    } finally {
      setIsSubmitting(false);
    }
  };

  const toggleExcludeInstance = (instanceId: string) => {
    setExcludeInstances((prev) => {
      const next = new Set(prev);
      if (next.has(instanceId)) {
        next.delete(instanceId);
      } else {
        next.add(instanceId);
      }
      return next;
    });
  };

  // Memoize derived values to prevent unnecessary re-renders
  const currentInstanceType = useMemo(() => {
    const writerInstance = clusterInfo?.instances.find((i) => i.role === 'writer');
    return writerInstance?.instance_type || '';
  }, [clusterInfo]);

  // Group instance types by family
  const instanceTypesByFamily = useMemo(
    () => groupInstanceTypesByFamily(instanceTypes),
    [instanceTypes]
  );

  // Group upgrade targets
  const { minorUpgrades, majorUpgrades } = useMemo(() => ({
    minorUpgrades: upgradeTargets.filter((t) => !t.is_major_version_upgrade),
    majorUpgrades: upgradeTargets.filter((t) => t.is_major_version_upgrade),
  }), [upgradeTargets]);

  // Non-autoscaled instances for exclusion
  const excludableInstances = useMemo(
    () => clusterInfo?.instances.filter((i) => !i.is_auto_scaled) || [],
    [clusterInfo]
  );

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-semibold">New Operation</CardTitle>
        <CardDescription>Create a new maintenance operation</CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={handleSubmit} className="space-y-4">
          {/* Region */}
          <div className="space-y-2">
            <Label>AWS Region</Label>
            <Select
              value={selectedRegion}
              onValueChange={(v) => {
                setSelectedRegion(v);
                setSelectedCluster('');
                setOperationType('');
              }}
            >
              <SelectTrigger>
                <SelectValue placeholder="Select region...">
                  {selectedRegion || undefined}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                {regions.map((region) => (
                  <SelectItem key={region} value={region}>
                    {region}
                    {region === defaultRegion && (
                      <span className="text-xs text-muted-foreground ml-2">(default)</span>
                    )}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {/* Cluster */}
          <div className="space-y-2">
            <Label>Cluster</Label>
            <Select
              value={selectedCluster}
              onValueChange={(v) => {
                setSelectedCluster(v);
                setOperationType('');
              }}
              disabled={!selectedRegion || isLoadingClusters}
            >
              <SelectTrigger>
                <SelectValue
                  placeholder={
                    isLoadingClusters
                      ? 'Loading clusters...'
                      : !selectedRegion
                        ? 'Select a region first...'
                        : clusters.length === 0
                          ? 'No Aurora clusters found'
                          : 'Select cluster...'
                  }
                >
                  {selectedCluster}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                {clusters.map((cluster) => (
                  <SelectItem key={cluster.cluster_id} value={cluster.cluster_id}>
                    <div className="flex flex-col items-start">
                      <span>{cluster.cluster_id}</span>
                      <span className="text-xs text-muted-foreground">
                        {cluster.engine} {cluster.engine_version}
                      </span>
                    </div>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {/* Operation Type */}
          <div className="space-y-2">
            <Label>Operation Type</Label>
            <Select
              value={operationType}
              onValueChange={(v) => setOperationType(v as OperationType)}
              disabled={!selectedCluster}
            >
              <SelectTrigger>
                <SelectValue
                  placeholder={
                    !selectedCluster
                      ? 'Select a cluster first...'
                      : 'Select operation...'
                  }
                />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="engine_upgrade">Engine Upgrade</SelectItem>
                <SelectItem value="instance_cycle">
                  Instance Cycle (Reboot)
                </SelectItem>
                <SelectItem value="instance_type_change">
                  Instance Type Change
                </SelectItem>
              </SelectContent>
            </Select>
          </div>

          {/* Operation-specific parameters */}
          {operationType === 'instance_type_change' && (
            <>
              <Separator />
              <div className="space-y-2">
                <Label>Target Instance Type</Label>
                <Select
                  value={targetInstanceType}
                  onValueChange={setTargetInstanceType}
                  disabled={instanceTypes.length === 0}

                >
                  <SelectTrigger>
                    <SelectValue
                      placeholder={
                        instanceTypes.length === 0
                          ? 'Loading instance types...'
                          : currentInstanceType
                            ? `Current: ${currentInstanceType}`
                            : 'Select instance type...'
                      }
                    />
                  </SelectTrigger>
                  <SelectContent
                    position="popper"
                    className="max-h-[300px]"
                    ref={(node) => {
                      // Scroll to current instance type when content mounts (if no value selected)
                      if (node && currentInstanceType && !targetInstanceType) {
                        // Use longer delay to wait for Radix animations/positioning
                        setTimeout(() => {
                          const item = node.querySelector(
                            `[data-instance-type="${currentInstanceType}"]`
                          ) as HTMLElement | null;
                          if (item) {
                            // Find the scrollable viewport
                            const viewport = node.querySelector('[data-radix-select-viewport]') as HTMLElement | null;
                            if (viewport) {
                              const itemTop = item.offsetTop;
                              const viewportHeight = viewport.clientHeight;
                              const itemHeight = item.offsetHeight;
                              // Center the item in the viewport
                              viewport.scrollTop = itemTop - (viewportHeight / 2) + (itemHeight / 2);
                            }
                          }
                        }, 100);
                      }
                    }}
                  >
                    {Object.entries(instanceTypesByFamily).map(
                      ([family, types]) => (
                        <SelectGroup key={family}>
                          <SelectLabel className="text-[10px] uppercase tracking-wider">
                            {family}
                            {family.endsWith('d') && (
                              <span className="ml-1 text-status-blue font-normal normal-case">
                                (read optimized)
                              </span>
                            )}
                          </SelectLabel>
                          {types.map((t) => (
                            <SelectItem
                              key={t.instance_class}
                              value={t.instance_class}
                              data-instance-type={t.instance_class}
                            >
                              {t.instance_class}
                              {t.instance_class === currentInstanceType && (
                                <span className="ml-2 text-status-blue text-xs">
                                  (current)
                                </span>
                              )}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      )
                    )}
                  </SelectContent>
                </Select>
              </div>

              {excludableInstances.length > 1 && (
                <ExcludeInstancesField
                  instances={excludableInstances}
                  selected={excludeInstances}
                  onToggle={toggleExcludeInstance}
                  helpText="Selected instances will be skipped and keep their current configuration."
                />
              )}

              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <Label htmlFor="skip-temp">Skip temp instance creation</Label>
                  <p className="text-xs text-muted-foreground">
                    Faster but less safe (no redundancy during operation)
                  </p>
                </div>
                <Switch
                  id="skip-temp"
                  checked={skipTempInstance}
                  onCheckedChange={setSkipTempInstance}
                />
              </div>
            </>
          )}

          {operationType === 'engine_upgrade' && (
            <>
              <Separator />

              {blueGreenDeployment && (
                <Alert variant="warning">
                  <AlertTriangle />
                  <AlertTitle>Existing Blue-Green Deployment Detected</AlertTitle>
                  <AlertDescription>
                    Status: {blueGreenDeployment.status} - This operation will
                    adopt the existing deployment instead of creating a new one.
                  </AlertDescription>
                </Alert>
              )}

              {isLoadingProxies ? (
                <Alert variant="info">
                  <Network className="h-4 w-4 animate-pulse" />
                  <AlertDescription>
                    Checking for RDS Proxies...
                  </AlertDescription>
                </Alert>
              ) : clusterProxies.length > 0 ? (
                <Alert variant="info">
                  <Network className="h-4 w-4" />
                  <AlertTitle>RDS Proxy Detected</AlertTitle>
                  <AlertDescription>
                    <span className="block mb-2">
                      {clusterProxies.length === 1
                        ? '1 RDS Proxy is targeting this cluster and will be automatically retargeted after switchover.'
                        : `${clusterProxies.length} RDS Proxies are targeting this cluster and will be automatically retargeted after switchover.`}
                    </span>
                    <div className="space-y-1">
                      {clusterProxies.map((p) => (
                        <div key={p.proxy.proxy_name} className="text-xs bg-muted/50 rounded px-2 py-1">
                          <span className="font-medium">{p.proxy.proxy_name}</span>
                          <span className="text-muted-foreground ml-2">
                            ({p.proxy.status})
                          </span>
                        </div>
                      ))}
                    </div>
                  </AlertDescription>
                </Alert>
              ) : null}

              <div className="space-y-2">
                <Label>Target Engine Version</Label>
                <Select
                  value={targetEngineVersion}
                  onValueChange={setTargetEngineVersion}
                  disabled={
                    blueGreenDeployment
                      ? true
                      : upgradeTargets.length === 0
                  }
                >
                  <SelectTrigger>
                    <SelectValue
                      placeholder={
                        blueGreenDeployment
                          ? blueGreenDeployment.target_engine_version || 'Existing deployment'
                          : upgradeTargets.length === 0
                            ? 'No upgrades available'
                            : clusterInfo?.engine_version
                              ? `Current: ${clusterInfo.engine_version}`
                              : 'Select target version...'
                      }
                    />
                  </SelectTrigger>
                  <SelectContent>
                    {clusterInfo && (
                      <SelectGroup>
                        <SelectLabel className="text-[10px] uppercase tracking-wider">
                          Current Version
                        </SelectLabel>
                        <SelectItem
                          key="current"
                          value={clusterInfo.engine_version}
                          disabled
                        >
                          {clusterInfo.engine_version}
                          <span className="ml-2 text-status-blue text-xs">
                            (current)
                          </span>
                        </SelectItem>
                      </SelectGroup>
                    )}
                    {minorUpgrades.length > 0 && (
                      <SelectGroup>
                        <SelectLabel className="text-[10px] uppercase tracking-wider">
                          Minor Upgrades
                        </SelectLabel>
                        {minorUpgrades.map((t) => (
                          <SelectItem
                            key={t.engine_version}
                            value={t.engine_version}
                          >
                            {t.engine_version}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    )}
                    {majorUpgrades.length > 0 && (
                      <SelectGroup>
                        <SelectLabel className="text-[10px] uppercase tracking-wider">
                          Major Upgrades
                        </SelectLabel>
                        {majorUpgrades.map((t) => (
                          <SelectItem
                            key={t.engine_version}
                            value={t.engine_version}
                          >
                            {t.engine_version}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    )}
                  </SelectContent>
                </Select>
              </div>

              <div className="space-y-2">
                <Label>
                  Parameter Group{' '}
                  <span className="font-normal text-muted-foreground">
                    (optional)
                  </span>
                </Label>
                <Input
                  value={parameterGroup}
                  onChange={(e) => setParameterGroup(e.target.value)}
                  placeholder=""
                  disabled={blueGreenDeployment !== null}
                />
                <p className="text-xs text-muted-foreground">
                  Leave blank to auto-detect. Custom parameter settings are
                  automatically migrated to a new parameter group for the target
                  version.
                </p>
              </div>

              <Separator />

              <div className="space-y-3">
                <Label className="text-xs uppercase tracking-wider text-muted-foreground">Pause Points</Label>
                
                <div className="flex items-center justify-between">
                  <div className="space-y-0.5">
                    <Label htmlFor="pause-proxy-deregister">Pause before proxy deregister</Label>
                    <p className="text-xs text-muted-foreground">
                      Pause before deregistering cluster from RDS Proxy (causes proxy downtime)
                    </p>
                  </div>
                  <Switch
                    id="pause-proxy-deregister"
                    checked={pauseBeforeProxyDeregister}
                    onCheckedChange={setPauseBeforeProxyDeregister}
                    disabled={blueGreenDeployment !== null}
                  />
                </div>

                <div className="flex items-center justify-between">
                  <div className="space-y-0.5">
                    <Label htmlFor="pause-switchover">Pause before switchover</Label>
                    <p className="text-xs text-muted-foreground">
                      Pause before Blue-Green switchover to upgraded cluster
                    </p>
                  </div>
                  <Switch
                    id="pause-switchover"
                    checked={pauseBeforeSwitchover}
                    onCheckedChange={setPauseBeforeSwitchover}
                    disabled={blueGreenDeployment !== null}
                  />
                </div>

                <div className="flex items-center justify-between">
                  <div className="space-y-0.5">
                    <Label htmlFor="pause-cleanup">Pause before cleanup</Label>
                    <p className="text-xs text-muted-foreground">
                      Pause before deleting old cluster resources
                    </p>
                  </div>
                  <Switch
                    id="pause-cleanup"
                    checked={pauseBeforeCleanup}
                    onCheckedChange={setPauseBeforeCleanup}
                    disabled={blueGreenDeployment !== null}
                  />
                </div>
              </div>

              <Alert variant="info">
                <Info />
                <AlertDescription>
                  This operation uses AWS Blue-Green Deployment for
                  near-zero-downtime upgrades.
                </AlertDescription>
              </Alert>
            </>
          )}

          {operationType === 'instance_cycle' && (
            <>
              <Separator />
              <Alert variant="info">
                <Info />
                <AlertDescription>
                  This operation will reboot all non-autoscaled instances in the
                  cluster one at a time, starting with readers and ending with
                  the writer.
                </AlertDescription>
              </Alert>

              {excludableInstances.length > 1 && (
                <ExcludeInstancesField
                  instances={excludableInstances}
                  selected={excludeInstances}
                  onToggle={toggleExcludeInstance}
                  helpText="Selected instances will be skipped and not rebooted."
                />
              )}

              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <Label htmlFor="skip-temp-cycle">
                    Skip temp instance creation
                  </Label>
                  <p className="text-xs text-muted-foreground">
                    Faster but less safe (no redundancy during operation)
                  </p>
                </div>
                <Switch
                  id="skip-temp-cycle"
                  checked={skipTempInstance}
                  onCheckedChange={setSkipTempInstance}
                />
              </div>
            </>
          )}

          <Button
            type="submit"
            className="w-full"
            disabled={!operationType || !selectedCluster || isSubmitting}
          >
            {isSubmitting ? 'Creating...' : 'Create Operation'}
          </Button>
        </form>
      </CardContent>
    </Card>
  );
});

// Exclude instances field component
function ExcludeInstancesField({
  instances,
  selected,
  onToggle,
  helpText,
}: {
  instances: InstanceInfo[];
  selected: Set<string>;
  onToggle: (id: string) => void;
  helpText: string;
}) {
  return (
    <div className="space-y-2">
      <Label>
        Exclude Instances{' '}
        <span className="font-normal text-muted-foreground">(optional)</span>
      </Label>
      <div className="space-y-1 rounded-md border border-border p-2">
        {instances.map((inst) => (
          <label
            key={inst.instance_id}
            className="flex items-start gap-3 rounded-md px-2 py-2 hover:bg-accent cursor-pointer transition-colors"
          >
            <input
              type="checkbox"
              checked={selected.has(inst.instance_id)}
              onChange={() => onToggle(inst.instance_id)}
              className="rounded border-border mt-0.5"
            />
            <div className="flex-1 min-w-0">
              <div className="text-xs font-medium text-foreground break-all">
                {inst.instance_id}
              </div>
              <div className="text-xs text-muted-foreground mt-0.5">
                {inst.instance_type}
              </div>
            </div>
            <span
              className={`shrink-0 text-[10px] font-medium px-1.5 py-0.5 rounded self-center ${
                inst.role === 'writer'
                  ? 'bg-status-blue-muted text-status-blue'
                  : 'bg-muted text-muted-foreground'
              }`}
            >
              {inst.role === 'writer' ? 'Writer' : 'Reader'}
            </span>
          </label>
        ))}
      </div>
      <p className="text-xs text-muted-foreground">{helpText}</p>
    </div>
  );
}

// Helper to group instance types by family
function groupInstanceTypesByFamily(
  types: InstanceTypeOption[]
): Record<string, InstanceTypeOption[]> {
  const families: Record<string, InstanceTypeOption[]> = {};

  types.forEach((t) => {
    const parts = t.instance_class.replace('db.', '').split('.');
    const family = parts[0] || 'other';
    if (!families[family]) families[family] = [];
    families[family].push(t);
  });

  // Sort families by type priority and generation
  const typePriority: Record<string, number> = { r: 0, x: 1, m: 2, t: 3 };
  const sizeOrder = [
    'micro',
    'small',
    'medium',
    'large',
    'xlarge',
    '2xlarge',
    '4xlarge',
    '8xlarge',
    '12xlarge',
    '16xlarge',
    '24xlarge',
    '32xlarge',
    '48xlarge',
  ];

  // Sort types within each family by size
  Object.values(families).forEach((familyTypes) => {
    familyTypes.sort((a, b) => {
      const sizeA = a.instance_class.replace('db.', '').split('.')[1] || '';
      const sizeB = b.instance_class.replace('db.', '').split('.')[1] || '';
      const orderA = sizeOrder.indexOf(sizeA);
      const orderB = sizeOrder.indexOf(sizeB);
      if (orderA === -1 && orderB === -1) return sizeA.localeCompare(sizeB);
      if (orderA === -1) return 1;
      if (orderB === -1) return -1;
      return orderA - orderB;
    });
  });

  // Sort families
  const sortedFamilies = Object.keys(families).sort((a, b) => {
    const typeA = a.charAt(0);
    const typeB = b.charAt(0);
    const prioA = typePriority[typeA] ?? 99;
    const prioB = typePriority[typeB] ?? 99;
    if (prioA !== prioB) return prioA - prioB;
    const numA = parseInt(a.substring(1)) || 0;
    const numB = parseInt(b.substring(1)) || 0;
    if (numA !== numB) return numB - numA;
    return a.localeCompare(b);
  });

  const result: Record<string, InstanceTypeOption[]> = {};
  sortedFamilies.forEach((f) => {
    result[f] = families[f];
  });
  return result;
}
