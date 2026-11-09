import { useEffect, useRef, useState, type ReactNode } from "react"
import {
  Activity,
  ArrowUpRight,
  Clock,
  Cpu,
  HardDrive,
  MemoryStick,
  Network,
  Server,
} from "lucide-react"
import {
  CartesianGrid,
  Line,
  LineChart,
  ReferenceLine,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts"
import { toast } from "sonner"
import { hostApi, nodeHealthApi, type HostDetailData, type HostSample, type NodeHealthData } from "@/api/host"
import { Card } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { cn } from "@/lib/utils"

// 进度条颜色阈值：使用率 >= 该值显示红色，低于该值显示绿色。
// 内存、磁盘、Swap、显存等所有资源进度条统一使用此阈值。
const USAGE_THRESHOLD = 80

// 颜色常量：与截图中的深色折线、橙色参考线、蓝色下行、绿色上行保持一致
const COLORS = {
  dark: "#1f2937",
  orange: "#f59e0b",
  blue: "#3b82f6",
  green: "#10b981",
  purple: "#8b5cf6",
}

// 字节转 GiB
function formatGiB(bytes: number): string {
  if (!bytes || bytes <= 0) return "0 GB"
  return `${(bytes / 1024 / 1024 / 1024).toFixed(1)} GB`
}

// 速率格式化（B/s → KB/s / MB/s）
function formatRate(value: number): string {
  if (!value || value <= 0) return "0 B/s"
  if (value >= 1024 * 1024) return `${(value / 1024 / 1024).toFixed(1)} MB/s`
  if (value >= 1024) return `${(value / 1024).toFixed(1)} KB/s`
  return `${value.toFixed(0)} B/s`
}

// 数值保留一位小数
function formatNum(value: number, digits = 1): string {
  return Number.isFinite(value) ? value.toFixed(digits) : "0.0"
}

// 运行时间：d h m
function formatUptime(seconds: number): string {
  if (!seconds || seconds <= 0) return "0d 0h 0m"
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  return `${days}d ${hours}h ${minutes}m`
}

// X 轴时间刻度
function formatClock(ts: number): string {
  return new Date(ts * 1000).toLocaleTimeString("zh-CN", { hour12: false })
}

// 顶部四个指标卡片共用的外壳
function MetricCard({ label, children, hint }: { label: string; children: ReactNode; hint?: string }) {
  return (
    <Card className="p-5">
      <div className="text-sm text-slate-500">{label}</div>
      <div className="mt-2 text-2xl font-semibold tracking-tight text-slate-900">{children}</div>
      {hint && <div className="mt-1 text-xs text-slate-400">{hint}</div>}
    </Card>
  )
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

// 折线图卡片外壳：右上角固定标注「近120采样点」
function ChartCard({ title, extra, children }: { title: string; extra?: ReactNode; children: ReactNode }) {
  return (
    <Card className="p-5">
      <div className="flex items-start justify-between gap-3">
        <div className="text-sm font-medium text-slate-900">{title}</div>
        <div className="flex items-center gap-3">
          {extra}
          <span className="rounded-md bg-slate-100 px-2 py-0.5 text-[11px] text-slate-500">近120采样点</span>
        </div>
      </div>
      <div className="mt-4 h-[180px]">{children}</div>
    </Card>
  )
}

// 通用折线图：samples 为空时展示占位
function TrendChart({
  samples,
  lines,
  unit,
  yDomain,
  reference,
  formatValue,
}: {
  samples: HostSample[]
  lines: Array<{ key: keyof HostSample; color: string; name: string }>
  unit?: string
  yDomain?: [number, number]
  reference?: { y: number; color: string; label: string }
  formatValue?: (value: number) => string
}) {
  if (samples.length === 0) {
    return <div className="flex h-full items-center justify-center text-xs text-slate-400">暂无采样数据</div>
  }
  return (
    <ResponsiveContainer width="100%" height="100%">
      <LineChart data={samples} margin={{ top: 5, right: 8, bottom: 0, left: -12 }}>
        <CartesianGrid strokeDasharray="3 3" stroke="#94a3b8" vertical={false} />
        <XAxis dataKey="ts" tickFormatter={formatClock} tick={{ fontSize: 10, fill: "#94a3b8" }} minTickGap={40} axisLine={false} tickLine={false} />
        <YAxis
          domain={yDomain ?? [0, "auto"]}
          tick={{ fontSize: 10, fill: "#94a3b8" }}
          axisLine={false}
          tickLine={false}
          width={46}
          tickFormatter={(value: number) => (formatValue ? formatValue(value) : `${value}`)}
        />
        <Tooltip
          contentStyle={{ fontSize: 12, borderRadius: 8, borderColor: "#e2e8f0" }}
          labelFormatter={(label: number) => formatClock(label)}
          formatter={(value: number, name: string) => [formatValue ? formatValue(value) : `${value}${unit ?? ""}`, name]}
        />
        {reference && (
          <ReferenceLine
            y={reference.y}
            stroke={reference.color}
            strokeDasharray="4 4"
            label={{ value: reference.label, position: "insideTopRight", fontSize: 10, fill: reference.color }}
          />
        )}
        {lines.map((line) => (
          <Line
            key={String(line.key)}
            type="monotone"
            dataKey={String(line.key)}
            name={line.name}
            stroke={line.color}
            strokeWidth={1.8}
            dot={false}
            isAnimationActive={false}
          />
        ))}
      </LineChart>
    </ResponsiveContainer>
  )
}

// 压力标签：存在压力时红色高亮，否则灰色
function PressureTag({ label, active }: { label: string; active: boolean }) {
  return (
    <span
      className={cn(
        "rounded px-1.5 py-0.5 text-[11px] font-medium",
        active ? "bg-red-50 text-red-600" : "bg-slate-100 text-slate-400",
      )}
    >
      {label}
    </span>
  )
}

export default function HostHealth() {
  const [data, setData] = useState<HostDetailData | null>(null)
  // 负载单独从 /api/node/health 获取，按需求每 10 秒刷新一次
  const [load, setLoad] = useState<NodeHealthData | null>(null)
  const [loading, setLoading] = useState(true)
  const failedRef = useRef(false)

  useEffect(() => {
    let alive = true

    async function load() {
      try {
        const result = await hostApi.detail("self")
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
    // 每 3 秒轮询，刷新实时指标与时序图表
    const timer = setInterval(load, 3000)
    return () => {
      alive = false
      clearInterval(timer)
    }
  }, [])

  // 负载采集：/proc/loadavg 变化较慢，按需求 10 秒刷新一次
  useEffect(() => {
    let alive = true

    async function loadHealth() {
      try {
        const result = await nodeHealthApi.detail()
        if (alive) setLoad(result)
      } catch {
        // 负载卡片失败时保留上一次数值，不重复弹错误提示
      }
    }

    loadHealth()
    const timer = setInterval(loadHealth, 10000)
    return () => {
      alive = false
      clearInterval(timer)
    }
  }, [])

  const node = data?.node
  const realtime = data?.realtime
  const samples = data?.series.samples ?? []
  const maxPoints = data?.series.maxPoints ?? 120
  const partitions = data?.partitions ?? []
  const nodes = data?.nodes ?? []
  const gpu = data?.gpu

  // 指标采集不可用时（例如目标节点不是后端所在节点）给出提示
  const collectUnavailable = data !== null && !realtime?.available

  return (
    <div className="mx-auto max-w-7xl space-y-5">
      {/* 1. 页面标题栏 */}
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight text-slate-900">主机健康</h1>
          <p className="mt-1 text-sm text-slate-500">集群节点状态与资源使用</p>
        </div>
        <a
          href="#"
          onClick={(event) => event.preventDefault()}
          className="inline-flex items-center gap-1 rounded-lg border border-slate-200 bg-white px-3 py-1.5 text-sm text-slate-600 transition-colors hover:text-blue-600"
        >
          主机监控文档
          <ArrowUpRight className="h-3.5 w-3.5" />
        </a>
      </div>

      {collectUnavailable && (
        <div className="rounded-lg border border-amber-200 bg-amber-50 px-4 py-2 text-xs text-amber-700">
          当前节点不是后端所在节点，仅展示 Kubernetes Node 信息；主机指标需在每个节点部署采集器后获取。
        </div>
      )}

      {/* 2. 第一行 4 个指标卡片 */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        {loading && !data ? (
          Array.from({ length: 4 }).map((_, index) => (
            <Card key={index} className="p-5">
              <Skeleton className="h-4 w-20" />
              <Skeleton className="mt-3 h-7 w-16" />
            </Card>
          ))
        ) : (
          <>
            <MetricCard
              label="负载(1m)"
              hint={`5min ${formatNum(load?.load5 ?? 0, 2)} · 15min ${formatNum(load?.load15 ?? 0, 2)}`}
            >
              {formatNum(load?.load1 ?? 0, 2)}
            </MetricCard>
            <MetricCard
              label="内存"
              hint={
                realtime?.memoryTotalBytes
                  ? `${formatGiB(realtime.memoryUsedBytes)} / ${formatGiB(realtime.memoryTotalBytes)}`
                  : "数据源不可用"
              }
            >
              <div className="space-y-2">
                <div className="flex items-center gap-2">
                  <MemoryStick className="h-4 w-4 text-emerald-500" />
                  {`${formatNum(realtime?.memoryPercent ?? 0)}%`}
                </div>
                <UsageBar value={realtime?.memoryPercent ?? 0} />
              </div>
            </MetricCard>
            <MetricCard
              label="磁盘 /"
              hint={
                realtime?.diskTotalBytes
                  ? `${formatGiB(realtime.diskUsedBytes)} / ${formatGiB(realtime.diskTotalBytes)}`
                  : "数据源不可用"
              }
            >
              <div className="space-y-2">
                <div className="flex items-center gap-2">
                  <HardDrive className="h-4 w-4 text-emerald-500" />
                  {`${formatNum(realtime?.diskPercent ?? 0)}%`}
                </div>
                <UsageBar value={realtime?.diskPercent ?? 0} />
              </div>
            </MetricCard>
            <MetricCard label="运行时间" hint="d h m">
              <div className="flex items-center gap-2">
                <Clock className="h-4 w-4 text-slate-400" />
                {formatUptime(node?.uptimeSeconds ?? 0)}
              </div>
            </MetricCard>
          </>
        )}
      </div>

      {/* 3. 系统信息卡片 */}
      <Card className="p-5">
        <div className="flex items-center gap-2">
          <Server className="h-4 w-4 text-slate-400" />
          <h2 className="text-base font-semibold text-slate-900">系统信息</h2>
        </div>
        <div className="mt-4 grid grid-cols-1 gap-x-6 gap-y-4 sm:grid-cols-2 lg:grid-cols-4">
          <InfoField label="操作系统" value={node?.osImage || "--"} />
          <InfoField label="内核版本" value={node?.kernelVersion || "--"} />
          <InfoField label="主机名 / 架构" value={`${node?.hostname || "--"} / ${node?.architecture || "--"}`} />
          <InfoField label="CPU 核数 / Pod 数" value={`${node?.cpuCores ?? 0} 核 / ${node?.podCount ?? 0} Pod`} />
          <InfoField label="运行时间" value={formatUptime(node?.uptimeSeconds ?? 0)} />
          <InfoField
            label="负载均值"
            value={`${formatNum(node?.load1 ?? 0, 2)} / ${formatNum(node?.load5 ?? 0, 2)} / ${formatNum(node?.load15 ?? 0, 2)}`}
          />
          <InfoField
            label="Swap"
            value={`${formatGiB(node?.swapUsedBytes ?? 0)} / ${formatGiB(node?.swapTotalBytes ?? 0)}`}
          />
          <InfoField label="K3s 版本" value={node?.k3sVersion || "--"} mono />
        </div>
      </Card>

      {/* 4. 集群指标标题 */}
      <div className="flex items-center justify-between">
        <h2 className="text-base font-semibold text-slate-900">集群指标</h2>
        <a
          href="#"
          onClick={(event) => event.preventDefault()}
          className="text-sm font-medium text-blue-600 hover:text-blue-700"
        >
          AI分析集群资源
        </a>
      </div>

      {/* 5. 6 个折线图卡片，2 列网格 */}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <ChartCard title="主机 CPU">
          <TrendChart samples={samples} lines={[{ key: "cpuPercent", color: COLORS.dark, name: "CPU" }]} yDomain={[0, 100]} formatValue={(v) => `${v.toFixed(0)}%`} />
        </ChartCard>
        <ChartCard title="主机内存">
          <TrendChart
            samples={samples}
            lines={[{ key: "memoryPercent", color: COLORS.orange, name: "内存" }]}
            yDomain={[0, 100]}
            reference={{ y: 90, color: COLORS.orange, label: "上限 90%" }}
            formatValue={(v) => `${v.toFixed(0)}%`}
          />
        </ChartCard>
        <ChartCard
          title="网络吞吐"
          extra={
            <span className="text-[11px] text-slate-500">
              下行 <span className="text-blue-600">{formatRate(realtime?.netRxBps ?? 0)}</span>
              {" · "}上行 <span className="text-emerald-600">{formatRate(realtime?.netTxBps ?? 0)}</span>
            </span>
          }
        >
          <TrendChart
            samples={samples}
            lines={[
              { key: "netRxBps", color: COLORS.blue, name: "下行" },
              { key: "netTxBps", color: COLORS.green, name: "上行" },
            ]}
            formatValue={formatRate}
          />
        </ChartCard>
        <ChartCard title="磁盘 IO">
          <TrendChart
            samples={samples}
            lines={[
              { key: "diskReadBps", color: COLORS.blue, name: "读取" },
              { key: "diskWriteBps", color: COLORS.orange, name: "写入" },
            ]}
            formatValue={formatRate}
          />
        </ChartCard>
        <ChartCard title="收发数据包">
          <TrendChart
            samples={samples}
            lines={[
              { key: "pktRx", color: COLORS.blue, name: "接收包" },
              { key: "pktTx", color: COLORS.green, name: "发送包" },
            ]}
            formatValue={(v) => `${v.toFixed(0)} pps`}
          />
        </ChartCard>
        <ChartCard title="磁盘 IOPS">
          <TrendChart
            samples={samples}
            lines={[
              { key: "iopsRead", color: COLORS.blue, name: "读 IOPS" },
              { key: "iopsWrite", color: COLORS.purple, name: "写 IOPS" },
            ]}
            formatValue={(v) => `${v.toFixed(0)}`}
          />
        </ChartCard>
      </div>

      {/* 6. 异常与延迟卡片 */}
      <Card className="p-5">
        <div className="flex flex-wrap items-start justify-between gap-2">
          <div className="flex items-center gap-2">
            <Activity className="h-4 w-4 text-slate-400" />
            <h2 className="text-base font-semibold text-slate-900">异常与延迟</h2>
          </div>
          <span className="text-[11px] text-slate-400">丢包按网卡 drop 计数估算</span>
        </div>
        <div className="mt-4 grid grid-cols-2 gap-4 lg:grid-cols-4">
          <AnomalyItem label="接收丢包率" value={`${formatNum(realtime?.rxDropPercent ?? 0, 2)}%`} danger={(realtime?.rxDropPercent ?? 0) > 0.1} />
          <AnomalyItem label="发送丢包率" value={`${formatNum(realtime?.txDropPercent ?? 0, 2)}%`} danger={(realtime?.txDropPercent ?? 0) > 0.1} />
          <AnomalyItem label="磁盘 await" value={`${formatNum(realtime?.diskAwaitMs ?? 0, 2)} ms`} danger={(realtime?.diskAwaitMs ?? 0) > 20} />
          <AnomalyItem label="磁盘繁忙" value={`${formatNum(realtime?.diskBusyPercent ?? 0)}%`} danger={(realtime?.diskBusyPercent ?? 0) > 80} />
        </div>
      </Card>

      {/* 7. 磁盘使用卡片 */}
      <Card className="p-5">
        <div className="flex items-center gap-2">
          <HardDrive className="h-4 w-4 text-slate-400" />
          <h2 className="text-base font-semibold text-slate-900">磁盘使用</h2>
        </div>
        <div className="mt-4 space-y-4">
          {partitions.length === 0 ? (
            <div className="py-6 text-center text-xs text-slate-400">暂未采集到磁盘分区信息</div>
          ) : (
            partitions.map((partition) => (
              <div key={partition.mount}>
                <div className="flex items-center justify-between text-sm">
                  <span className="font-medium text-slate-700">{partition.mount}</span>
                  <span className="text-slate-500">
                    {formatGiB(partition.usedBytes)} / {formatGiB(partition.totalBytes)}
                    <span className="ml-3 inline-block w-12 text-right font-medium text-slate-900">
                      {formatNum(partition.percent)}%
                    </span>
                  </span>
                </div>
                <div className="mt-2">
                  <UsageBar value={partition.percent} />
                </div>
              </div>
            ))
          )}
        </div>
      </Card>

      {/* 8. K3s 节点列表卡片 */}
      <Card className="p-5">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Cpu className="h-4 w-4 text-slate-400" />
            <h2 className="text-base font-semibold text-slate-900">K3s 节点列表</h2>
          </div>
          <span className="text-xs text-slate-400">节点总数 {nodes.length}</span>
        </div>
        <div className="mt-2 divide-y divide-slate-100">
          {nodes.map((item) => (
            <div key={item.name} className="flex flex-wrap items-center justify-between gap-3 py-3">
              <div className="flex items-center gap-2">
                <span
                  className={cn("h-2 w-2 rounded-full", item.ready ? "bg-emerald-500" : "bg-red-500")}
                  title={item.ready ? "Ready" : "NotReady"}
                />
                <span className="text-sm font-medium text-slate-900">{item.name}</span>
              </div>
              <div className="flex items-center gap-1.5">
                <PressureTag label="DiskPressure" active={item.diskPressure} />
                <PressureTag label="MemoryPressure" active={item.memoryPressure} />
                <PressureTag label="PIDPressure" active={item.pidPressure} />
              </div>
              <span className="font-mono text-xs text-slate-500">{item.version}</span>
            </div>
          ))}
        </div>
      </Card>

      {/* 9. GPU 汇总卡片 */}
      <Card className="p-5">
        <div className="flex items-center gap-2">
          <Network className="h-4 w-4 text-slate-400" />
          <h2 className="text-base font-semibold text-slate-900">GPU 汇总</h2>
        </div>
        <div className="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div>
            <div className="text-xs text-slate-500">GPU 总数</div>
            <div className="mt-1 text-xl font-semibold text-slate-900">{gpu?.count ?? 0} 块</div>
          </div>
          <div>
            <div className="text-xs text-slate-500">驱动版本</div>
            <div className="mt-1 text-xl font-semibold text-slate-900">{gpu?.driverVersion || "--"}</div>
          </div>
        </div>
        <button type="button" className="mt-4 text-sm font-medium text-blue-600 hover:text-blue-700">
          查看每卡利用率、显存与按Pod归属
        </button>
      </Card>

      {/* 采样点上限提示，避免误解为固定 120 个点 */}
      <div className="pb-2 text-center text-[11px] text-slate-400">
        采样窗口上限 {maxPoints} 点，每 3 秒刷新一次
      </div>
    </div>
  )
}

// 系统信息中的单个字段
function InfoField({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div>
      <div className="text-xs text-slate-500">{label}</div>
      <div className={cn("mt-1 truncate text-sm font-medium text-slate-900", mono && "font-mono text-xs")} title={value}>
        {value}
      </div>
    </div>
  )
}

// 异常与延迟中的单个指标
function AnomalyItem({ label, value, danger }: { label: string; value: string; danger: boolean }) {
  return (
    <div>
      <div className="text-xs text-slate-500">{label}</div>
      <div className={cn("mt-1 text-lg font-semibold", danger ? "text-red-600" : "text-slate-900")}>{value}</div>
    </div>
  )
}
