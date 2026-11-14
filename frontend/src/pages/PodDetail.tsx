import { useCallback, useEffect, useState } from "react"
import { useNavigate, useParams, useSearchParams } from "react-router-dom"
import { ArrowLeft, Code2, ExternalLink, Loader2, Play, RotateCw, Sparkles, Square } from "lucide-react"
import { toast } from "sonner"
import { podApi, type PodDetail as PodDetailType } from "@/api/pod"
import { Button } from "@/components/ui/button"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Skeleton } from "@/components/ui/skeleton"
import { StatusBadge } from "@/components/pod/StatusBadge"
import { FeatureDialog } from "@/components/common/FeatureDialog"
import { ComingSoon } from "@/components/pod/ComingSoon"
import { MonitorTab } from "./pod-tabs/MonitorTab"
import { TerminalTab } from "./pod-tabs/TerminalTab"
import { LogsTab } from "./pod-tabs/LogsTab"
import { AppLogsTab } from "./pod-tabs/AppLogsTab"
import { FilesTab } from "./pod-tabs/FilesTab"
import { SettingsTab } from "./pod-tabs/SettingsTab"
import { DeployTab } from "./pod-tabs/DeployTab"
import { ConnectionTab } from "./pod-tabs/ConnectionTab"
import { DomainTab } from "./pod-tabs/DomainTab"
import { CredentialsTab } from "./pod-tabs/CredentialsTab"
import { UsageTab } from "./pod-tabs/UsageTab"
import { formatTime } from "@/lib/format"

// 详情页 13 个 Tab，已实现监控 / 连接 / 终端 / 日志 / 部署 / 子域名 / 凭证 / 用量 / 设置
const TABS = [
  { value: "monitor", label: "监控" },
  { value: "connect", label: "连接" },
  { value: "terminal", label: "终端" },
  { value: "files", label: "文件" },
  { value: "logs", label: "日志" },
  { value: "applogs", label: "应用日志" },
  { value: "credentials", label: "凭证" },
  { value: "domain", label: "子域名" },
  { value: "deploy", label: "部署" },
  { value: "profile", label: "项目画像" },
  { value: "usage", label: "用量" },
  { value: "members", label: "成员" },
  { value: "settings", label: "设置" },
]

const READY_TABS = new Set(["monitor", "connect", "terminal", "files", "logs", "applogs", "deploy", "domain", "credentials", "usage", "settings"])

