import { useCallback, useEffect, useState } from "react"
import { Copy, Database, Eye, EyeOff, Loader2, Plus, RefreshCw, Trash2 } from "lucide-react"
import { toast } from "sonner"
import {
  middlewareApi,
  type CreateMiddlewarePayload,
  type MiddlewareStatus,
  type MiddlewareType,
  type MiddlewareView,
} from "@/api/middleware"
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
import { Skeleton } from "@/components/ui/skeleton"
import { formatTime } from "@/lib/format"

// 各类型中间件的默认值，与后端 middlewareRegistry 保持一致
const TYPE_META: Record<
  MiddlewareType,
  { label: string; version: string; storage: string; port: number; hasDatabase: boolean }
> = {
  mysql: { label: "MySQL", version: "8.0", storage: "5Gi", port: 3306, hasDatabase: true },
  minio: {
    label: "MinIO",
    version: "RELEASE.2024-06-13T22-53-53Z",
    storage: "10Gi",
    port: 9000,
    hasDatabase: false,
  },
}

// 状态徽章样式
const STATUS_STYLE: Record<
  MiddlewareStatus,
  { label: string; variant: "default" | "secondary" | "destructive" | "success" | "warning" }
> = {
  running: { label: "运行中", variant: "success" },
  creating: { label: "启动中", variant: "warning" },
  failed: { label: "失败", variant: "destructive" },
  notfound: { label: "NotFound", variant: "secondary" },
}

// 存储规格解析：支持 K8s Quantity 的常见后缀（5Gi / 1024Mi / 纯字节）
const UNIT_BYTES: Record<string, number> = {
  Ki: 1024,
  Mi: 1024 ** 2,
  Gi: 1024 ** 3,
  Ti: 1024 ** 4,
  K: 1e3,
  M: 1e6,
  G: 1e9,
  T: 1e12,
}

function parseStorage(value: string): number | null {
  const matched = /^([0-9]+(?:\.[0-9]+)?)\s*([A-Za-z]*)$/.exec(value.trim())
  if (!matched) return null
  const num = Number(matched[1])
  if (!Number.isFinite(num)) return null
  const unit = matched[2]
  if (!unit) return num
  const factor = UNIT_BYTES[unit]
  return factor ? num * factor : null
}

// 字节数格式化为易读规格，用于展示配额 / 已用 / 可用
function formatStorage(bytes: number): string {
  if (bytes <= 0) return "0"
  const gi = 1024 ** 3
  if (bytes >= gi) return `${Number((bytes / gi).toFixed(2))}Gi`
  const mi = 1024 ** 2
  return `${Math.max(1, Math.floor(bytes / mi))}Mi`
}

interface FormState {
  type: MiddlewareType
  name: string
  version: string
  storage: string
  database: string
}

// 切换类型时用该类型的默认值重置表单，避免残留上个类型的版本/容量
function defaultForm(type: MiddlewareType): FormState {
  const meta = TYPE_META[type]
  return {
    type,
    name: "",
    version: meta.version,
    storage: meta.storage,
    database: meta.hasDatabase ? "app" : "",
  }
}

