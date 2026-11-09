/**
 * 云 IDE 工作台（独立全屏页面，深色主题）
 *
 * 逻辑：
 * 1. 进入 /ide 先请求当前用户自己的 Pod 列表（GET /api/v1/ide/pods）
 * 2. 没有可用 Pod 时展示提示 + 创建按钮，跳转 /pods
 * 3. 有运行中的 Pod 时渲染：文件树 / 编辑器 / AI 面板 / 终端 / 日志
 * 4. 终端与日志分别通过 WebSocket 对接后端 SPDY exec 与日志流
 *
 * 说明：文件树为固定的 home / shared 目录（UI 占位），
 * 本阶段后端能力只包含 Pod 管理、终端与日志，不提供文件读写接口。
 */
import { useEffect, useMemo, useRef, useState } from "react"
import { useNavigate } from "react-router-dom"
import { Terminal as XTerm } from "@xterm/xterm"
import { FitAddon } from "@xterm/addon-fit"
import {
  ArrowLeft,
  Bot,
  ChevronDown,
  ChevronRight,
  Eraser,
  FileCode2,
  Folder,
  FolderOpen,
  Loader2,
  Pause,
  Play,
  RotateCw,
  ScrollText,
  Send,
  Sparkles,
  SquareTerminal,
  FolderTree,
  Box,
} from "lucide-react"
import "@xterm/xterm/css/xterm.css"
import { toast } from "sonner"
import { ideApi, type IDEPod } from "@/api/ide"
import { podApi } from "@/api/pod"
import { wsUrl } from "@/api/client"
import { getToken } from "@/lib/auth"
import { cn } from "@/lib/utils"

// ---------------- 面板相关类型与工具 ----------------

type PanelKey = "files" | "ai" | "terminal" | "logs"

// 数值区间限制，避免拖拽把面板拖没
function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value))
}

// 面板开关配置
const PANEL_TOGGLES: Array<{ key: PanelKey; label: string; icon: typeof FolderTree }> = [
  { key: "files", label: "文件树", icon: FolderTree },
  { key: "ai", label: "AI 助手", icon: Sparkles },
  { key: "terminal", label: "终端", icon: SquareTerminal },
  { key: "logs", label: "日志", icon: ScrollText },
]

// ---------------- 文件树（固定 home / shared） ----------------

interface FileNode {
  name: string
  type: "file" | "dir"
  children?: FileNode[]
  content?: string
}

// 固定目录结构：home、shared 始终存在；文件内容为本地草稿占位
const FILE_TREE: FileNode[] = [
  {
    name: "home",
    type: "dir",
    children: [
      {
        name: "workspace",
        type: "dir",
        children: [
          {
            name: "main.go",
            type: "file",
            content: [
              "package main",
              "",
              'import "fmt"',
              "",
              "func main() {",
              '    fmt.Println("Hello from SSEDeploy Cloud IDE")',
              "}",
            ].join("\n"),
          },
          {
            name: "README.md",
            type: "file",
            content: "# Home 工作区\n\n这是云 IDE 的默认工作目录，文件内容为本地草稿占位。",
          },
        ],
      },
      {
        name: "notes.md",
        type: "file",
        content: "# 开发笔记\n\n- 终端与日志面板已连接真实 Pod\n- 文件树与编辑器为界面占位",
      },
    ],
  },
  {
    name: "shared",
    type: "dir",
    children: [
      {
        name: "README.md",
        type: "file",
        content: "# Shared 共享目录\n\n用于团队共享文件的固定目录。",
      },
    ],
  },
]

// 把文件树拍平成 path -> 内容，便于编辑器读取与本地修改
function flattenFiles(nodes: FileNode[], parent = ""): Record<string, string> {
  const result: Record<string, string> = {}
  for (const node of nodes) {
    const path = parent ? `${parent}/${node.name}` : node.name
    if (node.type === "dir" && node.children) {
      Object.assign(result, flattenFiles(node.children, path))
    } else if (node.content !== undefined) {
      result[path] = node.content
    }
  }
  return result
}

const INITIAL_CONTENTS = flattenFiles(FILE_TREE)

