import { cn } from '@/lib/utils';
import type { Toast, ToastType } from '@/hooks/use-toast';
import { AlertCircle, CheckCircle2, Info, AlertTriangle, X } from 'lucide-react';

interface ToasterProps {
  toasts: Toast[];
  onRemove: (id: string) => void;
}

export function Toaster({ toasts, onRemove }: ToasterProps) {
  if (toasts.length === 0) return null;

  return (
    <div className="fixed bottom-5 right-5 z-50 flex flex-col gap-2">
      {toasts.map((toast) => (
        <ToastItem key={toast.id} toast={toast} onRemove={onRemove} />
      ))}
    </div>
  );
}

const iconClass = "h-4 w-4 shrink-0";

function ToastIcon({ type }: { type: ToastType }) {
  switch (type) {
    case 'success':
      return <CheckCircle2 className={iconClass} />;
    case 'error':
      return <AlertCircle className={iconClass} />;
    case 'warning':
      return <AlertTriangle className={iconClass} />;
    case 'info':
    default:
      return <Info className={iconClass} />;
  }
}

function getToastColors(type: ToastType): string {
  switch (type) {
    case 'success':
      return 'bg-status-green-muted border-status-green text-foreground';
    case 'error':
      return 'bg-status-red-muted border-status-red text-foreground';
    case 'warning':
      return 'bg-status-yellow-muted border-status-yellow text-foreground';
    case 'info':
      return 'bg-status-blue-muted border-status-blue text-foreground';
    default:
      return 'bg-card border-border text-foreground';
  }
}

function ToastItem({
  toast,
  onRemove,
}: {
  toast: Toast;
  onRemove: (id: string) => void;
}) {
  return (
    <div
      className={cn(
        'flex items-center gap-2 rounded-lg border px-4 py-3 text-sm shadow-lg',
        'animate-in slide-in-from-right-full duration-200',
        getToastColors(toast.type)
      )}
      role="alert"
    >
      <ToastIcon type={toast.type} />
      <span className="flex-1">{toast.message}</span>
      <button
        onClick={() => onRemove(toast.id)}
        className="ml-2 shrink-0 rounded p-0.5 opacity-70 hover:opacity-100"
      >
        <X className="h-3.5 w-3.5" />
      </button>
    </div>
  );
}
