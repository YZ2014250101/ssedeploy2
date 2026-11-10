/**
 * 云 IDE 工作台（独立全屏页面，支持全局明暗主题）
 *
 * 主题说明：
 * - 页面不再固定深色：亮色为基础样式 + dark: 变体，跟随 <html class="dark">；
 * - 明暗开关来自个人页（lib/theme.ts 写入 html 根节点），本页自动跟随；
 * - xterm 终端由 JS 配置配色，通过 useResolvedTheme 订阅全局主题实时切换；
 * - 亮色配色与平台其它页面一致：slate-50 页面底 + 白色面板 + slate-200 细边框。
 *
 * 数据链路（关键）：
 * 1. 平台创建的 Pod 会把数据盘 PVC 挂载到容器 /home/workspace；
 * 2. 文件树与编辑器通过后端 exec 接口读写该目录；
 * 3. Web 终端同样 exec 进同一个主容器并 cd 到 /home/workspace，
 *    因此编辑器保存的文件在终端立即可见，二者共享同一份持久化数据。
 *
 * 其他逻辑：
 * - 进入 /ide 先请求当前用户自己的 Pod 列表，无 Pod 时展示空态并跳转 /pods；
 * - 顶部四个按钮独立开关文件树、AI 助手、终端、日志面板；
 * - 三条拖拽分隔条可调整文件树宽度、AI 面板宽度、底部面板高度。
 */
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useNavigate } from "react-router-dom"
import { Terminal as XTerm } from "@xterm/xterm"
import { FitAddon } from "@xterm/addon-fit"
import {
  ArrowLeft,
  Bot,
  Box,
  ChevronDown,
  ChevronRight,
  Download,
  Eraser,
  FileCode2,
  FilePlus2,
  Folder,
  FolderOpen,
  FolderPlus,
  FolderTree,
  Loader2,
  Pause,
  Pencil,
  Play,
  RotateCw,
  Save,
  ScrollText,
  Send,
  Sparkles,
  SquareTerminal,
  Trash2,
  X,
} from "lucide-react"
import "@xterm/xterm/css/xterm.css"
import { toast } from "sonner"
import { downloadWorkspaceFile, ideApi, type IDEFileEntry, type IDEPod, type IDERoot } from "@/api/ide"
import { podApi } from "@/api/pod"
import { wsUrl } from "@/api/client"
import { getToken } from "@/lib/auth"
import { useResolvedTheme } from "@/lib/theme"
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

// 固定目录根：与容器内路径一一对应
const ROOTS: Array<{ key: IDERoot; label: string; hint: string }> = [
  { key: "home", label: "home", hint: "/home/workspace" },
  { key: "shared", label: "shared", hint: "/home/workspace/shared" },
]

// 空工作区首次进入时写入的示例文件（与终端共享，可直接在终端看到）
const SEED_FILES: Array<{ root: IDERoot; path: string; content: string }> = [
  {
    root: "home",
    path: "main.go",
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
    root: "home",
    path: "README.md",
    content: "# Home 工作区\n\n该目录对应容器内的 /home/workspace，与 Web 终端共享同一份数据盘。",
  },
  {
    root: "shared",
    path: "README.md",
    content: "# Shared 共享目录\n\n该目录对应容器内的 /home/workspace/shared。",
  },
]

// ---------------- 文件树 ----------------

// 前端构建的树节点：path 为相对目录根的路径
interface TreeNodeData {
  name: string
  path: string
  type: "file" | "dir"
  children?: TreeNodeData[]
}

// 目录优先、再按名称排序
function sortNodes(nodes: TreeNodeData[]): TreeNodeData[] {
  return [...nodes]
    .sort((a, b) => (a.type === b.type ? a.name.localeCompare(b.name) : a.type === "dir" ? -1 : 1))
    .map((node) => (node.children ? { ...node, children: sortNodes(node.children) } : node))
}

// 把后端返回的扁平列表构建成树
function buildTree(entries: IDEFileEntry[]): TreeNodeData[] {
  const roots: TreeNodeData[] = []
  const dirs = new Map<string, TreeNodeData>()
  // 先按路径排序，保证父目录先于子节点创建
  const sorted = [...entries].sort((a, b) => a.path.localeCompare(b.path))

  for (const entry of sorted) {
    const segments = entry.path.split("/").filter(Boolean)
    let list = roots
    let acc = ""

    segments.forEach((segment, index) => {
      acc = acc ? `${acc}/${segment}` : segment
      const isLast = index === segments.length - 1
      if (isLast && entry.type === "file") {
        list.push({ name: segment, path: entry.path, type: "file" })
        return
      }
      let dir = dirs.get(acc)
      if (!dir) {
        dir = { name: segment, path: acc, type: "dir", children: [] }
        dirs.set(acc, dir)
        list.push(dir)
      }
      list = dir.children ?? []
    })
  }
  return sortNodes(roots)
}

// 取树中第一个文件，用于默认打开
function firstFile(nodes: TreeNodeData[]): TreeNodeData | null {
  for (const node of nodes) {
    if (node.type === "file") return node
    const child = node.children ? firstFile(node.children) : null
    if (child) return child
  }
  return null
}

