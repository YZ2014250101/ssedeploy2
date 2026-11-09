/**
 * K3s 自动化部署平台 - 集群页（独立页面，自带左侧白色导航栏）
 *
 * 对接的 Go Gin 路由示例（统一响应 { code, msg, data }）：
 *   GET /api/cluster/nodes
 *   GET /api/cluster/metrics?range=15m|1h|6h|24h|7d
 *   GET /api/cluster/node/:nodeName/gpus
 *   GET /api/cluster/node/:nodeName/gpu-metrics?range=15m|1h|6h|24h|7d
 *
 * 后端可参考：
 *   cluster := r.Group("/api/cluster")
 *   cluster.Use(middleware.Auth(cfg.JWTSecret))
 *   cluster.GET("/nodes", h.ClusterNodes)
 *   cluster.GET("/metrics", h.ClusterMetrics)
 *   cluster.GET("/node/:nodeName/gpus", h.ClusterGPUs)
 *   cluster.GET("/node/:nodeName/gpu-metrics", h.ClusterGPUMetrics)
 */
import { useEffect, useRef, useState, type ReactNode } from "react"
import * as echarts from "echarts"
import {
  Box,
  Boxes,
  Code2,
  Cpu,
  HardDrive,
  LayoutDashboard,
  RefreshCw,
  Server,
  User,
} from "lucide-react"
import { toast } from "sonner"
import {
  clusterApi,
  type ClusterMetricsData,
  type ClusterNode,
  type ClusterRange,
  type GpuItem,
  type GpuMetricsData,
} from "@/api/cluster"
import { cn } from "@/lib/utils"

// 图表系列配色：前三个与设计稿一致（蓝 / 橙 / 绿）
const SERIES_COLORS = ["#4080ff", "#ff7d45", "#36c668", "#8b5cf6", "#06b6d4", "#f43f5e"]

// 时间粒度
const RANGES: ClusterRange[] = ["15m", "1h", "6h", "24h", "7d"]



// ECharts 折线图通用配置
function buildLineOption(
  timestamps: string[],
  series: Array<{ name: string; data: number[]; color: string }>,
  max: number,
): echarts.EChartsOption {
  const cleanData = (data: number[]) => {
    if (!data || data.length === 0) return data
    const copy = [...data] // 浅拷贝，防止修改原始数据源
    if (copy[copy.length - 1] === 0) {
    copy[copy.length - 1] = null as any // 将末尾的 0 转为 null，让 ECharts 断线
    }
    return copy
  }
  return {
    tooltip: { trigger: "axis", confine: true },
    legend: {
      top: 0,
      left: 0,
      type: "scroll",
      icon: "roundRect",
      itemWidth: 10,
      itemHeight: 3,
      textStyle: { fontSize: 11, color: "#64748b" },
    },
    grid: { left: 44, right: 16, top: 34, bottom: 24 },
    xAxis: {
      type: "category",
      boundaryGap: false,
      data: timestamps,
      axisLine: { lineStyle: { color: "#e2e8f0" } },
      axisTick: { show: false },
      axisLabel: { color: "#94a3b8", fontSize: 10, hideOverlap: true },
    },
    yAxis: {
      type: "value",
      min: 0,
      max,
      axisLabel: { color: "#94a3b8", fontSize: 10 },
      splitLine: { lineStyle: { color: "#94a3b8", type: "dashed" } },
    },
    series: series.map((item) => ({
      name: item.name,
      type: "line",
      smooth: true,
      showSymbol: false,
      data: cleanData(item.data), 
      lineStyle: { width: 2, color: item.color },
      itemStyle: { color: item.color },
    })),
  }
}

// 轻量 ECharts 封装：自动初始化、响应式 resize、销毁
function EChart({ option, empty, height = 250 }: { option: echarts.EChartsOption; empty?: string; height?: number }) {
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

  return (
    <div className="relative" style={{ height }}>
      <div ref={domRef} className="h-full w-full" />
      {empty && (
        <div className="pointer-events-none absolute inset-0 flex items-center justify-center bg-white/70 text-sm text-slate-400">
          {empty}
        </div>
      )}
    </div>
  )
}

// 图表卡片外壳
function ChartCard({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
      <div className="text-sm font-medium text-slate-900">{title}</div>
      <div className="mt-2">{children}</div>
    </div>
  )
}

