import { useCallback, useEffect, useState } from "react"
import {
  Activity,
  Cpu,
  GitBranch,
  Loader2,
  Pencil,
  Play,
  Plus,
  RefreshCw,
  Square,
  Trash2,
} from "lucide-react"
import { toast } from "sonner"
import {
  deployApi,
  type CreateDeployPayload,
  type DeploySource,
  type DeployStatus,
  type DeployType,
  type DeployView,
} from "@/api/deploy"
import { gpuApi, type GPUDeviceData } from "@/api/gpu"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { cn } from "@/lib/utils"
import { formatTime } from "@/lib/format"

// 部署状态徽章样式：fatal 用红色突出服务启动失败
const STATUS_STYLE: Record<DeployStatus, { label: string; variant: "default" | "secondary" | "destructive" | "success" | "warning" }> = {
  pending: { label: "启动中", variant: "warning" },
  running: { label: "运行中", variant: "success" },
  stopped: { label: "已停止", variant: "secondary" },
  fatal: { label: "fatal", variant: "destructive" },
  succeeded: { label: "执行完成", variant: "default" },
  notfound: { label: "NotFound", variant: "secondary" },
}

// 表单状态（下拉用字符串，提交时再映射为后端枚举）
interface FormState {
  source: DeploySource
  name: string
  repoUrl: string
  branch: string
  subDir: string
  type: DeployType
  scheduleType: string
  gpu: string
  token: string
  healthPath: string
  timeoutSec: string
  autoDeploy: boolean
}

const EMPTY_FORM: FormState = {
  source: "repo",
  name: "",
  repoUrl: "",
  branch: "main",
  subDir: "",
  type: "service",
  scheduleType: "inference",
  gpu: "",
  token: "",
  healthPath: "",
  timeoutSec: "5",
  autoDeploy: false,
}