// 递归渲染文件树节点
// - 文件节点：悬浮显示 重命名 / 下载 / 删除 操作按钮
// - 目录节点：点击选中（作为新建文件/文件夹的目标目录），同时展开/折叠
function TreeNode({
  node,
  root,
  depth,
  expanded,
  selectedFileKey,
  selectedDirKey,
  onToggle,
  onSelectFile,
  onSelectDir,
  onRename,
  onDownload,
  onDelete,
}: {
  node: TreeNodeData
  root: IDERoot
  depth: number
  expanded: Record<string, boolean>
  selectedFileKey: string
  selectedDirKey: string
  onToggle: (key: string) => void
  onSelectFile: (root: IDERoot, path: string) => void
  onSelectDir: (root: IDERoot, path: string) => void
  onRename: (root: IDERoot, path: string) => void
  onDownload: (root: IDERoot, path: string) => void
  onDelete: (root: IDERoot, path: string) => void
}) {
  const key = `${root}:${node.path}`
  // 目录默认展开
  const open = expanded[key] ?? true
  const padding = { paddingLeft: `${depth * 12 + 10}px` }

  if (node.type === "dir") {
    return (
      <div>
        <button
          type="button"
          onClick={() => {
            onSelectDir(root, node.path)
            onToggle(key)
          }}
          title={node.path}
          className={cn(
            "flex w-full items-center gap-1.5 py-1 text-xs",
            selectedDirKey === key ? "bg-blue-50 text-blue-700 dark:bg-blue-600/20 dark:text-blue-300" : "text-slate-700 dark:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-800/60",
          )}
          style={padding}
        >
          {open ? <ChevronDown className="h-3.5 w-3.5 shrink-0" /> : <ChevronRight className="h-3.5 w-3.5 shrink-0" />}
          {open ? (
            <FolderOpen className="h-3.5 w-3.5 shrink-0 text-amber-500 dark:text-amber-400" />
          ) : (
            <Folder className="h-3.5 w-3.5 shrink-0 text-amber-500 dark:text-amber-400" />
          )}
          <span className="truncate">{node.name}</span>
        </button>
        {open &&
          node.children?.map((child) => (
            <TreeNode
              key={`${root}:${child.path}`}
              node={child}
              root={root}
              depth={depth + 1}
              expanded={expanded}
              selectedFileKey={selectedFileKey}
              selectedDirKey={selectedDirKey}
              onToggle={onToggle}
              onSelectFile={onSelectFile}
              onSelectDir={onSelectDir}
              onRename={onRename}
              onDownload={onDownload}
              onDelete={onDelete}
            />
          ))}
      </div>
    )
  }

  return (
    <div
      className={cn(
        "group flex w-full items-center pr-1 text-xs",
        selectedFileKey === key
          ? "bg-blue-50 text-blue-700 dark:bg-blue-600/20 dark:text-blue-300"
          : "text-slate-500 dark:text-slate-400 hover:bg-slate-100 dark:hover:bg-slate-800/60 hover:text-slate-900 dark:hover:text-slate-200",
      )}
      style={padding}
    >
      <button
        type="button"
        onClick={() => onSelectFile(root, node.path)}
        title={node.path}
        className="flex min-w-0 flex-1 items-center gap-1.5 py-1 text-left"
      >
        <FileCode2 className="h-3.5 w-3.5 shrink-0" />
        <span className="truncate">{node.name}</span>
      </button>

      {/* 悬浮操作按钮：默认隐藏，鼠标悬浮文件节点时显示 */}
      <span className="hidden shrink-0 items-center gap-0.5 group-hover:flex">
        <button
          type="button"
          onClick={() => onRename(root, node.path)}
          title="重命名"
          className="rounded p-0.5 text-slate-500 hover:bg-slate-200 dark:hover:bg-slate-700 hover:text-slate-900 dark:hover:text-slate-100"
        >
          <Pencil className="h-3 w-3" />
        </button>
        <button
          type="button"
          onClick={() => onDownload(root, node.path)}
          title="下载"
          className="rounded p-0.5 text-slate-500 hover:bg-slate-200 dark:hover:bg-slate-700 hover:text-slate-900 dark:hover:text-slate-100"
        >
          <Download className="h-3 w-3" />
        </button>
        <button
          type="button"
          onClick={() => onDelete(root, node.path)}
          title="删除"
          className="rounded p-0.5 text-slate-500 hover:bg-slate-200 dark:hover:bg-slate-700 hover:text-rose-600 dark:hover:text-rose-400"
        >
          <Trash2 className="h-3 w-3" />
        </button>
      </span>
    </div>
  )
}

// ---------------- 输入弹窗（新建/重命名） ----------------

// 名称校验：单层名称，不含路径分隔符，非空且不是 . / ..
function validateEntryName(value: string): string | null {
  const name = value.trim()
  if (!name) return "名称不能为空"
  if (name === "." || name === "..") return "名称不合法"
  if (/[/\\]/.test(name)) return "名称不能包含 / 或 \\"
  if (name.length > 255) return "名称过长"
  return null
}