// 递归渲染文件树节点
function TreeNode({
  node,
  path,
  depth,
  expanded,
  selected,
  onToggle,
  onSelect,
}: {
  node: FileNode
  path: string
  depth: number
  expanded: Record<string, boolean>
  selected: string
  onToggle: (path: string) => void
  onSelect: (path: string) => void
}) {
  // 目录默认展开
  const open = expanded[path] ?? true
  const padding = { paddingLeft: `${depth * 12 + 10}px` }

  if (node.type === "dir") {
    return (
      <div>
        <button
          type="button"
          onClick={() => onToggle(path)}
          className="flex w-full items-center gap-1.5 py-1 text-xs text-slate-300 hover:bg-slate-800/60"
          style={padding}
        >
          {open ? <ChevronDown className="h-3.5 w-3.5 shrink-0" /> : <ChevronRight className="h-3.5 w-3.5 shrink-0" />}
          {open ? <FolderOpen className="h-3.5 w-3.5 shrink-0 text-amber-400" /> : <Folder className="h-3.5 w-3.5 shrink-0 text-amber-400" />}
          <span className="truncate">{node.name}</span>
        </button>
        {open &&
          node.children?.map((child) => (
            <TreeNode
              key={child.name}
              node={child}
              path={`${path}/${child.name}`}
              depth={depth + 1}
              expanded={expanded}
              selected={selected}
              onToggle={onToggle}
              onSelect={onSelect}
            />
          ))}
      </div>
    )
  }

  return (
    <button
      type="button"
      onClick={() => onSelect(path)}
      className={cn(
        "flex w-full items-center gap-1.5 py-1 text-xs",
        selected === path ? "bg-blue-600/20 text-blue-300" : "text-slate-400 hover:bg-slate-800/60 hover:text-slate-200",
      )}
      style={{ paddingLeft: `${depth * 12 + 10}px` }}
    >
      <FileCode2 className="h-3.5 w-3.5 shrink-0" />
      <span className="truncate">{node.name}</span>
    </button>
  )
}

// ---------------- 可拖拽分隔条 ----------------

function DragHandle({ direction, onDelta }: { direction: "horizontal" | "vertical"; onDelta: (delta: number) => void }) {
  function handleMouseDown(event: React.MouseEvent) {
    event.preventDefault()
    const horizontal = direction === "horizontal"
    let last = horizontal ? event.clientX : event.clientY

    // 拖拽期间锁定光标与文本选中
    document.body.style.cursor = horizontal ? "col-resize" : "row-resize"
    document.body.style.userSelect = "none"

    function handleMove(moveEvent: MouseEvent) {
      const current = horizontal ? moveEvent.clientX : moveEvent.clientY
      const delta = current - last
      if (delta !== 0) {
        last = current
        onDelta(delta)
      }
    }
    function handleUp() {
      window.removeEventListener("mousemove", handleMove)
      window.removeEventListener("mouseup", handleUp)
      document.body.style.cursor = ""
      document.body.style.userSelect = ""
    }
    window.addEventListener("mousemove", handleMove)
    window.addEventListener("mouseup", handleUp)
  }

  return (
    <div
      onMouseDown={handleMouseDown}
      className={cn(
        "shrink-0 bg-slate-800/70 transition-colors hover:bg-blue-500/70",
        direction === "horizontal" ? "w-1 cursor-col-resize" : "h-1 cursor-row-resize",
      )}
    />
  )
}

// ---------------- 代码编辑器 ----------------

function CodeEditor({ path, value, onChange }: { path: string; value: string; onChange: (value: string) => void }) {
  const lines = Math.max(value.split("\n").length, 1)
  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex h-9 shrink-0 items-center gap-2 border-b border-slate-800 bg-[#0f172a] px-3 text-xs text-slate-400">
        <FileCode2 className="h-3.5 w-3.5 text-blue-400" />
        <span className="truncate font-mono">{path}</span>
        <span className="ml-auto shrink-0 rounded bg-slate-800 px-1.5 py-0.5 text-[10px] text-slate-400">本地草稿</span>
      </div>
      <div className="flex min-h-0 flex-1 overflow-auto bg-[#0b1120] font-mono text-[13px] leading-6">
        {/* 行号列 */}
        <div className="shrink-0 select-none border-r border-slate-800/70 px-3 py-3 text-right text-slate-600">
          {Array.from({ length: lines }).map((_, index) => (
            <div key={index}>{index + 1}</div>
          ))}
        </div>
        {/* 编辑区：内容为本地草稿，不入库 */}
        <textarea
          value={value}
          onChange={(event) => onChange(event.target.value)}
          spellCheck={false}
          wrap="off"
          style={{ height: `${lines * 24 + 24}px` }}
          className="flex-1 resize-none overflow-hidden bg-transparent px-3 py-3 text-slate-200 outline-none"
        />
      </div>
    </div>
  )
}

