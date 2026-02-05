import { useState, useEffect } from 'react';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';

interface TimeoutDialogProps {
  open: boolean;
  currentValue: number;
  onClose: () => void;
  onSubmit: (timeout: number) => void;
}

export function TimeoutDialog({
  open,
  currentValue,
  onClose,
  onSubmit,
}: TimeoutDialogProps) {
  const [timeout, setTimeout] = useState(currentValue);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setTimeout(currentValue);
  }, [currentValue]);

  const handleSubmit = () => {
    if (timeout < 60) {
      setError('Timeout must be at least 60 seconds');
      return;
    }
    if (timeout > 7200) {
      setError('Timeout cannot exceed 7200 seconds (2 hours)');
      return;
    }
    setError(null);
    onSubmit(timeout);
  };

  return (
    <Dialog open={open} onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Update Wait Timeout</DialogTitle>
          <DialogDescription>
            Set the maximum time to wait for AWS operations to complete.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-4">
          <div className="space-y-2">
            <Label>Timeout (seconds)</Label>
            <Input
              type="number"
              min={60}
              max={7200}
              value={timeout}
              onChange={(e) => setTimeout(parseInt(e.target.value) || 0)}
              placeholder="2700"
            />
            <p className="text-xs text-muted-foreground">
              Default: 2700 (45 minutes). Max: 7200 (2 hours).
            </p>
            {error && <p className="text-xs text-status-red">{error}</p>}
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={handleSubmit}>Update Timeout</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
