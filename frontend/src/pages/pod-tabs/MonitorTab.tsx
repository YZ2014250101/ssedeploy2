import { useCallback, useEffect, useState } from "react"
import { AlertTriangle, Info, RefreshCw } from "lucide-react"
import {
  CartesianGrid,
  Legend,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts"
import { podApi, type PodEvent, type PodMetrics } from "@/api/pod"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { formatBytes, formatCPU, formatClock, formatTime, percent } from "@/lib/format"

// 监控 Tab：CPU/内存仪表盘 + 趋势折线图 + 最近事件
export function MonitorTab({ name }: { name: string }) {
  const [metrics, setMetrics] = useState<PodMetrics | null>(null)
  const [events, setEvents] = useState<PodEvent[]>([])
  const [error, setError] = useState("")

  const load = useCallback(async () => {
    try {
      const [metricResult, eventResult] = await Promise.all([
        podApi.metrics(name, 30),
        podApi.events(name),
      ])
      setMetrics(metricResult)
      setEvents(eventResult.items)
      setError("")
    } catch (err) {
      setError((err as Error).message)
    }
  }, [name])

  useEffect(() => {
    load()
    // 每 10 秒刷新一次实时指标
    const timer = setInterval(load, 10000)
    return () => clearInterval(timer)
  }, [load])

  const cpuPercent = percent(metrics?.cpuMilli, metrics?.cpuLimitMilli)
  const memPercent = percent(metrics?.memBytes, metrics?.memLimitBytes)

  // 折线图数据：统一换算成百分比，便于 CPU 与内存同轴对比
  const chartData = (metrics?.series ?? []).map((point) => ({
    time: formatClock(point.ts),
    cpu: metrics?.cpuLimitMilli ? Number(((point.cpuMilli / metrics.cpuLimitMilli) * 100).toFixed(2)) : 0,
    memory: metrics?.memLimitBytes ? Number(((point.memBytes / metrics.memLimitBytes) * 100).toFixed(2)) : 0,
  }))

  return (
    <div className="space-y-4">
      {error && (
        <div className="rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-700">{error}</div>
      )}

      {metrics && !metrics.available && (
        <div className="rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-700">
          metrics-server 未就绪，暂无实时指标数据（趋势图将在采样后自动展示）。
        </div>
      )}

      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader className="flex-row items-center justify-between space-y-0">
            <CardTitle>CPU 使用率</CardTitle>
            <Button variant="ghost" size="icon" title="刷新" onClick={load}>
              <RefreshCw className="h-4 w-4" />
            </Button>
          </CardHeader>
          <CardContent className="flex items-center justify-center pb-6">
            <Gauge
              value={cpuPercent}
              label="CPU"
              detail={`${formatCPU(metrics?.cpuMilli)} / ${formatCPU(metrics?.cpuLimitMilli)}`}
            />
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex-row items-center justify-between space-y-0">
            <CardTitle>内存使用率</CardTitle>
          </CardHeader>
          <CardContent className="flex items-center justify-center pb-6">
            <Gauge
              value={memPercent}
              label="内存"
              detail={`${formatBytes(metrics?.memBytes)} / ${formatBytes(metrics?.memLimitBytes)}`}
            />
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>资源趋势（最近 30 分钟）</CardTitle>
        </CardHeader>
        <CardContent>
          {chartData.length === 0 ? (
            <div className="flex h-56 items-center justify-center text-sm text-slate-400">
              暂无采样数据，后台每 15 秒采集一次
            </div>
          ) : (
            <div className="h-64 w-full">
              <ResponsiveContainer width="100%" height="100%">
                <LineChart data={chartData} margin={{ top: 8, right: 16, bottom: 0, left: -12 }}>
                  <CartesianGrid strokeDasharray="3 3" stroke="#e2e8f0" />
                  <XAxis dataKey="time" tick={{ fontSize: 11, fill: "#94a3b8" }} minTickGap={40} />
                  <YAxis
                    domain={[0, 100]}
                    tick={{ fontSize: 11, fill: "#94a3b8" }}
                    tickFormatter={(value) => `${value}%`}
                  />
                  <Tooltip formatter={(value) => `${Number(value).toFixed(1)}%`} />
                  <Legend />
                  <Line type="monotone" dataKey="cpu" name="CPU" stroke="#2563eb" strokeWidth={2} dot={false} />
                  <Line type="monotone" dataKey="memory" name="内存" stroke="#7c3aed" strokeWidth={2} dot={false} />
                </LineChart>
              </ResponsiveContainer>
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>最近事件</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          {events.length === 0 ? (
            <div className="py-6 text-center text-sm text-slate-400">暂无事件</div>
          ) : (
            events.map((event, index) => (
              <div key={index} className="flex gap-3 border-b border-slate-100 pb-3 last:border-0 last:pb-0">
                <div
                  className={`mt-0.5 flex h-6 w-6 shrink-0 items-center justify-center rounded-full ${
                    event.type === "Warning" ? "bg-amber-50 text-amber-600" : "bg-blue-50 text-blue-600"
                  }`}
                >
                  {event.type === "Warning" ? (
                    <AlertTriangle className="h-3.5 w-3.5" />
                  ) : (
                    <Info className="h-3.5 w-3.5" />
                  )}
                </div>
                <div className="min-w-0 flex-1">
                  <div className="flex items-center justify-between gap-3">
                    <span className="text-sm font-medium text-slate-800">{event.reason}</span>
                    <span className="shrink-0 text-xs text-slate-400">{formatTime(event.lastTime)}</span>
                  </div>
                  <p className="mt-0.5 break-words text-xs text-slate-500">{event.message}</p>
                </div>
              </div>
            ))
          )}
        </CardContent>
      </Card>
    </div>
  )
}

// 环形仪表盘
function Gauge({ value, label, detail }: { value: number; label: string; detail: string }) {
  const radius = 56
  const circumference = 2 * Math.PI * radius
  const safeValue = Math.min(100, Math.max(0, value))

  return (
    <div className="flex flex-col items-center">
      <svg width="150" height="150" viewBox="0 0 150 150">
        <circle cx="75" cy="75" r={radius} fill="none" stroke="#e2e8f0" strokeWidth="12" />
        <circle
          cx="75"
          cy="75"
          r={radius}
          fill="none"
          stroke="#2563eb"
          strokeWidth="12"
          strokeLinecap="round"
          strokeDasharray={`${(circumference * safeValue) / 100} ${circumference}`}
          transform="rotate(-90 75 75)"
        />
        <text x="75" y="72" textAnchor="middle" fontSize="24" fontWeight="600" fill="#0f172a">
          {safeValue.toFixed(0)}%
        </text>
        <text x="75" y="94" textAnchor="middle" fontSize="12" fill="#94a3b8">
          {label}
        </text>
      </svg>
      <div className="mt-1 text-xs text-slate-500">{detail}</div>
    </div>
  )
}
