import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

// Format duration in human-readable format
export function formatDuration(startTime?: string, endTime?: string): string {
  if (!startTime) return '-';
  const start = new Date(startTime);
  const end = endTime ? new Date(endTime) : new Date();
  const diffMs = end.getTime() - start.getTime();

  if (diffMs < 0) return '-';

  const seconds = Math.floor(diffMs / 1000);
  if (seconds < 60) return `${seconds}s`;

  const minutes = Math.floor(seconds / 60);
  const remSeconds = seconds % 60;
  if (minutes < 60) return `${minutes}m ${remSeconds}s`;

  const hours = Math.floor(minutes / 60);
  const remMinutes = minutes % 60;
  return `${hours}h ${remMinutes}m ${remSeconds}s`;
}

// Format relative time (e.g., "2m ago", "1h ago", "yesterday")
export function formatRelativeTime(timestamp: string): string {
  const now = new Date();
  const date = new Date(timestamp);
  const diffMs = now.getTime() - date.getTime();
  
  if (diffMs < 0) return 'just now';
  
  const seconds = Math.floor(diffMs / 1000);
  if (seconds < 60) return 'just now';
  
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  
  const days = Math.floor(hours / 24);
  if (days === 1) return 'yesterday';
  if (days < 7) return `${days}d ago`;
  
  // Fall back to short date for older items
  return date.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
}

// Format timeout for display
export function formatTimeout(seconds?: number): string {
  if (!seconds) return 'default (45 min)';
  const mins = Math.floor(seconds / 60);
  if (mins >= 60) {
    const hrs = Math.floor(mins / 60);
    const remMins = mins % 60;
    return `${hrs}h ${remMins > 0 ? `${remMins}m` : ''}`;
  }
  return `${mins} min`;
}

// Truncate text with ellipsis
export function truncateText(text: string, maxLength: number): string {
  if (text.length <= maxLength) return text;
  return text.substring(0, maxLength - 3) + '...';
}

// Copy to clipboard
export async function copyToClipboard(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}

// Operation type display names
export const OPERATION_TYPE_NAMES: Record<string, string> = {
  instance_type_change: 'Instance Type Change',
  storage_type_change: 'Storage Type Change',
  engine_upgrade: 'Engine Upgrade',
  instance_cycle: 'Instance Cycle',
};

// Status color mapping
export function getStatusColor(status: string): string {
  const normalizedStatus = status.toLowerCase().replace(/[_-]/g, '');

  // Success states
  if (
    normalizedStatus === 'available' ||
    normalizedStatus === 'completed' ||
    normalizedStatus === 'inservice'
  ) {
    return 'bg-status-green-muted text-status-green';
  }

  // Warning/pending states
  if (
    normalizedStatus.includes('pending') ||
    normalizedStatus.includes('modifying') ||
    normalizedStatus.includes('configuring') ||
    normalizedStatus === 'paused' ||
    normalizedStatus === 'waiting' ||
    normalizedStatus === 'provisioning'
  ) {
    return 'bg-status-yellow-muted text-status-yellow';
  }

  // Error states
  if (
    normalizedStatus === 'failed' ||
    normalizedStatus.includes('error') ||
    normalizedStatus.includes('inaccessible') ||
    normalizedStatus.includes('invalid') ||
    normalizedStatus.includes('credentials')
  ) {
    return 'bg-status-red-muted text-status-red';
  }

  // In-progress states
  if (
    normalizedStatus === 'running' ||
    normalizedStatus === 'inprogress' ||
    normalizedStatus === 'creating' ||
    normalizedStatus === 'deleting' ||
    normalizedStatus === 'rebooting' ||
    normalizedStatus === 'starting' ||
    normalizedStatus === 'stopping'
  ) {
    return 'bg-status-blue-muted text-status-blue';
  }

  // Rollback states
  if (normalizedStatus.includes('rollback') || normalizedStatus.includes('rolledback')) {
    return 'bg-status-orange-muted text-status-orange';
  }

  // Created/initial state
  if (normalizedStatus === 'created') {
    return 'bg-status-purple-muted text-status-purple';
  }

  // Default
  return 'bg-muted text-muted-foreground';
}

// Check if status represents an active/in-progress state (for animations)
export function isActiveStatus(status: string): boolean {
  const normalizedStatus = status.toLowerCase().replace(/[_-]/g, '');
  return (
    normalizedStatus === 'running' ||
    normalizedStatus === 'inprogress' ||
    normalizedStatus === 'creating' ||
    normalizedStatus === 'deleting' ||
    normalizedStatus === 'rebooting' ||
    normalizedStatus === 'starting' ||
    normalizedStatus === 'stopping' ||
    normalizedStatus === 'waiting' ||
    normalizedStatus === 'provisioning' ||
    normalizedStatus.includes('modifying') ||
    normalizedStatus.includes('configuring') ||
    normalizedStatus.includes('pending')
  );
}
