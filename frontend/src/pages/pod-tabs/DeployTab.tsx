import { useCallback, useEffect, useState } from "react"
import {
  Activity,
  Copy,
  Cpu,
  GitBranch,
  Loader2,
  Pencil,
  Play,
  Plus,
  RefreshCw,
  Square,
  Trash2,
  Webhook,
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
// 部署动作全部在当前 Pod 内完成：拉取代码到 /home/workspace/deployments/<name> 并执行 deploy.sh
export function DeployTab({ podName }: { podName: string }) {
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
  const [webhookTarget, setWebhookTarget] = useState<DeployView | null>(null)
  const [gpus, setGpus] = useState<GPUDeviceData[]>([])

  const load = useCallback(
    async (silent = false) => {
      if (!silent) setLoading(true)
      try {
        const result = await deployApi.list(podName)
        setItems(result.items)
      } catch (error) {
        if (!silent) toast.error((error as Error).message)
      } finally {
        setLoading(false)
      }
    },
    [podName],
  )

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
        await deployApi.update(podName, editing.name, {
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
        toast.success("部署配置已更新，实例正在重新拉取代码")
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
        await deployApi.create(podName, payload)
        toast.success("部署已创建，正在 Pod 内拉取仓库并执行 deploy.sh")
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
        await deployApi.stop(podName, item.name)
        toast.success("部署已停止")
      } else {
        await deployApi.start(podName, item.name)
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
      await deployApi.restart(podName, item.name)
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
      await deployApi.remove(podName, deleteTarget.name)
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
          <p className="mt-1 text-xs text-slate-400">
            在当前 Pod 内拉取仓库代码，并执行项目目录下的 deploy.sh
          </p>
        </div>
        <Button onClick={openCreate}>
          <Plus className="h-4 w-4" />
          创建部署
        </Button>
      </div>

      <div className="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-700">
        部署不会创建新的 Pod：平台会把仓库代码拉取到当前 Pod 的
        <span className="mx-1 font-mono">/home/workspace/deployments/&lt;名称&gt;</span>
        目录，并在该目录（或其子目录）执行 deploy.sh（依次尝试 deploy.sh、deploy/deploy.sh、start.sh）。
        常驻服务的脚本需在前台持续运行，脚本退出即视为停止。
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
              onWebhook={() => setWebhookTarget(item)}
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
              填写 Git 仓库与服务参数，提交后平台会在当前 Pod 内拉取代码并执行部署脚本。
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
              将停止部署进程「{deleteTarget?.name}」，并删除 Pod 内的代码目录
              /home/workspace/deployments/{deleteTarget?.name}，且不可恢复。
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

      {/* Webhook 回调配置 */}
      <WebhookDialog target={webhookTarget} onClose={() => setWebhookTarget(null)} />

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
  onWebhook,
}: {
  item: DeployView
  busy: boolean
  onToggle: () => void
  onRestart: () => void
  onEdit: () => void
  onDelete: () => void
  onWebhook: () => void
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
            <span className="font-mono">/home/workspace/deployments/{item.name}</span>
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
          {item.autoDeploy && (
            <Button size="sm" variant="outline" disabled={busy} onClick={onWebhook}>
              <Webhook className="h-4 w-4" />
              Webhook
            </Button>
          )}
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

// Webhook 回调配置弹窗：展示回调地址与密钥，复制到 Gitee / GitHub 仓库设置
function WebhookDialog({ target, onClose }: { target: DeployView | null; onClose: () => void }) {
  const visible = target !== null
  const url = target
    ? `${window.location.origin}${target.webhookPath}?token=${target.webhookToken}`
    : ""

  async function copy(text: string, label: string) {
    try {
      await navigator.clipboard.writeText(text)
      toast.success(`${label}已复制`)
    } catch {
      toast.error("复制失败，请手动选择文本复制")
    }
  }

  return (
    <Dialog open={visible} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle>Webhook 回调 · {target?.name}</DialogTitle>
          <DialogDescription>
            在 Git 仓库的 Webhook 设置中填入以下地址与密钥，推送代码后自动重新构建部署。
          </DialogDescription>
        </DialogHeader>

        {target && (
          <div className="space-y-4 text-sm">
            <div className="space-y-1.5">
              <Label>回调地址（Payload URL）</Label>
              <div className="flex items-center gap-2">
                <Input readOnly value={url} className="font-mono text-xs" />
                <Button variant="outline" size="icon" onClick={() => copy(url, "回调地址")}>
                  <Copy className="h-4 w-4" />
                </Button>
              </div>
              <p className="text-xs text-slate-400">地址中已携带密钥，请勿公开分享</p>
            </div>

            <div className="space-y-1.5">
              <Label>密钥（Secret / WebHook 密码）</Label>
              <div className="flex items-center gap-2">
                <Input
                  readOnly
                  value={target.webhookToken || "仅负责人 / 管理员可查看"}
                  className="font-mono text-xs"
                />
                <Button
                  variant="outline"
                  size="icon"
                  disabled={!target.webhookToken}
                  onClick={() => copy(target.webhookToken, "密钥")}
                >
                  <Copy className="h-4 w-4" />
                </Button>
              </div>
              <p className="text-xs text-slate-400">
                Gitee 填「WebHook 密码」；GitHub 填 Secret（平台按 HMAC-SHA256 校验签名）
              </p>
            </div>

            <div className="space-y-1 rounded-lg bg-slate-50 p-3 text-xs leading-5 text-slate-600">
              <p>1. 触发事件选择「Push」，内容类型为 application/json。</p>
              <p>2. 只有推送到分支 {target.branch || "main"} 才会触发自动部署。</p>
              <p>3. 平台需可被公网访问；本地部署请使用内网穿透工具映射 8081 端口。</p>
              <p>4. 保存后可在仓库的 Webhook 页面点击「测试」验证回调是否成功。</p>
            </div>
          </div>
        )}

        <DialogFooter>
          <Button onClick={onClose}>完成</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
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
            <p>2. 创建成功后，在部署卡片右侧点击「Webhook」，复制回调地址与密钥。</p>
            <p>3. 在 Gitee / GitHub 仓库的 Webhook 设置中新增回调：Payload URL 填回调地址，密码 / Secret 填密钥，触发事件选「Push」。</p>
            <p>4. 向部署配置的分支推送代码后，平台校验通过即自动重新构建部署；推送其他分支会被忽略。</p>
            <p>5. 仅在部署配置中切换分支不会自动部署，需要重新保存或手动重启实例。</p>
            <p className="text-xs text-slate-400">
              平台需可被公网访问（本地环境可用内网穿透）；私有仓库还需在仓库设置中填写对应的私有库 Token，公开仓库可留空。
            </p>
          </div>
        ) : (
          <div className="space-y-3 text-sm leading-6 text-slate-600">
            <p>1. 点击「创建部署」，来源选择「绑仓库」，填写 Git 仓库地址（支持 Gitee / GitHub）。</p>
            <p>2. 名称留空时自动使用仓库名称；子目录用于代码不在仓库根目录的场景。</p>
            <p>3. 提交后平台不会创建新 Pod，而是进入当前 Pod 拉取代码到 /home/workspace/deployments/&lt;名称&gt;。</p>
            <p>4. 随后在代码目录（或子目录）依次查找 deploy.sh、deploy/deploy.sh、start.sh 并执行；运行日志同步写入 /home/workspace/logs/&lt;名称&gt;.log，可在「应用日志」中直接查看（平台内部记录为 .ssedeploy/&lt;名称&gt;/deploy.log）。</p>
            <p>5. 常驻服务的脚本需在前台持续运行：进程存活即「运行中」，脚本退出即「已停止」，非 0 退出码为 fatal。</p>
            <p>6. 仓库为私有时填写访问 Token；容器内没有 git 时，平台会下载公开仓库源码归档后解压到工作区。</p>
            <p>7. 可在 Web 终端或文件树中直接查看 /home/workspace/deployments 下的代码与部署日志。</p>
          </div>
        )}

        <DialogFooter>
          <Button onClick={onClose}>我知道了</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