// PromptDialog 是一个轻量输入弹窗（明暗两套配色），用于新建与重命名。
// onSubmit 返回 true 表示操作成功，父组件据此关闭弹窗。
function PromptDialog({
  title,
  placeholder,
  initial,
  confirmText,
  busy,
  onCancel,
  onSubmit,
}: {
  title: string
  placeholder: string
  initial: string
  confirmText: string
  busy: boolean
  onCancel: () => void
  onSubmit: (value: string) => Promise<boolean>
}) {
  const [value, setValue] = useState(initial)
  const [error, setError] = useState("")

  async function submit() {
    const message = validateEntryName(value)
    if (message) {
      setError(message)
      return
    }
    const ok = await onSubmit(value.trim())
    if (!ok) return
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 px-4">
      <div className="w-full max-w-sm rounded-xl border border-slate-300 dark:border-slate-700 bg-white p-4 shadow-2xl">
        <div className="mb-3 flex items-center justify-between">
          <h3 className="text-sm font-medium text-slate-900 dark:text-slate-100">{title}</h3>
          <button type="button" onClick={onCancel} className="text-slate-500 hover:text-slate-700 dark:hover:text-slate-200" title="关闭">
            <X className="h-4 w-4" />
          </button>
        </div>
        <input
          autoFocus
          value={value}
          placeholder={placeholder}
          onChange={(event) => {
            setValue(event.target.value)
            setError("")
          }}
          onKeyDown={(event) => {
            if (event.key === "Enter") void submit()
            if (event.key === "Escape") onCancel()
          }}
          className="w-full rounded-md border border-slate-300 dark:border-slate-700 bg-slate-50 px-3 py-2 text-sm text-slate-900 dark:text-slate-100 outline-none focus:border-blue-500"
        />
        {error && <div className="mt-2 text-xs text-rose-600 dark:text-rose-400">{error}</div>}
        <div className="mt-4 flex justify-end gap-2">
          <button
            type="button"
            onClick={onCancel}
            className="rounded-md px-3 py-1.5 text-xs text-slate-700 dark:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-800"
          >
            取消
          </button>
          <button
            type="button"
            onClick={() => void submit()}
            disabled={busy}
            className="flex items-center gap-1 rounded-md bg-blue-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-blue-500 disabled:opacity-60"
          >
            {busy && <Loader2 className="h-3 w-3 animate-spin" />}
            {confirmText}
          </button>
        </div>
      </div>
    </div>
  )
}

// 文件树操作弹窗状态：
// create-file / create-dir 的 path 是目标目录；rename 的 path 是原路径
type PromptState =
  | { mode: "create-file"; root: IDERoot; path: string }
  | { mode: "create-dir"; root: IDERoot; path: string }
  | { mode: "rename"; root: IDERoot; path: string; initial: string }

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
        "shrink-0 bg-slate-200 dark:bg-slate-800/70 transition-colors hover:bg-blue-500/70",
        direction === "horizontal" ? "w-1 cursor-col-resize" : "h-1 cursor-row-resize",
      )}
    />
  )
}

// ---------------- 代码编辑器 ----------------

