import { useState } from "react"
import { NavLink, Outlet, useLocation, useNavigate } from "react-router-dom"
import {
  BookOpen,
  Box,
  Boxes,
  BrainCircuit,
  Code2,
  Cpu,
  Database,
  HardDrive,
  LayoutDashboard,
  LogOut,
  Server,
  Share2,
  Shield,
  User,
} from "lucide-react"
import { cn } from "@/lib/utils"
import { clearAuth, getUser } from "@/lib/auth"
import { Button } from "@/components/ui/button"
import { FeatureDialog } from "@/components/common/FeatureDialog"

// 侧边栏菜单：只有 Pod 可跳转，其余点击提示开发中
const MENU = [
  { key: "overview", label: "概览", icon: LayoutDashboard, path: "" },
  { key: "pod", label: "Pod", icon: Box, path: "/pods" },
  { key: "host", label: "主机", icon: Server, path: "" },
  { key: "cluster", label: "集群", icon: Boxes, path: "" },
  { key: "gpu", label: "GPU", icon: Cpu, path: "" },
  { key: "storage", label: "存储", icon: HardDrive, path: "" },
  { key: "database", label: "数据库", icon: Database, path: "" },
  { key: "ide", label: "IDE", icon: Code2, path: "" },
  { key: "llm", label: "LLM", icon: BrainCircuit, path: "" },
  { key: "share", label: "共享", icon: Share2, path: "" },
  { key: "security", label: "攻防", icon: Shield, path: "" },
  { key: "profile", label: "个人", icon: User, path: "" },
  { key: "docs", label: "文档", icon: BookOpen, path: "" },
]

export default function AppLayout() {
  const [feature, setFeature] = useState<string | null>(null)
  const navigate = useNavigate()
  const location = useLocation()
  const user = getUser()

  // 顶部面包屑：根据当前路径匹配菜单标题
  const active = MENU.find((item) => item.path && location.pathname.startsWith(item.path))
  const headerTitle = active ? `${active.label}` : "概览"

  function handleLogout() {
    clearAuth()
    navigate("/login", { replace: true })
  }

  return (
    <div className="flex h-screen overflow-hidden bg-slate-50">
      {/* 左侧深色导航 */}
      <aside className="flex w-56 shrink-0 flex-col bg-slate-900 text-slate-400">
        <div className="flex h-14 items-center gap-2 border-b border-white/10 px-5">
          <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-blue-600 text-sm font-bold text-white">
            S
          </div>
          <span className="text-[15px] font-semibold text-white">SSEDeploy</span>
        </div>

        <nav className="flex-1 space-y-0.5 overflow-y-auto px-2 py-3">
          {MENU.map((item) => {
            const Icon = item.icon
            if (!item.path) {
              return (
                <button
                  key={item.key}
                  type="button"
                  onClick={() => setFeature(item.label)}
                  className="flex w-full items-center gap-3 rounded-lg px-3 py-2 text-sm transition-colors hover:bg-white/5 hover:text-slate-200"
                >
                  <Icon className="h-4 w-4" />
                  {item.label}
                </button>
              )
            }
            return (
              <NavLink
                key={item.key}
                to={item.path}
                className={({ isActive }) =>
                  cn(
                    "flex w-full items-center gap-3 rounded-lg px-3 py-2 text-sm transition-colors",
                    isActive
                      ? "bg-blue-600 text-white shadow-sm"
                      : "hover:bg-white/5 hover:text-slate-200",
                  )
                }
              >
                <Icon className="h-4 w-4" />
                {item.label}
              </NavLink>
            )
          })}
        </nav>

        <div className="border-t border-white/10 p-3 text-xs text-slate-500">
          <div>Pod 模块 · 第一阶段</div>
          <div className="mt-1">v0.1.0</div>
        </div>
      </aside>

      {/* 右侧内容区 */}
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-14 shrink-0 items-center justify-between bg-slate-900 px-6 text-white">
          <div className="text-sm text-slate-300">{headerTitle}</div>
          <div className="flex items-center gap-3">
            <span className="text-sm text-slate-300">{user?.displayName || user?.username}</span>
            <Button
              variant="ghost"
              size="sm"
              className="text-slate-300 hover:bg-white/10 hover:text-white"
              onClick={handleLogout}
            >
              <LogOut className="h-4 w-4" />
              退出
            </Button>
          </div>
        </header>

        <main className="flex-1 overflow-auto p-6">
          <Outlet />
        </main>
      </div>

      <FeatureDialog open={feature !== null} onOpenChange={(open) => !open && setFeature(null)} title={feature || ""} />
    </div>
  )
}
