import { useState, useEffect, useRef } from 'react';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { ScrollArea } from '@/components/ui/scroll-area';
import { Separator } from '@/components/ui/separator';
import { Switch } from '@/components/ui/switch';
import { Label } from '@/components/ui/label';
import { StatusBadge } from '@/components/ui/status-badge';
import {
  OPERATION_TYPE_NAMES,
  formatDuration,
  formatTimeout,
  copyToClipboard,
  cn,
} from '@/lib/utils';
import type { Operation, OperationEvent, Step } from '@/types';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Copy, Check, Pencil, AlertCircle, PauseCircle } from 'lucide-react';

interface OperationDetailProps {
  operation: Operation;
  events: OperationEvent[];
  isDemoMode: boolean;
  onStart: () => void;
  onPause: () => void;
  onResume: () => void;
  onDelete: () => void;
  onEditTimeout?: (current: number) => void;
  onTogglePauseStep?: (stepIndex: number) => void;
}

export function OperationDetail({
  operation,
  events,
  isDemoMode,
  onStart,
  onPause,
  onResume,
  onDelete,
  onEditTimeout,
  onTogglePauseStep,
}: OperationDetailProps) {
  const [showErrorsOnly, setShowErrorsOnly] = useState(false);
  const [copiedId, setCopiedId] = useState(false);
  const stepsRef = useRef<HTMLDivElement>(null);
  const lastStepIndexRef = useRef(-1);

  const steps = operation.steps ?? [];
  const currentStepIndex = operation.current_step_index ?? 0;

  // Auto-scroll to current step when it changes
  useEffect(() => {
    if (
      currentStepIndex !== lastStepIndexRef.current &&
      stepsRef.current
    ) {
      const stepEl = stepsRef.current.querySelector(
        `[data-step-index="${currentStepIndex}"]`
      );
      if (stepEl) {
        stepEl.scrollIntoView({ behavior: 'smooth', block: 'center' });
      }
    }
    lastStepIndexRef.current = currentStepIndex;
  }, [currentStepIndex]);

  const handleCopyId = async () => {
    const success = await copyToClipboard(operation.id);
    if (success) {
      setCopiedId(true);
      setTimeout(() => setCopiedId(false), 1500);
    }
  };

  // Filter events - handle null/undefined events array
  const safeEvents = events ?? [];
  const filteredEvents = showErrorsOnly
    ? safeEvents.filter(
        (e) =>
          e.type === 'error' ||
          e.type === 'step_failed' ||
          e.type === 'operation_failed' ||
          e.message?.toLowerCase().includes('error')
      )
    : safeEvents;

  // Reverse for newest first
  const displayEvents = [...filteredEvents].reverse().slice(0, 50);

  const canEditTimeout =
    !isDemoMode &&
    ['created', 'running', 'paused'].includes(operation.state);

  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between space-y-0">
        <div>
          <CardTitle className="text-base">
            {OPERATION_TYPE_NAMES[operation.type] || operation.type}
          </CardTitle>
          <CardDescription>
            {operation.cluster_id}
            {!isDemoMode && operation.region && ` (${operation.region})`}
          </CardDescription>
        </div>
        <StatusBadge status={operation.state} />
      </CardHeader>

      <CardContent className="space-y-6">
        {/* Information Section */}
        <section>
          <h3 className="text-sm font-semibold mb-3">Information</h3>
          <div className="grid grid-cols-2 gap-3">
            <InfoItem
              label="Operation ID"
              value={
                <div className="flex items-center gap-2">
                  <code className="text-xs">{operation.id}</code>
                  <button
                    onClick={handleCopyId}
                    className="p-0.5 hover:bg-accent rounded"
                  >
                    {copiedId ? (
                      <Check className="h-3.5 w-3.5 text-status-green" />
                    ) : (
                      <Copy className="h-3.5 w-3.5 text-muted-foreground" />
                    )}
                  </button>
                </div>
              }
            />
            <InfoItem
              label="Created"
              value={new Date(operation.created_at).toLocaleString()}
            />
            <InfoItem
              label="Progress"
              value={`${currentStepIndex} / ${steps.length} steps`}
            />
            <InfoItem
              label="Duration"
              value={formatDuration(
                operation.started_at,
                operation.completed_at
              )}
            />
            {!isDemoMode && (
              <>
                <InfoItem
                  label="Wait Timeout"
                  value={
                    <div className="flex items-center gap-2">
                      <span>{formatTimeout(operation.wait_timeout)}</span>
                      {canEditTimeout && onEditTimeout && (
                        <button
                          onClick={() =>
                            onEditTimeout(operation.wait_timeout || 2700)
                          }
                          className="p-0.5 hover:bg-accent rounded"
                        >
                          <Pencil className="h-3.5 w-3.5 text-muted-foreground" />
                        </button>
                      )}
                    </div>
                  }
                />
                <InfoItem label="Region" value={operation.region || '-'} />
              </>
            )}
          </div>

          {operation.pause_reason && (
            <Alert variant="warning" className="mt-3">
              <PauseCircle />
              <AlertTitle>Paused</AlertTitle>
              <AlertDescription>{operation.pause_reason}</AlertDescription>
            </Alert>
          )}

          {operation.error && (
            <Alert variant="destructive" className="mt-3">
              <AlertCircle />
              <AlertTitle>Error</AlertTitle>
              <AlertDescription>{operation.error}</AlertDescription>
            </Alert>
          )}
        </section>

        <Separator />

        {/* Steps Section */}
        <section>
          <div className="flex items-center gap-2 mb-3">
            <h3 className="text-sm font-semibold">Steps</h3>
            {steps.length > 0 && (
              <span className="text-xs text-muted-foreground/60 tabular-nums">
                ({currentStepIndex}/{steps.length})
              </span>
            )}
          </div>
          <ScrollArea className="h-[300px]">
            {steps.length === 0 ? (
              <div className="flex flex-col items-center justify-center p-6 text-center">
                <p className="text-sm text-muted-foreground">No steps yet</p>
                <p className="text-xs text-muted-foreground/60 mt-0.5">
                  Start the operation to generate steps
                </p>
              </div>
            ) : (
              <div className="pl-1 pr-4" ref={stepsRef}>
                {steps.map((step, i) => (
                  <StepItem
                    key={step.id}
                    step={step}
                    index={i}
                    isCurrent={
                      i === currentStepIndex && operation.state === 'running'
                    }
                    isLast={i === steps.length - 1}
                    willPause={(operation.pause_before_steps ?? []).includes(i)}
                    canTogglePause={
                      onTogglePauseStep !== undefined &&
                      step.state !== 'completed' &&
                      step.state !== 'failed' &&
                      step.state !== 'skipped' &&
                      ['created', 'paused', 'running'].includes(operation.state)
                    }
                    onTogglePause={() => onTogglePauseStep?.(i)}
                  />
                ))}
              </div>
            )}
          </ScrollArea>
        </section>

        <Separator />

        {/* Events Section */}
        <section>
          <div className="flex items-center justify-between mb-3">
            <div className="flex items-center gap-2">
              <h3 className="text-sm font-semibold">Events</h3>
              <span className="text-xs text-muted-foreground/60 tabular-nums">
                ({filteredEvents.length})
              </span>
            </div>
            <div className="flex items-center gap-2">
              <Switch
                id="errors-only"
                checked={showErrorsOnly}
                onCheckedChange={setShowErrorsOnly}
              />
              <Label htmlFor="errors-only" className="text-xs text-muted-foreground">
                Errors only
              </Label>
            </div>
          </div>
          <ScrollArea className="h-[200px]">
            {displayEvents.length === 0 ? (
              <div className="flex flex-col items-center justify-center p-6 text-center">
                <p className="text-sm text-muted-foreground">
                  {showErrorsOnly ? 'No errors found' : 'No events yet'}
                </p>
                {showErrorsOnly && (
                  <p className="text-xs text-muted-foreground/60 mt-0.5">
                    All clear
                  </p>
                )}
              </div>
            ) : (
              <div className="space-y-1 pr-4">
                {displayEvents.map((event) => (
                  <EventItem key={event.id} event={event} />
                ))}
              </div>
            )}
          </ScrollArea>
        </section>

        {/* Action Buttons */}
        {(operation.state === 'created' ||
          operation.state === 'paused' ||
          operation.state === 'running') && (
          <>
            <Separator />
            <div className="flex gap-2">
              {operation.state === 'created' && (
                <>
                  <Button onClick={onStart} className="flex-1">
                    Start Operation
                  </Button>
                  <Button variant="destructive" onClick={onDelete}>
                    Delete
                  </Button>
                </>
              )}
              {operation.state === 'paused' && (
                <Button onClick={onResume} className="flex-1">
                  Resume / Rollback / Abort
                </Button>
              )}
              {operation.state === 'running' && (
                <Button variant="secondary" onClick={onPause} className="flex-1">
                  Pause
                </Button>
              )}
            </div>
          </>
        )}
      </CardContent>
    </Card>
  );
}