// ---------------- AI 助手面板（本地 UI 演示） ----------------

interface ChatMessage {
  role: "user" | "assistant"
  text: string
}

function AIPanel() {
  const [messages, setMessages] = useState<ChatMessage[]>([
    { role: "assistant", text: "你好，我是 AI 编程助手。当前为 UI 演示面板，尚未接入模型服务。" },
  ])
  const [input, setInput] = useState("")
  const scrollRef = useRef<HTMLDivElement>(null)

  // 新消息自动滚动到底部
  useEffect(() => {
    const element = scrollRef.current
    if (element) element.scrollTop = element.scrollHeight
  }, [messages])

  function send() {
    const text = input.trim()
    if (!text) return
    setMessages((prev) => [
      ...prev,
      { role: "user", text },
      { role: "assistant", text: "AI 服务尚未接入，当前仅展示面板交互。" },
    ])
    setInput("")
  }

  return (
    <div className="flex h-full min-h-0 flex-col bg-[#0f172a]">
      <div className="flex h-9 shrink-0 items-center gap-2 border-b border-slate-800 px-3 text-xs font-medium text-slate-300">
        <Bot className="h-4 w-4 text-violet-400" />
        AI 助手
      </div>
      <div ref={scrollRef} className="min-h-0 flex-1 space-y-3 overflow-auto p-3">
        {messages.map((message, index) => (
          <div
            key={index}
            className={cn(
              "rounded-lg px-3 py-2 text-xs leading-relaxed",
              message.role === "user"
                ? "ml-6 bg-blue-600/20 text-blue-100"
                : "mr-4 bg-slate-800/80 text-slate-300",
            )}
          >
            {message.text}
          </div>
        ))}
      </div>
      <div className="shrink-0 border-t border-slate-800 p-2">
        <div className="flex items-end gap-2 rounded-lg border border-slate-700 bg-slate-900/80 p-2">
          <textarea
            value={input}
            onChange={(event) => setInput(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && !event.shiftKey) {
                event.preventDefault()
                send()
              }
            }}
            rows={2}
            placeholder="向 AI 助手提问…"
            className="max-h-24 min-h-[38px] flex-1 resize-none bg-transparent text-xs text-slate-200 placeholder:text-slate-500 outline-none"
          />
          <button
            type="button"
            onClick={send}
            className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-blue-600 text-white hover:bg-blue-500"
            title="发送"
          >
            <Send className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>
    </div>
  )
}

// ---------------- 终端面板（WebSocket -> SPDY exec） ----------------

function TerminalPanel({ podName }: { podName: string }) {
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
        background: "#0b1120",
        foreground: "#e2e8f0",
        cursor: "#60a5fa",
        selectionBackground: "#334155",
      },
    })
    const fitAddon = new FitAddon()
    term.loadAddon(fitAddon)
    term.open(container)
    fitAddon.fit()

    // 携带 JWT 建立终端连接；后端会校验 Pod 归属
    const socket = new WebSocket(
      wsUrl(`/pods/${encodeURIComponent(podName)}/terminal?token=${encodeURIComponent(getToken() || "")}&shell=/bin/sh`),
    )

    function sendResize() {
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

    const inputDisposable = term.onData((data) => {
      if (socket.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({ type: "input", data }))
      }
    })

    // 容器尺寸变化（拖拽/面板开关）时同步远端 pty
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
  }, [podName, reloadKey])

  return (
    <div className="flex h-full min-h-0 flex-col bg-[#0b1120]">
      <div className="flex h-9 shrink-0 items-center justify-between border-b border-slate-800 px-3">
        <div className="flex items-center gap-2 text-xs text-slate-400">
          <SquareTerminal className="h-3.5 w-3.5 text-emerald-400" />
          <span className="font-medium text-slate-300">终端</span>
          <span className={cn("h-1.5 w-1.5 rounded-full", connected ? "bg-emerald-500" : "bg-slate-600")} />
          <span>{connected ? "已连接" : "未连接"}</span>
        </div>
        <button
          type="button"
          onClick={() => setReloadKey((key) => key + 1)}
          className="text-slate-500 hover:text-slate-200"
          title="重新连接"
        >
          <RotateCw className="h-3.5 w-3.5" />
        </button>
      </div>
      <div ref={containerRef} className="min-h-0 flex-1 overflow-hidden p-2" />
    </div>
  )
}

