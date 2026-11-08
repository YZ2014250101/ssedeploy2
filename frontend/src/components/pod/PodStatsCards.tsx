import { Activity, AlertTriangle, PauseCircle, UserCheck } from "lucide-react"
import type { PodStats } from "@/api/pod"
import { Card, CardContent } from "@/components/ui/card"

// 顶部四张统计卡片
const CARDS = [
  { key: "running" as const, label: "运行", icon: Activity, tone: "bg-emerald-50 text-emerald-600" },
  { key: "stopped" as const, label: "已停止", icon: PauseCircle, tone: "bg-slate-100 text-slate-500" },
  { key: "failed" as const, label: "失败", icon: AlertTriangle, tone: "bg-red-50 text-red-600" },
  { key: "mine" as const, label: "我的", icon: UserCheck, tone: "bg-blue-50 text-blue-600" },
]

export function PodStatsCards({ stats }: { stats: PodStats }) {
  return (
    <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
      {CARDS.map((item) => {
        const Icon = item.icon
        return (
          <Card key={item.key}>
            <CardContent className="flex items-center gap-4 p-5">
              <div className={`flex h-11 w-11 items-center justify-center rounded-xl ${item.tone}`}>
                <Icon className="h-5 w-5" />
              </div>
              <div>
                <div className="text-2xl font-semibold leading-tight text-slate-900">{stats[item.key]}</div>
                <div className="mt-0.5 text-xs text-slate-500">{item.label}</div>
              </div>
            </CardContent>
          </Card>
        )
      })}
    </div>
  )
}