function InfoItem({
  label,
  value,
}: {
  label: string;
  value: React.ReactNode;
}) {
  return (
    <div className="rounded-md border border-border p-2.5">
      <p className="text-[11px] font-medium text-muted-foreground uppercase tracking-wider">
        {label}
      </p>
      <div className="text-sm mt-0.5">{value}</div>
    </div>
  );
}

function StepItem({
  step,
  index,
  isCurrent,
  isLast,
  willPause,
  canTogglePause,
  onTogglePause,
}: {
  step: Step;
  index: number;
  isCurrent: boolean;
  isLast: boolean;
  willPause: boolean;
  canTogglePause: boolean;
  onTogglePause: () => void;
}) {
  const duration =
    step.started_at && formatDuration(step.started_at, step.completed_at);

  return (
    <div className="flex gap-3" data-step-index={index}>
      {/* Timeline column */}
      <div className="flex flex-col items-center">
        <div
          className={cn(
            'flex h-7 w-7 shrink-0 items-center justify-center rounded-full text-xs font-semibold transition-all',
            step.state === 'completed' && 'bg-status-green/20 text-status-green border border-status-green/30',
            step.state === 'failed' && 'bg-status-red/20 text-status-red border border-status-red/30',
            step.state === 'in_progress' && 'bg-status-blue/20 text-status-blue border border-status-blue/30',
            step.state === 'waiting' && 'bg-status-yellow/20 text-status-yellow border border-status-yellow/30',
            step.state === 'skipped' && 'bg-muted text-muted-foreground border border-border',
            step.state === 'pending' && 'bg-secondary text-muted-foreground/60 border border-border/50',
            isCurrent && 'ring-2 ring-status-blue ring-offset-2 ring-offset-background'
          )}
        >
          {step.state === 'completed' ? (
            <CheckIcon className="h-3.5 w-3.5" />
          ) : step.state === 'failed' ? (
            <XIcon className="h-3.5 w-3.5" />
          ) : (
            index + 1
          )}
        </div>
        {/* Connector line */}
        {!isLast && (
          <div
            className={cn(
              'w-px flex-1 min-h-[16px] my-1',
              step.state === 'completed' ? 'bg-status-green/30' : 'bg-border'
            )}
          />
        )}
      </div>

      {/* Content */}
      <div className="flex-1 min-w-0 pb-4">
        <div className="flex items-center gap-2">
          <span
            className={cn(
              'text-sm font-medium truncate',
              step.state === 'pending' && 'text-muted-foreground'
            )}
          >
            {step.name}
          </span>
          {duration && duration !== '-' && (
            <span className="text-[11px] text-muted-foreground/70 tabular-nums shrink-0">
              {duration}
            </span>
          )}
          {/* Auto-pause indicator/toggle */}
          {willPause && (
            <span className="text-[10px] text-status-yellow bg-status-yellow/10 px-1.5 py-0.5 rounded shrink-0">
              will pause
            </span>
          )}
          {canTogglePause && (
            <button
              onClick={onTogglePause}
              className={cn(
                'p-0.5 rounded hover:bg-accent transition-colors shrink-0',
                willPause ? 'text-status-yellow' : 'text-muted-foreground/40 hover:text-muted-foreground'
              )}
              title={willPause ? 'Remove auto-pause' : 'Add auto-pause before this step'}
            >
              <PauseCircle className="h-3.5 w-3.5" />
            </button>
          )}
        </div>
        <p
          className={cn(
            'text-xs mt-0.5 truncate',
            step.state === 'pending'
              ? 'text-muted-foreground/50'
              : 'text-muted-foreground'
          )}
        >
          {step.description}
        </p>
        {step.wait_condition && (
          <div className="flex items-center gap-1.5 mt-1.5">
            <span className="inline-block h-1.5 w-1.5 rounded-full bg-status-yellow animate-pulse" />
            <span className="text-xs text-status-yellow">{step.wait_condition}</span>
          </div>
        )}
        {step.state === 'failed' && step.error && (
          <p className="text-xs text-status-red mt-1.5 bg-status-red/10 rounded px-2 py-1">
            {step.error}
          </p>
        )}
      </div>
    </div>
  );
}

