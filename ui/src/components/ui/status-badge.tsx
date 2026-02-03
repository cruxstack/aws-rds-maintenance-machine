import { cn, getStatusColor, isActiveStatus } from '@/lib/utils';

interface StatusBadgeProps {
  status: string;
  className?: string;
}

export function StatusBadge({ status, className }: StatusBadgeProps) {
  const displayStatus = formatStatusDisplay(status);
  const colorClasses = getStatusColor(status);
  const isActive = isActiveStatus(status);

  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-medium transition-colors',
        colorClasses,
        className
      )}
      title={status !== displayStatus ? status : undefined}
    >
      <span
        className={cn(
          'h-1.5 w-1.5 rounded-full',
          isActive && 'animate-pulse'
        )}
        style={{ backgroundColor: 'currentColor' }}
      />
      {displayStatus}
    </span>
  );
}

function formatStatusDisplay(status: string): string {
  // Truncate long configuring statuses
  if (status.startsWith('configuring-')) {
    return 'configuring';
  }
  if (
    status === 'inaccessible-encryption-credentials' ||
    status === 'inaccessible-encryption-credentials-recoverable'
  ) {
    return 'inaccessible';
  }
  if (status === 'resetting-master-credentials') {
    return 'resetting';
  }
  // Replace underscores with spaces for display
  return status.replace(/_/g, ' ');
}
