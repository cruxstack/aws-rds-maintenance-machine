import { useState, useEffect, useCallback } from 'react';
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
import { Slider } from '@/components/ui/slider';
import { Separator } from '@/components/ui/separator';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { StatusBadge } from '@/components/ui/status-badge';
import { ScrollArea } from '@/components/ui/scroll-area';
import * as api from '@/api/client';
import type { MockState, MockFault, MockTiming } from '@/types';
import { X } from 'lucide-react';

interface DemoControlsProps {
  onError: (error: string) => void;
  onResetAll: () => Promise<void>;
}

export function DemoControls({ onError, onResetAll }: DemoControlsProps) {
  const [state, setState] = useState<MockState | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);

  // Timing controls
  const [fastMode, setFastMode] = useState(false);
  const [baseWait, setBaseWait] = useState(500);
  const [randomRange, setRandomRange] = useState(200);

  // Fault form
  const [faultType, setFaultType] = useState<'api_error' | 'delay' | 'stuck'>(
    'api_error'
  );
  const [faultAction, setFaultAction] = useState('');
  const [faultTarget, setFaultTarget] = useState('');
  const [faultProbability, setFaultProbability] = useState(100);
  const [faultErrorCode, setFaultErrorCode] = useState('InternalFailure');
  const [faultErrorMsg, setFaultErrorMsg] = useState('Injected fault');
  const [faultDelay, setFaultDelay] = useState(5000);

  const loadMockState = useCallback(async () => {
    try {
      const data = await api.getMockState();
      setState(data);
      setFastMode(data.timing.FastMode);
      setBaseWait(data.timing.BaseWaitMs);
      setRandomRange(data.timing.RandomRangeMs);
      setLoadError(null);
    } catch (err) {
      const error = err as Error & { statusCode?: number };
      if (error.statusCode === 404) {
        setLoadError('Demo controls are only available when the server is running in demo mode.');
      } else {
        setLoadError(`Failed to load mock state: ${error.message}`);
      }
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    loadMockState();
  }, [loadMockState]);

  const handleTimingChange = async (
    updates: Partial<MockTiming>,
    fromSlider = false
  ) => {
    // If slider adjusted while fast mode is on, turn off fast mode
    const newFastMode = fromSlider && fastMode ? false : updates.FastMode ?? fastMode;

    const timing: MockTiming = {
      BaseWaitMs: updates.BaseWaitMs ?? baseWait,
      RandomRangeMs: updates.RandomRangeMs ?? randomRange,
      FastMode: newFastMode,
    };

    setFastMode(timing.FastMode);
    setBaseWait(timing.BaseWaitMs);
    setRandomRange(timing.RandomRangeMs);

    try {
      await api.updateMockTiming(timing);
    } catch (err) {
      onError(`Failed to update timing: ${(err as Error).message}`);
    }
  };

  const handleAddFault = async (e: React.FormEvent) => {
    e.preventDefault();

    const fault: Omit<MockFault, 'id'> = {
      type: faultType,
      action: faultAction || undefined,
      target: faultTarget || undefined,
      probability: faultProbability / 100,
      enabled: true,
      params:
        faultType === 'api_error'
          ? { error_code: faultErrorCode, error_message: faultErrorMsg }
          : faultType === 'delay'
            ? { delay_ms: faultDelay }
            : undefined,
    };

    try {
      await api.addMockFault(fault);
      loadMockState();
      // Reset form
      setFaultTarget('');
      setFaultProbability(100);
    } catch (err) {
      onError(`Failed to add fault: ${(err as Error).message}`);
    }
  };

  const handleToggleFault = async (id: string, enabled: boolean) => {
    try {
      await api.toggleMockFault(id, enabled);
      loadMockState();
    } catch (err) {
      onError(`Failed to toggle fault: ${(err as Error).message}`);
    }
  };

  const handleDeleteFault = async (id: string) => {
    try {
      await api.deleteMockFault(id);
      loadMockState();
    } catch (err) {
      onError(`Failed to delete fault: ${(err as Error).message}`);
    }
  };

  const handleClearFaults = async () => {
    try {
      await api.clearMockFaults();
      loadMockState();
    } catch (err) {
      onError(`Failed to clear faults: ${(err as Error).message}`);
    }
  };

  const handleResetAll = async () => {
    if (!confirm('Reset all mock state and clear operations? This cannot be undone.')) {
      return;
    }
    try {
      await api.resetMockState();
      await onResetAll();
      loadMockState();
    } catch (err) {
      onError(`Failed to reset state: ${(err as Error).message}`);
    }
  };

  if (isLoading) {
    return (
      <div className="flex items-center justify-center p-12 text-muted-foreground">
        Loading mock state...
      </div>
    );
  }

  if (loadError) {
    return (
      <div className="flex items-center justify-center p-12 text-muted-foreground">
        {loadError}
      </div>
    );
  }

  const faultTypeNames: Record<string, string> = {
    api_error: 'API Error',
    delay: 'Extra Delay',
    stuck: 'Stuck in State',
  };

  return (
    <div className="space-y-6">
      {/* Top row: Timing, Fault Injection, Configured Faults */}
      <div className="grid grid-cols-3 gap-6">
        {/* Timing Settings */}
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-semibold">
              Timing Settings
            </CardTitle>
            <CardDescription>Control mock server timing</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="flex items-center justify-between">
              <Label>Fast Mode</Label>
              <Switch
                checked={fastMode}
                onCheckedChange={(checked) =>
                  handleTimingChange({ FastMode: checked })
                }
              />
            </div>

            <div className="space-y-2">
              <Label>Base Wait Time</Label>
              <div className="flex items-center gap-4">
                <Slider
                  value={[baseWait]}
                  min={100}
                  max={5000}
                  step={100}
                  onValueChange={([v]) => {
                    setBaseWait(v);
                  }}
                  onValueCommit={([v]) => {
                    handleTimingChange({ BaseWaitMs: v }, true);
                  }}
                  className="flex-1"
                />
                <span className="text-sm text-muted-foreground w-16 text-right">
                  {baseWait}ms
                </span>
              </div>
            </div>

            <div className="space-y-2">
              <Label>Random Range</Label>
              <div className="flex items-center gap-4">
                <Slider
                  value={[randomRange]}
                  min={0}
                  max={2000}
                  step={100}
                  onValueChange={([v]) => {
                    setRandomRange(v);
                  }}
                  onValueCommit={([v]) => {
                    handleTimingChange({ RandomRangeMs: v }, true);
                  }}
                  className="flex-1"
                />
                <span className="text-sm text-muted-foreground w-16 text-right">
                  {randomRange}ms
                </span>
              </div>
            </div>

            <Separator />

            <Button
              variant="destructive"
              className="w-full"
              onClick={handleResetAll}
            >
              Reset All State
            </Button>
          </CardContent>
        </Card>

        {/* Fault Injection */}
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-semibold">
              Fault Injection
            </CardTitle>
            <CardDescription>Simulate failures and delays</CardDescription>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleAddFault} className="space-y-4">
              <div className="space-y-2">
                <Label>Fault Type</Label>
                <Select
                  value={faultType}
                  onValueChange={(v) =>
                    setFaultType(v as 'api_error' | 'delay' | 'stuck')
                  }
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="api_error">API Error</SelectItem>
                    <SelectItem value="delay">Extra Delay</SelectItem>
                    <SelectItem value="stuck">Stuck in State</SelectItem>
                  </SelectContent>
                </Select>
              </div>

              <div className="space-y-2">
                <Label>Target Action</Label>
                <Select value={faultAction || '__any__'} onValueChange={(v) => setFaultAction(v === '__any__' ? '' : v)}>
                  <SelectTrigger>
                    <SelectValue placeholder="Any Action" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="__any__">Any Action</SelectItem>
                    <SelectItem value="CreateDBInstance">
                      CreateDBInstance
                    </SelectItem>
                    <SelectItem value="ModifyDBInstance">
                      ModifyDBInstance
                    </SelectItem>
                    <SelectItem value="DeleteDBInstance">
                      DeleteDBInstance
                    </SelectItem>
                    <SelectItem value="FailoverDBCluster">
                      FailoverDBCluster
                    </SelectItem>
                    <SelectItem value="ModifyDBCluster">
                      ModifyDBCluster
                    </SelectItem>
                    <SelectItem value="CreateDBClusterSnapshot">
                      CreateDBClusterSnapshot
                    </SelectItem>
                  </SelectContent>
                </Select>
              </div>

              <div className="space-y-2">
                <Label>
                  Target Resource{' '}
                  <span className="font-normal text-muted-foreground">
                    (optional)
                  </span>
                </Label>
                <Input
                  value={faultTarget}
                  onChange={(e) => setFaultTarget(e.target.value)}
                  placeholder="e.g., demo-multi-writer"
                />
              </div>

              <div className="space-y-2">
                <Label>Probability (%)</Label>
                <Input
                  type="number"
                  min={1}
                  max={100}
                  value={faultProbability}
                  onChange={(e) => setFaultProbability(parseInt(e.target.value))}
                />
              </div>

              {faultType === 'api_error' && (
                <>
                  <div className="space-y-2">
                    <Label>Error Code</Label>
                    <Input
                      value={faultErrorCode}
                      onChange={(e) => setFaultErrorCode(e.target.value)}
                    />
                  </div>
                  <div className="space-y-2">
                    <Label>Error Message</Label>
                    <Input
                      value={faultErrorMsg}
                      onChange={(e) => setFaultErrorMsg(e.target.value)}
                    />
                  </div>
                </>
              )}

              {faultType === 'delay' && (
                <div className="space-y-2">
                  <Label>Delay (ms)</Label>
                  <Input
                    type="number"
                    value={faultDelay}
                    onChange={(e) => setFaultDelay(parseInt(e.target.value))}
                  />
                </div>
              )}

              <Button type="submit" variant="secondary" className="w-full">
                Add Fault
              </Button>
            </form>
          </CardContent>
        </Card>

        {/* Configured Faults */}
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0">
            <div>
              <CardTitle className="text-sm font-semibold">
                Configured Faults
              </CardTitle>
              <CardDescription>Active fault injections</CardDescription>
            </div>
            <Button
              variant="destructive"
              size="sm"
              onClick={handleClearFaults}
              disabled={!state?.faults.length}
            >
              Clear All
            </Button>
          </CardHeader>
          <CardContent>
            <ScrollArea className="h-[300px]">
              {!state?.faults.length ? (
                <div className="flex items-center justify-center p-6 text-sm text-muted-foreground">
                  No faults configured
                </div>
              ) : (
                <div className="space-y-2">
                  {state.faults.map((fault) => (
                    <div
                      key={fault.id}
                      className="flex items-center justify-between rounded-md border border-border p-3"
                    >
                      <div>
                        <p className="text-sm font-medium">
                          {faultTypeNames[fault.type]} (
                          {Math.round(fault.probability * 100)}%)
                        </p>
                        <p className="text-xs text-muted-foreground">
                          {fault.action || 'Any action'}
                          {fault.target && ` on ${fault.target}`}
                        </p>
                      </div>
                      <div className="flex items-center gap-2">
                        <Switch
                          checked={fault.enabled}
                          onCheckedChange={(checked) =>
                            handleToggleFault(fault.id, checked)
                          }
                        />
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-8 w-8 p-0 text-status-red hover:text-status-red hover:bg-status-red-muted"
                          onClick={() => handleDeleteFault(fault.id)}
                        >
                          <X className="h-4 w-4" />
                        </Button>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </ScrollArea>
          </CardContent>
        </Card>
      </div>

      {/* Mock Clusters */}
      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0">
          <div>
            <CardTitle className="text-sm font-semibold">Mock Clusters</CardTitle>
            <CardDescription>Current cluster state</CardDescription>
          </div>
          <Button variant="ghost" size="sm" onClick={loadMockState}>
            Refresh
          </Button>
        </CardHeader>
        <CardContent>
          {!state?.clusters.length ? (
            <div className="flex items-center justify-center p-6 text-sm text-muted-foreground">
              No clusters
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-[280px]">Cluster / Instance</TableHead>
                  <TableHead className="w-[60px]">Role</TableHead>
                  <TableHead className="w-[140px]">Type</TableHead>
                  <TableHead className="w-[60px]">PI</TableHead>
                  <TableHead className="text-right">Status</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {state.clusters.map((cluster) => {
                  const clusterInstances = state.instances.filter(
                    (i) => i.ClusterID === cluster.ID
                  );
                  return (
                    <>
                      {/* Cluster row */}
                      <TableRow key={cluster.ID} className="bg-muted/30 hover:bg-muted/50">
                        <TableCell className="font-medium">
                          <div>
                            <span>{cluster.ID}</span>
                            <span className="ml-2 text-xs text-muted-foreground font-normal">
                              {cluster.Engine} {cluster.EngineVersion}
                            </span>
                          </div>
                        </TableCell>
                        <TableCell />
                        <TableCell />
                        <TableCell />
                        <TableCell className="text-right">
                          <StatusBadge status={cluster.Status} />
                        </TableCell>
                      </TableRow>
                      {/* Instance rows */}
                      {clusterInstances.map((inst) => (
                        <TableRow key={inst.ID}>
                          <TableCell className="pl-6 text-muted-foreground">
                            {inst.ID}
                          </TableCell>
                          <TableCell>
                            <span
                              className={`inline-flex items-center justify-center w-6 h-6 rounded text-[10px] font-semibold ${
                                inst.IsWriter
                                  ? 'bg-status-blue-muted text-status-blue'
                                  : inst.IsAutoScaled
                                    ? 'bg-status-purple-muted text-status-purple'
                                    : 'bg-muted text-muted-foreground'
                              }`}
                            >
                              {inst.IsWriter
                                ? 'W'
                                : inst.IsAutoScaled
                                  ? 'A'
                                  : 'R'}
                            </span>
                          </TableCell>
                          <TableCell className="text-muted-foreground">
                            {inst.InstanceType}
                          </TableCell>
                          <TableCell>
                            {inst.PerformanceInsightsEnabled ? (
                              <span className="text-[10px] font-medium px-1.5 py-0.5 rounded bg-status-green-muted text-status-green">
                                ON
                              </span>
                            ) : inst.Status ===
                              'configuring-performance-insights' ? (
                              <span className="text-[10px] font-medium px-1.5 py-0.5 rounded bg-status-yellow-muted text-status-yellow">
                                ...
                              </span>
                            ) : (
                              '-'
                            )}
                          </TableCell>
                          <TableCell className="text-right">
                            <StatusBadge status={inst.Status} />
                          </TableCell>
                        </TableRow>
                      ))}
                    </>
                  );
                })}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