// ---------------- 日志面板（WebSocket 实时推送） ----------------

function LogsPanel({ podName }: { podName: string }) {
  const [content, setContent] = useState("")
  const [connected, setConnected] = useState(false)
  const [paused, setPaused] = useState(false)
  const [loading, setLoading] = useState(true)
  const [reloadKey, setReloadKey] = useState(0)

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
        const history = await podApi.logs(podName, 300)
        if (!disposed) setContent(history.content)
      } catch (error) {
        if (!disposed) toast.error((error as Error).message)
      } finally {
        if (!disposed) setLoading(false)
      }
      if (disposed) return

      // 实时日志流
      socket = new WebSocket(wsUrl(`/pods/${encodeURIComponent(podName)}/logs/stream?token=${encodeURIComponent(getToken() || "")}`))
      socket.onopen = () => setConnected(true)
      socket.onmessage = (event) => {
        if (pausedRef.current) return
        setContent((prev) => prev + event.data)
      }
      socket.onclose = () => setConnected(false)
      socket.onerror = () => setConnected(false)
    }

    void start()
    return () => {
      disposed = true
      socket?.close()
    }
  }, [podName, reloadKey])

  // 未暂停时自动滚动到底部
  useEffect(() => {
    if (paused) return
    const element = scrollRef.current
    if (element) element.scrollTop = element.scrollHeight
  }, [content, paused])

  return (
    <div className="flex h-full min-h-0 flex-col bg-[#0b1120]">
      <div className="flex h-9 shrink-0 items-center justify-between border-b border-slate-800 px-3">
        <div className="flex items-center gap-2 text-xs text-slate-400">
          <ScrollText className="h-3.5 w-3.5 text-amber-400" />
          <span className="font-medium text-slate-300">日志</span>
          <span className={cn("h-1.5 w-1.5 rounded-full", connected ? "bg-emerald-500" : "bg-slate-600")} />
          <span>{connected ? "实时" : "未连接"}</span>
        </div>
        <div className="flex items-center gap-2 text-slate-500">
          <button type="button" onClick={() => setPaused((value) => !value)} title={paused ? "继续" : "暂停"} className="hover:text-slate-200">
            {paused ? <Play className="h-3.5 w-3.5" /> : <Pause className="h-3.5 w-3.5" />}
          </button>
          <button type="button" onClick={() => setContent("")} title="清空" className="hover:text-slate-200">
            <Eraser className="h-3.5 w-3.5" />
          </button>
          <button type="button" onClick={() => setReloadKey((key) => key + 1)} title="重连" className="hover:text-slate-200">
            <RotateCw className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>
      <pre ref={scrollRef} className="min-h-0 flex-1 overflow-auto whitespace-pre-wrap break-all p-3 font-mono text-[11px] leading-relaxed text-slate-300">
        {loading && content === "" ? "正在加载日志…" : content || "暂无日志输出"}
      </pre>
    </div>
  )
}

// ---------------- 主页面 ----------------