export default function PodDetail() {
  const { name = "" } = useParams()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()

  const [detail, setDetail] = useState<PodDetailType | null>(null)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [feature, setFeature] = useState<string | null>(null)
  // 加载失败原因：403 表示无权访问他人创建的 Pod
  const [loadError, setLoadError] = useState<{ status?: number; message: string } | null>(null)

  const tab = searchParams.get("tab") || "monitor"

  const load = useCallback(
    async (silent = false) => {
      if (!silent) setLoading(true)
      try {
        const result = await podApi.get(name)
        setDetail(result)
        setLoadError(null)
      } catch (error) {
        const err = error as Error & { status?: number }
        setLoadError({ status: err.status, message: err.message })
        if (!silent) toast.error(err.message)
      } finally {
        setLoading(false)
      }
    },
    [name],
  )

  useEffect(() => {
    load()
    // 轮询详情，保证状态与副本数实时更新
    const timer = setInterval(() => load(true), 15000)
    return () => clearInterval(timer)
  }, [load])

  async function handleToggle() {
    if (!detail) return
    setBusy(true)
    try {
      if (detail.status === "running") {
        await podApi.stop(detail.name)
        toast.success("Pod 已停止")
      } else {
        await podApi.start(detail.name)
        toast.success("Pod 已启动")
      }
      await load(true)
    } catch (error) {
      toast.error((error as Error).message)
    } finally {
      setBusy(false)
    }
  }

  async function handleRestart() {
    if (!detail) return
    setBusy(true)
    try {
      await podApi.restart(detail.name)
      toast.success("Pod 正在重启")
      await load(true)
    } catch (error) {
      toast.error((error as Error).message)
    } finally {
      setBusy(false)
    }
  }

  if (loading && !detail) {
    return (
      <div className="mx-auto max-w-7xl space-y-4">
        <Skeleton className="h-10 w-64" />
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-72 w-full" />
      </div>
    )
  }

  if (!detail) {
    // 普通用户访问他人创建的 Pod 时后端返回 403
    const forbidden = loadError?.status === 403
    return (
      <div className="mx-auto max-w-7xl rounded-xl border border-slate-200 bg-white p-16 text-center">
        <div className="text-base font-medium text-slate-700">{forbidden ? "无权访问该 Pod" : "未找到该 Pod"}</div>
        <div className="mt-1 text-sm text-slate-500">
          {forbidden ? "只有创建者本人或管理员可以查看和管理该 Pod" : "该 Pod 可能已被删除"}
        </div>
        <Button variant="outline" className="mt-5" onClick={() => navigate("/pods")}>
          返回 Pod 列表
        </Button>
      </div>
    )
  }

  return (
    <div className="mx-auto max-w-7xl space-y-4">
      {/* 头部：返回 + 名称 + 操作按钮 */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-3">
          <Button variant="ghost" size="icon" onClick={() => navigate("/pods")} title="返回">
            <ArrowLeft className="h-4 w-4" />
          </Button>
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <h1 className="truncate text-xl font-semibold text-slate-900">{detail.name}</h1>
              <StatusBadge status={detail.status} />
            </div>
            <div className="mt-0.5 flex flex-wrap items-center gap-x-3 text-xs text-slate-400">
              <span className="font-mono">{detail.spec}</span>
              <span>{detail.image}</span>
              <span>创建于 {formatTime(detail.createdAt)}</span>
              {detail.nodePort > 0 && (
                <a
                  href={`http://${window.location.hostname}:${detail.nodePort}`}
                  target="_blank"
                  rel="noreferrer"
                  title={`${detail.serviceName}：容器端口 ${detail.servicePort} → 节点端口 ${detail.nodePort}`}
                  className="inline-flex items-center gap-1 font-medium text-sky-600 hover:text-sky-700 hover:underline"
                >
                  <ExternalLink className="h-3 w-3" />
                  外部访问 :{detail.nodePort}
                </a>
              )}
            </div>
          </div>
        </div>

        <div className="flex items-center gap-2">
          <Button variant="outline" onClick={() => setFeature("IDE")}>
            <Code2 className="h-4 w-4" />
            IDE
          </Button>
          <Button
            variant={detail.status === "running" ? "outline" : "default"}
            disabled={busy || detail.status === "notfound"}
            onClick={handleToggle}
          >
            {busy ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : detail.status === "running" ? (
              <Square className="h-4 w-4" />
            ) : (
              <Play className="h-4 w-4" />
            )}
            {detail.status === "running" ? "停止" : "启动"}
          </Button>
          <Button
            variant="outline"
            disabled={busy || detail.status !== "running"}
            onClick={handleRestart}
          >
            <RotateCw className="h-4 w-4" />
            重启
          </Button>
          <Button variant="outline" onClick={() => setFeature("AI 助手")}>
            <Sparkles className="h-4 w-4" />
            AI 助手
          </Button>
        </div>
      </div>

      {/* Tab 导航 */}
      <Tabs
        value={tab}
        onValueChange={(value) => setSearchParams({ tab: value }, { replace: true })}
      >
        <div className="overflow-x-auto">
          <TabsList className="w-max min-w-full">
            {TABS.map((item) => (
              <TabsTrigger key={item.value} value={item.value}>
                {item.label}
              </TabsTrigger>
            ))}
          </TabsList>
        </div>
      </Tabs>

      {/* Tab 内容 */}
      <div>
        {tab === "monitor" && <MonitorTab name={detail.name} />}
        {tab === "connect" && <ConnectionTab podName={detail.name} disk={detail.disk} />}
        {tab === "terminal" && <TerminalTab name={detail.name} />}
        {tab === "files" && <FilesTab podName={detail.name} />}
        {tab === "logs" && <LogsTab name={detail.name} />}
        {tab === "applogs" && <AppLogsTab podName={detail.name} />}
        {tab === "deploy" && <DeployTab podName={detail.name} />}
        {tab === "domain" && <DomainTab podName={detail.name} defaultPort={detail.servicePort} />}
        {tab === "credentials" && <CredentialsTab podName={detail.name} />}
        {tab === "usage" && <UsageTab podName={detail.name} />}
        {tab === "settings" && (
          <SettingsTab
            key={detail.name}
            detail={detail}
            onUpdated={() => load(true)}
            onDeleted={() => navigate("/pods", { replace: true })}
          />
        )}
        {!READY_TABS.has(tab) && <ComingSoon title={TABS.find((item) => item.value === tab)?.label || "该模块"} />}
      </div>

      <FeatureDialog open={feature !== null} onOpenChange={(open) => !open && setFeature(null)} title={feature || ""} />
    </div>
  )
}