// 部署 Tab：部署实例列表 + 创建/编辑弹窗 + 文档说明
export function DeployTab() {
  const [items, setItems] = useState<DeployView[]>([])
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState("")

  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<DeployView | null>(null)
  const [form, setForm] = useState<FormState>(EMPTY_FORM)
  const [submitting, setSubmitting] = useState(false)

  const [deleteTarget, setDeleteTarget] = useState<DeployView | null>(null)
  const [deleting, setDeleting] = useState(false)

  const [doc, setDoc] = useState<"tutorial" | "webhook" | null>(null)
  const [gpus, setGpus] = useState<GPUDeviceData[]>([])

  const load = useCallback(async (silent = false) => {
    if (!silent) setLoading(true)
    try {
      const result = await deployApi.list()
      setItems(result.items)
    } catch (error) {
      if (!silent) toast.error((error as Error).message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
    // 轮询保证状态（启动中 → 运行中 / fatal）实时刷新
    const timer = setInterval(() => load(true), 10000)
    return () => clearInterval(timer)
  }, [load])

  // GPU 列表用于绑卡选择；宿主机没有 GPU 时下拉只保留「不绑定」
  useEffect(() => {
    gpuApi
      .detail()
      .then((data) => setGpus(data.gpuList || []))
      .catch(() => setGpus([]))
  }, [])

  function openCreate() {
    setEditing(null)
    setForm(EMPTY_FORM)
    setFormOpen(true)
  }

  function openEdit(item: DeployView) {
    setEditing(item)
    setForm({
      source: item.source,
      name: item.name,
      repoUrl: item.repoUrl,
      branch: item.branch || "main",
      subDir: item.subDir,
      type: item.type,
      scheduleType: item.scheduleType || "inference",
      gpu: item.gpu,
      token: "",
      healthPath: item.healthPath,
      timeoutSec: String(item.timeoutSec || 5),
      autoDeploy: item.autoDeploy,
    })
    setFormOpen(true)
  }

  function update<K extends keyof FormState>(key: K, value: FormState[K]) {
    setForm((prev) => ({ ...prev, [key]: value }))
  }

  async function handleSubmit() {
    if (form.source === "repo" && !form.repoUrl.trim()) {
      toast.error("请填写 Git 仓库地址")
      return
    }
    const timeout = Number(form.timeoutSec)
    if (!Number.isFinite(timeout) || timeout <= 0) {
      toast.error("超时时间需为大于 0 的秒数")
      return
    }

    setSubmitting(true)
    try {
      if (editing) {
        await deployApi.update(editing.name, {
          repoUrl: form.repoUrl.trim(),
          branch: form.branch.trim(),
          subDir: form.subDir.trim(),
          type: form.type,
          scheduleType: form.scheduleType,
          gpu: form.gpu,
          token: form.token,
          healthPath: form.healthPath.trim(),
          timeoutSec: timeout,
          autoDeploy: form.autoDeploy,
        })
        toast.success("部署配置已更新，实例正在重新构建")
      } else {
        const payload: CreateDeployPayload = {
          source: form.source,
          name: form.name.trim(),
          repoUrl: form.repoUrl.trim(),
          branch: form.branch.trim(),
          subDir: form.subDir.trim(),
          type: form.type,
          scheduleType: form.scheduleType,
          gpu: form.gpu,
          token: form.token,
          healthPath: form.healthPath.trim(),
          timeoutSec: timeout,
          autoDeploy: form.autoDeploy,
        }
        await deployApi.create(payload)
        toast.success("部署已创建，正在拉取仓库并启动实例")
      }
      setFormOpen(false)
      await load(true)
    } catch (error) {
      toast.error((error as Error).message)
    } finally {
      setSubmitting(false)
    }
  }

  async function handleToggle(item: DeployView) {
    setBusy(item.name)
    try {
      if (item.status === "running" || item.status === "pending") {
        await deployApi.stop(item.name)
        toast.success("部署已停止")
      } else {
        await deployApi.start(item.name)
        toast.success("部署正在启动")
      }
      await load(true)
    } catch (error) {
      toast.error((error as Error).message)
    } finally {
      setBusy("")
    }
  }

  async function handleRestart(item: DeployView) {
    setBusy(item.name)
    try {
      await deployApi.restart(item.name)
      toast.success("部署正在重启")
      await load(true)
    } catch (error) {
      toast.error((error as Error).message)
    } finally {
      setBusy("")
    }
  }

  async function handleDelete() {
    if (!deleteTarget) return
    setDeleting(true)
    try {
      await deployApi.remove(deleteTarget.name)
      toast.success("部署已删除")
      setDeleteTarget(null)
      await load(true)
    } catch (error) {
      toast.error((error as Error).message)
    } finally {
      setDeleting(false)
    }
  }

  return (
    <div className="space-y-4">
      {/* 顶部：文档入口 + 创建部署 */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="text-base font-semibold text-slate-900">部署</h2>
            <Button variant="link" size="sm" className="h-auto p-0" onClick={() => setDoc("tutorial")}>
              部署教程
            </Button>
            <Button variant="link" size="sm" className="h-auto p-0" onClick={() => setDoc("webhook")}>
              Webhook 说明
            </Button>
          </div>
          <p className="mt-1 text-xs text-slate-400">从 Git 仓库拉取代码，构建并运行部署实例</p>
        </div>
        <Button onClick={openCreate}>
          <Plus className="h-4 w-4" />
          创建部署
        </Button>
      </div>

      <div className="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-700">
        当前平台尚未接入 Git 构建流水线：实例会先尝试拉取公开仓库源码归档，再以平台内置占位运行时启动；仓库、分支、健康检查等配置均已完整记录。
      </div>

      {/* 部署实例卡片列表 */}
      {loading && items.length === 0 ? (
        <div className="flex items-center justify-center rounded-xl border border-slate-200 bg-white p-12 text-sm text-slate-400">
          <Loader2 className="mr-2 h-4 w-4 animate-spin" />
          加载部署列表…
        </div>
      ) : items.length === 0 ? (
        <div className="rounded-xl border border-dashed border-slate-200 bg-white p-12 text-center">
          <div className="text-sm font-medium text-slate-700">还没有部署实例</div>
          <div className="mt-1 text-xs text-slate-500">点击右上角「创建部署」，填写 Git 仓库信息开始部署</div>
        </div>
      ) : (
        <div className="space-y-3">
          {items.map((item) => (
            <DeployCard
              key={item.name}
              item={item}
              busy={busy === item.name}
              onToggle={() => handleToggle(item)}
              onRestart={() => handleRestart(item)}
              onEdit={() => openEdit(item)}
              onDelete={() => setDeleteTarget(item)}
            />
          ))}
        </div>
      )}

      {/* 创建 / 编辑弹窗 */}
      <Dialog open={formOpen} onOpenChange={setFormOpen}>
        <DialogContent className="max-h-[90vh] max-w-2xl overflow-y-auto">
          <DialogHeader>
            <DialogTitle>{editing ? `编辑部署 · ${editing.name}` : "创建部署"}</DialogTitle>
            <DialogDescription>
              填写 Git 仓库与服务参数，提交后平台会拉取代码、构建并调度资源启动进程。
            </DialogDescription>
          </DialogHeader>

          <div className="grid gap-4 md:grid-cols-2">
            <div className="space-y-1.5">
              <Label>来源</Label>
              <Select
                value={form.source}
                onValueChange={(value) => update("source", value as DeploySource)}
                disabled={!!editing}
              >
                <SelectTrigger>
                  <SelectValue placeholder="选择部署来源" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="repo">绑仓库（从 Git 仓库拉取代码部署）</SelectItem>
                  <SelectItem value="local">本地（使用平台内置运行时）</SelectItem>
                </SelectContent>
              </Select>
              {editing && <p className="text-xs text-slate-400">部署来源创建后不可修改</p>}
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="deploy-name">名称</Label>
              <Input
                id="deploy-name"
                value={form.name}
                disabled={!!editing}
                onChange={(e) => update("name", e.target.value)}
                placeholder="留空自动使用仓库名称"
              />
            </div>

            <div className="space-y-1.5 md:col-span-2">
              <Label htmlFor="deploy-repo">仓库地址</Label>
              <Input
                id="deploy-repo"
                value={form.repoUrl}
                onChange={(e) => update("repoUrl", e.target.value)}
                placeholder="https://gitee.com/owner/repo.git 或 https://github.com/owner/repo"
              />
              <p className="text-xs text-slate-400">支持 Gitee / GitHub 仓库</p>
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="deploy-branch">分支</Label>
              <Input
                id="deploy-branch"
                value={form.branch}
                onChange={(e) => update("branch", e.target.value)}
                placeholder="main"
              />
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="deploy-subdir">子目录</Label>
              <Input
                id="deploy-subdir"
                value={form.subDir}
                onChange={(e) => update("subDir", e.target.value)}
                placeholder="项目代码不在仓库根目录时填写，根目录留空"
              />
            </div>

            <div className="space-y-1.5">
              <Label>类型</Label>
              <Select value={form.type} onValueChange={(value) => update("type", value as DeployType)}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="service">常驻服务</SelectItem>
                  <SelectItem value="job">一次性脚本</SelectItem>
                </SelectContent>
              </Select>
              <p className="text-xs text-slate-400">
                常驻服务后台持续运行、异常自动重启；一次性脚本执行完成即退出
              </p>
            </div>

            <div className="space-y-1.5">
              <Label>调度类型</Label>
              <Select value={form.scheduleType} onValueChange={(value) => update("scheduleType", value)}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="inference">推理服务</SelectItem>
                  <SelectItem value="general">通用任务</SelectItem>
                </SelectContent>
              </Select>
              <p className="text-xs text-slate-400">推理服务会标记为 AI 推理负载，走推理资源池调度</p>
            </div>

            <div className="space-y-1.5">
              <Label>绑卡 GPU</Label>
              <Select
                value={form.gpu || "none"}
                onValueChange={(value) => update("gpu", value === "none" ? "" : value)}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="none">不绑定 GPU</SelectItem>
                  {gpus.map((gpu) => (
                    <SelectItem key={gpu.id} value={`GPU-${gpu.id}`}>
                      GPU-{gpu.id} · {gpu.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <p className="text-xs text-slate-400">本地大模型推理时绑定 GPU；纯 API 转发服务可不绑定</p>
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="deploy-token">私有库 Token</Label>
              <Input
                id="deploy-token"
                type="password"
                value={form.token}
                onChange={(e) => update("token", e.target.value)}
                placeholder={editing ? "留空表示不修改" : "公开仓库留空"}
              />
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="deploy-health">健康检查路径（可选）</Label>
              <Input
                id="deploy-health"
                value={form.healthPath}
                onChange={(e) => update("healthPath", e.target.value)}
                placeholder="/healthz，留空跳过健康探测"
              />
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="deploy-timeout">超时（秒）</Label>
              <Input
                id="deploy-timeout"
                type="number"
                min={1}
                value={form.timeoutSec}
                onChange={(e) => update("timeoutSec", e.target.value)}
              />
              <p className="text-xs text-slate-400">健康检查请求超时，超时未响应判定服务异常</p>
            </div>

            <div className="flex items-center justify-between gap-3 rounded-lg border border-slate-200 px-3 py-2 md:col-span-2">
              <div>
                <div className="text-sm font-medium text-slate-700">push 自动部署</div>
                <p className="text-xs text-slate-400">
                  开启后向指定分支推送代码会自动触发重新构建部署；需管理员提前配置 Webhook 回调，仅切换分支不会自动部署
                </p>
              </div>
              <button
                type="button"
                role="switch"
                aria-checked={form.autoDeploy}
                onClick={() => update("autoDeploy", !form.autoDeploy)}
                className={cn(
                  "relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors",
                  form.autoDeploy ? "bg-blue-600" : "bg-slate-300",
                )}
              >
                <span
                  className={cn(
                    "inline-block h-4 w-4 transform rounded-full bg-white transition-transform",
                    form.autoDeploy ? "translate-x-4" : "translate-x-0.5",
                  )}
                />
              </button>
            </div>
          </div>

          <DialogFooter>
            <Button variant="outline" onClick={() => setFormOpen(false)} disabled={submitting}>
              取消
            </Button>
            <Button onClick={handleSubmit} disabled={submitting}>
              {submitting ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
              确认
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 删除确认 */}
      <Dialog open={deleteTarget !== null} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>删除部署</DialogTitle>
            <DialogDescription>
              将删除部署实例「{deleteTarget?.name}」及其集群工作负载，且不可恢复。
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteTarget(null)} disabled={deleting}>
              取消
            </Button>
            <Button variant="destructive" onClick={handleDelete} disabled={deleting}>
              {deleting ? <Loader2 className="h-4 w-4 animate-spin" /> : <Trash2 className="h-4 w-4" />}
              确认删除
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 文档说明 */}
      <DocDialog type={doc} onClose={() => setDoc(null)} />
    </div>
  )
}

// 单个部署实例卡片
function DeployCard({
  item,
  busy,
  onToggle,
  onRestart,
  onEdit,
  onDelete,
}: {
  item: DeployView
  busy: boolean
  onToggle: () => void
  onRestart: () => void
  onEdit: () => void
  onDelete: () => void
}) {
  const active = item.status === "running" || item.status === "pending"
  const badge = STATUS_STYLE[item.status] ?? STATUS_STYLE.notfound

  return (
    <Card>
      <CardContent className="flex flex-wrap items-start justify-between gap-4 p-5">
        <div className="min-w-0 space-y-2">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-medium text-slate-900">{item.name}</span>
            <Badge variant={badge.variant}>{badge.label}</Badge>
            <Badge variant="outline">{item.sourceText}</Badge>
            <Badge variant="secondary">{item.typeText}</Badge>
          </div>

          <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-slate-500">
            <span className="inline-flex max-w-[360px] items-center gap-1 truncate">
              <GitBranch className="h-3.5 w-3.5 text-slate-400" />
              {item.repoUrl || "本地运行时"}
            </span>
            <span>分支 {item.branch || "-"}</span>
            {item.subDir && <span>子目录 /{item.subDir}</span>}
            {item.gpu && (
              <span className="inline-flex items-center gap-1">
                <Cpu className="h-3.5 w-3.5 text-slate-400" />
                {item.gpu}
              </span>
            )}
            {item.healthPath && (
              <span className="inline-flex items-center gap-1">
                <Activity className="h-3.5 w-3.5 text-slate-400" />
                {item.healthPath} · {item.timeoutSec}s
              </span>
            )}
            <span>创建于 {formatTime(item.createdAt)}</span>
          </div>

          {item.message && <p className="text-xs text-red-500">{item.message}</p>}
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <Button size="sm" variant={active ? "outline" : "default"} disabled={busy} onClick={onToggle}>
            {busy ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : active ? (
              <Square className="h-4 w-4" />
            ) : (
              <Play className="h-4 w-4" />
            )}
            {active ? "停止" : "启动"}
          </Button>
          <Button size="sm" variant="outline" disabled={busy} onClick={onRestart}>
            <RefreshCw className="h-4 w-4" />
            重启
          </Button>
          <Button size="sm" variant="outline" disabled={busy} onClick={onEdit}>
            <Pencil className="h-4 w-4" />
            编辑
          </Button>
          <Button
            size="sm"
            variant="ghost"
            className="text-red-600 hover:bg-red-50 hover:text-red-700"
            disabled={busy}
            onClick={onDelete}
          >
            <Trash2 className="h-4 w-4" />
            删除
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}

// 部署教程 / Webhook 说明
function DocDialog({ type, onClose }: { type: "tutorial" | "webhook" | null; onClose: () => void }) {
  return (
    <Dialog open={type !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-h-[80vh] max-w-lg overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{type === "webhook" ? "Webhook 说明" : "部署教程"}</DialogTitle>
          <DialogDescription>
            {type === "webhook"
              ? "push 自动部署的触发条件与配置方式"
              : "从 Git 仓库创建部署实例的完整流程"}
          </DialogDescription>
        </DialogHeader>

        {type === "webhook" ? (
          <div className="space-y-3 text-sm leading-6 text-slate-600">
            <p>1. 在创建部署时开启「push 自动部署」开关。</p>
            <p>2. 由管理员在仓库（Gitee / GitHub）的 Webhook 设置中配置平台回调地址，并选择推送事件。</p>
            <p>3. 开启后，向部署配置的分支推送代码会自动触发重新构建与部署。</p>
            <p>4. 仅在部署配置中切换分支不会自动部署，需要重新保存或手动重启实例。</p>
            <p className="text-xs text-slate-400">
              私有仓库还需在仓库设置中填写对应的私有库 Token，公开仓库可留空。
            </p>
          </div>
        ) : (
          <div className="space-y-3 text-sm leading-6 text-slate-600">
            <p>1. 点击「创建部署」，来源选择「绑仓库」，填写 Git 仓库地址（支持 Gitee / GitHub）。</p>
            <p>2. 名称留空时自动使用仓库名称；子目录用于代码不在仓库根目录的场景。</p>
            <p>3. 选择部署类型：常驻服务会持续运行并在异常时自动重启；一次性脚本执行完成即退出。</p>
            <p>4. 推理服务请选择调度类型「推理服务」并按需绑定 GPU；纯 API 转发服务可不绑定。</p>
            <p>5. 健康检查路径可选：平台定时 GET 该地址，返回 200 判定健康，留空跳过探测。</p>
            <p>6. 提交后平台拉取仓库代码、构建并启动进程，生成部署实例。</p>
            <p>7. 实例运行后可在顶部菜单查看日志、监控与子域名；状态为 fatal 时查看日志并重新启动。</p>
          </div>
        )}

        <DialogFooter>
          <Button onClick={onClose}>我知道了</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
