import { useCallback, useEffect, useMemo, useState } from "react"
import { LayoutGrid, LayoutList, Loader2, Plus, RefreshCw, Search, Sparkles } from "lucide-react"
import { toast } from "sonner"
import { podApi, type PodStats, type PodView } from "@/api/pod"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { FeatureDialog } from "@/components/common/FeatureDialog"
import { PodStatsCards } from "@/components/pod/PodStatsCards"
import { PodCardGrid, PodTable } from "@/components/pod/PodTable"
import { CreatePodDialog } from "@/components/pod/CreatePodDialog"
import { getUser } from "@/lib/auth"
import { cn } from "@/lib/utils"

const STATUS_OPTIONS = [
  { value: "all", label: "全部状态" },
  { value: "running", label: "运行中" },
  { value: "stopped", label: "已停止" },
  { value: "failed", label: "失败" },
  { value: "notfound", label: "NotFound" },
]

export default function PodList() {
  // 管理员可管理全部用户的 Pod，普通用户只能管理自己创建的 Pod（列表由后端按角色过滤）
  const isAdmin = getUser()?.role === "admin"
  const [items, setItems] = useState<PodView[]>([])
  const [stats, setStats] = useState<PodStats>({ running: 0, stopped: 0, failed: 0, mine: 0 })
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [keyword, setKeyword] = useState("")
  const [status, setStatus] = useState("all")
  const [view, setView] = useState<"table" | "cards">("table")
  const [createOpen, setCreateOpen] = useState(false)
  const [feature, setFeature] = useState<string | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<PodView | null>(null)
  const [busy, setBusy] = useState("")

  // 拉取列表与统计，silent=true 时用于轮询不显示 loading
  const load = useCallback(async (silent = false) => {
    if (!silent) setLoading(true)
    try {
      const [listResult, statsResult] = await Promise.all([podApi.list(), podApi.stats()])
      setItems(listResult.items)
      setStats(statsResult)
    } catch (error) {
      if (!silent) toast.error((error as Error).message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
    // 每 10 秒自动刷新，及时反映 Pod 状态变化
    const timer = setInterval(() => load(true), 10000)
    return () => clearInterval(timer)
  }, [load])

  // 前端过滤：名称模糊匹配 + 状态筛选
  const filtered = useMemo(() => {
    const key = keyword.trim().toLowerCase()
    return items.filter((pod) => {
      const matchKeyword = key === "" || pod.name.toLowerCase().includes(key)
      const matchStatus = status === "all" || pod.status === status
      return matchKeyword && matchStatus
    })
  }, [items, keyword, status])

  async function handleRefresh() {
    setRefreshing(true)
    await load(true)
    setRefreshing(false)
  }

  async function handleToggle(pod: PodView) {
    setBusy(pod.name)
    try {
      if (pod.status === "running") {
        await podApi.stop(pod.name)
        toast.success(`${pod.name} 已停止`)
      } else {
        await podApi.start(pod.name)
        toast.success(`${pod.name} 已启动`)
      }
      await load(true)
    } catch (error) {
      toast.error((error as Error).message)
    } finally {
      setBusy("")
    }
  }

  async function handleRestart(pod: PodView) {
    setBusy(pod.name)
    try {
      await podApi.restart(pod.name)
      toast.success(`${pod.name} 正在重启`)
      await load(true)
    } catch (error) {
      toast.error((error as Error).message)
    } finally {
      setBusy("")
    }
  }

  async function confirmDelete() {
    if (!deleteTarget) return
    setBusy(deleteTarget.name)
    try {
      await podApi.remove(deleteTarget.name)
      toast.success(`${deleteTarget.name} 已删除`)
      setDeleteTarget(null)
      await load(true)
    } catch (error) {
      toast.error((error as Error).message)
    } finally {
      setBusy("")
    }
  }

  const actions = {
    busy,
    onToggle: handleToggle,
    onRestart: handleRestart,
    onDelete: (pod: PodView) => setDeleteTarget(pod),
  }

  return (
    <div className="mx-auto max-w-7xl space-y-5">
      {/* 标题与操作区 */}
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight text-slate-900">Pod</h1>
          <p className="mt-1 text-sm text-slate-500">
            {isAdmin ? "管理员视图：可查看并管理全部用户创建的 Pod" : "仅展示并管理你自己创建的 Pod"}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" onClick={() => setFeature("AI 助手")}>
            <Sparkles className="h-4 w-4" />
            AI 助手
          </Button>
          <div className="flex items-center rounded-lg border border-slate-200 bg-white p-0.5">
            <Button
              variant="ghost"
              size="icon"
              title="列表视图"
              className={cn("h-8 w-8", view === "table" && "bg-slate-100")}
              onClick={() => setView("table")}
            >
              <LayoutList className="h-4 w-4" />
            </Button>
            <Button
              variant="ghost"
              size="icon"
              title="卡片视图"
              className={cn("h-8 w-8", view === "cards" && "bg-slate-100")}
              onClick={() => setView("cards")}
            >
              <LayoutGrid className="h-4 w-4" />
            </Button>
          </div>
          <Button onClick={() => setCreateOpen(true)}>
            <Plus className="h-4 w-4" />
            创建 Pod
          </Button>
        </div>
      </div>

      {/* 统计卡片 */}
      <PodStatsCards stats={stats} />

      {/* 搜索与筛选 */}
      <div className="flex flex-wrap items-center gap-3">
        <div className="relative w-full max-w-xs">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400" />
          <Input
            value={keyword}
            onChange={(e) => setKeyword(e.target.value)}
            placeholder="搜索 Pod 名称"
            className="pl-9"
          />
        </div>
        <Select value={status} onValueChange={setStatus}>
          <SelectTrigger className="w-36">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {STATUS_OPTIONS.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {option.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button variant="ghost" size="icon" title="刷新" onClick={handleRefresh} disabled={refreshing}>
          {refreshing ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
        </Button>
        <span className="ml-auto text-xs text-slate-400">共 {filtered.length} 个 Pod</span>
      </div>

      {/* 列表 / 卡片 */}
      {view === "table" ? (
        <Card className="overflow-hidden p-0">
          <PodTable items={filtered} loading={loading} {...actions} />
        </Card>
      ) : (
        <PodCardGrid items={filtered} loading={loading} {...actions} />
      )}

      <CreatePodDialog open={createOpen} onOpenChange={setCreateOpen} onCreated={() => load(true)} />
      <FeatureDialog open={feature !== null} onOpenChange={(open) => !open && setFeature(null)} title={feature || ""} />

      {/* 删除确认 */}
      <Dialog open={deleteTarget !== null} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>删除 Pod</DialogTitle>
            <DialogDescription>
              确认删除 <span className="font-medium text-slate-900">{deleteTarget?.name}</span>
              ？该操作会同时删除数据盘，且不可恢复。
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteTarget(null)}>
              取消
            </Button>
            <Button variant="destructive" onClick={confirmDelete} disabled={busy !== ""}>
              删除
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
