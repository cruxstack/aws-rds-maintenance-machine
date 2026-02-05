import { useState } from 'react';
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import type { ResumeAction } from '@/types';

interface ResumeDialogProps {
  open: boolean;
  onClose: () => void;
  onSubmit: (action: ResumeAction, comment: string) => void;
}

export function ResumeDialog({ open, onClose, onSubmit }: ResumeDialogProps) {
  const [action, setAction] = useState<ResumeAction>('continue');
  const [comment, setComment] = useState('');

  const handleSubmit = () => {
    onSubmit(action, comment);
    setAction('continue');
    setComment('');
  };

  return (
    <Dialog open={open} onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Resume Operation</DialogTitle>
          <DialogDescription>
            Choose how to proceed with this paused operation.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-4">
          <div className="space-y-2">
            <Label>Action</Label>
            <Select
              value={action}
              onValueChange={(v) => setAction(v as ResumeAction)}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="continue">Continue</SelectItem>
                <SelectItem value="rollback">Rollback</SelectItem>
                <SelectItem value="abort">Abort</SelectItem>
                <SelectItem value="mark_complete">Mark Complete</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div className="space-y-2">
            <Label>
              Comment{' '}
              <span className="font-normal text-muted-foreground">
                (optional)
              </span>
            </Label>
            <Input
              value={comment}
              onChange={(e) => setComment(e.target.value)}
              placeholder="Reason for action..."
            />
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={handleSubmit}>Submit</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
