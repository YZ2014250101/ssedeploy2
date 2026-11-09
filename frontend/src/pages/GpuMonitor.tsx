import { useEffect, useRef, useState, type ReactNode } from "react"
import { ArrowUpRight, Cpu, Thermometer, Zap } from "lucide-react"
import {
  CartesianGrid,
  Cell,
  Line,
  LineChart,
  Pie,
  PieChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts"
import { toast } from "sonner"
import { gpuApi, type GPUDeviceData, type GPUOverviewData, type GPUPodUsage, type GPUTrendPoint } from "@/api/gpu"
import { Card } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { cn } from "@/lib/utils"

// 进度条颜色阈值：使用率 >= 80% 显示红色，低于则绿色。
// 平均利用率、单卡利用率、显存等所有进度条统一使用该阈值。
const USAGE_THRESHOLD = 80

// 折线图配色：利用率用蓝色，显存用橙色
const COLORS = {
  util: "#3b82f6",
  mem: "#f59e0b",
}

// 环形饼图配色：按 Pod 依次取色，最后一项固定给「其他未归属」
const PIE_COLORS = ["#3b82f6", "#10b981", "#8b5cf6", "#f59e0b", "#ef4444", "#06b6d4"]

// 字节转 GiB
function formatGiB(bytes: number): string {
  if (!bytes || bytes <= 0) return "0 GB"
  return `${(bytes / 1024 / 1024 / 1024).toFixed(1)} GB`
}

// 数值保留一位小数
function formatNum(value: number, digits = 1): string {
  return Number.isFinite(value) ? value.toFixed(digits) : "0.0"
}

// X 轴时间刻度
function formatClock(ts: number): string {
  return new Date(ts * 1000).toLocaleTimeString("zh-CN", { hour12: false })
}

// 顶部统计卡片外壳
function StatCard({ label, children, hint }: { label: string; children: ReactNode; hint?: string }) {
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
        className={cn("h-full rounded-full transition-all duration-500", danger ? "bg-red-500" : "bg-emerald-500")}
        style={{ width: `${percent}%` }}
      />
    </div>
  )
}

// 单卡趋势折线图：直接消费后端 trend 数组
function MiniTrend({
  points,
  color,
  name,
  yDomain,
  formatValue,
}: {
  points: GPUTrendPoint[]
  color: string
  name: string
  yDomain?: [number, number | "auto"]
  formatValue?: (value: number) => string
}) {
  if (points.length === 0) {
    return <div className="flex h-full items-center justify-center text-xs text-slate-400">暂无采样数据</div>
  }
  return (
    <ResponsiveContainer width="100%" height="100%">
      <LineChart data={points} margin={{ top: 5, right: 8, bottom: 0, left: -14 }}>
        <CartesianGrid strokeDasharray="3 3" stroke="#94a3b8" vertical={false} />
        <XAxis
          dataKey="ts"
          tickFormatter={formatClock}
          tick={{ fontSize: 10, fill: "#94a3b8" }}
          minTickGap={50}
          axisLine={false}
          tickLine={false}
        />
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
          formatter={(value: number) => [formatValue ? formatValue(value) : `${value}`, name]}
        />
        <Line
          type="monotone"
          dataKey="value"
          name={name}
          stroke={color}
          strokeWidth={1.8}
          dot={false}
          isAnimationActive={false}
        />
      </LineChart>
    </ResponsiveContainer>
  )
}

// 环形饼图：该卡显存按 Pod 归属占比
function AllocPie({ pods, memUsedBytes }: { pods: GPUPodUsage[]; memUsedBytes: number }) {
  const podSum = pods.reduce((sum, pod) => sum + pod.memUsedBytes, 0)
  // 未归属显存 = 卡已用显存 - 已识别 Pod 占用
  const others = Math.max(0, memUsedBytes - podSum)

  const data = [
    ...pods.map((pod) => ({ name: pod.name, value: pod.memUsedBytes })),
    { name: "其他未归属", value: others },
  ].filter((item) => item.value > 0)

  if (data.length === 0) {
    return <div className="flex h-full items-center justify-center text-xs text-slate-400">暂无显存占用</div>
  }

  return (
    <ResponsiveContainer width="100%" height="100%">
      <PieChart>
        <Pie data={data} dataKey="value" nameKey="name" innerRadius={44} outerRadius={68} paddingAngle={2} stroke="none">
          {data.map((item, index) => (
            <Cell
              key={item.name}
              fill={item.name === "其他未归属" ? "#cbd5e1" : PIE_COLORS[index % PIE_COLORS.length]}
            />
          ))}
        </Pie>
        <Tooltip
          contentStyle={{ fontSize: 12, borderRadius: 8, borderColor: "#e2e8f0" }}
          formatter={(value: number, name: string) => [formatGiB(value), name]}
        />
      </PieChart>
    </ResponsiveContainer>
  )
}

