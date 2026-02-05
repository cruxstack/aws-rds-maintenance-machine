import { useState, useEffect, useCallback } from 'react';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog';
import { Header } from '@/components/layout/header';
import { OperationList } from '@/components/operations/operation-list';
import { CreateOperationForm } from '@/components/operations/create-operation-form';
import { OperationDetail } from '@/components/operations/operation-detail';
import { ClusterStatus } from '@/components/cluster/cluster-status';
import { ResumeDialog } from '@/components/operations/resume-dialog';
import { TimeoutDialog } from '@/components/operations/timeout-dialog';
import { DemoControls } from '@/components/demo/demo-controls';
import { Toaster } from '@/components/ui/toaster';
import { useToast } from '@/hooks/use-toast';
import * as api from '@/api/client';
import { ApiError } from '@/api/client';
import type {
  Operation,
  OperationEvent,
  CreateOperationRequest,
  ResumeAction,
} from '@/types';

function App() {
  const { toasts, removeToast, error: showError, success: showSuccess } = useToast();

  // App config (loaded from server)
  const [isDemoMode, setIsDemoMode] = useState(false);
  const [configLoaded, setConfigLoaded] = useState(false);

  // Connection status
  const [isConnected, setIsConnected] = useState(true);
  const [credentialError, setCredentialError] = useState<string | null>(null);

  // Operations state
  const [operations, setOperations] = useState<Operation[]>([]);
  const [selectedOperationId, setSelectedOperationId] = useState<string | null>(
    null
  );
  const [selectedOperation, setSelectedOperation] = useState<Operation | null>(
    null
  );
  const [operationEvents, setOperationEvents] = useState<OperationEvent[]>([]);
  const [isLoadingOperations, setIsLoadingOperations] = useState(false);

  // Dialogs
  const [resumeDialogOpen, setResumeDialogOpen] = useState(false);
  const [timeoutDialogOpen, setTimeoutDialogOpen] = useState(false);
  const [timeoutValue, setTimeoutValue] = useState(2700);
  const [confirmDialogOpen, setConfirmDialogOpen] = useState(false);
  const [confirmDialogConfig, setConfirmDialogConfig] = useState<{
    title: string;
    description: string;
    onConfirm: () => void;
  } | null>(null);

  // Load app config
  useEffect(() => {
    fetch('/api/config')
      .then((res) => res.json())
      .then((config) => {
        setIsDemoMode(config.demo_mode === true);
        setConfigLoaded(true);
      })
      .catch(() => {
        // Default to non-demo mode if config fails
        setConfigLoaded(true);
      });
  }, []);

  // Update favicon based on demo mode
  useEffect(() => {
    const favicon = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
    if (favicon) {
      favicon.href = isDemoMode ? '/favicon-demo.svg' : '/favicon.svg';
    }
  }, [isDemoMode]);

  // Tab state for demo mode
  const [activeTab, setActiveTab] = useState('operations');

  // Handle errors with credential detection
  const handleError = useCallback((message: string, error?: unknown) => {
    if (error instanceof ApiError && error.isCredentialError) {
      setCredentialError(message);
      setIsConnected(false);
    } else {
      showError(message);
    }
  }, [showError]);

  // Load operations
  const loadOperations = useCallback(async () => {
    setIsLoadingOperations(true);
    try {
      const ops = await api.getOperations();
      setOperations(ops);
      setIsConnected(true);
    } catch (err) {
      setIsConnected(false);
      handleError(`Failed to load operations: ${(err as Error).message}`, err);
    } finally {
      setIsLoadingOperations(false);
    }
  }, [handleError]);

  // Load operation detail
  const loadOperationDetail = useCallback(async (id: string) => {
    try {
      const [op, events] = await Promise.all([
        api.getOperation(id),
        api.getOperationEvents(id),
      ]);
      setSelectedOperation(op);
      setOperationEvents(events);
    } catch (err) {
      console.error('Failed to load operation details:', err);
    }
  }, []);

  // Initial load
  useEffect(() => {
    loadOperations();
    const interval = setInterval(loadOperations, 10000);
    return () => clearInterval(interval);
  }, [loadOperations]);

  // Poll selected operation
  useEffect(() => {
    if (!selectedOperationId) return;

    loadOperationDetail(selectedOperationId);
    const pollMs = isDemoMode ? 1000 : 3000;
    const interval = setInterval(
      () => loadOperationDetail(selectedOperationId),
      pollMs
    );
    return () => clearInterval(interval);
  }, [selectedOperationId, loadOperationDetail, isDemoMode]);

  // Handlers
  const handleSelectOperation = (id: string) => {
    setSelectedOperationId(id);
  };

  const handleCreateOperation = useCallback(async (req: CreateOperationRequest) => {
    try {
      const op = await api.createOperation(req);
      loadOperations();
      setSelectedOperationId(op.id);
      showSuccess('Operation created successfully');
    } catch (err) {
      showError((err as Error).message);
      throw err;
    }
  }, [loadOperations, showSuccess, showError]);

  const handleStartOperation = async () => {
    if (!selectedOperationId) return;
    try {
      await api.startOperation(selectedOperationId);
      loadOperationDetail(selectedOperationId);
      loadOperations();
    } catch (err) {
      showError((err as Error).message);
    }
  };

  const handlePauseOperation = async () => {
    if (!selectedOperationId) return;
    const reason = prompt('Pause reason:');
    if (reason === null) return;

    try {
      await api.pauseOperation(selectedOperationId, reason);
      loadOperationDetail(selectedOperationId);
      loadOperations();
    } catch (err) {
      showError((err as Error).message);
    }
  };

  const handleResumeOperation = async (action: ResumeAction, comment: string) => {
    if (!selectedOperationId) return;
    try {
      await api.resumeOperation(selectedOperationId, { action, comment });
      setResumeDialogOpen(false);
      loadOperationDetail(selectedOperationId);
      loadOperations();
    } catch (err) {
      showError((err as Error).message);
    }
  };

  const handleDeleteOperation = async () => {
    if (!selectedOperationId) return;

    setConfirmDialogConfig({
      title: 'Delete Operation',
      description:
        'Are you sure you want to delete this operation? This cannot be undone.',
      onConfirm: async () => {
        try {
          await api.deleteOperation(selectedOperationId);
          setSelectedOperationId(null);
          setSelectedOperation(null);
          loadOperations();
          showSuccess('Operation deleted');
        } catch (err) {
          showError((err as Error).message);
        }
      },
    });
    setConfirmDialogOpen(true);
  };

  const handleEditTimeout = (current: number) => {
    setTimeoutValue(current);
    setTimeoutDialogOpen(true);
  };

  const handleUpdateTimeout = async (timeout: number) => {
    if (!selectedOperationId) return;
    try {
      await api.updateOperationTimeout(selectedOperationId, timeout);
      setTimeoutDialogOpen(false);
      loadOperationDetail(selectedOperationId);
    } catch (err) {
      showError((err as Error).message);
    }
  };

  const handleTogglePauseStep = async (stepIndex: number) => {
    if (!selectedOperationId || !selectedOperation) return;
    try {
      const currentPauseSteps = selectedOperation.pause_before_steps ?? [];
      let newPauseSteps: number[];
      
      if (currentPauseSteps.includes(stepIndex)) {
        // Remove from list
        newPauseSteps = currentPauseSteps.filter((i) => i !== stepIndex);
      } else {
        // Add to list
        newPauseSteps = [...currentPauseSteps, stepIndex].sort((a, b) => a - b);
      }
      
      await api.updateOperationPauseSteps(selectedOperationId, newPauseSteps);
      loadOperationDetail(selectedOperationId);
    } catch (err) {
      showError((err as Error).message);
    }
  };

  const handleResetAll = async () => {
    setSelectedOperationId(null);
    setSelectedOperation(null);
    await api.deleteAllOperations();
    loadOperations();
  };

  // Cluster status refresh interval (faster in demo mode)
  const clusterRefreshInterval = isDemoMode ? 2000 : 5000;

  // Show loading until config is loaded
  if (!configLoaded) {
    return (
      <div className="min-h-screen bg-background flex items-center justify-center">
        <div className="text-muted-foreground">Loading...</div>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-background">
      <div className="mx-auto max-w-[1400px] p-6">
        <Header isDemoMode={isDemoMode} isConnected={isConnected} hasCredentialError={credentialError !== null} />

        {isDemoMode ? (
          <Tabs value={activeTab} onValueChange={setActiveTab}>
            <TabsList className="mb-6">
              <TabsTrigger value="operations">Operations</TabsTrigger>
              <TabsTrigger value="demo">Demo Controls</TabsTrigger>
            </TabsList>

            <TabsContent value="operations">
              <OperationsView
                operations={operations}
                selectedOperationId={selectedOperationId}
                selectedOperation={selectedOperation}
                operationEvents={operationEvents}
                isLoadingOperations={isLoadingOperations}
                isDemoMode={isDemoMode}
                clusterRefreshInterval={clusterRefreshInterval}
                onSelectOperation={handleSelectOperation}
                onRefreshOperations={loadOperations}
                onCreateOperation={handleCreateOperation}
                onStartOperation={handleStartOperation}
                onPauseOperation={handlePauseOperation}
                onResumeOperation={() => setResumeDialogOpen(true)}
                onDeleteOperation={handleDeleteOperation}
                onEditTimeout={handleEditTimeout}
                onTogglePauseStep={handleTogglePauseStep}
                onError={handleError}
              />
            </TabsContent>

            <TabsContent value="demo">
              <DemoControls onError={handleError} onResetAll={handleResetAll} />
            </TabsContent>
          </Tabs>
        ) : (
          <OperationsView
            operations={operations}
            selectedOperationId={selectedOperationId}
            selectedOperation={selectedOperation}
            operationEvents={operationEvents}
            isLoadingOperations={isLoadingOperations}
            isDemoMode={isDemoMode}
            clusterRefreshInterval={clusterRefreshInterval}
            onSelectOperation={handleSelectOperation}
            onRefreshOperations={loadOperations}
            onCreateOperation={handleCreateOperation}
            onStartOperation={handleStartOperation}
            onPauseOperation={handlePauseOperation}
            onResumeOperation={() => setResumeDialogOpen(true)}
            onDeleteOperation={handleDeleteOperation}
            onEditTimeout={handleEditTimeout}
            onTogglePauseStep={handleTogglePauseStep}
            onError={handleError}
          />
        )}
      </div>

      {/* Dialogs */}
      <ResumeDialog
        open={resumeDialogOpen}
        onClose={() => setResumeDialogOpen(false)}
        onSubmit={handleResumeOperation}
      />

      <TimeoutDialog
        open={timeoutDialogOpen}
        currentValue={timeoutValue}
        onClose={() => setTimeoutDialogOpen(false)}
        onSubmit={handleUpdateTimeout}
      />

      <AlertDialog open={confirmDialogOpen} onOpenChange={setConfirmDialogOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{confirmDialogConfig?.title}</AlertDialogTitle>
            <AlertDialogDescription>
              {confirmDialogConfig?.description}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                confirmDialogConfig?.onConfirm();
                setConfirmDialogOpen(false);
              }}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              Confirm
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={credentialError !== null} onOpenChange={(open) => !open && setCredentialError(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>AWS Credentials Invalid</AlertDialogTitle>
            <AlertDialogDescription asChild>
              <div>
                <p className="mb-3">Please update your AWS credentials or session, restart the server, and reload this page.</p>
                <code className="block bg-muted px-3 py-2 rounded text-xs font-mono text-foreground whitespace-pre-wrap break-all">
                  {credentialError}
                </code>
              </div>
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogAction onClick={() => setCredentialError(null)}>
              Dismiss
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Toast notifications */}
      <Toaster toasts={toasts} onRemove={removeToast} />
    </div>
  );
}

// Extracted operations view for reuse
interface OperationsViewProps {
  operations: Operation[];
  selectedOperationId: string | null;
  selectedOperation: Operation | null;
  operationEvents: OperationEvent[];
  isLoadingOperations: boolean;
  isDemoMode: boolean;
  clusterRefreshInterval: number;
  onSelectOperation: (id: string) => void;
  onRefreshOperations: () => void;
  onCreateOperation: (req: CreateOperationRequest) => Promise<void>;
  onStartOperation: () => void;
  onPauseOperation: () => void;
  onResumeOperation: () => void;
  onDeleteOperation: () => void;
  onEditTimeout: (current: number) => void;
  onTogglePauseStep: (stepIndex: number) => void;
  onError: (error: string, originalError?: unknown) => void;
}

function OperationsView({
  operations,
  selectedOperationId,
  selectedOperation,
  operationEvents,
  isLoadingOperations,
  isDemoMode,
  clusterRefreshInterval,
  onSelectOperation,
  onRefreshOperations,
  onCreateOperation,
  onStartOperation,
  onPauseOperation,
  onResumeOperation,
  onDeleteOperation,
  onEditTimeout,
  onTogglePauseStep,
  onError,
}: OperationsViewProps) {
  return (
    <div className="grid grid-cols-[380px_1fr] gap-6">
      {/* Sidebar */}
      <div className="space-y-6">
        <CreateOperationForm onSubmit={onCreateOperation} onError={onError} />
        <OperationList
          operations={operations}
          selectedId={selectedOperationId}
          onSelect={onSelectOperation}
          onRefresh={onRefreshOperations}
          isLoading={isLoadingOperations}
        />
      </div>

      {/* Main content */}
      <div className="space-y-6">
        {selectedOperation ? (
          <>
            <OperationDetail
              operation={selectedOperation}
              events={operationEvents}
              isDemoMode={isDemoMode}
              onStart={onStartOperation}
              onPause={onPauseOperation}
              onResume={onResumeOperation}
              onDelete={onDeleteOperation}
              onEditTimeout={isDemoMode ? undefined : onEditTimeout}
              onTogglePauseStep={onTogglePauseStep}
            />
            <ClusterStatus
              clusterId={selectedOperation.cluster_id}
              region={selectedOperation.region}
              refreshInterval={clusterRefreshInterval}
            />
          </>
        ) : (
          <div className="flex flex-col items-center justify-center rounded-xl border border-dashed border-border/50 bg-card/30 p-16 text-center">
            <div className="flex h-12 w-12 items-center justify-center rounded-full bg-muted mb-4">
              <svg
                className="h-6 w-6 text-muted-foreground"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
                strokeWidth={1.5}
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  d="M9 12h3.75M9 15h3.75M9 18h3.75m3 .75H18a2.25 2.25 0 002.25-2.25V6.108c0-1.135-.845-2.098-1.976-2.192a48.424 48.424 0 00-1.123-.08m-5.801 0c-.065.21-.1.433-.1.664 0 .414.336.75.75.75h4.5a.75.75 0 00.75-.75 2.25 2.25 0 00-.1-.664m-5.8 0A2.251 2.251 0 0113.5 2.25H15c1.012 0 1.867.668 2.15 1.586m-5.8 0c-.376.023-.75.05-1.124.08C9.095 4.01 8.25 4.973 8.25 6.108V8.25m0 0H4.875c-.621 0-1.125.504-1.125 1.125v11.25c0 .621.504 1.125 1.125 1.125h9.75c.621 0 1.125-.504 1.125-1.125V9.375c0-.621-.504-1.125-1.125-1.125H8.25zM6.75 12h.008v.008H6.75V12zm0 3h.008v.008H6.75V15zm0 3h.008v.008H6.75V18z"
                />
              </svg>
            </div>
            <p className="text-foreground font-medium">No operation selected</p>
            <p className="text-sm text-muted-foreground mt-1 max-w-[260px]">
              Select an operation from the list or create a new one to get started
            </p>
          </div>
        )}
      </div>
    </div>
  );
}

export default App;