export default function IDE() {
  const navigate = useNavigate()

  const [pods, setPods] = useState<IDEPod[]>([])
  const [podName, setPodName] = useState("")
  const [loading, setLoading] = useState(true)

  // 面板显隐
  const [panels, setPanels] = useState<Record<PanelKey, boolean>>({
    files: true,
    ai: true,
    terminal: true,
    logs: true,
  })

  // 拖拽尺寸
  const [filesWidth, setFilesWidth] = useState(240)
  const [aiWidth, setAiWidth] = useState(300)
  const [bottomHeight, setBottomHeight] = useState(260)

  // 文件树展开状态与当前文件
  const [expanded, setExpanded] = useState<Record<string, boolean>>({})
  const [selectedFile, setSelectedFile] = useState("home/workspace/main.go")
  const [contents, setContents] = useState<Record<string, string>>({ ...INITIAL_CONTENTS })

  // 初始化：拉取当前用户自己的 Pod，默认选中第一个运行中的 Pod
  useEffect(() => {
    let alive = true
    async function load() {
      setLoading(true)
      try {
        const result = await ideApi.pods()
        if (!alive) return
        setPods(result.items)
        const preferred = result.items.find((item) => item.running) ?? result.items[0]
        setPodName(preferred?.name ?? "")
      } catch (error) {
        if (alive) toast.error((error as Error).message)
      } finally {
        if (alive) setLoading(false)
      }
    }
    void load()
    return () => {
      alive = false
    }
  }, [])

  const selectedPod = useMemo(() => pods.find((item) => item.name === podName) ?? null, [pods, podName])
  const runningPods = useMemo(() => pods.filter((item) => item.running), [pods])

  function togglePanel(key: PanelKey) {
    setPanels((prev) => ({ ...prev, [key]: !prev[key] }))
  }

  // 顶部栏：返回按钮 + Pod 选择器 + 面板开关
  const header = (
    <header className="flex h-12 shrink-0 items-center justify-between gap-3 border-b border-slate-800 bg-[#0f172a] px-3">
      <div className="flex min-w-0 items-center gap-3">
        <button
          type="button"
          onClick={() => navigate("/pods")}
          className="flex h-8 w-8 items-center justify-center rounded-md text-slate-400 hover:bg-slate-800 hover:text-slate-100"
          title="返回 Pod 列表"
        >
          <ArrowLeft className="h-4 w-4" />
        </button>
        <span className="hidden shrink-0 text-sm font-semibold tracking-tight text-slate-200 sm:block">Cloud IDE</span>

        {/* Pod 下拉选择器：列出当前用户全部 Pod */}
        <div className="relative">
          <select
            value={podName}
            onChange={(event) => setPodName(event.target.value)}
            disabled={pods.length === 0}
            className="h-8 max-w-[220px] appearance-none rounded-md border border-slate-700 bg-slate-900 pl-3 pr-7 text-xs text-slate-200 outline-none focus:border-blue-500 disabled:opacity-50"
          >
            {pods.length === 0 && <option value="">暂无 Pod</option>}
            {pods.map((pod) => (
              <option key={pod.name} value={pod.name}>
                {pod.name} · {pod.statusText}
              </option>
            ))}
          </select>
          <ChevronDown className="pointer-events-none absolute right-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-slate-500" />
        </div>

        {selectedPod && (
          <span className="hidden items-center gap-1.5 text-[11px] text-slate-500 md:flex">
            <span className={cn("h-1.5 w-1.5 rounded-full", selectedPod.running ? "bg-emerald-500" : "bg-slate-500")} />
            {selectedPod.podName || selectedPod.statusText}
          </span>
        )}
      </div>

      {/* 右上角：四个面板独立开关 */}
      <div className="flex shrink-0 items-center gap-1">
        {PANEL_TOGGLES.map((item) => {
          const Icon = item.icon
          const active = panels[item.key]
          return (
            <button
              key={item.key}
              type="button"
              onClick={() => togglePanel(item.key)}
              title={`${active ? "关闭" : "开启"}${item.label}`}
              className={cn(
                "flex h-8 items-center gap-1.5 rounded-md border px-2 text-xs transition-colors",
                active
                  ? "border-blue-500/40 bg-blue-600/20 text-blue-300"
                  : "border-transparent text-slate-500 hover:bg-slate-800 hover:text-slate-300",
              )}
            >
              <Icon className="h-3.5 w-3.5" />
              <span className="hidden lg:inline">{item.label}</span>
            </button>
          )
        })}
      </div>
    </header>
  )

  // 加载态
  if (loading) {
    return (
      <div className="flex h-screen flex-col bg-[#0b1120] text-slate-200">
        {header}
        <div className="flex flex-1 items-center justify-center gap-2 text-sm text-slate-400">
          <Loader2 className="h-4 w-4 animate-spin" />
          正在加载开发环境…
        </div>
      </div>
    )
  }

  // 无 Pod：提示创建并跳转 Pod 管理
  if (pods.length === 0) {
    return (
      <div className="flex h-screen flex-col bg-[#0b1120] text-slate-200">
        {header}
        <div className="flex flex-1 items-center justify-center p-6">
          <div className="w-full max-w-md rounded-2xl border border-slate-800 bg-[#0f172a] p-10 text-center">
            <div className="mx-auto flex h-14 w-14 items-center justify-center rounded-2xl bg-slate-800 text-slate-400">
              <Box className="h-7 w-7" />
            </div>
            <h1 className="mt-5 text-lg font-semibold text-slate-100">暂无可用 Pod</h1>
            <p className="mt-2 text-sm text-slate-400">
              请先在 Pod 管理中创建并启动一个 Pod，云 IDE 会自动加载对应的开发环境。
            </p>
            <button
              type="button"
              onClick={() => navigate("/pods")}
              className="mt-6 rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-500"
            >
              创建 / 管理 Pod
            </button>
          </div>
        </div>
      </div>
    )
  }

  // 有 Pod 但当前选中项未运行：保留顶部栏，提示启动
  if (!selectedPod?.running) {
    return (
      <div className="flex h-screen flex-col bg-[#0b1120] text-slate-200">
        {header}
        <div className="flex flex-1 items-center justify-center p-6">
          <div className="w-full max-w-md rounded-2xl border border-slate-800 bg-[#0f172a] p-10 text-center">
            <div className="mx-auto flex h-14 w-14 items-center justify-center rounded-2xl bg-amber-500/10 text-amber-400">
              <Box className="h-7 w-7" />
            </div>
            <h1 className="mt-5 text-lg font-semibold text-slate-100">当前 Pod 未运行</h1>
            <p className="mt-2 text-sm text-slate-400">
              {runningPods.length > 0
                ? "可在顶部下拉切换到运行中的 Pod，或前往 Pod 管理启动当前 Pod。"
                : "当前账号下没有运行中的 Pod，请前往 Pod 管理启动一个 Pod。"}
            </p>
            <button
              type="button"
              onClick={() => navigate("/pods")}
              className="mt-6 rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-500"
            >
              前往 Pod 管理
            </button>
          </div>
        </div>
      </div>
    )
  }

  // 工作台
  return (
    <div className="flex h-screen flex-col overflow-hidden bg-[#0b1120] text-slate-200">
      {header}

      <div className="flex min-h-0 flex-1">
        {/* 左侧文件树 */}
        {panels.files && (
          <>
            <aside className="flex min-h-0 shrink-0 flex-col border-r border-slate-800 bg-[#0f172a]" style={{ width: filesWidth }}>
              <div className="flex h-9 shrink-0 items-center gap-2 border-b border-slate-800 px-3 text-xs font-medium text-slate-300">
                <FolderTree className="h-4 w-4 text-blue-400" />
                资源管理器
              </div>
              <div className="min-h-0 flex-1 overflow-auto py-1">
                {FILE_TREE.map((node) => (
                  <TreeNode
                    key={node.name}
                    node={node}
                    path={node.name}
                    depth={0}
                    expanded={expanded}
                    selected={selectedFile}
                    onToggle={(path) => setExpanded((prev) => ({ ...prev, [path]: !(prev[path] ?? true) }))}
                    onSelect={setSelectedFile}
                  />
                ))}
              </div>
            </aside>
            <DragHandle direction="horizontal" onDelta={(delta) => setFilesWidth((width) => clamp(width + delta, 180, 420))} />
          </>
        )}

        {/* 中间：编辑器 + 底部终端 / 日志 */}
        <div className="flex min-h-0 min-w-0 flex-1 flex-col">
          <div className="flex min-h-0 flex-1">
            <section className="min-h-0 min-w-0 flex-1">
              <CodeEditor
                path={selectedFile}
                value={contents[selectedFile] ?? ""}
                onChange={(value) => setContents((prev) => ({ ...prev, [selectedFile]: value }))}
              />
            </section>

            {/* 右侧 AI 助手 */}
            {panels.ai && (
              <>
                <DragHandle direction="horizontal" onDelta={(delta) => setAiWidth((width) => clamp(width - delta, 220, 480))} />
                <aside className="min-h-0 shrink-0 border-l border-slate-800" style={{ width: aiWidth }}>
                  <AIPanel />
                </aside>
              </>
            )}
          </div>

          {/* 底部终端 + 日志 */}
          {(panels.terminal || panels.logs) && (
            <>
              <DragHandle direction="vertical" onDelta={(delta) => setBottomHeight((height) => clamp(height - delta, 140, 560))} />
              <div className="flex shrink-0 border-t border-slate-800" style={{ height: bottomHeight }}>
                {panels.terminal && (
                  <div className={cn("min-h-0 min-w-0 flex-1", panels.logs && "border-r border-slate-800")}>
                    <TerminalPanel podName={selectedPod.name} />
                  </div>
                )}
                {panels.logs && (
                  <div className="min-h-0 min-w-0 flex-1">
                    <LogsPanel podName={selectedPod.name} />
                  </div>
                )}
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  )
}
