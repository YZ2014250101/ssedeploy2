import { Box, Play, RotateCw, Square, Trash2 } from "lucide-react"
import { useNavigate } from "react-router-dom"
import type { PodView } from "@/api/pod"
import { Button } from "@/components/ui/button"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Skeleton } from "@/components/ui/skeleton"
import { StatusBadge } from "./StatusBadge"
import { cn } from "@/lib/utils"

interface PodActions {
  busy: string
  onToggle: (pod: PodView) => void
  onRestart: (pod: PodView) => void
  onDelete: (pod: PodView) => void
}

// Pod 表格视图
export function PodTable({ items, loading, busy, onToggle, onRestart, onDelete }: PodActions & { items: PodView[]; loading: boolean }) {
  const navigate = useNavigate()

  if (loading && items.length === 0) {
    return (
      <div className="space-y-3 p-5">
        {[0, 1, 2].map((i) => (
          <Skeleton key={i} className="h-11 w-full" />
        ))}
      </div>
    )
  }

  if (items.length === 0) {
    return <EmptyState />
  }

  return (
    <Table>
      <TableHeader>
        <TableRow className="hover:bg-slate-50/70">
          <TableHead className="w-32">状态</TableHead>
          <TableHead>名称</TableHead>
          <TableHead className="w-40">规格</TableHead>
          <TableHead className="w-32">负责人</TableHead>
          <TableHead className="w-28">我的角色</TableHead>
          <TableHead className="w-40 text-right">操作</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {items.map((pod) => (
          <TableRow key={pod.name}>
            <TableCell>
              <StatusBadge status={pod.status} />
            </TableCell>
            <TableCell>
              <button
                type="button"
                onClick={() => navigate(`/pods/${pod.name}`)}
                className="text-left text-sm font-medium text-slate-900 hover:text-blue-600"
              >
                {pod.name}
              </button>
              <div className="mt-0.5 max-w-[280px] truncate text-xs text-slate-400">{pod.image}</div>
            </TableCell>
            <TableCell className="font-mono text-xs text-slate-600">{pod.spec}</TableCell>
            <TableCell className="text-slate-600">{pod.owner}</TableCell>
            <TableCell>
              <span
                className={cn(
                  "rounded-full px-2 py-0.5 text-xs",
                  pod.myRole === "负责人" ? "bg-blue-50 text-blue-700" : "bg-slate-100 text-slate-500",
                )}
              >
                {pod.myRole}
              </span>
            </TableCell>
            <TableCell className="text-right">
              <div className="flex items-center justify-end gap-1">
                <Button
                  variant="ghost"
                  size="icon"
                  title={pod.status === "running" ? "停止" : "启动"}
                  disabled={busy === pod.name || pod.status === "notfound"}
                  onClick={() => onToggle(pod)}
                >
                  {pod.status === "running" ? <Square className="h-4 w-4" /> : <Play className="h-4 w-4" />}
                </Button>
                <Button
                  variant="ghost"
                  size="icon"
                  title="重启"
                  disabled={busy === pod.name || pod.status === "stopped" || pod.status === "notfound"}
                  onClick={() => onRestart(pod)}
                >
                  <RotateCw className="h-4 w-4" />
                </Button>
                <Button
                  variant="ghost"
                  size="icon"
                  title="删除"
                  className="text-red-500 hover:bg-red-50 hover:text-red-600"
                  disabled={busy === pod.name}
                  onClick={() => onDelete(pod)}
                >
                  <Trash2 className="h-4 w-4" />
                </Button>
              </div>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

// Pod 卡片视图
export function PodCardGrid({ items, loading, busy, onToggle, onRestart, onDelete }: PodActions & { items: PodView[]; loading: boolean }) {
  const navigate = useNavigate()

  if (loading && items.length === 0) {
    return (
      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        {[0, 1, 2].map((i) => (
          <Skeleton key={i} className="h-36 w-full" />
        ))}
      </div>
    )
  }

  if (items.length === 0) {
    return <EmptyState />
  }

  return (
    <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
      {items.map((pod) => (
        <div key={pod.name} className="rounded-xl border border-slate-200 bg-white p-5 shadow-sm">
          <div className="flex items-start justify-between">
            <button
              type="button"
              onClick={() => navigate(`/pods/${pod.name}`)}
              className="text-left text-sm font-semibold text-slate-900 hover:text-blue-600"
            >
              {pod.name}
            </button>
            <StatusBadge status={pod.status} />
          </div>
          <div className="mt-3 space-y-1 text-xs text-slate-500">
            <div className="truncate">镜像：{pod.image}</div>
            <div className="font-mono">规格：{pod.spec}</div>
          </div>
          <div className="mt-4 flex items-center justify-between border-t border-slate-100 pt-3">
            <span className="text-xs text-slate-500">
              {pod.owner} · {pod.myRole}
            </span>
            <div className="flex items-center gap-1">
              <Button
                variant="ghost"
                size="icon"
                title={pod.status === "running" ? "停止" : "启动"}
                disabled={busy === pod.name || pod.status === "notfound"}
                onClick={() => onToggle(pod)}
              >
                {pod.status === "running" ? <Square className="h-4 w-4" /> : <Play className="h-4 w-4" />}
              </Button>
              <Button
                variant="ghost"
                size="icon"
                title="重启"
                disabled={busy === pod.name || pod.status === "stopped" || pod.status === "notfound"}
                onClick={() => onRestart(pod)}
              >
                <RotateCw className="h-4 w-4" />
              </Button>
              <Button
                variant="ghost"
                size="icon"
                title="删除"
                className="text-red-500 hover:bg-red-50 hover:text-red-600"
                disabled={busy === pod.name}
                onClick={() => onDelete(pod)}
              >
                <Trash2 className="h-4 w-4" />
              </Button>
            </div>
          </div>
        </div>
      ))}
    </div>
  )
}

// 空状态
function EmptyState() {
  return (
    <div className="flex flex-col items-center justify-center px-6 py-16 text-center">
      <div className="flex h-12 w-12 items-center justify-center rounded-xl bg-slate-100 text-slate-400">
        <Box className="h-6 w-6" />
      </div>
      <div className="mt-4 text-sm font-medium text-slate-700">暂无 Pod</div>
      <div className="mt-1 text-xs text-slate-400">点击右上角「创建 Pod」开始使用</div>
    </div>
  )
}