export default function Cluster() {
  // 侧边栏仅 UI 切换
  const [activeMenu, setActiveMenu] = useState("cluster")

  const [nodes, setNodes] = useState<ClusterNode[]>([])
  const [selected, setSelected] = useState("")
  const [range, setRange] = useState<ClusterRange>("15m")
  const [clusterMetrics, setClusterMetrics] = useState<ClusterMetricsData | null>(null)
  const [gpus, setGpus] = useState<GpuItem[]>([])
  const [gpuMetrics, setGpuMetrics] = useState<GpuMetricsData | null>(null)
  const [activeGpu, setActiveGpu] = useState("all")
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [gpuLoading, setGpuLoading] = useState(false)

  // 拉取节点 GPU 列表 + GPU 时序
  async function loadGpu(nodeName: string, nextRange: ClusterRange) {
    if (!nodeName) {
      setGpus([])
      setGpuMetrics(null)
      return
    }
    setGpuLoading(true)
    try {
      const [gpuList, gpuSeries] = await Promise.all([
        clusterApi.gpus(nodeName),
        clusterApi.gpuMetrics(nodeName, nextRange),
      ])
      setGpus(gpuList)
      setGpuMetrics(gpuSeries)
      setActiveGpu("all")
    } catch (error) {
      toast.error((error as Error).message)
    } finally {
      setGpuLoading(false)
    }
  }

  // 初始化：节点列表 + 全局指标 + 默认节点 GPU 数据
  useEffect(() => {
    let alive = true

    async function init() {
      setLoading(true)
      try {
        const [nodeList, metrics] = await Promise.all([clusterApi.nodes(), clusterApi.metrics("15m")])
        if (!alive) return
        setNodes(nodeList)
        setClusterMetrics(metrics)
        const first = nodeList[0]?.nodeName ?? ""
        setSelected(first)
        if (first) {
          const [gpuList, gpuSeries] = await Promise.all([
            clusterApi.gpus(first),
            clusterApi.gpuMetrics(first, "15m"),
          ])
          if (!alive) return
          setGpus(gpuList)
          setGpuMetrics(gpuSeries)
        }
      } catch (error) {
        if (alive) toast.error((error as Error).message)
      } finally {
        if (alive) setLoading(false)
      }
    }

    init()
    return () => {
      alive = false
    }
  }, [])

  // 选择节点：切换选中态并拉取该节点 GPU 数据
  function selectNode(nodeName: string) {
    if (nodeName === selected) return
    setSelected(nodeName)
    setActiveGpu("all")
    void loadGpu(nodeName, range)
  }

  // 切换时间粒度：同时刷新集群指标与当前节点 GPU 指标
  async function changeRange(next: ClusterRange) {
    if (next === range) return
    setRange(next)
    setGpuLoading(true)
    try {
      const [metrics, gpuSeries] = await Promise.all([
        clusterApi.metrics(next),
        selected ? clusterApi.gpuMetrics(selected, next) : Promise.resolve(null),
      ])
      setClusterMetrics(metrics)
      if (gpuSeries) setGpuMetrics(gpuSeries)
    } catch (error) {
      toast.error((error as Error).message)
    } finally {
      setGpuLoading(false)
    }
  }

  // 刷新按钮：重新拉取全部接口
  async function refresh() {
    setRefreshing(true)
    try {
      const nodeList = await clusterApi.nodes()
      setNodes(nodeList)
      const target = nodeList.some((node) => node.nodeName === selected)
        ? selected
        : nodeList[0]?.nodeName ?? ""
      setSelected(target)

      const [metrics, gpuList, gpuSeries] = await Promise.all([
        clusterApi.metrics(range),
        target ? clusterApi.gpus(target) : Promise.resolve<GpuItem[]>([]),
        target ? clusterApi.gpuMetrics(target, range) : Promise.resolve<GpuMetricsData | null>(null),
      ])
      setClusterMetrics(metrics)
      setGpus(gpuList)
      setGpuMetrics(gpuSeries)
      toast.success("已刷新最新数据")
    } catch (error) {
      toast.error((error as Error).message)
    } finally {
      setRefreshing(false)
    }
  }

  // 集群三张图：系列按节点名动态生成
  const nodeNames = Object.keys(clusterMetrics?.cpu ?? {})
  const clusterTimestamps = clusterMetrics?.timestamps ?? []
  const clusterEmpty = loading ? "加载中..." : "暂无数据"

  const cpuOption = buildLineOption(
    clusterTimestamps,
    nodeNames.map((name, index) => ({
      name,
      data: clusterMetrics?.cpu[name] ?? [],
      color: SERIES_COLORS[index % SERIES_COLORS.length],
    })),
    80,
  )
  const memOption = buildLineOption(
    clusterTimestamps,
    nodeNames.map((name, index) => ({
      name,
      data: clusterMetrics?.mem[name] ?? [],
      color: SERIES_COLORS[index % SERIES_COLORS.length],
    })),
    80,
  )
  const loadOption = buildLineOption(
    clusterTimestamps,
    nodeNames.map((name, index) => ({
      name,
      data: clusterMetrics?.load[name] ?? [],
      color: SERIES_COLORS[index % SERIES_COLORS.length],
    })),
    20,
  )

  // GPU 三张图：颜色按 UUID 在节点 GPU 列表中的位置固定
  const gpuIds = Object.keys(gpuMetrics?.util ?? {})
  const shownGpuIds = activeGpu === "all" ? gpuIds : gpuIds.filter((uuid) => uuid === activeGpu)
  const gpuTimestamps = gpuMetrics?.timestamps ?? []
  const colorOfGpu = (uuid: string, index: number) => {
    const known = gpus.findIndex((item) => item.uuid === uuid)
    const colorIndex = known >= 0 ? known : index
    return SERIES_COLORS[colorIndex % SERIES_COLORS.length]
  }
  const gpuEmpty = gpuLoading ? "加载中..." : gpus.length === 0 ? "当前节点暂无 GPU 数据" : "暂无数据"

  const utilOption = buildLineOption(
    gpuTimestamps,
    shownGpuIds.map((uuid, index) => ({
      name: uuid,
      data: gpuMetrics?.util[uuid] ?? [],
      color: colorOfGpu(uuid, index),
    })),
    100,
  )
  const gpuMemOption = buildLineOption(
    gpuTimestamps,
    shownGpuIds.map((uuid, index) => ({
      name: uuid,
      data: gpuMetrics?.memUsed[uuid] ?? [],
      color: colorOfGpu(uuid, index),
    })),
    8,
  )
  const gpuTempOption = buildLineOption(
    gpuTimestamps,
    shownGpuIds.map((uuid, index) => ({
      name: uuid,
      data: gpuMetrics?.temp[uuid] ?? [],
      color: colorOfGpu(uuid, index),
    })),
    100,
  )

  return (
    <div className="mx-auto max-w-7xl space-y-5">
      {/* 右侧主内容区 */}
        {/* 顶部标题栏 */}
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="flex flex-wrap items-baseline gap-3">
            <h1 className="text-xl font-semibold tracking-tight">主机集群</h1>
            <span className="text-sm text-slate-500">{nodes.length}个节点</span>
            <a
              href="#"
              onClick={(event) => event.preventDefault()}
              className="text-sm text-blue-600 hover:underline"
            >
              集群文档
            </a>
          </div>
          <div className="flex items-center gap-2">
            <button
              type="button"
              className="rounded-lg border border-slate-200 bg-white px-3 py-1.5 text-sm text-slate-600 transition-colors hover:bg-slate-50"
            >
              表格
            </button>
            <button
              type="button"
              onClick={refresh}
              title="刷新数据"
              className="rounded-lg border border-slate-200 bg-white p-2 text-slate-600 transition-colors hover:bg-slate-50"
            >
              <RefreshCw className={cn("h-4 w-4", refreshing && "animate-spin")} />
            </button>
          </div>
        </div>

        {/* 第一行：节点卡片（动态 n 个，自动换行） */}
        <div className="mt-4 flex flex-wrap gap-3">
          {loading && nodes.length === 0
            ? Array.from({ length: 3 }).map((_, index) => (
                <div key={index} className="h-[92px] w-60 animate-pulse rounded-xl border border-slate-200 bg-white" />
              ))
            : nodes.map((node) => {
                const selectedNode = node.nodeName === selected
                return (
                  <button
                    key={node.nodeName}
                    type="button"
                    onClick={() => selectNode(node.nodeName)}
                    className={cn(
                      "w-60 rounded-xl border bg-white p-4 text-left shadow-sm transition-all duration-200 hover:-translate-y-0.5 hover:shadow-md",
                      selectedNode ? "border-blue-500 ring-1 ring-blue-500" : "border-slate-200",
                    )}
                  >
                    <div className="flex items-center justify-between gap-2">
                      <span className="truncate text-sm font-medium" title={node.nodeName}>
                        {node.nodeName}
                      </span>
                      <span
                        className={cn(
                          "h-2 w-2 shrink-0 rounded-full",
                          node.status === "online" ? "bg-emerald-500" : "bg-slate-300",
                        )}
                        title={node.status === "online" ? "在线" : "离线"}
                      />
                    </div>
                    <div className="mt-3 flex items-center justify-between text-xs text-slate-500">
                      <span>
                        CPU <span className="font-medium text-slate-700">{node.cpuPercent.toFixed(0)}%</span>
                      </span>
                      <span>
                        内存 <span className="font-medium text-slate-700">{node.memPercent.toFixed(0)}%</span>
                      </span>
                      <span>
                        负载 <span className="font-medium text-slate-700">{node.load.toFixed(2)}</span>
                      </span>
                    </div>
                  </button>
                )
              })}
          {!loading && nodes.length === 0 && (
            <div className="w-full rounded-xl border border-dashed border-slate-200 bg-white p-6 text-center text-sm text-slate-400">
              暂无节点数据
            </div>
          )}
        </div>

        {/* 第二行：集群 CPU / 内存 / 负载三张图 */}
        <div className="mt-4 grid grid-cols-1 gap-4 xl:grid-cols-3">
          <ChartCard title="CPU使用率">
            <EChart option={cpuOption} empty={nodeNames.length === 0 ? clusterEmpty : undefined} />
          </ChartCard>
          <ChartCard title="内存使用率">
            <EChart option={memOption} empty={nodeNames.length === 0 ? clusterEmpty : undefined} />
          </ChartCard>
          <ChartCard title="系统负载">
            <EChart option={loadOption} empty={nodeNames.length === 0 ? clusterEmpty : undefined} />
          </ChartCard>
        </div>

        {/* 第三部分：GPU 详情标题 + 时间粒度 */}
        <div className="mt-6 flex flex-wrap items-center justify-between gap-3">
          <h2 className="text-base font-semibold">{selected || "未选择节点"} GPU详情</h2>
          <div className="flex rounded-lg border border-slate-200 bg-white p-0.5">
            {RANGES.map((item) => (
              <button
                key={item}
                type="button"
                onClick={() => changeRange(item)}
                className={cn(
                  "rounded-md px-3 py-1 text-xs transition-colors",
                  range === item ? "bg-blue-600 font-medium text-white" : "text-slate-500 hover:text-slate-900",
                )}
              >
                {item}
              </button>
            ))}
          </div>
        </div>

        {/* GPU 标签栏：全部GPU + 动态 UUID */}
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <button
            type="button"
            onClick={() => setActiveGpu("all")}
            className={cn(
              "rounded-full px-3 py-1 text-xs transition-colors",
              activeGpu === "all" ? "bg-blue-600 text-white" : "bg-white text-slate-600 ring-1 ring-slate-200 hover:text-slate-900",
            )}
          >
            全部GPU
          </button>
          {gpus.map((gpu) => (
            <button
              key={gpu.uuid}
              type="button"
              title={gpu.uuid}
              onClick={() => setActiveGpu(gpu.uuid)}
              className={cn(
                "max-w-[220px] truncate rounded-full px-3 py-1 font-mono text-[11px] transition-colors",
                activeGpu === gpu.uuid
                  ? "bg-blue-600 text-white"
                  : "bg-white text-slate-600 ring-1 ring-slate-200 hover:text-slate-900",
              )}
            >
              {gpu.uuid}
            </button>
          ))}
        </div>

        {/* 第四行：GPU 利用率 / 显存 / 温度 */}
        <div className="mt-4 grid grid-cols-1 gap-4 xl:grid-cols-3">
          <ChartCard title="GPU利用率">
            <EChart option={utilOption} empty={shownGpuIds.length === 0 ? gpuEmpty : undefined} />
          </ChartCard>
          <ChartCard title="GPU显存使用">
            <EChart option={gpuMemOption} empty={shownGpuIds.length === 0 ? gpuEmpty : undefined} />
          </ChartCard>
          <ChartCard title="GPU温度">
            <EChart option={gpuTempOption} empty={shownGpuIds.length === 0 ? gpuEmpty : undefined} />
          </ChartCard>
        </div>

        <div className="mb-2 mt-6 text-center text-[11px] text-slate-400">数据每 15 秒采样一次</div>
    </div>
  )
}
