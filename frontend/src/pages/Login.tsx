import { useEffect, useMemo, useState, type FormEvent } from "react"
import { useNavigate } from "react-router-dom"
import { toast } from "sonner"
import { Box, Eye, EyeOff, Loader2 } from "lucide-react"
import { authApi } from "@/api/auth"
import { saveAuth } from "@/lib/auth"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"

// 与后端保持一致的校验规则
const USERNAME_RE = /^[A-Za-z0-9_]{4,20}$/
const EMAIL_RE = /^[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+$/
const PASSWORD_MIN = 6
const PASSWORD_MAX = 24
// 防抖时长：输入停顿后再做实时校验，避免逐字符提示
const DEBOUNCE_MS = 300

type TabKey = "login" | "register"
type RegisterField = "username" | "email" | "password" | "confirmPassword"

interface RegisterForm {
  username: string
  email: string
  password: string
  confirmPassword: string
}

// 注册表单校验：返回字段 -> 错误信息，空对象表示全部合法
function validateRegister(form: RegisterForm): Partial<Record<RegisterField, string>> {
  const errors: Partial<Record<RegisterField, string>> = {}
  const username = form.username.trim()
  const email = form.email.trim()

  if (!username) {
    errors.username = "请输入用户名"
  } else if (!USERNAME_RE.test(username)) {
    errors.username = "用户名需为 4-20 位字母、数字或下划线"
  }

  if (!email) {
    errors.email = "请输入邮箱"
  } else if (!EMAIL_RE.test(email)) {
    errors.email = "请输入合法的邮箱地址"
  }

  if (!form.password) {
    errors.password = "请输入密码"
  } else if (form.password.length < PASSWORD_MIN || form.password.length > PASSWORD_MAX) {
    errors.password = `密码长度需为 ${PASSWORD_MIN}-${PASSWORD_MAX} 位`
  }

  if (!form.confirmPassword) {
    errors.confirmPassword = "请再次输入密码"
  } else if (form.confirmPassword !== form.password) {
    errors.confirmPassword = "两次输入的密码不一致"
  }

  return errors
}

// 密码强度：长度、字母数字组合、长密码、特殊字符各计 1 分
function passwordStrength(password: string): { score: number; label: string; color: string } {
  let score = 0
  if (password.length >= PASSWORD_MIN) score++
  if (/[A-Za-z]/.test(password) && /\d/.test(password)) score++
  if (password.length >= 12) score++
  if (/[^A-Za-z0-9]/.test(password)) score++

  if (score <= 1) return { score: 1, label: "弱", color: "bg-red-500" }
  if (score === 2) return { score: 2, label: "中", color: "bg-amber-500" }
  return { score: 3, label: "强", color: "bg-emerald-500" }
}

// 登录页：深色背景 + 居中卡片，内置「登录 / 注册」Tab
export default function Login() {
  const navigate = useNavigate()

  // 当前 Tab，默认登录
  const [tab, setTab] = useState<TabKey>("login")

  // ---------- 登录表单 ----------
  const [loginUsername, setLoginUsername] = useState("admin")
  const [loginPassword, setLoginPassword] = useState("")
  const [showLoginPassword, setShowLoginPassword] = useState(false)
  const [loginLoading, setLoginLoading] = useState(false)

  // ---------- 注册表单 ----------
  const [form, setForm] = useState<RegisterForm>({
    username: "",
    email: "",
    password: "",
    confirmPassword: "",
  })
  const [showRegisterPassword, setShowRegisterPassword] = useState(false)
  const [showConfirmPassword, setShowConfirmPassword] = useState(false)
  const [registerLoading, setRegisterLoading] = useState(false)
  // 已触碰过的字段：只有用户输入过才展示行内错误，避免一进页面满屏红字
  const [touched, setTouched] = useState<Partial<Record<RegisterField, boolean>>>({})
  // 点击提交后，未填写字段也展示错误
  const [submitted, setSubmitted] = useState(false)
  // 防抖后的表单快照，用于实时校验
  const [debouncedForm, setDebouncedForm] = useState<RegisterForm>(form)

  useEffect(() => {
    const timer = window.setTimeout(() => setDebouncedForm(form), DEBOUNCE_MS)
    return () => window.clearTimeout(timer)
  }, [form])

  // 实时校验结果（基于防抖后的值）；提交后改用当前值立即校验，保证行内提示即时出现
  const liveErrors = useMemo(() => validateRegister(debouncedForm), [debouncedForm])
  const displayErrors = useMemo(
    () => (submitted ? validateRegister(form) : liveErrors),
    [submitted, form, liveErrors],
  )
  const strength = useMemo(() => passwordStrength(form.password), [form.password])

  // 某字段是否需要展示错误：触碰过或已提交
  function fieldError(field: RegisterField): string | undefined {
    if (!touched[field] && !submitted) return undefined
    return displayErrors[field]
  }

  function updateField(field: RegisterField, value: string) {
    setForm((prev) => ({ ...prev, [field]: value }))
    setTouched((prev) => ({ ...prev, [field]: true }))
  }

  async function handleLogin(event: FormEvent) {
    event.preventDefault()
    if (!loginUsername || !loginPassword) {
      toast.error("请输入账号和密码")
      return
    }
    setLoginLoading(true)
    try {
      const data = await authApi.login(loginUsername, loginPassword)
      saveAuth(data.token, data.user)
      toast.success("登录成功")
      navigate("/", { replace: true })
    } catch (error) {
      toast.error((error as Error).message)
    } finally {
      setLoginLoading(false)
    }
  }

  async function handleRegister(event: FormEvent) {
    event.preventDefault()
    setSubmitted(true)

    // 提交时用当前值立即校验，不等待防抖
    const errors = validateRegister(form)
    const firstError = Object.values(errors)[0]
    if (firstError) {
      toast.error(firstError)
      return
    }

    setRegisterLoading(true)
    try {
      const username = form.username.trim()
      await authApi.register(username, form.email.trim(), form.password)
      toast.success("注册成功，请登录")
      // 注册成功：回到登录 Tab，并回填用户名
      setTab("login")
      setLoginUsername(username)
      setLoginPassword("")
      setForm({ username: "", email: "", password: "", confirmPassword: "" })
      setTouched({})
      setSubmitted(false)
    } catch (error) {
      // 展示后端返回的 msg（用户名已被占用 / 该邮箱已注册等）
      toast.error((error as Error).message)
    } finally {
      setRegisterLoading(false)
    }
  }

  // 行内错误输入框样式
  function inputClass(hasError: boolean, extra = "") {
    return `${extra} ${hasError ? "border-red-300 focus-visible:border-red-500 focus-visible:ring-red-500/30" : ""}`
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-slate-900 p-4">
      <div className="w-full max-w-sm rounded-2xl bg-white p-8 shadow-2xl">
        <div className="mb-6 flex flex-col items-center gap-3">
          <div className="flex h-11 w-11 items-center justify-center rounded-xl bg-blue-600 text-white">
            <Box className="h-6 w-6" />
          </div>
          <div className="text-center">
            <h1 className="text-xl font-semibold text-slate-900">SSEDeploy</h1>
            <p className="mt-1 text-sm text-slate-500">轻量化云原生部署平台</p>
          </div>
        </div>

        <Tabs value={tab} onValueChange={(value) => setTab(value as TabKey)}>
          <TabsList className="w-full">
            <TabsTrigger value="login" className="flex-1">
              登录
            </TabsTrigger>
            <TabsTrigger value="register" className="flex-1">
              注册账号
            </TabsTrigger>
          </TabsList>

          {/* ---------- 登录 ---------- */}
          <TabsContent value="login">
            <form onSubmit={handleLogin} className="space-y-4">
              <div className="space-y-1.5">
                <Label htmlFor="login-username">账号</Label>
                <Input
                  id="login-username"
                  value={loginUsername}
                  onChange={(e) => setLoginUsername(e.target.value)}
                  placeholder="请输入账号"
                  autoComplete="username"
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="login-password">密码</Label>
                <div className="relative">
                  <Input
                    id="login-password"
                    type={showLoginPassword ? "text" : "password"}
                    value={loginPassword}
                    onChange={(e) => setLoginPassword(e.target.value)}
                    placeholder="请输入密码"
                    autoComplete="current-password"
                    className="pr-9"
                  />
                  <button
                    type="button"
                    aria-label={showLoginPassword ? "隐藏密码" : "显示密码"}
                    onClick={() => setShowLoginPassword((v) => !v)}
                    className="absolute right-1 top-1/2 flex h-7 w-7 -translate-y-1/2 items-center justify-center rounded-md text-slate-400 transition-colors hover:text-slate-600"
                  >
                    {showLoginPassword ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                  </button>
                </div>
              </div>
              <Button type="submit" className="w-full" disabled={loginLoading}>
                {loginLoading && <Loader2 className="h-4 w-4 animate-spin" />}
                登录
              </Button>
            </form>

            <p className="mt-6 text-center text-xs text-slate-400">默认账号 admin / admin123</p>
          </TabsContent>

          {/* ---------- 注册 ---------- */}
          <TabsContent value="register">
            <form onSubmit={handleRegister} className="space-y-4" noValidate>
              <div className="space-y-1.5">
                <Label htmlFor="register-username">用户名</Label>
                <Input
                  id="register-username"
                  value={form.username}
                  onChange={(e) => updateField("username", e.target.value)}
                  onBlur={() => setTouched((prev) => ({ ...prev, username: true }))}
                  placeholder="4-20 位字母、数字或下划线"
                  autoComplete="username"
                  maxLength={20}
                  className={inputClass(Boolean(fieldError("username")))}
                />
                {fieldError("username") && (
                  <p className="text-xs text-red-500">{fieldError("username")}</p>
                )}
              </div>

              <div className="space-y-1.5">
                <Label htmlFor="register-email">邮箱</Label>
                <Input
                  id="register-email"
                  type="email"
                  value={form.email}
                  onChange={(e) => updateField("email", e.target.value)}
                  onBlur={() => setTouched((prev) => ({ ...prev, email: true }))}
                  placeholder="name@example.com"
                  autoComplete="email"
                  className={inputClass(Boolean(fieldError("email")))}
                />
                {fieldError("email") && <p className="text-xs text-red-500">{fieldError("email")}</p>}
              </div>

              <div className="space-y-1.5">
                <Label htmlFor="register-password">密码</Label>
                <div className="relative">
                  <Input
                    id="register-password"
                    type={showRegisterPassword ? "text" : "password"}
                    value={form.password}
                    onChange={(e) => updateField("password", e.target.value)}
                    onBlur={() => setTouched((prev) => ({ ...prev, password: true }))}
                    placeholder={`${PASSWORD_MIN}-${PASSWORD_MAX} 位密码`}
                    autoComplete="new-password"
                    maxLength={PASSWORD_MAX}
                    className={inputClass(Boolean(fieldError("password")), "pr-9")}
                  />
                  <button
                    type="button"
                    aria-label={showRegisterPassword ? "隐藏密码" : "显示密码"}
                    onClick={() => setShowRegisterPassword((v) => !v)}
                    className="absolute right-1 top-1/2 flex h-7 w-7 -translate-y-1/2 items-center justify-center rounded-md text-slate-400 transition-colors hover:text-slate-600"
                  >
                    {showRegisterPassword ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                  </button>
                </div>
                {fieldError("password") && (
                  <p className="text-xs text-red-500">{fieldError("password")}</p>
                )}
                {/* 密码强度实时提示 */}
                {form.password && !fieldError("password") && (
                  <div className="flex items-center gap-2">
                    <div className="flex flex-1 gap-1">
                      {[1, 2, 3].map((level) => (
                        <span
                          key={level}
                          className={`h-1 flex-1 rounded-full ${
                            strength.score >= level ? strength.color : "bg-slate-200"
                          }`}
                        />
                      ))}
                    </div>
                    <span className="text-xs text-slate-500">强度：{strength.label}</span>
                  </div>
                )}
              </div>

              <div className="space-y-1.5">
                <Label htmlFor="register-confirm">确认密码</Label>
                <div className="relative">
                  <Input
                    id="register-confirm"
                    type={showConfirmPassword ? "text" : "password"}
                    value={form.confirmPassword}
                    onChange={(e) => updateField("confirmPassword", e.target.value)}
                    onBlur={() => setTouched((prev) => ({ ...prev, confirmPassword: true }))}
                    placeholder="请再次输入密码"
                    autoComplete="new-password"
                    maxLength={PASSWORD_MAX}
                    className={inputClass(Boolean(fieldError("confirmPassword")), "pr-9")}
                  />
                  <button
                    type="button"
                    aria-label={showConfirmPassword ? "隐藏密码" : "显示密码"}
                    onClick={() => setShowConfirmPassword((v) => !v)}
                    className="absolute right-1 top-1/2 flex h-7 w-7 -translate-y-1/2 items-center justify-center rounded-md text-slate-400 transition-colors hover:text-slate-600"
                  >
                    {showConfirmPassword ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                  </button>
                </div>
                {fieldError("confirmPassword") && (
                  <p className="text-xs text-red-500">{fieldError("confirmPassword")}</p>
                )}
              </div>

              <Button type="submit" className="w-full" disabled={registerLoading}>
                {registerLoading && <Loader2 className="h-4 w-4 animate-spin" />}
                注册
              </Button>
            </form>
          </TabsContent>
        </Tabs>
      </div>
    </div>
  )
}