function CheckIcon({ className }: { className?: string }) {
  return (
    <svg
      className={className}
      fill="none"
      viewBox="0 0 24 24"
      stroke="currentColor"
      strokeWidth={3}
    >
      <path strokeLinecap="round" strokeLinejoin="round" d="M5 13l4 4L19 7" />
    </svg>
  );
}

function XIcon({ className }: { className?: string }) {
  return (
    <svg
      className={className}
      fill="none"
      viewBox="0 0 24 24"
      stroke="currentColor"
      strokeWidth={3}
    >
      <path strokeLinecap="round" strokeLinejoin="round" d="M6 18L18 6M6 6l12 12" />
    </svg>
  );
}

function EventItem({ event }: { event: OperationEvent }) {
  const isError =
    event.type === 'error' ||
    event.type === 'step_failed' ||
    event.type === 'operation_failed';

  const isSuccess =
    event.type === 'step_completed' ||
    event.type === 'operation_completed';

  const isStart =
    event.type === 'step_started' ||
    event.type === 'operation_started';

  return (
    <div
      className={cn(
        'flex items-start gap-2.5 px-2.5 py-2 rounded-md text-xs border border-transparent',
        isError && 'bg-status-red/10 border-status-red/20',
        isSuccess && 'bg-status-green/5 border-status-green/10'
      )}
    >
      {/* Event type indicator dot */}
      <span
        className={cn(
          'mt-1 h-1.5 w-1.5 rounded-full shrink-0',
          isError && 'bg-status-red',
          isSuccess && 'bg-status-green',
          isStart && 'bg-status-blue',
          !isError && !isSuccess && !isStart && 'bg-muted-foreground/40'
        )}
      />
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span
            className={cn(
              'font-medium',
              isError && 'text-status-red',
              isSuccess && 'text-status-green',
              isStart && 'text-status-blue',
              !isError && !isSuccess && !isStart && 'text-muted-foreground'
            )}
          >
            {formatEventType(event.type)}
          </span>
          <span className="text-muted-foreground/50 tabular-nums text-[10px]">
            {new Date(event.timestamp).toLocaleTimeString()}
          </span>
        </div>
        <p className="text-muted-foreground mt-0.5 break-words">{event.message}</p>
      </div>
    </div>
  );
}

function formatEventType(type: string): string {
  return type
    .replace(/_/g, ' ')
    .replace(/\b\w/g, (c) => c.toUpperCase());
}