function CodeEditor({
  path,
  value,
  loading,
  dirty,
  saving,
  onChange,
  onSave,
}: {
  path: string
  value: string
  loading: boolean
  dirty: boolean
  saving: boolean
  onChange: (value: string) => void
  onSave: () => void
}) {
  const lines = Math.max(value.split("\n").length, 1)

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex h-9 shrink-0 items-center gap-2 border-b border-slate-200 bg-white px-3 text-xs text-slate-400">
        <FileCode2 className="h-3.5 w-3.5 text-blue-600 dark:text-blue-400" />
        <span className="truncate font-mono">{path || "未选择文件"}</span>
        {dirty && <span className="shrink-0 rounded bg-amber-100 px-1.5 py-0.5 text-[10px] text-amber-700 dark:bg-amber-500/20 dark:text-amber-300">未保存</span>}
        <button
          type="button"
          onClick={onSave}
          disabled={!path || saving || !dirty}
          className="ml-auto flex shrink-0 items-center gap-1 rounded bg-slate-100 dark:bg-slate-800 px-2 py-1 text-[11px] text-slate-700 dark:text-slate-300 hover:bg-slate-200 dark:hover:bg-slate-700 disabled:cursor-not-allowed disabled:opacity-40"
          title="保存到工作区（Ctrl/Cmd + S）"
        >
          {saving ? <Loader2 className="h-3 w-3 animate-spin" /> : <Save className="h-3 w-3" />}
          保存
        </button>
      </div>

      {loading ? (
        <div className="flex flex-1 items-center justify-center gap-2 bg-slate-50 text-xs text-slate-400">
          <Loader2 className="h-4 w-4 animate-spin" />
          正在读取文件…
        </div>
      ) : !path ? (
        <div className="flex flex-1 items-center justify-center bg-slate-50 text-xs text-slate-500">
          从左侧文件树选择文件开始编辑
        </div>
      ) : (
        <div className="flex min-h-0 flex-1 overflow-auto bg-slate-50 font-mono text-[13px] leading-6">
          {/* 行号列 */}
          <div className="shrink-0 select-none border-r border-slate-200 dark:border-slate-800/70 px-3 py-3 text-right text-slate-400 dark:text-slate-600">
            {Array.from({ length: lines }).map((_, index) => (
              <div key={index}>{index + 1}</div>
            ))}
          </div>
          {/* 编辑区：内容直接写入工作区文件 */}
          <textarea
            value={value}
            onChange={(event) => onChange(event.target.value)}
            onKeyDown={(event) => {
              // Ctrl/Cmd + S 保存
              if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "s") {
                event.preventDefault()
                onSave()
              }
            }}
            spellCheck={false}
            wrap="off"
            style={{ height: `${lines * 24 + 24}px` }}
            className="flex-1 resize-none overflow-hidden bg-transparent px-3 py-3 text-slate-800 dark:text-slate-200 outline-none"
          />
        </div>
      )}
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
    <div className="flex h-full min-h-0 flex-col bg-white">
      <div className="flex h-9 shrink-0 items-center gap-2 border-b border-slate-200 px-3 text-xs font-medium text-slate-700 dark:text-slate-300">
        <Bot className="h-4 w-4 text-violet-600 dark:text-violet-400" />
        AI 助手
      </div>
      <div ref={scrollRef} className="min-h-0 flex-1 space-y-3 overflow-auto p-3">
        {messages.map((message, index) => (
          <div
            key={index}
            className={cn(
              "rounded-lg px-3 py-2 text-xs leading-relaxed",
              message.role === "user"
                ? "ml-6 bg-blue-50 text-blue-700 dark:bg-blue-600/20 dark:text-blue-100"
                : "mr-4 bg-slate-100 dark:bg-slate-800/80 text-slate-700 dark:text-slate-300",
            )}
          >
            {message.text}
          </div>
        ))}
      </div>
      <div className="shrink-0 border-t border-slate-200 p-2">
        <div className="flex items-end gap-2 rounded-lg border border-slate-300 dark:border-slate-700 bg-slate-50 dark:bg-slate-900/80 p-2">
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
            className="max-h-24 min-h-[38px] flex-1 resize-none bg-transparent text-xs text-slate-800 dark:text-slate-200 placeholder:text-slate-400 dark:placeholder:text-slate-500 outline-none"
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

// 终端明暗两套配色：深色沿用原方案，亮色使用白底深字，与平台亮色主题一致
function terminalTheme(mode: "light" | "dark") {
  return mode === "dark"
    ? {
        background: "#0b1220",
        foreground: "#e2e8f0",
        cursor: "#60a5fa",
        selectionBackground: "#334155",
      }
    : {
        background: "#f8fafc",
        foreground: "#0f172a",
        cursor: "#2563eb",
        selectionBackground: "#bfdbfe",
      }
}

