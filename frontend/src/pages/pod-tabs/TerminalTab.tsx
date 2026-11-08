import { useEffect, useRef, useState } from "react"
import { Terminal as XTerm } from "@xterm/xterm"
import { FitAddon } from "@xterm/addon-fit"
import { PlugZap, RotateCw } from "lucide-react"
import "@xterm/xterm/css/xterm.css"
import { wsUrl } from "@/api/client"
import { getToken } from "@/lib/auth"
import { Button } from "@/components/ui/button"

// 终端 Tab：xterm.js 通过 WebSocket 连接后端 SPDY exec
export function TerminalTab({ name }: { name: string }) {
  const containerRef = useRef<HTMLDivElement>(null)
  const [connected, setConnected] = useState(false)
  const [reloadKey, setReloadKey] = useState(0)

  useEffect(() => {
    const container = containerRef.current
    if (!container) return

    const term = new XTerm({
      fontSize: 13,
      fontFamily: '"JetBrains Mono", Menlo, Consolas, monospace',
      cursorBlink: true,
      convertEol: true,
      theme: {
        background: "#0f172a",
        foreground: "#e2e8f0",
        cursor: "#60a5fa",
        selectionBackground: "#334155",
      },
    })
    const fitAddon = new FitAddon()
    term.loadAddon(fitAddon)
    term.open(container)
    fitAddon.fit()

    const socket = new WebSocket(
      wsUrl(`/pods/${name}/terminal?token=${encodeURIComponent(getToken() || "")}&shell=/bin/sh`),
    )

    // 发送窗口尺寸，保证 vim/top 等程序正常渲染
    const sendResize = () => {
      if (socket.readyState !== WebSocket.OPEN) return
      socket.send(JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows }))
    }

    socket.onopen = () => {
      setConnected(true)
      sendResize()
      term.focus()
    }
    socket.onmessage = (event) => {
      term.write(typeof event.data === "string" ? event.data : new Uint8Array(event.data))
    }
    socket.onclose = () => {
      setConnected(false)
      term.writeln("\r\n\x1b[33m[连接已断开]\x1b[0m")
    }
    socket.onerror = () => {
      setConnected(false)
      term.writeln("\r\n\x1b[31m[连接异常，请确认 Pod 正在运行]\x1b[0m")
    }

    // 键盘输入 -> 后端 stdin
    const inputDisposable = term.onData((data) => {
      if (socket.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({ type: "input", data }))
      }
    })

    // 容器尺寸变化时同步终端与远端 pty
    const observer = new ResizeObserver(() => {
      fitAddon.fit()
      sendResize()
    })
    observer.observe(container)

    return () => {
      observer.disconnect()
      inputDisposable.dispose()
      socket.close()
      term.dispose()
    }
  }, [name, reloadKey])

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2 text-xs text-slate-500">
          <span className={`h-1.5 w-1.5 rounded-full ${connected ? "bg-emerald-500" : "bg-slate-300"}`} />
          {connected ? "终端已连接" : "终端未连接"}
          <span className="ml-2 text-slate-400">shell: /bin/sh</span>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={() => setReloadKey((key) => key + 1)}>
            <RotateCw className="h-3.5 w-3.5" />
            重新连接
          </Button>
        </div>
      </div>
      <div className="overflow-hidden rounded-xl border border-slate-800 bg-[#0f172a]">
        <div className="flex items-center gap-2 border-b border-slate-800 px-4 py-2 text-xs text-slate-400">
          <PlugZap className="h-3.5 w-3.5" />
          {name}
        </div>
        <div ref={containerRef} className="xterm-wrapper h-[520px] w-full" />
      </div>
    </div>
  )
}
