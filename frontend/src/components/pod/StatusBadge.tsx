import { cn } from "@/lib/utils"
import type { PodStatus } from "@/api/pod"

// 状态徽章：颜色与业务映射保持一致
const STYLES: Record<PodStatus, { dot: string; text: string; badge: string }> = {
  running: { dot: "bg-emerald-500", text: "运行中", badge: "bg-emerald-50 text-emerald-700 ring-emerald-100" },
  stopped: { dot: "bg-slate-400", text: "已停止", badge: "bg-slate-100 text-slate-600 ring-slate-200" },
  failed: { dot: "bg-red-500", text: "失败", badge: "bg-red-50 text-red-700 ring-red-100" },
  notfound: { dot: "bg-amber-400", text: "NotFound", badge: "bg-amber-50 text-amber-700 ring-amber-100" },
}

export function StatusBadge({ status, className }: { status: PodStatus; className?: string }) {
  const style = STYLES[status] ?? STYLES.notfound
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-medium ring-1 ring-inset",
        style.badge,
        className,
      )}
    >
      <span className={cn("h-1.5 w-1.5 rounded-full", style.dot)} />
      {style.text}
    </span>
  )
}
