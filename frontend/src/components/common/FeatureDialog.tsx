import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"

interface FeatureDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
}

// 未开发模块的统一提示弹窗
export function FeatureDialog({ open, onOpenChange, title }: FeatureDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>该模块开发中，敬请期待。</DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button onClick={() => onOpenChange(false)}>我知道了</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
