import { StatusBadge } from '@/components/ui/status-badge';
import { Database } from 'lucide-react';

interface HeaderProps {
  isDemoMode: boolean;
  isConnected: boolean;
  hasCredentialError?: boolean;
}

export function Header({ isDemoMode, isConnected, hasCredentialError }: HeaderProps) {
  const getStatus = () => {
    if (hasCredentialError) return 'credentials invalid';
    if (isConnected) return 'available';
    return 'failed';
  };

  return (
    <header className="flex items-center justify-between border-b border-border/50 pb-4 mb-6">
      <div className="flex items-center gap-3">
        <div className="flex items-center justify-center h-9 w-9 rounded-lg bg-primary/10 text-primary">
          <Database className="h-5 w-5" />
        </div>
        <div>
          <h1 className="text-lg font-semibold tracking-tight leading-tight">
            RDS Maintenance Machine
          </h1>
          <span className={`text-[10px] font-medium uppercase tracking-wider ${isDemoMode ? 'text-status-purple' : 'text-status-blue'}`}>
            {isDemoMode ? 'Demo' : 'Live'}
          </span>
        </div>
      </div>
      <div className="flex items-center gap-2">
        <span className="text-xs text-muted-foreground">Status:</span>
        <StatusBadge status={getStatus()} />
      </div>
    </header>
  );
}
