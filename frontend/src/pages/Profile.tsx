import { useEffect, useState } from "react"
import { ArrowUpRight, Monitor, Moon, Sparkles, Sun } from "lucide-react"
import { profileApi, type UserProfileData } from "@/api/profile"
import { getTheme, saveTheme, type Theme } from "@/lib/theme"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { FeatureDialog } from "@/components/common/FeatureDialog"
import { cn } from "@/lib/utils"

// 三个主题选项：白天 / 黑夜 / 跟随浏览器
const THEME_OPTIONS: Array<{ value: Theme; label: string; icon: typeof Sun }> = [
  { value: "light", label: "白天", icon: Sun },
  { value: "dark", label: "黑夜", icon: Moon },
  { value: "auto", label: "跟随浏览器", icon: Monitor },
]

// 键值对字段
function InfoField({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="text-xs text-slate-500">{label}</div>
      <div className="mt-1 truncate text-sm font-medium text-slate-900" title={value}>
        {value}
      </div>
    </div>
  )
}

export default function Profile() {
  const [profile, setProfile] = useState<UserProfileData | null>(null)
  // 主题偏好从 localStorage 读取，切换后立即全局生效
  const [theme, setTheme] = useState<Theme>(() => getTheme())
  const [feature, setFeature] = useState<string | null>(null)
  const [passwordOpen, setPasswordOpen] = useState(false)

  useEffect(() => {
    let alive = true
    profileApi
      .detail()
      .then((result) => {
        if (alive) setProfile(result)
      })
      .catch(() => {
        // 个人页信息加载失败时保持占位符，不打断页面
      })
    return () => {
      alive = false
    }
  }, [])

  // 切换主题：写入 localStorage 并立即应用
  function changeTheme(next: Theme) {
    setTheme(next)
    saveTheme(next)
  }

  const avatarText = (profile?.displayName || profile?.username || "U").charAt(0).toUpperCase()

  return (
    <div className="mx-auto max-w-5xl space-y-5">
      {/* 页面标题栏：账户文档链接 + AI 助手按钮（均仅占位） */}
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight text-slate-900">个人管理</h1>
          <p className="mt-1 text-sm text-slate-500">账户设置与身份绑定</p>
        </div>
        <div className="flex items-center gap-2">
          <a
            href="#"
            onClick={(event) => event.preventDefault()}
            className="inline-flex items-center gap-1 rounded-lg border border-slate-200 bg-white px-3 py-1.5 text-sm text-slate-600 transition-colors hover:text-blue-600"
          >
            账户文档
            <ArrowUpRight className="h-3.5 w-3.5" />
          </a>
          <Button variant="outline" onClick={() => setFeature("AI 助手")}>
            <Sparkles className="h-4 w-4" />
            AI 助手
          </Button>
        </div>
      </div>

      {/* 卡片1：用户信息 */}
      <Card className="p-5">
        <div className="flex items-center gap-4">
          <div className="flex h-14 w-14 shrink-0 items-center justify-center rounded-full bg-blue-600 text-xl font-semibold text-white">
            {avatarText}
          </div>
          <div>
            <div className="flex items-center gap-2">
              <span className="font-mono text-sm text-slate-900">{profile?.uid || "--"}</span>
              <Badge variant="secondary">user</Badge>
            </div>
            <div className="mt-1 text-xs text-slate-400">用户 ID</div>
          </div>
        </div>

        <div className="mt-6 grid grid-cols-1 gap-x-8 gap-y-4 sm:grid-cols-2">
          <InfoField label="用户名" value={profile?.username || "--"} />
          <InfoField label="角色" value={profile?.role || "--"} />
          <InfoField label="权限数" value={`${profile?.permissionCount ?? 0} 项`} />
          <InfoField label="邮箱" value={profile?.email || "--"} />
          <InfoField label="显示名" value={profile?.displayName || "--"} />
        </div>
      </Card>

      {/* 卡片2：外观主题 */}
      <Card className="p-5">
        <h2 className="text-base font-semibold text-slate-900">外观主题</h2>
        <p className="mt-1 text-xs text-slate-400">选择白天、黑夜，或自动跟随浏览器。偏好保存在当前浏览器。</p>
        <div className="mt-4 grid grid-cols-1 gap-3 sm:grid-cols-3" role="radiogroup" aria-label="外观主题">
          {THEME_OPTIONS.map((option) => {
            const Icon = option.icon
            const selected = theme === option.value
            return (
              <button
                key={option.value}
                type="button"
                role="radio"
                aria-checked={selected}
                onClick={() => changeTheme(option.value)}
                className={cn(
                  "flex items-center gap-3 rounded-lg border px-4 py-3 text-sm transition-colors",
                  selected
                    ? "border-blue-500 bg-blue-50 text-blue-700"
                    : "border-slate-200 text-slate-600 hover:bg-slate-50",
                )}
              >
                <span
                  className={cn(
                    "flex h-4 w-4 shrink-0 items-center justify-center rounded-full border",
                    selected ? "border-blue-500" : "border-slate-300",
                  )}
                >
                  {selected && <span className="h-2 w-2 rounded-full bg-blue-500" />}
                </span>
                <Icon className="h-4 w-4" />
                {option.label}
              </button>
            )
          })}
        </div>
      </Card>

      {/* 卡片3：修改密码（弹窗仅 UI 占位） */}
      <Card className="p-5">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 className="text-base font-semibold text-slate-900">修改密码</h2>
            <p className="mt-1 text-xs text-slate-400">更新你的登录密码</p>
          </div>
          <Button variant="outline" onClick={() => setPasswordOpen(true)}>
            修改
          </Button>
        </div>
      </Card>

      {/* 修改密码弹窗：仅 UI 占位，不提交 */}
      <Dialog open={passwordOpen} onOpenChange={setPasswordOpen}>
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>修改密码</DialogTitle>
            <DialogDescription>密码修改功能暂未开放，此处仅展示交互占位。</DialogDescription>
          </DialogHeader>
          <div className="space-y-3">
            <Input type="password" placeholder="当前密码" disabled />
            <Input type="password" placeholder="新密码" disabled />
            <Input type="password" placeholder="确认新密码" disabled />
          </div>
          <DialogFooter>
            <Button onClick={() => setPasswordOpen(false)}>我知道了</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 未开发模块的统一提示弹窗（AI 助手） */}
      <FeatureDialog open={feature !== null} onOpenChange={(open) => !open && setFeature(null)} title={feature || ""} />
    </div>
  )
}
