import { useEffect, useRef, useState, type ReactNode } from "react"
import {
  ArrowUpRight,
  Bot,
  Cpu,
  HardDrive,
  MemoryStick,
  Plus,
  Server,
  Shield,
  Sparkles,
} from "lucide-react"
import { toast } from "sonner"
import { overviewApi, type GPUCard, type OverviewData } from "@/api/overview"
import { Card } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { cn } from "@/lib/utils"

// 资源进度条颜色阈值：使用率 >= 80% 红色，低于则绿色
const USAGE_THRESHOLD = 80

// 字节转 GiB，GPU 显存统一按 GB 展示
function formatGB(bytes: number): string {
  if (!bytes || bytes <= 0) return "0 GB"
  return `${(bytes / 1024 / 1024 / 1024).toFixed(1)} GB`
}

// 百分比文案，保留一位小数
function formatPercent(value: number): string {
  return `${Number.isFinite(value) ? value.toFixed(1) : "0.0"}%`
}

// 资源使用率进度条：低于阈值绿色，达到阈值红色
function UsageBar({ value }: { value: number }) {
  const percent = Math.min(100, Math.max(0, value))
  const danger = percent >= USAGE_THRESHOLD
  return (
    <div className="h-1.5 w-full overflow-hidden rounded-full bg-slate-100">
      <div
        className={cn(
          "h-full rounded-full transition-all duration-500",
          danger ? "bg-red-500" : "bg-emerald-500",
        )}
        style={{ width: `${percent}%` }}
      />
    </div>
  )
}

// 在线状态圆点
function Dot({ tone }: { tone: "green" | "red" }) {
  return (
    <span
      className={cn(
        "inline-block h-2 w-2 shrink-0 rounded-full",
        tone === "green" ? "bg-emerald-500" : "bg-red-500",
      )}
    />
  )
}

// 单个统计卡片
function StatCard({
  label,
  children,
  hint,
}: {
  label: string
  children: ReactNode
  hint?: string
}) {
  return (
    <Card className="p-5">
      <div className="text-sm text-slate-500">{label}</div>
      <div className="mt-2 text-2xl font-semibold tracking-tight text-slate-900">{children}</div>
      {hint && <div className="mt-1 text-xs text-slate-400">{hint}</div>}
    </Card>
  )
}

// 单张 GPU 卡片
function GPUCardItem({ gpu }: { gpu: GPUCard }) {
  const percent = gpu.memoryTotal > 0 ? (gpu.memoryUsed / gpu.memoryTotal) * 100 : 0
  return (
    <Card className="flex min-w-[240px] flex-1 flex-col p-5">
      <div className="flex items-start justify-between gap-2">
        <div>
          <div className="text-sm font-semibold text-slate-900">GPU{gpu.index}</div>
          <div className="mt-0.5 max-w-[500px] text-xs text-slate-400" title={gpu.name}>
            {gpu.name || "未知型号"}
          </div>
        </div>
        <div className="text-2xl font-semibold tracking-tight text-slate-900">
          {gpu.utilization}%
        </div>
      </div>

      <div className="mt-4 flex items-center justify-between text-xs text-slate-500">
        <span>显存</span>
        <span className="text-slate-600">
          {formatGB(gpu.memoryUsed)} / {formatGB(gpu.memoryTotal)}
        </span>
      </div>
      <div className="mt-2">
        <UsageBar value={percent} />
      </div>

      {/* 占位链接：第一阶段不做跳转 */}
      <button
        type="button"
        className="mt-4 text-left text-xs font-medium text-blue-600 hover:text-blue-700"
      >
        查看每卡利用率、显存、温度与按Pod归属
      </button>
    </Card>
  )
}