// 单块 GPU 卡片
function GpuCard({
  device,
  utilPoints,
  memPoints,
  pods,
  maxPoints,
}: {
  device: GPUDeviceData
  utilPoints: GPUTrendPoint[]
  memPoints: GPUTrendPoint[]
  pods: GPUPodUsage[]
  maxPoints: number
}) {
  const memPercent = device.memTotalBytes > 0 ? (device.memUsedBytes / device.memTotalBytes) * 100 : 0

  return (
    <Card className="p-5">
      {/* 头部：编号 + 型号 + 温度 / 功耗 */}
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <div className="flex items-center gap-2">
            <Cpu className="h-4 w-4 text-slate-400" />
            <h3 className="text-base font-semibold text-slate-900">GPU #{device.id}</h3>
          </div>
          <div className="mt-1 text-xs text-slate-500">{device.name}</div>
        </div>
        <div className="flex items-center gap-4 text-xs text-slate-500">
          <span className="inline-flex items-center gap-1">
            <Thermometer className="h-3.5 w-3.5" />
            {device.temp} ℃
          </span>
          <span className="inline-flex items-center gap-1">
            <Zap className="h-3.5 w-3.5" />
            {formatNum(device.power)} W / {device.powerMax > 0 ? `${formatNum(device.powerMax)} W` : "--"}
          </span>
        </div>
      </div>

      {/* 第一行：利用率 + 显存进度条 */}
      <div className="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div>
          <div className="flex items-center justify-between text-xs">
            <span className="text-slate-500">利用率</span>
            <span className="font-medium text-slate-900">{formatNum(device.util)}%</span>
          </div>
          <div className="mt-2">
            <UsageBar value={device.util} />
          </div>
        </div>
        <div>
          <div className="flex items-center justify-between text-xs">
            <span className="text-slate-500">显存</span>
            <span className="font-medium text-slate-900">
              {formatGiB(device.memUsedBytes)} / {formatGiB(device.memTotalBytes)}
            </span>
          </div>
          <div className="mt-2">
            <UsageBar value={memPercent} />
          </div>
        </div>
      </div>

      {/* 第二行：利用率趋势 + 显存趋势 */}
      <div className="mt-5 grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div>
          <div className="flex items-center justify-between">
            <span className="text-xs font-medium text-slate-700">利用率趋势</span>
            <span className="rounded-md bg-slate-100 px-2 py-0.5 text-[11px] text-slate-500">近{maxPoints}采样点</span>
          </div>
          <div className="mt-2 h-[140px]">
            <MiniTrend points={utilPoints} color={COLORS.util} name="利用率" yDomain={[0, 100]} formatValue={(v) => `${v.toFixed(0)}%`} />
          </div>
        </div>
        <div>
          <div className="flex items-center justify-between">
            <span className="text-xs font-medium text-slate-700">显存趋势</span>
            <span className="rounded-md bg-slate-100 px-2 py-0.5 text-[11px] text-slate-500">近{maxPoints}采样点</span>
          </div>
          <div className="mt-2 h-[140px]">
            <MiniTrend points={memPoints} color={COLORS.mem} name="显存" formatValue={formatGiB} />
          </div>
        </div>
      </div>

      {/* 第三块：按 Pod 实际占用 */}
      <div className="mt-5">
        <div className="text-xs font-medium text-slate-700">按 Pod 实际占用</div>
        <div className="mt-3 grid grid-cols-1 gap-4 sm:grid-cols-[200px_1fr]">
          <div className="h-[160px]">
            <AllocPie pods={pods} memUsedBytes={device.memUsedBytes} />
          </div>
          <div className="min-w-0">
            <table className="w-full text-xs">
              <thead>
                <tr className="text-left text-slate-400">
                  <th className="pb-2 font-normal">Pod 名称</th>
                  <th className="pb-2 text-right font-normal">占用显存</th>
                  <th className="pb-2 text-right font-normal">占比</th>
                  <th className="pb-2 text-right font-normal">利用率</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-100">
                {pods.length === 0 ? (
                  <tr>
                    <td colSpan={4} className="py-6 text-center text-slate-400">
                      暂无 Pod 占用
                    </td>
                  </tr>
                ) : (
                  pods.map((pod) => (
                    <tr key={`${pod.namespace}/${pod.name}`} className="text-slate-700">
                      <td className="max-w-[150px] truncate py-2" title={`${pod.namespace}/${pod.name}`}>
                        {pod.name}
                      </td>
                      <td className="py-2 text-right">{formatGiB(pod.memUsedBytes)}</td>
                      <td className="py-2 text-right">{formatNum(pod.percent)}%</td>
                      <td className="py-2 text-right">{formatNum(pod.util)}%</td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        </div>
      </div>

      {/* 底部：已分配 Pod 标签 */}
      <div className="mt-4 flex flex-wrap items-center gap-2 border-t border-slate-100 pt-3">
        <span className="text-xs text-slate-400">已分配 Pod</span>
        {pods.length === 0 ? (
          <span className="text-xs text-slate-400">无</span>
        ) : (
          pods.map((pod) => (
            <span
              key={`${pod.namespace}/${pod.name}`}
              className="rounded-full bg-slate-100 px-2 py-0.5 text-[11px] text-slate-600"
              title={`${pod.namespace}/${pod.name}`}
            >
              {pod.name}
            </span>
          ))
        )}
      </div>
    </Card>
  )
}

export default function GpuMonitor() {
  const [data, setData] = useState<GPUOverviewData | null>(null)
  const [loading, setLoading] = useState(true)
  const failedRef = useRef(false)

  useEffect(() => {
    let alive = true

    async function load() {
      try {
        const result = await gpuApi.detail()
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

  const summary = data?.summary
  const devices = data?.gpuList ?? []
  const series = data?.trend.series ?? []
  const podAlloc = data?.podAlloc ?? []
  const maxPoints = data?.trend.maxPoints ?? 120

  // 总显存使用百分比
  const memPercent =
    summary && summary.memTotalBytes > 0 ? (summary.memUsedBytes / summary.memTotalBytes) * 100 : 0

  return (
    <div className="mx-auto max-w-7xl space-y-5">
      {/* 1. 页面标题栏：GPU监控｜驱动版本｜GPU文档链接 */}
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex flex-wrap items-baseline gap-3">
          <h1 className="text-2xl font-semibold tracking-tight text-slate-900">GPU监控</h1>
          <span className="text-sm text-slate-500">驱动版本 {summary?.driverVersion || "--"}</span>
        </div>
        <a
          href="#"
          onClick={(event) => event.preventDefault()}
          className="inline-flex items-center gap-1 rounded-lg border border-slate-200 bg-white px-3 py-1.5 text-sm text-slate-600 transition-colors hover:text-blue-600"
        >
          GPU 文档
          <ArrowUpRight className="h-3.5 w-3.5" />
        </a>
      </div>

      {/* 2. 顶部 4 个统计卡片 */}
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
            <StatCard label="GPU 总数" hint="nvidia-smi 采集">
              {summary?.count ?? 0} 块
            </StatCard>
            <StatCard label="平均利用率" hint={`阈值 ${USAGE_THRESHOLD}%`}>
              <div className="space-y-2">
                <div>{`${formatNum(summary?.avgUtil ?? 0)}%`}</div>
                <UsageBar value={summary?.avgUtil ?? 0} />
              </div>
            </StatCard>
            <StatCard
              label="总显存"
              hint={
                summary?.memTotalBytes
                  ? `${formatGiB(summary.memUsedBytes)} / ${formatGiB(summary.memTotalBytes)}`
                  : "数据源不可用"
              }
            >
              <div className="space-y-2">
                <div>{`${formatNum(memPercent)}%`}</div>
                <UsageBar value={memPercent} />
              </div>
            </StatCard>
            <StatCard label="最高温度" hint="全部 GPU 中的最大值">
              {summary?.maxTemp ?? 0} ℃
            </StatCard>
          </>
        )}
      </div>

      {/* 3. GPU 卡片网格：一行 2 张 */}
      {devices.length === 0 ? (
        <Card className="p-10 text-center text-sm text-slate-400">
          {loading ? "正在采集 GPU 数据..." : "未检测到 GPU，请确认节点已安装驱动并安装 metrics 组件"}
        </Card>
      ) : (
        <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
          {devices.map((device) => {
            const trend = series.find((item) => item.gpuId === device.id)
            const pods = podAlloc.find((item) => item.gpuId === device.id)?.pods ?? []
            return (
              <GpuCard
                key={device.id}
                device={device}
                utilPoints={trend?.util ?? []}
                memPoints={trend?.mem ?? []}
                pods={pods}
                maxPoints={maxPoints}
              />
            )
          })}
        </div>
      )}

      {/* 采样点上限提示 */}
      <div className="pb-2 text-center text-[11px] text-slate-400">
        采样窗口上限 {maxPoints} 点，每 3 秒刷新一次
      </div>
    </div>
  )
}
