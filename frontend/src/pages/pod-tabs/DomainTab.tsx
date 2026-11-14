import { useCallback, useEffect, useState } from "react"
import { Copy, Globe, Loader2, Plus, RefreshCw, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { domainApi, type CreateDomainPayload, type DomainMapping } from "@/api/domain"
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
import { Skeleton } from "@/components/ui/skeleton"
import { formatTime } from "@/lib/format"

// 与后端 prefixRe 保持一致：小写字母/数字开头结尾，中间可含中划线，长度 1-40
const PREFIX_RE = /^[a-z0-9]([-a-z0-9]{0,38}[a-z0-9])?$/
// 系统保留前缀，避免与平台/常见入口冲突
const RESERVED = new Set(["www", "api", "admin", "console", "traefik", "ssedeploy", "mail"])

// 校验前缀，返回错误文案；为空表示通过
function validatePrefix(prefix: string): string | null {
  if (!prefix) return "请填写子域名前缀"
  if (!PREFIX_RE.test(prefix)) return "仅允许小写字母、数字与中划线，且不能以中划线开头或结尾"
  if (RESERVED.has(prefix)) return "该前缀为系统保留，请更换"
  return null
}

// 子域名 Tab：为 Pod 配置 Ingress 域名映射，实时预览完整域名
export function DomainTab({ podName, defaultPort }: { podName: string; defaultPort?: number }) {
  const [items, setItems] = useState<DomainMapping[]>([])
  const [baseDomain, setBaseDomain] = useState("")
  const [loading, setLoading] = useState(true)

  const [dialogOpen, setDialogOpen] = useState(false)
  const [prefix, setPrefix] = useState("")
  const [port, setPort] = useState("")
  const [remark, setRemark] = useState("")
  const [submitting, setSubmitting] = useState(false)

  const [deleteTarget, setDeleteTarget] = useState<DomainMapping | null>(null)
  const [deleting, setDeleting] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const result = await domainApi.list(podName)
      setItems(result.items)
      setBaseDomain(result.baseDomain)
    } catch (error) {
      toast.error((error as Error).message)
    } finally {
      setLoading(false)
    }
  }, [podName])

  useEffect(() => {
    load()
  }, [load])

  // 打开弹窗时用 Pod 的外网端口作为默认后端端口
  function openDialog() {
    setPrefix("")
    setPort(defaultPort ? String(defaultPort) : "")
    setRemark("")
    setDialogOpen(true)
  }

  async function copy(text: string, label: string) {
    try {
      await navigator.clipboard.writeText(text)
      toast.success(`${label}已复制`)
    } catch {
      toast.error("复制失败，请手动选择文本复制")
    }
  }

  async function handleCreate() {
    const error = validatePrefix(prefix)
    if (error) {
      toast.error(error)
      return
    }
    const portNumber = Number(port)
    if (port && (!Number.isInteger(portNumber) || portNumber <= 0 || portNumber > 65535)) {
      toast.error("后端端口需为 1-65535 的整数")
      return
    }

    setSubmitting(true)
    try {
      const payload: CreateDomainPayload = {
        prefix,
        port: port ? portNumber : undefined,
        remark: remark.trim(),
      }
      const mapping = await domainApi.create(podName, payload)
      toast.success(`已开通 ${mapping.host}`)
      setDialogOpen(false)
      await load()
    } catch (err) {
      // 409 等冲突由后端返回明确文案，直接提示
      toast.error((err as Error).message)
    } finally {
      setSubmitting(false)
    }
  }

  async function handleDelete() {
    if (!deleteTarget) return
    setDeleting(true)
    try {
      await domainApi.remove(podName, deleteTarget.prefix)
      toast.success("已删除子域名映射")
      setDeleteTarget(null)
      await load()
    } catch (error) {
      toast.error((error as Error).message)
    } finally {
      setDeleting(false)
    }
  }

  const prefixError = prefix ? validatePrefix(prefix) : null
  const previewHost = `${prefix || "your-app"}.${baseDomain || "主域名"}`

  return (
    <div className="space-y-4">
      {/* 顶部说明与操作 */}
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-base font-semibold text-slate-900">子域名映射</h2>
          <p className="mt-1 text-sm text-slate-500">
            为 Pod 分配公网域名，系统自动创建 Ingress 并指向该 Pod 的 Service。
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={load} title="刷新">
            <RefreshCw className="h-3.5 w-3.5" />
            刷新
          </Button>
          <Button size="sm" onClick={openDialog}>
            <Plus className="h-3.5 w-3.5" />
            添加子域名
          </Button>
        </div>
      </div>

      {/* DNS 前置提示：泛解析未配置时域名无法访问 */}
      {baseDomain && (
        <div className="rounded-lg border border-sky-100 bg-sky-50 px-3 py-2 text-xs text-sky-700">
          需将 DNS 泛解析 <span className="font-mono">*.{baseDomain}</span> 指向集群入口 IP，
          域名解析生效后即可通过 Ingress 访问。
        </div>
      )}

      {loading && items.length === 0 ? (
        <Skeleton className="h-32 w-full" />
      ) : items.length === 0 ? (
        <div className="rounded-xl border border-dashed border-slate-200 bg-white p-12 text-center">
          <Globe className="mx-auto h-8 w-8 text-slate-300" />
          <div className="mt-2 text-sm text-slate-600">还没有子域名映射</div>
          <div className="mt-1 text-xs text-slate-400">
            添加后可通过 <span className="font-mono">自定义前缀.{baseDomain || "主域名"}</span> 访问该 Pod
          </div>
          <Button className="mt-4" size="sm" onClick={openDialog}>
            <Plus className="h-3.5 w-3.5" />
            添加子域名
          </Button>
        </div>
      ) : (
        <div className="space-y-3">
          {items.map((item) => (
            <Card key={item.ingressName}>
              <CardContent className="flex flex-wrap items-start justify-between gap-3 p-4">
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <Globe className="h-4 w-4 shrink-0 text-slate-400" />
                    <span className="truncate font-medium text-slate-900">{item.host}</span>
                    <button
                      type="button"
                      title="复制域名"
                      onClick={() => copy(`http://${item.host}`, "域名")}
                    >
                      <Copy className="h-3.5 w-3.5 text-slate-400 hover:text-slate-600" />
                    </button>
                  </div>
                  <div className="mt-1 text-xs text-slate-400">
                    后端 <span className="font-mono">{item.serviceName}:{item.port}</span>
                    {item.remark ? ` · ${item.remark}` : ""} · 创建于 {formatTime(item.createdAt)}
                  </div>
                </div>
                <Button
                  variant="ghost"
                  size="icon"
                  title="删除映射"
                  onClick={() => setDeleteTarget(item)}
                >
                  <Trash2 className="h-4 w-4 text-slate-400" />
                </Button>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      {/* 创建弹窗：前缀格式校验 + 完整域名实时预览 */}
      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>添加子域名</DialogTitle>
            <DialogDescription>
              自动创建 Ingress（IngressClass: k3s 默认 Traefik），域名冲突时会拒绝提交
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-3">
            <div className="space-y-1.5">
              <Label>子域名前缀</Label>
              <div className="flex items-center gap-2">
                <Input
                  value={prefix}
                  onChange={(event) => {
                    // 统一转小写并去掉空格，实时校验
                    setPrefix(event.target.value.trim().toLowerCase())
                  }}
                  placeholder="my-app"
                  aria-invalid={Boolean(prefixError)}
                />
                <span className="shrink-0 font-mono text-xs text-slate-400">
                  .{baseDomain || "主域名"}
                </span>
              </div>
              <p className={`text-xs ${prefixError ? "text-red-500" : "text-slate-400"}`}>
                {prefixError || "仅允许小写字母、数字与中划线，长度 1-40"}
              </p>
            </div>

            {/* 实时预览 */}
            <div className="rounded-lg bg-slate-50 p-3">
              <div className="text-xs text-slate-400">访问地址预览</div>
              <div className="mt-1 break-all font-mono text-sm text-slate-800">http://{previewHost}</div>
            </div>

            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1.5">
                <Label>后端端口</Label>
                <Input
                  value={port}
                  onChange={(event) => setPort(event.target.value.replace(/[^\d]/g, ""))}
                  placeholder={defaultPort ? String(defaultPort) : "5173"}
                />
                <p className="text-xs text-slate-400">留空则使用 Service 的第一个端口</p>
              </div>
              <div className="space-y-1.5">
                <Label>备注（可选）</Label>
                <Input
                  value={remark}
                  onChange={(event) => setRemark(event.target.value)}
                  placeholder="例如 前端测试环境"
                />
              </div>
            </div>
          </div>

          <DialogFooter>
            <Button variant="outline" onClick={() => setDialogOpen(false)}>
              取消
            </Button>
            <Button onClick={handleCreate} disabled={submitting || Boolean(prefixError)}>
              {submitting && <Loader2 className="h-4 w-4 animate-spin" />}
              创建映射
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 删除确认 */}
      <Dialog open={deleteTarget !== null} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>删除子域名映射</DialogTitle>
            <DialogDescription>将同时删除对应的 Ingress，域名会立即失效。</DialogDescription>
          </DialogHeader>
          <div className="rounded-lg bg-slate-50 p-3 font-mono text-sm text-slate-700">
            {deleteTarget?.host}
          </div>
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
