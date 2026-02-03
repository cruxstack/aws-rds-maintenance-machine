import { useEffect, useState } from 'react';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { StatusBadge } from '@/components/ui/status-badge';
import { Separator } from '@/components/ui/separator';
import * as api from '@/api/client';
import type { ClusterInfo, BlueGreenDeployment } from '@/types';

interface ClusterStatusProps {
  clusterId: string | null;
  region: string | null;
  refreshInterval?: number;
}

export function ClusterStatus({
  clusterId,
  region,
  refreshInterval = 5000,
}: ClusterStatusProps) {
  const [cluster, setCluster] = useState<ClusterInfo | null>(null);
  const [blueGreenDeployments, setBlueGreenDeployments] = useState<
    BlueGreenDeployment[]
  >([]);
  const [lastRefresh, setLastRefresh] = useState<Date | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    
    const fetchData = async () => {
      if (!clusterId) {
        setCluster(null);
        setBlueGreenDeployments([]);
        return;
      }

      try {
        const [clusterInfo, bgDeployments] = await Promise.all([
          api.getClusterInfo(clusterId, region || undefined),
          api
            .getClusterBlueGreen(clusterId, region || undefined)
            .catch(() => []),
        ]);

        if (!cancelled) {
          setCluster(clusterInfo);
          setBlueGreenDeployments(bgDeployments);
          setLastRefresh(new Date());
          setError(null);
        }
      } catch (err) {
        if (!cancelled) {
          setError((err as Error).message);
        }
      }
    };

    fetchData();
    const interval = setInterval(fetchData, refreshInterval);
    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, [clusterId, region, refreshInterval]);

  if (!clusterId) {
    return null;
  }

  // Sort instances: writer first, then by ID
  const sortedInstances = cluster?.instances
    ? [...cluster.instances].sort((a, b) => {
        if (a.role === 'writer' && b.role !== 'writer') return -1;
        if (a.role !== 'writer' && b.role === 'writer') return 1;
        return a.instance_id.localeCompare(b.instance_id);
      })
    : [];

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-4">
        <div>
          <CardTitle className="text-sm font-semibold">Cluster Status</CardTitle>
          <CardDescription>Real-time view of cluster instances</CardDescription>
        </div>
        {lastRefresh && (
          <span className="text-xs text-muted-foreground font-mono">
            {lastRefresh.toLocaleTimeString()}
          </span>
        )}
      </CardHeader>
      <CardContent>
        {error ? (
          <div className="flex flex-col items-center justify-center p-6 text-sm text-muted-foreground">
            <span>Failed to load cluster status</span>
            <span className="text-xs mt-1">Will retry automatically...</span>
          </div>
        ) : !cluster ? (
          <div className="flex items-center justify-center p-6 text-sm text-muted-foreground">
            Loading...
          </div>
        ) : (
          <>
            {/* Cluster Header */}
            <div className="flex items-center justify-between mb-4">
              <div>
                <h4 className="font-medium">{cluster.cluster_id}</h4>
                <p className="text-xs text-muted-foreground">
                  {cluster.engine} {cluster.engine_version}
                </p>
              </div>
              <StatusBadge status={cluster.status} />
            </div>

            {/* Instances Table */}
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Instance</TableHead>
                  <TableHead className="w-[60px]">Role</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead>Storage</TableHead>
                  <TableHead className="text-right">Status</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {sortedInstances.map((inst) => (
                  <TableRow key={inst.instance_id}>
                    <TableCell className="font-medium">
                      {inst.instance_id}
                    </TableCell>
                    <TableCell>
                      <span
                        className={`inline-flex items-center justify-center w-6 h-6 rounded text-[10px] font-semibold ${
                          inst.role === 'writer'
                            ? 'bg-status-blue-muted text-status-blue'
                            : inst.is_auto_scaled
                              ? 'bg-status-purple-muted text-status-purple'
                              : 'bg-muted text-muted-foreground'
                        }`}
                      >
                        {inst.role === 'writer'
                          ? 'W'
                          : inst.is_auto_scaled
                            ? 'A'
                            : 'R'}
                      </span>
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {inst.instance_type}
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {inst.storage_type || '-'}
                      {inst.iops && ` (${inst.iops} IOPS)`}
                    </TableCell>
                    <TableCell className="text-right">
                      <StatusBadge status={inst.status} />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>

            {/* Blue-Green Deployments */}
            {blueGreenDeployments.length > 0 && (
              <>
                <Separator className="my-4" />
                <div className="space-y-3">
                  <p className="text-[11px] font-semibold text-muted-foreground uppercase tracking-wider">
                    Blue-Green Deployments
                  </p>
                  {blueGreenDeployments.map((bg) => {
                    const isSource =
                      bg.source && bg.source.includes(cluster.cluster_id);
                    const roleLabel = isSource
                      ? 'BLUE (Source)'
                      : 'GREEN (Target)';

                    return (
                      <div
                        key={bg.identifier}
                        className="rounded-md border border-border p-3"
                      >
                        <div className="flex items-center justify-between mb-2">
                          <span className="font-medium text-sm">
                            {bg.name || bg.identifier}
                          </span>
                          <span
                            className={`text-[10px] font-semibold px-2 py-0.5 rounded ${
                              isSource
                                ? 'bg-status-blue-muted text-status-blue'
                                : 'bg-status-green-muted text-status-green'
                            }`}
                          >
                            {roleLabel}
                          </span>
                        </div>
                        <StatusBadge status={bg.status} />
                        {bg.tasks && bg.tasks.length > 0 && (
                          <div className="mt-2 space-y-1">
                            {bg.tasks.map((task, i) => (
                              <div
                                key={i}
                                className="flex items-center justify-between text-xs"
                              >
                                <span className="text-muted-foreground">
                                  {task.name?.replace(/_/g, ' ')}
                                </span>
                                <StatusBadge status={task.status} />
                              </div>
                            ))}
                          </div>
                        )}
                      </div>
                    );
                  })}
                </div>
              </>
            )}
          </>
        )}
      </CardContent>
    </Card>
  );
}
