import { useEffect, useMemo, useRef, useState } from "react"
import * as echarts from "echarts"
import {
  ArrowDown,
  ArrowUp,
  ArrowUpDown,
  Cpu,
  Database,
  FolderOpen,
  Gauge,
  Loader2,
  MemoryStick,
  Monitor,
  Zap,
  type LucideIcon,
} from "lucide-react"
import { toast } from "sonner"
import { usageApi, type UsageCard, type UsageData, type UsagePoint, type UsageRange } from "@/api/usage"
import { cn } from "@/lib/utils"

// 时间范围切换：默认 7 天
const RANGES: { value: UsageRange; label: string }[] = [
  { value: "24h", label: "24小时" },
  { value: "7d", label: "7天" },
  { value: "30d", label: "30天" },
]

// 卡片图标与配色，按后端返回的 key 匹配
const CARD_STYLE: Record<string, { icon: LucideIcon; className: string }> = {
  cpu_used: { icon: Cpu, className: "bg-sky-50 text-sky-600" },
  cpu_requested: { icon: Cpu, className: "bg-indigo-50 text-indigo-600" },
  cpu_utilization: { icon: Gauge, className: "bg-emerald-50 text-emerald-600" },
  gpu_memory: { icon: Monitor, className: "bg-violet-50 text-violet-600" },
  memory: { icon: MemoryStick, className: "bg-amber-50 text-amber-600" },
  storage_dir: { icon: FolderOpen, className: "bg-teal-50 text-teal-600" },
  storage_bucket: { icon: Database, className: "bg-rose-50 text-rose-600" },
  gpu_card: { icon: Zap, className: "bg-orange-50 text-orange-600" },
}

// 明细表列定义
type SortKey = Exclude<keyof UsagePoint, "time">
const COLUMNS: { key: SortKey; label: string; numeric: boolean }[] = [
  { key: "ts", label: "时段", numeric: false },
  { key: "cpuUsed", label: "实测核时", numeric: true },
  { key: "cpuRequested", label: "申请核时", numeric: true },
  { key: "gpuMem", label: "GPU 显存 GiB·h", numeric: true },
  { key: "gpuCardHours", label: "算力卡时", numeric: true },
  { key: "memory", label: "内存 GiB·h", numeric: true },
  { key: "storageDir", label: "存储·目录", numeric: true },
  { key: "storageBucket", label: "存储·桶", numeric: true },
]

// 概览卡片数值展示：利用率保留 1 位，其余保留 2 位
function formatCardValue(card: UsageCard) {
  if (card.key === "cpu_utilization") return card.value.toFixed(1)
  return card.value.toFixed(2)
}