export default function Overview() {
  const [data, setData] = useState<OverviewData | null>(null)
  const [loading, setLoading] = useState(true)
  // 首次加载失败才提示，后续轮询失败静默处理
  const failedRef = useRef(false)

  useEffect(() => {
    let alive = true

    async function load() {
      try {
        const result = await overviewApi.get()
        if (!alive) return
        setData(result)
        failedRef.current = false
      } catch (error) {
        if (!alive) return
        if (!failedRef.current) {
          failedRef.current = true
          toast.error((error as Error).message)
        }
      } finally {
        if (alive) setLoading(false)
      }
    }

    load()
    // 每 3 秒刷新一次，保证概览数据实时
    const timer = setInterval(load, 3000)
    return () => {
      alive = false
      clearInterval(timer)
    }
  }, [])

  const pods = data?.pods
  const cpu = data?.cpu
  const memory = data?.memory
  const disk = data?.disk
  const gpu = data?.gpu
  const cluster = data?.cluster
  const modules = data?.modules

  return (
    <div className="mx-auto max-w-7xl space-y-5">
      {/* 顶部标题 */}
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight text-slate-900">概览</h1>
          <p className="mt-1 text-sm text-slate-500">集群运行状态</p>
        </div>
        <a
          href="#"
          onClick={(event) => event.preventDefault()}
          className="inline-flex items-center gap-1 rounded-lg border border-slate-200 bg-white px-3 py-1.5 text-sm text-slate-600 transition-colors hover:text-blue-600"
        >
          总览文档
          <ArrowUpRight className="h-3.5 w-3.5" />
        </a>
      </div>

      {/* 第一行统计卡片 */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-5">
        {loading && !data ? (
          Array.from({ length: 5 }).map((_, index) => (
            <Card key={index} className="p-5">
              <Skeleton className="h-4 w-20" />
              <Skeleton className="mt-3 h-7 w-16" />
            </Card>
          ))
        ) : (
          <>
            <StatCard label="运行 Pod" hint="运行 / 总数">
              {pods?.running ?? 0} / {pods?.total ?? 0}
            </StatCard>
            <StatCard
              label="CPU 负载(1m)"
              hint={cpu?.available ? `${cpu?.cores ?? 0} 核` : "数据源不可用"}
            >
              {cpu?.available ? (cpu?.load1m ?? 0).toFixed(2) : "--"}
            </StatCard>
            <StatCard
              label="内存使用"
              hint={
                memory?.available
                  ? `${formatGB(memory.usedBytes)} / ${formatGB(memory.totalBytes)}`
                  : "数据源不可用"
              }
            >
              <div className="space-y-2">
                <div className="flex items-center gap-2">
                  <MemoryStick className="h-4 w-4 text-emerald-500" />
                  {memory?.available ? formatPercent(memory.percent) : "--"}
                </div>
                <UsageBar value={memory?.percent ?? 0} />
              </div>
            </StatCard>
            <StatCard
              label="磁盘使用(/)"
              hint={
                disk?.available
                  ? `${formatGB(disk.usedBytes)} / ${formatGB(disk.totalBytes)}`
                  : "数据源不可用"
              }
            >
              <div className="space-y-2">
                <div className="flex items-center gap-2">
                  <HardDrive className="h-4 w-4 text-emerald-500" />
                  {disk?.available ? formatPercent(disk.percent) : "--"}
                </div>
                <UsageBar value={disk?.percent ?? 0} />
              </div>
            </StatCard>
            <StatCard label="GPU 总数量" hint="nvidia-smi">
              {gpu?.count ?? 0} 块
            </StatCard>
          </>
        )}
      </div>

      {/* 集群信息 */}
      <Card className="p-5">
        <div className="flex items-center gap-2">
          <Server className="h-4 w-4 text-slate-400" />
          <h2 className="text-base font-semibold text-slate-900">集群信息</h2>
        </div>
        <div className="mt-4 grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-5">
          <InfoItem label="运行 / 总计" value={`${cluster?.runningPods ?? 0} / ${cluster?.totalPods ?? 0}`} />
          <InfoItem label="已停止" value={String(cluster?.stoppedPods ?? 0)} />
          <InfoItem label="分组" value={String(cluster?.groups ?? 0)} />
          <InfoItem label="K3s 节点数量" value={String(cluster?.nodeCount ?? 0)} />
          <InfoItem label="K3s 版本" value={cluster?.version || "--"} mono />
        </div>
      </Card>

      {/* 模块卡片 */}
      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        <ModuleCard
          icon={<Server className="h-4 w-4 text-slate-500" />}
          title="基础设施"
          value={modules?.nodes ?? 0}
          unit="节点"
          tone={modules?.nodesOnline ? "green" : "red"}
          statusText={modules?.nodesOnline ? "在线" : "离线"}
        />
        <ModuleCard
          icon={<Bot className="h-4 w-4 text-slate-500" />}
          title="开发"
          value={modules?.agents ?? 0}
          unit="Agent"
          tone={modules?.agentsOnline ? "green" : "red"}
          statusText={modules?.agentsOnline ? "在线" : "离线"}
        />
        <ModuleCard
          icon={<Shield className="h-4 w-4 text-slate-500" />}
          title="攻防"
          value={modules?.attacks ?? 0}
          unit="攻击"
          extra={`封禁 ${modules?.bans ?? 0}`}
          tone="red"
          statusText="告警"
        />
      </div>

      {/* GPU 卡片组 */}
      <div className="space-y-3">
        <div className="flex items-center gap-2">
          <Cpu className="h-4 w-4 text-slate-400" />
          <h2 className="text-base font-semibold text-slate-900">GPU 资源</h2>
        </div>
        <div className="flex gap-4 overflow-x-auto pb-1">
          {gpu && gpu.items.length > 0 ? (
            gpu.items.map((item) => <GPUCardItem key={item.index} gpu={item} />)
          ) : (
            <Card className="flex min-h-[132px] w-full items-center justify-center p-5 text-sm text-slate-400">
              未检测到 GPU，请在后端所在节点安装 nvidia-smi 并确保存在可用显卡
            </Card>
          )}
        </div>
      </div>

      {/* 底部三个大操作按钮 */}
      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        <ActionButton icon={<Plus className="h-5 w-5" />} title="创建 Pod" subtitle="新建容器开发环境" />
        <ActionButton icon={<Sparkles className="h-5 w-5" />} title="AI 对话" subtitle="与助手交互" />
        <ActionButton icon={<Shield className="h-5 w-5" />} title="查看攻防" subtitle="实时威胁态势" />
      </div>
    </div>
  )
}

// 集群信息中的单个字段
function InfoItem({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div>
      <div className="text-xs text-slate-500">{label}</div>
      <div
        className={cn(
          "mt-1 truncate text-sm font-medium text-slate-900",
          mono && "font-mono text-xs",
        )}
        title={value}
      >
        {value}
      </div>
    </div>
  )
}

// 模块卡片：数字 + 状态圆点
function ModuleCard({
  icon,
  title,
  value,
  unit,
  extra,
  tone,
  statusText,
}: {
  icon: ReactNode
  title: string
  value: number
  unit: string
  extra?: string
  tone: "green" | "red"
  statusText: string
}) {
  return (
    <Card className="p-5">
      <div className="flex items-center gap-2">
        {icon}
        <span className="text-sm text-slate-500">{title}</span>
      </div>
      <div className="mt-2 flex items-baseline gap-2">
        <span className="text-2xl font-semibold tracking-tight text-slate-900">{value}</span>
        <span className="text-sm text-slate-500">{unit}</span>
        {extra && <span className="text-sm text-slate-400">· {extra}</span>}
      </div>
      <div className="mt-2 flex items-center gap-1.5 text-xs text-slate-500">
        <Dot tone={tone} />
        {statusText}
      </div>
    </Card>
  )
}

// 底部大操作按钮：仅做占位，点击无动作
function ActionButton({
  icon,
  title,
  subtitle,
  primary,
}: {
  icon: ReactNode
  title: string
  subtitle: string
  primary?: boolean
}) {
  return (
    <button
      type="button"
      className={cn(
        "flex items-center gap-4 rounded-xl border p-5 text-left transition-shadow hover:shadow-md",
        primary ? "border-blue-600 bg-blue-600 text-white" : "border-slate-200 bg-white text-slate-900",
      )}
    >
      <span
        className={cn(
          "flex h-10 w-10 shrink-0 items-center justify-center rounded-lg",
          primary ? "bg-white/15 text-white" : "bg-blue-50 text-blue-600",
        )}
      >
        {icon}
      </span>
      <span>
        <span className="block text-sm font-semibold">{title}</span>
        <span className={cn("mt-0.5 block text-xs", primary ? "text-blue-100" : "text-slate-500")}>
          {subtitle}
        </span>
      </span>
    </button>
  )
}
