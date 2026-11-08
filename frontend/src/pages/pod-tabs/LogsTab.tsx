import { useEffect, useRef, useState } from "react"
import { Eraser, Pause, Play, RotateCw } from "lucide-react"
import { toast } from "sonner"
import { podApi } from "@/api/pod"
import { wsUrl } from "@/api/client"
import { getToken } from "@/lib/auth"
import { Button } from "@/components/ui/button"

// 日志 Tab：历史日志 + WebSocket 实时跟随
export function LogsTab({ name }: { name: string }) {
  const [content, setContent] = useState("")
  const [connected, setConnected] = useState(false)
  const [paused, setPaused] = useState(false)
  const [reloadKey, setReloadKey] = useState(0)
  const [loading, setLoading] = useState(true)

  const pausedRef = useRef(false)
  const scrollRef = useRef<HTMLPreElement>(null)

  useEffect(() => {
    pausedRef.current = paused
  }, [paused])

  useEffect(() => {
    let disposed = false
    let socket: WebSocket | null = null

    async function start() {
      setLoading(true)
      setContent("")
      try {
        // 先拉取历史日志，避免首屏空白
        const history = await podApi.logs(name, 300)
        if (!disposed) setContent(history.content)
      } catch (error) {
        if (!disposed) toast.error((error as Error).message)
      } finally {
        if (!disposed) setLoading(false)
      }
      if (disposed) return

      // 建立实时日志流
      socket = new WebSocket(wsUrl(`/pods/${name}/logs/stream?token=${encodeURIComponent(getToken() || "")}`))
      socket.onopen = () => setConnected(true)
      socket.onmessage = (event) => {
        if (pausedRef.current) return
        setContent((prev) => prev + event.data)
      }
      socket.onclose = () => setConnected(false)
      socket.onerror = () => setConnected(false)
    }

    start()
    return () => {
      disposed = true
      socket?.close()
    }
  }, [name, reloadKey])

  // 内容变化时自动滚动到底部（暂停时不滚动）
  useEffect(() => {
    if (paused) return
    const element = scrollRef.current
    if (element) {
      element.scrollTop = element.scrollHeight
    }
  }, [content, paused])

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-3 text-xs text-slate-500">
          <span className={`h-1.5 w-1.5 rounded-full ${connected ? "bg-emerald-500" : "bg-slate-300"}`} />
          {connected ? "实时日志已连接" : "日志流未连接"}
          <span className="text-slate-400">容器: main</span>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={() => setPaused((value) => !value)}>
            {paused ? <Play className="h-3.5 w-3.5" /> : <Pause className="h-3.5 w-3.5" />}
            {paused ? "继续" : "暂停"}
          </Button>
          <Button variant="outline" size="sm" onClick={() => setContent("")}>
            <Eraser className="h-3.5 w-3.5" />
            清空
          </Button>
          <Button variant="outline" size="sm" onClick={() => setReloadKey((key) => key + 1)}>
            <RotateCw className="h-3.5 w-3.5" />
            重连
          </Button>
        </div>
      </div>

      <pre
        ref={scrollRef}
        className="h-[540px] overflow-auto whitespace-pre-wrap break-all rounded-xl border border-slate-800 bg-[#0f172a] p-4 font-mono text-xs leading-relaxed text-slate-200"
      >
        {loading && content === "" ? "正在加载日志..." : content || "暂无日志输出"}
      </pre>
    </div>
  )
}