export function UsageTab({ podName }: { podName: string }) {
  const [range, setRange] = useState<UsageRange>("7d")
  const [data, setData] = useState<UsageData | null>(null)
  const [loading, setLoading] = useState(true)
  // 排序状态：默认按时间升序
  const [sort, setSort] = useState<{ key: SortKey; dir: "asc" | "desc" }>({ key: "ts", dir: "asc" })

  useEffect(() => {
    let active = true
    setLoading(true)
    usageApi
      .get(podName, range)
      .then((result) => {
        if (active) setData(result)
      })
      .catch((error) => {
        if (active) toast.error((error as Error).message)
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [podName, range])

  // 表格排序：数字列按数值、时间列按时间戳
  const rows = useMemo(() => {
    const list = [...(data?.points ?? [])]
    list.sort((a, b) => {
      const diff = Number(a[sort.key]) - Number(b[sort.key])
      return sort.dir === "asc" ? diff : -diff
    })
    return list
  }, [data, sort])

  const chartOption = useMemo<echarts.EChartsOption>(() => {
    const points = data?.points ?? []
    // 数据点过多时抽稀 X 轴标签，保证可读
    const labelInterval = points.length > 48 ? Math.ceil(points.length / 8) : 0
    return {
      animation: false,
      tooltip: {
        trigger: "axis",
        axisPointer: { type: "shadow" },
        valueFormatter: (value) => `${Number(value).toFixed(4)} 核时`,
      },
      legend: {
        bottom: 0,
        icon: "circle",
        itemWidth: 8,
        itemHeight: 8,
        textStyle: { color: "#64748b", fontSize: 12 },
      },
      grid: { left: 8, right: 16, top: 24, bottom: 40, containLabel: true },
      xAxis: {
        type: "category",
        data: points.map((item) => item.time),
        axisTick: { show: false },
        axisLine: { lineStyle: { color: "#e2e8f0" } },
        axisLabel: { color: "#94a3b8", fontSize: 10, interval: labelInterval },
      },
      yAxis: {
        type: "value",
        name: "核时",
        nameTextStyle: { color: "#94a3b8", fontSize: 10 },
        axisLabel: { color: "#94a3b8", fontSize: 10 },
        splitLine: { lineStyle: { color: "#f1f5f9" } },
      },
      series: [
        {
          name: "实测核时",
          type: "bar",
          data: points.map((item) => item.cpuUsed),
          itemStyle: { color: "#3b82f6" },
          barMaxWidth: 6,
          barGap: "20%",
        },
        {
          name: "申请核时",
          type: "bar",
          data: points.map((item) => item.cpuRequested),
          itemStyle: { color: "#f59e0b" },
          barMaxWidth: 6,
        },
      ],
    }
  }, [data])

  return (
    <div className="space-y-4">
      {/* 顶部：时间范围切换 */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <div className="text-sm font-medium text-slate-700">资源用量与计费</div>
          <div className="mt-0.5 text-xs text-slate-400">按小时聚合，末尾桶为进行中的数据</div>
        </div>
        <div className="flex items-center gap-2">
          {RANGES.map((item) => (
            <button
              key={item.value}
              type="button"
              onClick={() => setRange(item.value)}
              className={cn(
                "rounded-full px-3.5 py-1.5 text-xs font-medium transition-colors",
                range === item.value
                  ? "bg-sky-600 text-white shadow-sm"
                  : "bg-slate-100 text-slate-600 hover:bg-slate-200",
              )}
            >
              {item.label}
            </button>
          ))}
        </div>
      </div>

      {/* 概览卡片：两行四列 */}
      {loading || !data ? (
        <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
          {Array.from({ length: 8 }).map((_, index) => (
            <div key={index} className="h-[104px] animate-pulse rounded-xl border border-slate-200 bg-slate-50" />
          ))}
        </div>
      ) : (
        <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
          {data.cards.map((card) => (
            <Card key={card.key} card={card} />
          ))}
        </div>
      )}

      {/* 核时对比图 */}
      <div className="rounded-xl border border-slate-200 bg-white p-4">
        <div className="flex flex-wrap items-baseline justify-between gap-2">
          <div className="text-sm font-medium text-slate-800">核时：实测 vs 申请</div>
          <div className="text-xs text-slate-400">差值 = 占着不用的浪费</div>
        </div>
        <div className="relative mt-2" style={{ height: 280 }}>
          <ChartHost option={chartOption} />
          {loading && (
            <div className="absolute inset-0 flex items-center justify-center bg-white/60">
              <Loader2 className="h-5 w-5 animate-spin text-slate-400" />
            </div>
          )}
        </div>
      </div>

      {/* 明细表格 */}
      <div className="overflow-hidden rounded-xl border border-slate-200 bg-white">
        <div className="max-h-[440px] overflow-auto">
          <table className="w-full border-collapse text-sm">
            <thead className="sticky top-0 z-10 bg-slate-50">
              <tr>
                {COLUMNS.map((column) => {
                  const active = sort.key === column.key
                  const Icon = !active ? ArrowUpDown : sort.dir === "asc" ? ArrowUp : ArrowDown
                  return (
                    <th
                      key={column.key}
                      className={cn(
                        "whitespace-nowrap px-4 py-2.5 text-xs font-medium text-slate-500",
                        column.numeric ? "text-right" : "text-left",
                      )}
                    >
                      <button
                        type="button"
                        onClick={() =>
                          setSort((prev) =>
                            prev.key === column.key
                              ? { key: column.key, dir: prev.dir === "asc" ? "desc" : "asc" }
                              : { key: column.key, dir: "asc" },
                          )
                        }
                        className={cn(
                          "inline-flex items-center gap-1 transition-colors hover:text-slate-800",
                          active && "text-slate-800",
                        )}
                      >
                        {column.label}
                        <Icon className="h-3 w-3" />
                      </button>
                    </th>
                  )
                })}
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={row.ts} className="border-t border-slate-100 hover:bg-slate-50/60">
                  <td className="whitespace-nowrap px-4 py-2 text-slate-600">{row.time}</td>
                  <td className="px-4 py-2 text-right tabular-nums text-slate-700">{row.cpuUsed.toFixed(3)}</td>
                  <td className="px-4 py-2 text-right tabular-nums text-slate-700">{row.cpuRequested.toFixed(3)}</td>
                  <td className="px-4 py-2 text-right tabular-nums text-slate-700">{row.gpuMem.toFixed(3)}</td>
                  <td className="px-4 py-2 text-right tabular-nums text-slate-700">{row.gpuCardHours.toFixed(3)}</td>
                  <td className="px-4 py-2 text-right tabular-nums text-slate-700">{row.memory.toFixed(3)}</td>
                  <td className="px-4 py-2 text-right tabular-nums text-slate-700">{row.storageDir.toFixed(3)}</td>
                  <td className="px-4 py-2 text-right tabular-nums text-slate-700">{row.storageBucket.toFixed(3)}</td>
                </tr>
              ))}
              {rows.length === 0 && (
                <tr>
                  <td colSpan={COLUMNS.length} className="px-4 py-10 text-center text-sm text-slate-400">
                    暂无用量数据
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}

// Card 概览卡片：标题 + 数值 + 可选进度条
function Card({ card }: { card: UsageCard }) {
  const style = CARD_STYLE[card.key] ?? { icon: Cpu, className: "bg-slate-50 text-slate-500" }
  const Icon = style.icon
  return (
    <div className="rounded-xl border border-slate-200 bg-white p-4">
      <div className="flex items-center gap-2">
        <span className={cn("flex h-7 w-7 items-center justify-center rounded-lg", style.className)}>
          <Icon className="h-4 w-4" />
        </span>
        <span className="truncate text-xs text-slate-500">{card.title}</span>
      </div>
      <div className="mt-3 flex items-baseline gap-1">
        <span className="text-2xl font-semibold tabular-nums text-slate-900">{formatCardValue(card)}</span>
        <span className="text-xs text-slate-400">{card.unit}</span>
      </div>
      {card.utilization !== undefined ? (
        <div className="mt-2 h-1.5 w-full overflow-hidden rounded-full bg-slate-100">
          <div
            className="h-full rounded-full bg-emerald-500 transition-all"
            style={{ width: `${Math.min(Math.max(card.utilization, 1), 100)}%` }}
          />
        </div>
      ) : (
        <div className="mt-2 h-1.5" />
      )}
    </div>
  )
}

// ChartHost：初始化 ECharts、跟随容器尺寸变化并销毁
function ChartHost({ option }: { option: echarts.EChartsOption }) {
  const domRef = useRef<HTMLDivElement | null>(null)
  const chartRef = useRef<echarts.ECharts | null>(null)

  useEffect(() => {
    if (!domRef.current) return
    const chart = echarts.init(domRef.current)
    chartRef.current = chart
    const observer = new ResizeObserver(() => chart.resize())
    observer.observe(domRef.current)
    return () => {
      observer.disconnect()
      chart.dispose()
      chartRef.current = null
    }
  }, [])

  useEffect(() => {
    chartRef.current?.setOption(option, true)
  }, [option])

  return <div ref={domRef} className="h-full w-full" />
}