function TerminalPanel({ podName }: { podName: string }) {
  const containerRef = useRef<HTMLDivElement>(null)
  const [connected, setConnected] = useState(false)
  const [reloadKey, setReloadKey] = useState(0)

  // 主题改动：订阅全局明暗，xterm 需要在实例上动态更新配色
  const resolvedTheme = useResolvedTheme()
  const termRef = useRef<XTerm | null>(null)
  const themeRef = useRef<"light" | "dark">(resolvedTheme)

  // 主题切换时同步已创建终端的配色（不重建连接，避免丢失会话）
  useEffect(() => {
    themeRef.current = resolvedTheme
    if (termRef.current) {
      termRef.current.options.theme = terminalTheme(resolvedTheme)
    }
  }, [resolvedTheme])

  useEffect(() => {
    const container = containerRef.current
    if (!container) return

    // disposed 防止 React 严格模式二次挂载 / 卸载后残留回调继续操作已销毁实例
    let disposed = false
    let term: XTerm | null = null
    let fitAddon: FitAddon | null = null
    let socket: WebSocket | null = null
    let inputDisposable: { dispose(): void } | null = null

    function sendResize() {
      if (disposed || !term || !socket || socket.readyState !== WebSocket.OPEN) return
      socket.send(JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows }))
    }

    // 创建 xterm 并建立 WebSocket；只有容器已具备尺寸时才调用
    function start() {
      if (disposed || term || !container) return

      const instance = new XTerm({
        fontSize: 13,
        fontFamily: '"JetBrains Mono", Menlo, Consolas, monospace',
        cursorBlink: true,
        convertEol: true,
        // 主题改动：初始配色取当前全局主题
        theme: terminalTheme(themeRef.current),
      })
      const fit = new FitAddon()
      instance.loadAddon(fit)
      instance.open(container)
      fit.fit()
      term = instance
      termRef.current = instance
      fitAddon = fit

      // 携带 JWT 建立终端连接；后端会校验 Pod 归属并切到 /home/workspace
      const ws = new WebSocket(
        wsUrl(`/pods/${encodeURIComponent(podName)}/terminal?token=${encodeURIComponent(getToken() || "")}&shell=/bin/sh`),
      )
      socket = ws
      setConnected(false)

      ws.onopen = () => {
        setConnected(true)
        sendResize()
        instance.focus()
      }
      ws.onmessage = (event) => {
        if (disposed) return
        instance.write(typeof event.data === "string" ? event.data : new Uint8Array(event.data))
      }
      ws.onclose = () => {
        setConnected(false)
        if (!disposed) instance.writeln("\r\n\x1b[33m[连接已断开]\x1b[0m")
      }
      ws.onerror = () => {
        setConnected(false)
        if (!disposed) instance.writeln("\r\n\x1b[31m[连接异常，请确认 Pod 正在运行]\x1b[0m")
      }

      inputDisposable = instance.onData((data) => {
        if (ws.readyState === WebSocket.OPEN) {
          ws.send(JSON.stringify({ type: "input", data }))
        }
      })
    }

    // 容器宽高为 0（面板隐藏或布局尚未完成）时先不创建 xterm：
    // 0 尺寸下 open/write 会让 xterm 渲染器拿不到 dimensions 而抛 TypeError
    if (container.clientWidth > 0 && container.clientHeight > 0) start()

    // 容器尺寸变化（拖拽/面板开关/首次可见）时初始化并同步远端 pty
    const observer = new ResizeObserver(() => {
      if (disposed) return
      if (!term) {
        if (container.clientWidth > 0 && container.clientHeight > 0) start()
        return
      }
      if (container.clientWidth === 0 || container.clientHeight === 0) return
      fitAddon?.fit()
      sendResize()
    })
    observer.observe(container)

    return () => {
      disposed = true
      observer.disconnect()
      inputDisposable?.dispose()
      socket?.close()
      term?.dispose()
      // 主题切换 effect 可能晚于卸载执行，这里清空引用避免操作已销毁实例
      termRef.current = null
      // 清掉 xterm 注入的 DOM，避免严格模式二次挂载时终端叠加互相遮挡
      container.innerHTML = ""
    }
  }, [podName, reloadKey])

  return (
    <div className="flex h-full min-h-0 flex-col bg-slate-50">
      <div className="flex h-9 shrink-0 items-center justify-between border-b border-slate-200 px-3">
        <div className="flex items-center gap-2 text-xs text-slate-400">
          <SquareTerminal className="h-3.5 w-3.5 text-emerald-600 dark:text-emerald-400" />
          <span className="font-medium text-slate-700 dark:text-slate-300">终端</span>
          <span className={cn("h-1.5 w-1.5 rounded-full", connected ? "bg-emerald-500" : "bg-slate-400 dark:bg-slate-600")} />
          <span>{connected ? "已连接" : "未连接"}</span>
        </div>
        <button
          type="button"
          onClick={() => setReloadKey((key) => key + 1)}
          className="text-slate-500 hover:text-slate-700 dark:hover:text-slate-200"
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
    <div className="flex h-full min-h-0 flex-col bg-slate-50">
      <div className="flex h-9 shrink-0 items-center justify-between border-b border-slate-200 px-3">
        <div className="flex items-center gap-2 text-xs text-slate-400">
          <ScrollText className="h-3.5 w-3.5 text-amber-500 dark:text-amber-400" />
          <span className="font-medium text-slate-700 dark:text-slate-300">日志</span>
          <span className={cn("h-1.5 w-1.5 rounded-full", connected ? "bg-emerald-500" : "bg-slate-400 dark:bg-slate-600")} />
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
      <pre ref={scrollRef} className="min-h-0 flex-1 overflow-auto whitespace-pre-wrap break-all p-3 font-mono text-[11px] leading-relaxed text-slate-700 dark:text-slate-300">
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

  // 文件树
  const [trees, setTrees] = useState<Record<IDERoot, TreeNodeData[]>>({ home: [], shared: [] })
  const [rootOpen, setRootOpen] = useState<Record<IDERoot, boolean>>({ home: true, shared: true })
  const [expanded, setExpanded] = useState<Record<string, boolean>>({})
  const [treeLoading, setTreeLoading] = useState(false)
  // 当前选中的目录（新建文件/文件夹的目标），默认工作区根
  const [selectedDir, setSelectedDir] = useState<{ root: IDERoot; path: string }>({ root: "home", path: "" })
  // 新建/重命名弹窗状态
  const [prompt, setPrompt] = useState<PromptState | null>(null)
  const [promptBusy, setPromptBusy] = useState(false)

  // 编辑器
  const [openFile, setOpenFile] = useState<{ root: IDERoot; path: string } | null>(null)
  const [fileContent, setFileContent] = useState("")
  const [fileLoading, setFileLoading] = useState(false)
  const [dirty, setDirty] = useState(false)
  const [saving, setSaving] = useState(false)

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

  // 同时拉取两个固定目录根
  const fetchTrees = useCallback(async (name: string): Promise<Record<IDERoot, TreeNodeData[]>> => {
    const [home, shared] = await Promise.all([ideApi.files(name, "home"), ideApi.files(name, "shared")])
    return { home: buildTree(home.items), shared: buildTree(shared.items) }
  }, [])

  // 刷新文件树；seed=true 时在空工作区写入示例文件
  const refreshTrees = useCallback(
    async (name: string, seed: boolean) => {
      setTreeLoading(true)
      try {
        let tree = await fetchTrees(name)
        const empty = tree.home.length === 0 && tree.shared.length === 0
        if (seed && empty) {
          for (const seedFile of SEED_FILES) {
            await ideApi.saveFile(name, seedFile.root, seedFile.path, seedFile.content)
          }
          tree = await fetchTrees(name)
        }
        setTrees(tree)

        // 首次进入默认打开 home 下第一个文件
        setOpenFile((current) => {
          if (current) return current
          const first = firstFile(tree.home)
          return first ? { root: "home", path: first.path } : null
        })
      } catch (error) {
        toast.error((error as Error).message)
      } finally {
        setTreeLoading(false)
      }
    },
    [fetchTrees],
  )

  // 切换 Pod 后重新加载工作区
  useEffect(() => {
    if (!selectedPod?.running) return
    setOpenFile(null)
    setFileContent("")
    setDirty(false)
    // 切换 Pod 时重置目录选择与弹窗，避免把 A 的路径带到 B
    setSelectedDir({ root: "home", path: "" })
    setPrompt(null)
    void refreshTrees(selectedPod.name, true)
  }, [selectedPod?.name, selectedPod?.running, refreshTrees])

  // 选中文件后读取内容
  useEffect(() => {
    if (!openFile || !selectedPod?.running) return
    let alive = true
    setFileLoading(true)
    ideApi
      .file(selectedPod.name, openFile.root, openFile.path)
      .then((result) => {
        if (!alive) return
        setFileContent(result.content)
        setDirty(false)
      })
      .catch((error) => {
        if (alive) toast.error((error as Error).message)
      })
      .finally(() => {
        if (alive) setFileLoading(false)
      })
    return () => {
      alive = false
    }
  }, [openFile, selectedPod?.name, selectedPod?.running])

  function togglePanel(key: PanelKey) {
    setPanels((prev) => ({ ...prev, [key]: !prev[key] }))
  }

  // 保存到工作区：写的是 PVC，终端立即可见
  async function handleSave() {
    if (!openFile || !selectedPod || saving) return
    setSaving(true)
    try {
      await ideApi.saveFile(selectedPod.name, openFile.root, openFile.path, fileContent)
      setDirty(false)
      toast.success("已保存到工作区")
      void refreshTrees(selectedPod.name, false)
    } catch (error) {
      toast.error((error as Error).message)
    } finally {
      setSaving(false)
    }
  }

  // ---------------- 文件树操作 ----------------

  // 新建入口：目标目录取当前选中的目录（默认工作区根）
  function openCreateDialog(mode: "create-file" | "create-dir") {
    if (!selectedPod?.running) {
      toast.error("当前没有运行中的 Pod")
      return
    }
    setPrompt({ mode, root: selectedDir.root, path: selectedDir.path })
  }

  // 重命名入口
  function openRenameDialog(root: IDERoot, path: string) {
    if (!selectedPod?.running) return
    setPrompt({ mode: "rename", root, path, initial: path.split("/").pop() ?? "" })
  }

  // 弹窗提交：新建文件 / 新建文件夹 / 重命名；返回 true 表示成功并关闭弹窗
  async function submitPrompt(value: string): Promise<boolean> {
    const task = prompt
    if (!task || !selectedPod) return false
    setPromptBusy(true)
    try {
      if (task.mode === "rename") {
        await ideApi.renameEntry(selectedPod.name, task.root, task.path, value)
        // 同步修正编辑器里正在打开的文件路径（重命名目录时连带子文件）
        const oldPath = task.path
        const parent = oldPath.includes("/") ? oldPath.slice(0, oldPath.lastIndexOf("/")) : ""
        const newPath = parent ? `${parent}/${value}` : value
        setOpenFile((current) => {
          if (!current || current.root !== task.root) return current
          if (current.path === oldPath) return { root: current.root, path: newPath }
          if (current.path.startsWith(`${oldPath}/`)) {
            return { root: current.root, path: newPath + current.path.slice(oldPath.length) }
          }
          return current
        })
        toast.success("已重命名")
      } else {
        const type = task.mode === "create-dir" ? "dir" : "file"
        await ideApi.createEntry(selectedPod.name, task.root, task.path, value, type)
        // 展开目标目录，新建文件时直接打开
        setExpanded((prev) => ({ ...prev, [`${task.root}:${task.path}`]: true }))
        if (type === "file") {
          const newPath = task.path ? `${task.path}/${value}` : value
          setOpenFile({ root: task.root, path: newPath })
        }
        toast.success(type === "dir" ? "已新建文件夹" : "已新建文件")
      }
      // 新建/重命名后自动刷新文件树
      await refreshTrees(selectedPod.name, false)
      setPrompt(null)
      return true
    } catch (error) {
      toast.error((error as Error).message)
      return false
    } finally {
      setPromptBusy(false)
    }
  }

  // 删除文件/目录（目录递归删除）
  async function handleDeleteEntry(root: IDERoot, path: string) {
    if (!selectedPod?.running) return
    if (!window.confirm(`确定删除「${path}」吗？目录会连同内部文件一起删除。`)) return
    try {
      await ideApi.deleteEntry(selectedPod.name, root, path)
      // 关闭被删除（或位于被删目录内）的编辑器
      setOpenFile((current) => {
        if (!current || current.root !== root) return current
        if (current.path === path || current.path.startsWith(`${path}/`)) return null
        return current
      })
      toast.success("已删除")
      await refreshTrees(selectedPod.name, false)
    } catch (error) {
      toast.error((error as Error).message)
    }
  }

  // 下载文件：后端返回文件流，前端触发浏览器下载
  async function handleDownloadEntry(root: IDERoot, path: string) {
    if (!selectedPod?.running) return
    try {
      await downloadWorkspaceFile(selectedPod.name, root, path)
    } catch (error) {
      toast.error((error as Error).message)
    }
  }

  // 编辑器标题展示的路径（home 根对应容器 /home/workspace）
  const openFilePath = openFile
    ? openFile.root === "home"
      ? `home/workspace/${openFile.path}`
      : `shared/${openFile.path}`
    : ""
  const selectedKey = openFile ? `${openFile.root}:${openFile.path}` : ""
  // 新建目标目录的展示路径（文件树顶部提示用）
  const selectedDirLabel =
    selectedDir.root === "shared"
      ? `/home/workspace/shared${selectedDir.path ? `/${selectedDir.path}` : ""}`
      : `/home/workspace${selectedDir.path ? `/${selectedDir.path}` : ""}`

  // 顶部栏：返回按钮 + Pod 选择器 + 面板开关
  const header = (
    <header className="flex h-12 shrink-0 items-center justify-between gap-3 border-b border-slate-200 bg-white px-3">
      <div className="flex min-w-0 items-center gap-3">
        <button
          type="button"
          onClick={() => navigate("/pods")}
          className="flex h-8 w-8 items-center justify-center rounded-md text-slate-400 hover:bg-slate-100 dark:hover:bg-slate-800 hover:text-slate-900 dark:hover:text-slate-100"
          title="返回 Pod 列表"
        >
          <ArrowLeft className="h-4 w-4" />
        </button>
        <span className="hidden shrink-0 text-sm font-semibold tracking-tight text-slate-800 dark:text-slate-200 sm:block">Cloud IDE</span>

        {/* Pod 下拉选择器：列出当前用户全部 Pod */}
        <div className="relative">
          <select
            value={podName}
            onChange={(event) => setPodName(event.target.value)}
            disabled={pods.length === 0}
            className="h-8 max-w-[220px] appearance-none rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-900 pl-3 pr-7 text-xs text-slate-800 dark:text-slate-200 outline-none focus:border-blue-500 disabled:opacity-50"
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
                  ? "border-blue-300 bg-blue-50 dark:border-blue-500/40 text-blue-700 dark:bg-blue-600/20 dark:text-blue-300"
                  : "border-transparent text-slate-500 hover:bg-slate-100 dark:hover:bg-slate-800 hover:text-slate-700 dark:hover:text-slate-300",
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
      <div className="flex h-screen flex-col bg-slate-50 text-slate-800 dark:text-slate-200">
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
      <div className="flex h-screen flex-col bg-slate-50 text-slate-800 dark:text-slate-200">
        {header}
        <div className="flex flex-1 items-center justify-center p-6">
          <div className="w-full max-w-md rounded-2xl border border-slate-200 bg-white p-10 text-center">
            <div className="mx-auto flex h-14 w-14 items-center justify-center rounded-2xl bg-slate-100 dark:bg-slate-800 text-slate-400">
              <Box className="h-7 w-7" />
            </div>
            <h1 className="mt-5 text-lg font-semibold text-slate-900 dark:text-slate-100">暂无可用 Pod</h1>
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
      <div className="flex h-screen flex-col bg-slate-50 text-slate-800 dark:text-slate-200">
        {header}
        <div className="flex flex-1 items-center justify-center p-6">
          <div className="w-full max-w-md rounded-2xl border border-slate-200 bg-white p-10 text-center">
            <div className="mx-auto flex h-14 w-14 items-center justify-center rounded-2xl bg-amber-50 text-amber-500 dark:bg-amber-500/10 dark:text-amber-400">
              <Box className="h-7 w-7" />
            </div>
            <h1 className="mt-5 text-lg font-semibold text-slate-900 dark:text-slate-100">当前 Pod 未运行</h1>
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
    <div className="flex h-screen flex-col overflow-hidden bg-slate-50 text-slate-800 dark:text-slate-200">
      {header}

      <div className="flex min-h-0 flex-1">
        {/* 左侧文件树 */}
        {panels.files && (
          <>
            <aside className="flex min-h-0 shrink-0 flex-col border-r border-slate-200 bg-white" style={{ width: filesWidth }}>
              <div className="flex h-9 shrink-0 items-center gap-2 border-b border-slate-200 px-3 text-xs font-medium text-slate-700 dark:text-slate-300">
                <FolderTree className="h-4 w-4 text-blue-600 dark:text-blue-400" />
                资源管理器
                <div className="ml-auto flex items-center gap-1">
                  {/* 新建文件 / 新建文件夹：作用于当前选中的目录 */}
                  <button
                    type="button"
                    onClick={() => openCreateDialog("create-file")}
                    className="text-slate-500 hover:text-slate-700 dark:hover:text-slate-200"
                    title="新建文件（在选中的目录下）"
                  >
                    <FilePlus2 className="h-3.5 w-3.5" />
                  </button>
                  <button
                    type="button"
                    onClick={() => openCreateDialog("create-dir")}
                    className="text-slate-500 hover:text-slate-700 dark:hover:text-slate-200"
                    title="新建文件夹（在选中的目录下）"
                  >
                    <FolderPlus className="h-3.5 w-3.5" />
                  </button>
                  <button
                    type="button"
                    onClick={() => void refreshTrees(selectedPod.name, false)}
                    className="text-slate-500 hover:text-slate-700 dark:hover:text-slate-200"
                    title="刷新文件树"
                  >
                    <RotateCw className={cn("h-3.5 w-3.5", treeLoading && "animate-spin")} />
                  </button>
                </div>
              </div>
              {/* 新建目标目录提示，点击目录节点可切换 */}
              <div className="shrink-0 truncate border-b border-slate-200 dark:border-slate-800/60 px-3 py-1 text-[10px] text-slate-500" title={selectedDirLabel}>
                目标目录：{selectedDirLabel}
              </div>
              <div className="min-h-0 flex-1 overflow-auto py-1">
                {ROOTS.map((root) => {
                  const open = rootOpen[root.key]
                  return (
                    <div key={root.key}>
                      <button
                        type="button"
                        onClick={() => {
                          setRootOpen((prev) => ({ ...prev, [root.key]: !prev[root.key] }))
                          setSelectedDir({ root: root.key, path: "" })
                        }}
                        title={root.hint}
                        className="flex w-full items-center gap-1.5 py-1 pl-2.5 text-xs text-slate-700 dark:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-800/60"
                      >
                        {open ? <ChevronDown className="h-3.5 w-3.5 shrink-0" /> : <ChevronRight className="h-3.5 w-3.5 shrink-0" />}
                        {open ? (
                          <FolderOpen className="h-3.5 w-3.5 shrink-0 text-amber-500 dark:text-amber-400" />
                        ) : (
                          <Folder className="h-3.5 w-3.5 shrink-0 text-amber-500 dark:text-amber-400" />
                        )}
                        <span className="truncate">{root.label}</span>
                      </button>
                      {open &&
                        trees[root.key].map((node) => (
                          <TreeNode
                            key={`${root.key}:${node.path}`}
                            node={node}
                            root={root.key}
                            depth={1}
                            expanded={expanded}
                            selectedFileKey={selectedKey}
                            selectedDirKey={`${selectedDir.root}:${selectedDir.path}`}
                            onToggle={(key) => setExpanded((prev) => ({ ...prev, [key]: !(prev[key] ?? true) }))}
                            onSelectFile={(selectRoot, path) => setOpenFile({ root: selectRoot, path })}
                            onSelectDir={(selectRoot, path) => setSelectedDir({ root: selectRoot, path })}
                            onRename={openRenameDialog}
                            onDownload={(selectRoot, path) => void handleDownloadEntry(selectRoot, path)}
                            onDelete={(selectRoot, path) => void handleDeleteEntry(selectRoot, path)}
                          />
                        ))}
                      {open && trees[root.key].length === 0 && (
                        <div className="py-1 pl-9 text-[11px] text-slate-400 dark:text-slate-600">{treeLoading ? "加载中…" : "空目录"}</div>
                      )}
                    </div>
                  )
                })}
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
                path={openFilePath}
                value={fileContent}
                loading={fileLoading}
                dirty={dirty}
                saving={saving}
                onChange={(value) => {
                  setFileContent(value)
                  setDirty(true)
                }}
                onSave={handleSave}
              />
            </section>

            {/* 右侧 AI 助手 */}
            {panels.ai && (
              <>
                <DragHandle direction="horizontal" onDelta={(delta) => setAiWidth((width) => clamp(width - delta, 220, 480))} />
                <aside className="min-h-0 shrink-0 border-l border-slate-200" style={{ width: aiWidth }}>
                  <AIPanel />
                </aside>
              </>
            )}
          </div>

          {/* 底部终端 + 日志 */}
          {(panels.terminal || panels.logs) && (
            <>
              <DragHandle direction="vertical" onDelta={(delta) => setBottomHeight((height) => clamp(height - delta, 140, 560))} />
              <div className="flex shrink-0 border-t border-slate-200" style={{ height: bottomHeight }}>
                {panels.terminal && (
                  <div className={cn("min-h-0 min-w-0 flex-1", panels.logs && "border-r border-slate-200")}>
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

      {/* 新建文件 / 新建文件夹 / 重命名 输入弹窗 */}
      {prompt && (
        <PromptDialog
          title={prompt.mode === "rename" ? "重命名" : prompt.mode === "create-dir" ? "新建文件夹" : "新建文件"}
          placeholder={prompt.mode === "rename" ? "输入新名称" : "输入名称"}
          initial={prompt.mode === "rename" ? prompt.initial : ""}
          confirmText={prompt.mode === "rename" ? "重命名" : "创建"}
          busy={promptBusy}
          onCancel={() => setPrompt(null)}
          onSubmit={submitPrompt}
        />
      )}
    </div>
  )
}
