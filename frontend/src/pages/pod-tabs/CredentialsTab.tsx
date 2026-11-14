import { useCallback, useEffect, useState } from "react"
import { CheckCircle2, Copy, Eye, EyeOff, KeyRound, Loader2, RefreshCw, Terminal, XCircle } from "lucide-react"
import { toast } from "sonner"
import { credentialApi, type SSHCredential } from "@/api/credential"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { formatTime } from "@/lib/format"

// 凭证 Tab：展示创建 Pod 时自动签发的 SSH 连接凭证，并支持一键测试连通性
export function CredentialsTab({ podName }: { podName: string }) {
  const [cred, setCred] = useState<SSHCredential | null>(null)
  const [loading, setLoading] = useState(true)
  // 密码默认打码，点击眼睛图标临时展示
  const [revealed, setRevealed] = useState(false)
  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState<{ ok: boolean; message: string } | null>(null)

  const load = useCallback(
    async (silent = false) => {
      if (!silent) setLoading(true)
      try {
        const result = await credentialApi.get(podName)
        setCred(result)
      } catch (error) {
        if (!silent) toast.error((error as Error).message)
      } finally {
        setLoading(false)
      }
    },
    [podName],
  )

  useEffect(() => {
    load()
  }, [load])

  // 复制到剪贴板，失败时兜底提示
  async function copy(text: string, label: string) {
    try {
      await navigator.clipboard.writeText(text)
      toast.success(`${label}已复制`)
    } catch {
      toast.error("复制失败，请手动选择文本复制")
    }
  }

  async function handleTest() {
    setTesting(true)
    setTestResult(null)
    try {
      const result = await credentialApi.test(podName)
      setTestResult({ ok: true, message: result.output || "连接成功" })
      toast.success("SSH 连接成功")
    } catch (error) {
      const message = (error as Error).message
      setTestResult({ ok: false, message })
      toast.error(message)
    } finally {
      setTesting(false)
    }
  }

  if (loading && !cred) {
    return <Skeleton className="h-72 w-full" />
  }

  if (!cred) {
    return (
      <div className="rounded-xl border border-dashed border-slate-200 bg-white p-12 text-center">
        <KeyRound className="mx-auto h-8 w-8 text-slate-300" />
        <div className="mt-2 text-sm text-slate-600">未获取到 SSH 凭证</div>
        <div className="mt-1 text-xs text-slate-400">请确认 Pod 已创建成功，然后点击下方刷新重试</div>
        <Button className="mt-4" size="sm" variant="outline" onClick={() => load()}>
          <RefreshCw className="h-3.5 w-3.5" />
          刷新
        </Button>
      </div>
    )
  }

  const sshCommand = `ssh -p ${cred.port} ${cred.username}@${cred.host}`

  return (
    <div className="space-y-4">
      {/* 顶部说明与操作 */}
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-base font-semibold text-slate-900">SSH 连接凭证</h2>
          <p className="mt-1 text-sm text-slate-500">
            创建 Pod 时自动签发，密码由平台随机生成且仅本 Pod 独享。拿到凭证后可通过 SSH 直接登录该 Pod。
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={() => load(true)} title="刷新">
            <RefreshCw className="h-3.5 w-3.5" />
            刷新
          </Button>
          <Button size="sm" onClick={handleTest} disabled={testing}>
            {testing ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Terminal className="h-3.5 w-3.5" />}
            测试连接
          </Button>
        </div>
      </div>

      <Card>
        <CardContent className="space-y-4 p-5">
          {/* 连接信息 */}
          <div className="space-y-2 rounded-lg bg-slate-50 p-4 text-sm">
            <Row label="主机地址" value={cred.host} onCopy={() => copy(cred.host, "主机地址")} />
            <Row label="端口" value={String(cred.port)} onCopy={() => copy(String(cred.port), "端口")} />
            <Row label="用户名" value={cred.username} onCopy={() => copy(cred.username, "用户名")} />
            <div className="flex items-center justify-between gap-3">
              <span className="shrink-0 text-slate-400">密码</span>
              <span className="flex min-w-0 items-center gap-2 font-mono text-slate-700">
                <span className="truncate">{revealed ? cred.password : "••••••••••••"}</span>
                <button
                  type="button"
                  title={revealed ? "隐藏密码" : "显示密码"}
                  onClick={() => setRevealed((value) => !value)}
                >
                  {revealed ? (
                    <EyeOff className="h-3.5 w-3.5 text-slate-400 hover:text-slate-600" />
                  ) : (
                    <Eye className="h-3.5 w-3.5 text-slate-400 hover:text-slate-600" />
                  )}
                </button>
                <button type="button" title="复制密码" onClick={() => copy(cred.password, "密码")}>
                  <Copy className="h-3.5 w-3.5 text-slate-400 hover:text-slate-600" />
                </button>
              </span>
            </div>
            <div className="flex items-center justify-between gap-3">
              <span className="shrink-0 text-slate-400">签发时间</span>
              <span className="text-slate-700">{formatTime(cred.createdAt)}</span>
            </div>
          </div>

          {/* 登录命令 */}
          <div>
            <div className="mb-1.5 text-xs text-slate-400">在本地终端执行以下命令登录</div>
            <div className="flex items-center gap-2 rounded-lg bg-slate-900 px-3 py-2.5">
              <code className="min-w-0 flex-1 truncate font-mono text-xs text-slate-100">{sshCommand}</code>
              <button type="button" title="复制登录命令" onClick={() => copy(sshCommand, "登录命令")}>
                <Copy className="h-3.5 w-3.5 text-slate-400 hover:text-slate-100" />
              </button>
            </div>
          </div>

          {/* 测试结果 */}
          {testResult && (
            <div
              className={`flex items-start gap-2 rounded-lg p-3 text-xs ${
                testResult.ok ? "bg-emerald-50 text-emerald-700" : "bg-red-50 text-red-600"
              }`}
            >
              {testResult.ok ? (
                <CheckCircle2 className="mt-0.5 h-3.5 w-3.5 shrink-0" />
              ) : (
                <XCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
              )}
              <pre className="min-w-0 flex-1 whitespace-pre-wrap break-all font-mono">{testResult.message}</pre>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

// Row 渲染一行「标签 + 等宽值 + 复制按钮」
function Row({ label, value, onCopy }: { label: string; value: string; onCopy: () => void }) {
  return (
    <div className="flex items-center justify-between gap-3">
      <span className="shrink-0 text-slate-400">{label}</span>
      <span className="flex min-w-0 items-center gap-2 font-mono text-slate-700">
        <span className="truncate">{value}</span>
        <button type="button" title={`复制${label}`} onClick={onCopy}>
          <Copy className="h-3.5 w-3.5 text-slate-400 hover:text-slate-600" />
        </button>
      </span>
    </div>
  )
}
