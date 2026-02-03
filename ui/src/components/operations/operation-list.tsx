import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { ScrollArea } from '@/components/ui/scroll-area';
import { StatusBadge } from '@/components/ui/status-badge';
import { OPERATION_TYPE_NAMES, cn, formatRelativeTime } from '@/lib/utils';
import type { Operation } from '@/types';
import { RefreshCw, Database } from 'lucide-react';

interface OperationListProps {
  operations: Operation[];
  selectedId: string | null;
  onSelect: (id: string) => void;
  onRefresh: () => void;
  isLoading?: boolean;
}

export function OperationList({
  operations,
  selectedId,
  onSelect,
  onRefresh,
  isLoading,
}: OperationListProps) {
  // Sort by created_at descending
  const sortedOperations = [...operations].sort(
    (a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
  );

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-3">
        <div className="flex items-center gap-2">
          <CardTitle className="text-sm font-semibold">Operations</CardTitle>
          <span className="text-xs text-muted-foreground tabular-nums">
            ({sortedOperations.length})
          </span>
        </div>
        <Button
          variant="ghost"
          size="icon"
          onClick={onRefresh}
          disabled={isLoading}
          className="h-7 w-7"
        >
          <RefreshCw className={cn('h-3.5 w-3.5', isLoading && 'animate-spin')} />
        </Button>
      </CardHeader>
      <CardContent className="p-0">
        <ScrollArea className="h-[400px]">
          {sortedOperations.length === 0 ? (
            <div className="flex flex-col items-center justify-center p-8 text-center">
              <Database className="h-8 w-8 text-muted-foreground/40 mb-2" />
              <p className="text-sm text-muted-foreground">No operations yet</p>
              <p className="text-xs text-muted-foreground/70 mt-0.5">
                Create one above to get started
              </p>
            </div>
          ) : (
            <div className="px-2 pb-2">
              {sortedOperations.map((op, index) => (
                <button
                  key={op.id}
                  onClick={() => onSelect(op.id)}
                  className={cn(
                    'w-full rounded-md px-3 py-2.5 text-left transition-all',
                    'hover:bg-accent/80',
                    'border border-transparent',
                    selectedId === op.id
                      ? 'bg-accent border-border/50'
                      : 'hover:border-border/30',
                    index > 0 && 'mt-1'
                  )}
                >
                  {/* Row 1: Operation type + timestamp */}
                  <div className="flex items-center justify-between gap-2">
                    <span className="text-sm font-medium truncate">
                      {OPERATION_TYPE_NAMES[op.type] || op.type}
                    </span>
                    <span className="text-[10px] text-muted-foreground/70 whitespace-nowrap">
                      {formatRelativeTime(op.created_at)}
                    </span>
                  </div>
                  {/* Row 2: Cluster ID (full width, can wrap) */}
                  <div className="mt-1 text-xs text-muted-foreground font-mono break-all">
                    {op.cluster_id}
                  </div>
                  {/* Row 3: Region + Status */}
                  <div className="mt-2 flex items-center justify-between gap-2">
                    <StatusBadge status={op.state} />
                    {op.region && (
                      <span className="text-[10px] text-muted-foreground/60">
                        {op.region}
                      </span>
                    )}
                  </div>
                </button>
              ))}
            </div>
          )}
        </ScrollArea>
      </CardContent>
    </Card>
  );
}