// 连接 Tab：展示当前 Pod 名下的中间件连接信息，并支持一键创建 / 删除
// disk 为创建 Pod 时设置的磁盘配额，中间件数据盘累计不能超过该值（与后端校验一致）
export function ConnectionTab({ podName, disk }: { podName: string; disk?: string }) {
  const [items, setItems] = useState<MiddlewareView[]>([])
  const [loading, setLoading] = useState(true)

  const [formOpen, setFormOpen] = useState(false)
  const [form, setForm] = useState<FormState>(defaultForm("mysql"))
  const [submitting, setSubmitting] = useState(false)

  const [deleteTarget, setDeleteTarget] = useState<MiddlewareView | null>(null)
  const [keepPvc, setKeepPvc] = useState(true)
  const [deleting, setDeleting] = useState(false)

  // 密码默认打码，点击眼睛图标临时展示
  const [revealed, setRevealed] = useState<Record<number, boolean>>({})

  const meta = TYPE_META[form.type]

  // 存储配额：配额来自 Pod 磁盘规格，已用为当前中间件数据盘累计
  const quotaBytes = disk ? parseStorage(disk) : null
  const usedBytes = items.reduce((sum, item) => sum + (parseStorage(item.storage) ?? 0), 0)
  const remainingBytes = quotaBytes === null ? null : Math.max(0, quotaBytes - usedBytes)
  // 表单未填时按该类型默认容量估算，与后端缺省逻辑一致
  const requestedBytes = parseStorage(form.storage.trim() || meta.storage)
  const storageError = (() => {
    if (quotaBytes === null) return ""
    if (requestedBytes === null) return "存储规格格式不正确，例如 5Gi / 1024Mi"
    if (usedBytes + requestedBytes > quotaBytes) {
      return `超出可用存储：Pod 配额 ${formatStorage(quotaBytes)}，已用 ${formatStorage(usedBytes)}，可用 ${formatStorage(remainingBytes ?? 0)}`
    }
    return ""
  })()

  const load = useCallback(
    async (silent = false) => {
      if (!silent) setLoading(true)
      try {
        const result = await middlewareApi.list(podName)
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
    // 轮询保证「启动中 → 运行中 / 失败」实时刷新
    const timer = setInterval(() => load(true), 10000)
    return () => clearInterval(timer)
  }, [load])

  // 复制到剪贴板，失败时兜底提示
  async function copy(text: string, label: string) {
    try {
      await navigator.clipboard.writeText(text)
      toast.success(`${label}已复制`)
    } catch {
      toast.error("复制失败，请手动选择文本复制")
    }
  }

  // 不同中间件的连接串格式不同
  function connectionString(item: MiddlewareView) {
    if (item.type === "mysql") {
      return `mysql://${item.username}:${item.password}@${item.host}:${item.port}/${item.database}`
    }
    return `http://${item.username}:${item.password}@${item.host}:${item.port}`
  }

  async function handleCreate() {
    const name = form.name.trim()
    if (!name) {
      toast.error("请填写服务名")
      return
    }
    if (storageError) {
      toast.error(storageError)
      return
    }
    setSubmitting(true)
    try {
      const payload: CreateMiddlewarePayload = {
        type: form.type,
        name,
        version: form.version.trim(),
        storage: form.storage.trim(),
        database: form.database.trim(),
      }
      await middlewareApi.create(podName, payload)
      toast.success("中间件已提交创建，启动完成后即可连接")
      setFormOpen(false)
      setForm(defaultForm(form.type))
      await load(true)
    } catch (error) {
      toast.error((error as Error).message)
    } finally {
      setSubmitting(false)
    }
  }

  async function handleDelete() {
    if (!deleteTarget) return
    setDeleting(true)
    try {
      await middlewareApi.remove(podName, deleteTarget.name, keepPvc)
      toast.success(keepPvc ? "已删除，数据盘已保留" : "已删除，数据盘一并回收")
      setDeleteTarget(null)
      await load(true)
    } catch (error) {
      toast.error((error as Error).message)
    } finally {
      setDeleting(false)
    }
  }

  // 打开创建弹窗：默认容量超过剩余配额时自动收缩，避免一打开就超限
  function openCreateForm() {
    const next = defaultForm(form.type)
    const defaults = parseStorage(next.storage)
    if (
      remainingBytes !== null &&
      remainingBytes > 0 &&
      defaults !== null &&
      defaults > remainingBytes
    ) {
      next.storage = formatStorage(remainingBytes)
    }
    setForm(next)
    setFormOpen(true)
  }

  return (
    <div className="space-y-4">
      {/* 顶部说明与操作 */}
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-base font-semibold text-slate-900">中间件连接</h2>
          <p className="mt-1 text-sm text-slate-500">
            一键部署 MySQL / MinIO，自动生成随机密码；集群内通过
            <code className="mx-1 rounded bg-slate-100 px-1 font-mono text-xs text-slate-600">
              服务名.命名空间.svc.cluster.local
            </code>
            访问。
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={() => load(true)} title="刷新">
            <RefreshCw className="h-3.5 w-3.5" />
            刷新
          </Button>
          <Button size="sm" onClick={openCreateForm}>
            <Plus className="h-3.5 w-3.5" />
            创建中间件
          </Button>
        </div>
      </div>

      {loading && items.length === 0 ? (
        <div className="grid gap-4 md:grid-cols-2">
          <Skeleton className="h-56 w-full" />
          <Skeleton className="h-56 w-full" />
        </div>
      ) : items.length === 0 ? (
        <div className="rounded-xl border border-dashed border-slate-200 bg-white p-12 text-center">
          <Database className="mx-auto h-8 w-8 text-slate-300" />
          <div className="mt-2 text-sm text-slate-600">该 Pod 还没有中间件</div>
          <div className="mt-1 text-xs text-slate-400">
            创建 MySQL / MinIO 后，服务名、端口、账号密码会显示在这里
          </div>
          <Button className="mt-4" size="sm" onClick={openCreateForm}>
            <Plus className="h-3.5 w-3.5" />
            创建中间件
          </Button>
        </div>
      ) : (
        <div className="grid gap-4 md:grid-cols-2">
          {items.map((item) => {
            const status = STATUS_STYLE[item.status] ?? STATUS_STYLE.creating
            const typeMeta = TYPE_META[item.type]
            return (
              <Card key={item.id}>
                <CardContent className="space-y-3 p-4">
                  {/* 标题：名称 + 状态 + 删除 */}
                  <div className="flex items-start justify-between gap-2">
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <Database className="h-4 w-4 shrink-0 text-slate-400" />
                        <span className="truncate font-medium text-slate-900">{item.name}</span>
                        <Badge variant={status.variant}>{status.label}</Badge>
                      </div>
                      <div className="mt-1 text-xs text-slate-400">
                        {typeMeta?.label ?? item.type} {item.version} · {item.storage} · 创建于{" "}
                        {formatTime(item.createdAt)}
                      </div>
                    </div>
                    <Button
                      variant="ghost"
                      size="icon"
                      title="删除中间件"
                      onClick={() => {
                        setDeleteTarget(item)
                        setKeepPvc(true)
                      }}
                    >
                      <Trash2 className="h-4 w-4 text-slate-400" />
                    </Button>
                  </div>

                  {/* 连接信息 */}
                  <div className="space-y-1.5 rounded-lg bg-slate-50 p-3 text-xs">
                    <div className="flex items-center justify-between gap-2">
                      <span className="text-slate-400">地址</span>
                      <span className="flex min-w-0 items-center gap-1.5 font-mono text-slate-700">
                        <span className="truncate">
                          {item.host}:{item.port}
                        </span>
                        <button
                          type="button"
                          title="复制地址"
                          onClick={() => copy(`${item.host}:${item.port}`, "地址")}
                        >
                          <Copy className="h-3 w-3 text-slate-400 hover:text-slate-600" />
                        </button>
                      </span>
                    </div>
                    <div className="flex items-center justify-between gap-2">
                      <span className="text-slate-400">用户名</span>
                      <span className="flex min-w-0 items-center gap-1.5 font-mono text-slate-700">
                        <span className="truncate">{item.username}</span>
                        <button
                          type="button"
                          title="复制用户名"
                          onClick={() => copy(item.username, "用户名")}
                        >
                          <Copy className="h-3 w-3 text-slate-400 hover:text-slate-600" />
                        </button>
                      </span>
                    </div>
                    <div className="flex items-center justify-between gap-2">
                      <span className="text-slate-400">密码</span>
                      <span className="flex min-w-0 items-center gap-1.5 font-mono text-slate-700">
                        <span className="truncate">
                          {revealed[item.id] ? item.password : "••••••••••••"}
                        </span>
                        <button
                          type="button"
                          title={revealed[item.id] ? "隐藏密码" : "显示密码"}
                          onClick={() =>
                            setRevealed((prev) => ({ ...prev, [item.id]: !prev[item.id] }))
                          }
                        >
                          {revealed[item.id] ? (
                            <EyeOff className="h-3 w-3 text-slate-400 hover:text-slate-600" />
                          ) : (
                            <Eye className="h-3 w-3 text-slate-400 hover:text-slate-600" />
                          )}
                        </button>
                        <button
                          type="button"
                          title="复制密码"
                          onClick={() => copy(item.password, "密码")}
                        >
                          <Copy className="h-3 w-3 text-slate-400 hover:text-slate-600" />
                        </button>
                      </span>
                    </div>
                    {item.database && (
                      <div className="flex items-center justify-between gap-2">
                        <span className="text-slate-400">数据库</span>
                        <span className="font-mono text-slate-700">{item.database}</span>
                      </div>
                    )}
                  </div>

                  <div className="flex flex-wrap items-center gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => copy(connectionString(item), "连接串")}
                    >
                      <Copy className="h-3.5 w-3.5" />
                      复制连接串
                    </Button>
                    {item.type === "mysql" && (
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() =>
                          copy(
                            `jdbc:mysql://${item.host}:${item.port}/${item.database}?useSSL=false&allowPublicKeyRetrieval=true`,
                            "JDBC 地址",
                          )
                        }
                      >
                        JDBC
                      </Button>
                    )}
                  </div>
                </CardContent>
              </Card>
            )
          })}
        </div>
      )}

      {/* 创建弹窗 */}
      <Dialog open={formOpen} onOpenChange={setFormOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>创建中间件</DialogTitle>
            <DialogDescription>
              自动创建数据盘、Secret、Deployment 与 ClusterIP Service，并生成随机密码
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-3">
            <div className="space-y-1.5">
              <Label>类型</Label>
              <Select
                value={form.type}
                onValueChange={(value) => setForm(defaultForm(value as MiddlewareType))}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="mysql">MySQL</SelectItem>
                  <SelectItem value="minio">MinIO</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-1.5">
              <Label>服务名</Label>
              <Input
                value={form.name}
                onChange={(event) => setForm({ ...form, name: event.target.value })}
                placeholder="例如 mysql-1"
              />
              <p className="text-xs text-slate-400">
                作为 Deployment / Service 名称，也是集群内域名前缀，需全局唯一
              </p>
            </div>

            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1.5">
                <Label>版本</Label>
                <Input
                  value={form.version}
                  onChange={(event) => setForm({ ...form, version: event.target.value })}
                  placeholder={meta.version}
                />
              </div>
              <div className="space-y-1.5">
                <Label>存储大小</Label>
                <Input
                  value={form.storage}
                  onChange={(event) => setForm({ ...form, storage: event.target.value })}
                  placeholder={meta.storage}
                />
                {quotaBytes !== null && (
                  <p
                    className={`text-xs ${storageError ? "text-red-500" : "text-slate-400"}`}
                  >
                    {storageError ||
                      `Pod 磁盘配额 ${formatStorage(quotaBytes)}，已用 ${formatStorage(usedBytes)}，可用 ${formatStorage(remainingBytes ?? 0)}`}
                  </p>
                )}
              </div>
            </div>

            {meta.hasDatabase && (
              <div className="space-y-1.5">
                <Label>初始库名</Label>
                <Input
                  value={form.database}
                  onChange={(event) => setForm({ ...form, database: event.target.value })}
                  placeholder="app"
                />
              </div>
            )}
          </div>

          <DialogFooter>
            <Button variant="outline" onClick={() => setFormOpen(false)}>
              取消
            </Button>
            <Button onClick={handleCreate} disabled={submitting || Boolean(storageError)}>
              {submitting && <Loader2 className="h-4 w-4 animate-spin" />}
              创建
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 删除确认：默认保留数据盘，避免误删数据 */}
      <Dialog open={deleteTarget !== null} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>删除中间件</DialogTitle>
            <DialogDescription>
              将删除工作负载、Service 与密码 Secret。数据盘默认保留，可用同名重新创建恢复数据。
            </DialogDescription>
          </DialogHeader>
          <div className="rounded-lg bg-slate-50 p-3 font-mono text-sm text-slate-700">
            {deleteTarget?.name}
          </div>
          <label className="flex items-center gap-2 text-sm text-slate-600">
            <input
              type="checkbox"
              className="h-4 w-4 rounded border-slate-300"
              checked={!keepPvc}
              onChange={(event) => setKeepPvc(!event.target.checked)}
            />
            同时删除数据盘（PVC），数据将无法恢复
          </label>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteTarget(null)}>
              取消
            </Button>
            <Button variant="destructive" onClick={handleDelete} disabled={deleting}>
              {deleting && <Loader2 className="h-4 w-4 animate-spin" />}
              确认删除
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
