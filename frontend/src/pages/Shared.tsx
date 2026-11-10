/**
 * 共享目录页面（支持全局明暗主题）
 *
 * 功能：
 * 1. 顶部：AI 洞察折叠卡片（关键状态 / 潜在风险 / 建议操作）、AI 助手、新目录、上传；
 * 2. 左侧：/shared 文件树，懒加载实现无限层级，支持重命名、删除、下载、新建文件/子目录；
 * 3. 右侧：预览占位卡片（本页面不提供在线预览，文件仅支持下载）。
 *
 * 主题说明：
 * - 本页不再固定深色，改为「亮色为基础样式 + dark: 变体」跟随全局 <html class="dark">；
 * - 明暗开关位于个人页，切换后由 lib/theme.ts 写入 html 根节点，本页自动跟随；
 * - 亮色配色与平台其它页面一致：slate-50 页面底 + 白色卡片 + slate-200 细边框；
 * - 深色配色沿用平台全局暗色方案（由 index.css 的 .dark 覆盖兜底）。
 *
 * 安全说明：
 * - 根目录锁定为 /shared，前端所有请求只传相对路径；
 * - 路径逃逸（../、绝对路径）由后端统一拦截并返回 403，前端弹窗提示；
 * - 名称在提交前做单层名称校验，与后端规则保持一致。
 */
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import {
  Activity,
  AlertTriangle,
  BookOpen,
  ChevronDown,
  ChevronRight,
  ChevronUp,
  Download,
  File as FileIcon,
  FilePlus2,
  Folder,
  FolderOpen,
  FolderPlus,
  FolderSearch,
  Lightbulb,
  Loader2,
  Lock,
  MessagesSquare,
  Pencil,
  RefreshCw,
  ShieldCheck,
  Sparkles,
  Trash2,
  Upload,
  X,
} from "lucide-react"
import { toast } from "sonner"
import {
  downloadSharedEntry,
  searchShared,
  sharedApi,
  type SharedEntry,
  type SharedInsight,
} from "@/api/shared"
import { cn } from "@/lib/utils"

// ---------------- 工具函数 ----------------

// 字节数格式化
function formatBytes(size: number): string {
  if (!size) return "0 B"
  const units = ["B", "KB", "MB", "GB", "TB"]
  let value = size
  let index = 0
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024
    index++
  }
  return `${value.toFixed(index === 0 ? 0 : 1)} ${units[index]}`
}

// Unix 秒格式化为「MM-DD HH:mm」
function formatTime(unix: number): string {
  if (!unix) return "-"
  const date = new Date(unix * 1000)
  const pad = (n: number) => String(n).padStart(2, "0")
  return `${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`
}

// 名称校验：只允许单层名称，不含路径分隔符
function validateName(value: string): string | null {
  const name = value.trim()
  if (!name) return "名称不能为空"
  if (name === "." || name === "..") return "名称不合法"
  if (/[/\\]/.test(name)) return "名称不能包含 / 或 \\"
  if (name.length > 255) return "名称过长"
  return null
}

// 拼接展示路径（根目录显示 /shared）
function displayPath(path: string): string {
  return path ? `/shared/${path}` : "/shared"
}

// ---------------- 输入弹窗 ----------------

// 弹窗状态：新建目录 / 新建文件 / 重命名
type PromptState =
  | { mode: "mkdir"; path: string }
  | { mode: "file"; path: string }
  | { mode: "rename"; path: string; initial: string }

// PromptDialog 是轻量输入弹窗，明暗两套配色跟随全局主题。
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
    const message = validateName(value)
    if (message) {
      setError(message)
      return
    }
    await onSubmit(value.trim())
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 px-4 dark:bg-black/60">
      {/* 主题改动：弹窗面板由固定深色改为白底 + dark 深色 */}
      <div className="w-full max-w-sm rounded-xl border border-slate-200 bg-white p-4 shadow-2xl dark:border-slate-700 dark:bg-[#0f172a]">
        <div className="mb-3 flex items-center justify-between">
          <h3 className="text-sm font-medium text-slate-900 dark:text-slate-100">{title}</h3>
          <button
            type="button"
            onClick={onCancel}
            className="text-slate-400 hover:text-slate-700 dark:text-slate-500 dark:hover:text-slate-200"
            title="关闭"
          >
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
          className="w-full rounded-md border border-slate-300 bg-slate-50 px-3 py-2 text-sm text-slate-900 outline-none focus:border-blue-500 dark:border-slate-700 dark:bg-[#0b1120] dark:text-slate-100"
        />
        {error && <div className="mt-2 text-xs text-rose-600 dark:text-rose-400">{error}</div>}
        <div className="mt-4 flex justify-end gap-2">
          <button
            type="button"
            onClick={onCancel}
            className="rounded-md px-3 py-1.5 text-xs text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800"
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

// ---------------- 文件树节点 ----------------

interface TreeContext {
  childrenMap: Record<string, SharedEntry[]>
  expanded: Record<string, boolean>
  loadingDirs: Record<string, boolean>
  errorDirs: Record<string, string>
  selectedDir: string
  onToggle: (entry: SharedEntry) => void
  onSelectDir: (path: string) => void
  onRename: (entry: SharedEntry) => void
  onDelete: (entry: SharedEntry) => void
  onDownload: (entry: SharedEntry) => void
  onCreateFile: (entry: SharedEntry) => void
  onCreateDir: (entry: SharedEntry) => void
}

// 悬浮操作按钮的统一样式（主题改动：亮色浅灰悬浮底 / 深色深灰悬浮底）
const ACTION_BTN =
  "rounded p-0.5 text-slate-500 hover:bg-slate-200 hover:text-slate-900 dark:hover:bg-slate-700 dark:hover:text-slate-100"

// TreeNode 递归渲染单个节点：目录点击展开/收起，文件点击下载。
function TreeNode({ entry, depth, ctx }: { entry: SharedEntry; depth: number; ctx: TreeContext }) {
  const isDir = entry.type === "dir"
  const open = ctx.expanded[entry.path] ?? false
  const loading = ctx.loadingDirs[entry.path]
  const error = ctx.errorDirs[entry.path]
  const kids = ctx.childrenMap[entry.path] ?? []
  const padding = { paddingLeft: `${depth * 14 + 12}px` }

  return (
    <div>
      <div
        className={cn(
          "group flex w-full items-center gap-1.5 py-1 pr-2 text-xs",
          // 主题改动：选中态亮色用浅蓝底，深色保留半透明蓝
          ctx.selectedDir === entry.path
            ? "bg-blue-50 dark:bg-blue-600/10"
            : "hover:bg-slate-100 dark:hover:bg-slate-800/60",
        )}
        style={padding}
      >
        <button
          type="button"
          onClick={() => (isDir ? ctx.onToggle(entry) : ctx.onDownload(entry))}
          title={displayPath(entry.path)}
          className="flex min-w-0 flex-1 items-center gap-1.5 text-left"
        >
          {isDir ? (
            <>
              {open ? (
                <ChevronDown className="h-3.5 w-3.5 shrink-0 text-slate-400 dark:text-slate-500" />
              ) : (
                <ChevronRight className="h-3.5 w-3.5 shrink-0 text-slate-400 dark:text-slate-500" />
              )}
              {open ? (
                <FolderOpen className="h-3.5 w-3.5 shrink-0 text-amber-500 dark:text-amber-400" />
              ) : (
                <Folder className="h-3.5 w-3.5 shrink-0 text-amber-500 dark:text-amber-400" />
              )}
            </>
          ) : (
            <>
              <span className="h-3.5 w-3.5 shrink-0" />
              <FileIcon className="h-3.5 w-3.5 shrink-0 text-slate-400 dark:text-slate-500" />
            </>
          )}
          <span
            className={cn(
              "truncate",
              isDir
                ? "text-slate-800 dark:text-slate-200"
                : "text-slate-500 group-hover:text-slate-900 dark:text-slate-400 dark:group-hover:text-slate-200",
            )}
          >
            {entry.name}
          </span>
          {isDir && loading && <Loader2 className="h-3 w-3 shrink-0 animate-spin text-slate-400 dark:text-slate-500" />}
        </button>

        {/* 文件条目右侧固定下载按钮 */}
        {!isDir && (
          <span className="shrink-0 text-[10px] text-slate-400 group-hover:hidden dark:text-slate-600">
            {formatBytes(entry.size)}
          </span>
        )}
        {!isDir && (
          <button
            type="button"
            onClick={() => ctx.onDownload(entry)}
            title="下载"
            className="shrink-0 rounded p-0.5 text-slate-400 hover:bg-slate-200 hover:text-slate-900 group-hover:hidden dark:text-slate-500 dark:hover:bg-slate-700 dark:hover:text-slate-100"
          >
            <Download className="h-3 w-3" />
          </button>
        )}

        {/* 悬浮操作菜单：目录额外提供新建文件/新建子目录（主题改动：白底浮层 / 深色浮层） */}
        <span className="hidden shrink-0 items-center gap-0.5 rounded bg-white px-1 py-0.5 shadow-md ring-1 ring-slate-200 group-hover:flex dark:bg-slate-900/90 dark:ring-slate-700">
          {isDir && (
            <>
              <button
                type="button"
                onClick={() => {
                  ctx.onSelectDir(entry.path)
                  ctx.onCreateFile(entry)
                }}
                title="新建文件"
                className={ACTION_BTN}
              >
                <FilePlus2 className="h-3 w-3" />
              </button>
              <button
                type="button"
                onClick={() => {
                  ctx.onSelectDir(entry.path)
                  ctx.onCreateDir(entry)
                }}
                title="新建子目录"
                className={ACTION_BTN}
              >
                <FolderPlus className="h-3 w-3" />
              </button>
            </>
          )}
          <button type="button" onClick={() => ctx.onRename(entry)} title="重命名" className={ACTION_BTN}>
            <Pencil className="h-3 w-3" />
          </button>
          <button type="button" onClick={() => ctx.onDownload(entry)} title="下载" className={ACTION_BTN}>
            <Download className="h-3 w-3" />
          </button>
          <button
            type="button"
            onClick={() => ctx.onDelete(entry)}
            title="删除"
            className="rounded p-0.5 text-slate-500 hover:bg-slate-200 hover:text-rose-600 dark:hover:bg-slate-700 dark:hover:text-rose-400"
          >
            <Trash2 className="h-3 w-3" />
          </button>
        </span>
      </div>

      {/* 子节点：仅展开且已加载时渲染，实现无限层级递归 */}
      {isDir && open && (
        <div>
          {kids.map((child) => (
            <TreeNode key={child.path} entry={child} depth={depth + 1} ctx={ctx} />
          ))}
          {!loading && error && (
            <div className="py-1 pl-9 text-[11px] text-rose-600 dark:text-rose-400">{error}</div>
          )}
          {!loading && !error && kids.length === 0 && (
            <div className="py-1 pl-9 text-[11px] text-slate-400 dark:text-slate-600">空目录</div>
          )}
        </div>
      )}
    </div>
  )
}

// ---------------- 页面主体 ----------------

export default function Shared() {
  // 文件树：childrenMap 按目录路径缓存已加载的子项，实现懒加载无限层级
  const [childrenMap, setChildrenMap] = useState<Record<string, SharedEntry[]>>({})
  const [expanded, setExpanded] = useState<Record<string, boolean>>({ "": true })
  const [loadingDirs, setLoadingDirs] = useState<Record<string, boolean>>({})
  const [errorDirs, setErrorDirs] = useState<Record<string, string>>({})
  const [rootLoading, setRootLoading] = useState(true)
  // 新建/上传的目标目录（相对 /shared）
  const [selectedDir, setSelectedDir] = useState("")

  // AI 搜索
  const [search, setSearch] = useState("")
  const [searching, setSearching] = useState(false)
  const [searchResults, setSearchResults] = useState<SharedEntry[] | null>(null)

  // 洞察卡片
  const [insight, setInsight] = useState<SharedInsight | null>(null)
  const [insightLoading, setInsightLoading] = useState(false)
  const [insightOpen, setInsightOpen] = useState(true)

  // 弹窗与上传
  const [prompt, setPrompt] = useState<PromptState | null>(null)
  const [promptBusy, setPromptBusy] = useState(false)
  const [uploading, setUploading] = useState(false)
  const fileInputRef = useRef<HTMLInputElement>(null)

  // 读取单个目录
  const loadDir = useCallback(async (path: string) => {
    setLoadingDirs((prev) => ({ ...prev, [path]: true }))
    try {
      const result = await sharedApi.list(path)
      setChildrenMap((prev) => ({ ...prev, [path]: result.items }))
      setErrorDirs((prev) => {
        const next = { ...prev }
        delete next[path]
        return next
      })
    } catch (error) {
      const message = (error as Error).message
      setErrorDirs((prev) => ({ ...prev, [path]: message }))
      toast.error(message)
    } finally {
      setLoadingDirs((prev) => ({ ...prev, [path]: false }))
    }
  }, [])

  // 读取 AI 洞察
  const loadInsight = useCallback(async () => {
    setInsightLoading(true)
    try {
      setInsight(await sharedApi.insight())
    } catch (error) {
      toast.error((error as Error).message)
    } finally {
      setInsightLoading(false)
    }
  }, [])

  // 首屏：加载根目录与洞察摘要
  useEffect(() => {
    let alive = true
    setRootLoading(true)
    sharedApi
      .list("")
      .then((result) => {
        if (alive) setChildrenMap({ "": result.items })
      })
      .catch((error) => toast.error((error as Error).message))
      .finally(() => {
        if (alive) setRootLoading(false)
      })
    void loadInsight()
    return () => {
      alive = false
    }
  }, [loadInsight])

  // 搜索防抖：关键词变化 350ms 后执行
  useEffect(() => {
    const keyword = search.trim()
    if (!keyword) {
      setSearchResults(null)
      setSearching(false)
      return
    }
    let alive = true
    setSearching(true)
    const timer = window.setTimeout(() => {
      searchShared(keyword)
        .then((items) => {
          if (alive) setSearchResults(items)
        })
        .catch((error) => {
          if (alive) toast.error((error as Error).message)
        })
        .finally(() => {
          if (alive) setSearching(false)
        })
    }, 350)
    return () => {
      alive = false
      window.clearTimeout(timer)
    }
  }, [search])

  // 展开/收起目录：首次展开时懒加载子项
  const handleToggle = useCallback(
    (entry: SharedEntry) => {
      const path = entry.path
      setSelectedDir(path)
      const next = !(expanded[path] ?? false)
      setExpanded((prev) => ({ ...prev, [path]: next }))
      if (next && childrenMap[path] === undefined) {
        void loadDir(path)
      }
    },
    [childrenMap, expanded, loadDir],
  )

  // 下载：文件直接下载，目录由后端打包 zip
  const handleDownload = useCallback(async (entry: SharedEntry) => {
    try {
      toast.info(`开始下载 ${entry.name}`)
      await downloadSharedEntry(entry.path)
    } catch (error) {
      toast.error((error as Error).message)
    }
  }, [])

  // 父目录路径（重命名/删除后刷新用）
  const parentOf = (path: string) => (path.includes("/") ? path.slice(0, path.lastIndexOf("/")) : "")

  // 删除文件/目录
  const handleDelete = useCallback(
    async (entry: SharedEntry) => {
      const tip = entry.type === "dir" ? "目录会连同内部所有内容一起删除。" : ""
      if (!window.confirm(`确定删除「${displayPath(entry.path)}」吗？${tip}`)) return
      try {
        await sharedApi.rm(entry.path)
        toast.success("已删除")
        await loadDir(parentOf(entry.path))
        void loadInsight()
      } catch (error) {
        toast.error((error as Error).message)
      }
    },
    [loadDir, loadInsight],
  )

  // 弹窗提交：新建目录 / 新建文件 / 重命名
  const submitPrompt = useCallback(
    async (value: string): Promise<boolean> => {
      const task = prompt
      if (!task) return false
      setPromptBusy(true)
      try {
        if (task.mode === "mkdir") {
          await sharedApi.mkdir(task.path, value)
          toast.success("已新建目录")
          await loadDir(task.path)
        } else if (task.mode === "file") {
          // 新建文件复用上传接口：上传一个空文件即可
          await sharedApi.upload(task.path, [new File([], value, { type: "text/plain" })])
          toast.success("已新建文件")
          await loadDir(task.path)
        } else {
          await sharedApi.rename(task.path, value)
          toast.success("已重命名")
          await loadDir(parentOf(task.path))
        }
        setPrompt(null)
        void loadInsight()
        return true
      } catch (error) {
        toast.error((error as Error).message)
        return false
      } finally {
        setPromptBusy(false)
      }
    },
    [loadDir, loadInsight, prompt],
  )

  // 上传：支持多选，上传到当前选中的目录
  async function handleUpload(files: FileList | null) {
    if (!files || files.length === 0) return
    const list = Array.from(files)
    setUploading(true)
    try {
      const result = await sharedApi.upload(selectedDir, list)
      toast.success(`已上传 ${result.uploaded.length} 个文件`)
      await loadDir(selectedDir)
      void loadInsight()
    } catch (error) {
      toast.error((error as Error).message)
    } finally {
      setUploading(false)
      if (fileInputRef.current) fileInputRef.current.value = ""
    }
  }

  // 点击搜索结果中的目录：清空搜索并逐级展开定位
  async function revealDir(target: string) {
    setSearch("")
    setSearchResults(null)
    const segments = target.split("/").filter(Boolean)
    setExpanded((prev) => ({ ...prev, "": true }))
    let acc = ""
    for (const segment of segments) {
      acc = acc ? `${acc}/${segment}` : segment
      setExpanded((prev) => ({ ...prev, [acc]: true }))
      await loadDir(acc)
    }
    setSelectedDir(target)
  }

  // 树组件上下文
  const treeCtx = useMemo<TreeContext>(
    () => ({
      childrenMap,
      expanded,
      loadingDirs,
      errorDirs,
      selectedDir,
      onToggle: handleToggle,
      onSelectDir: setSelectedDir,
      onRename: (entry) => setPrompt({ mode: "rename", path: entry.path, initial: entry.name }),
      onDelete: (entry) => void handleDelete(entry),
      onDownload: (entry) => void handleDownload(entry),
      onCreateFile: (entry) => setPrompt({ mode: "file", path: entry.path }),
      onCreateDir: (entry) => setPrompt({ mode: "mkdir", path: entry.path }),
    }),
    [childrenMap, expanded, errorDirs, handleDelete, handleDownload, handleToggle, loadingDirs, selectedDir],
  )

  return (
    // 主题改动：页面底色由固定 #0b1120 改为亮色 slate-50，深色由全局 .dark 覆盖为暗底
    <div className="-m-6 flex min-h-[calc(100vh-56px)] flex-col gap-4 bg-slate-50 p-6 text-slate-900 dark:text-slate-200">
      {/* 页面头部 */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <div className="flex items-center gap-2">
            <h1 className="text-lg font-semibold text-slate-900 dark:text-slate-100">共享目录</h1>
            <a
              href="#"
              onClick={(event) => event.preventDefault()}
              title="共享目录使用文档"
              className="text-slate-400 hover:text-slate-600 dark:text-slate-500 dark:hover:text-slate-300"
            >
              <BookOpen className="h-4 w-4" />
            </a>
          </div>
          <p className="mt-0.5 text-xs text-slate-500">跨Pod共享文件管理</p>
        </div>

        <div className="flex items-center gap-2">
          {/* 主题改动：次级按钮亮色白底浅边框，深色保留深底 */}
          <button
            type="button"
            onClick={() => {
              setInsightOpen(true)
              void loadInsight()
            }}
            className="flex items-center gap-1.5 rounded-md border border-slate-300 bg-white px-3 py-1.5 text-xs text-slate-600 hover:border-blue-400 hover:text-blue-600 dark:border-slate-700 dark:bg-[#0f172a] dark:text-slate-300 dark:hover:border-blue-500/50 dark:hover:text-blue-300"
          >
            <Sparkles className="h-3.5 w-3.5" />
            AI助手
          </button>
          <button
            type="button"
            onClick={() => setPrompt({ mode: "mkdir", path: selectedDir })}
            className="flex items-center gap-1.5 rounded-md border border-slate-300 bg-white px-3 py-1.5 text-xs text-slate-600 hover:border-blue-400 hover:text-blue-600 dark:border-slate-700 dark:bg-[#0f172a] dark:text-slate-300 dark:hover:border-blue-500/50 dark:hover:text-blue-300"
          >
            <FolderPlus className="h-3.5 w-3.5" />
            新目录
          </button>
          <button
            type="button"
            disabled={uploading}
            onClick={() => fileInputRef.current?.click()}
            className="flex items-center gap-1.5 rounded-md bg-blue-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-blue-500 disabled:opacity-60"
          >
            {uploading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Upload className="h-3.5 w-3.5" />}
            上传
          </button>
          <input
            ref={fileInputRef}
            type="file"
            multiple
            className="hidden"
            onChange={(event) => void handleUpload(event.target.files)}
          />
        </div>
      </div>

      {/* AI 洞察折叠卡片（主题改动：白底卡片 / 深色卡片） */}
      <section className="rounded-xl border border-slate-200 bg-white dark:border-slate-800 dark:bg-[#0f172a]">
        <div className="flex items-center justify-between gap-2 border-b border-slate-200 px-4 py-2.5 dark:border-slate-800">
          <div className="flex items-center gap-2 text-sm font-medium text-slate-900 dark:text-slate-200">
            <Sparkles className="h-4 w-4 text-violet-600 dark:text-violet-400" />
            共享目录洞察
            {insightLoading && <Loader2 className="h-3.5 w-3.5 animate-spin text-slate-400 dark:text-slate-500" />}
          </div>
          <div className="flex items-center gap-2">
            {insight && (
              <span className="hidden text-[11px] text-slate-400 sm:inline dark:text-slate-500">
                更新于 {insight.updatedAt}
              </span>
            )}
            <span className="rounded border border-violet-200 bg-violet-50 px-1.5 py-0.5 text-[10px] font-medium tracking-wider text-violet-700 dark:border-violet-500/40 dark:bg-violet-500/10 dark:text-violet-300">
              SHARED
            </span>
            <button
              type="button"
              onClick={() => void loadInsight()}
              title="刷新洞察"
              className="text-slate-400 hover:text-slate-700 dark:text-slate-500 dark:hover:text-slate-200"
            >
              <RefreshCw className={cn("h-3.5 w-3.5", insightLoading && "animate-spin")} />
            </button>
            <button
              type="button"
              onClick={() => setInsightOpen((open) => !open)}
              title={insightOpen ? "折叠" : "展开"}
              className="text-slate-400 hover:text-slate-700 dark:text-slate-500 dark:hover:text-slate-200"
            >
              {insightOpen ? <ChevronUp className="h-3.5 w-3.5" /> : <ChevronDown className="h-3.5 w-3.5" />}
            </button>
          </div>
        </div>

        {insightOpen && (
          <div className="space-y-3 px-4 py-3 text-xs">
            {!insight && insightLoading && <div className="text-slate-500">正在分析共享目录…</div>}
            {!insight && !insightLoading && <div className="text-slate-500">暂无洞察数据，点击刷新重试。</div>}
            {insight && (
              <>
                <div className="flex gap-2">
                  <Activity className="mt-0.5 h-3.5 w-3.5 shrink-0 text-emerald-600 dark:text-emerald-400" />
                  <div>
                    <div className="font-medium text-slate-700 dark:text-slate-300">关键状态</div>
                    <p className="mt-0.5 leading-relaxed text-slate-500 dark:text-slate-400">{insight.keyStatus}</p>
                  </div>
                </div>
                <div className="flex gap-2">
                  <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-amber-500 dark:text-amber-400" />
                  <div>
                    <div className="font-medium text-slate-700 dark:text-slate-300">潜在风险</div>
                    <ul className="mt-0.5 space-y-0.5 leading-relaxed text-slate-500 dark:text-slate-400">
                      {insight.risks.map((item, index) => (
                        <li key={index}>· {item}</li>
                      ))}
                    </ul>
                  </div>
                </div>
                <div className="flex gap-2">
                  <Lightbulb className="mt-0.5 h-3.5 w-3.5 shrink-0 text-blue-600 dark:text-blue-400" />
                  <div>
                    <div className="font-medium text-slate-700 dark:text-slate-300">建议操作</div>
                    <ul className="mt-0.5 space-y-0.5 leading-relaxed text-slate-500 dark:text-slate-400">
                      {insight.suggestions.map((item, index) => (
                        <li key={index}>· {item}</li>
                      ))}
                    </ul>
                  </div>
                </div>
                <div className="flex justify-end">
                  <button
                    type="button"
                    onClick={() => toast.info("AI 对话能力将在后续版本接入")}
                    className="flex items-center gap-1.5 rounded-md border border-slate-300 px-2.5 py-1 text-[11px] text-slate-500 hover:border-violet-400 hover:text-violet-600 dark:border-slate-700 dark:text-slate-400 dark:hover:border-violet-500/50 dark:hover:text-violet-300"
                  >
                    <MessagesSquare className="h-3 w-3" />
                    追问 / 继续对话
                  </button>
                </div>
              </>
            )}
          </div>
        )}
      </section>

      {/* 主体：左右双卡片 */}
      <div className="grid min-h-0 flex-1 grid-cols-1 gap-4 xl:grid-cols-[minmax(0,1.15fr)_minmax(0,0.85fr)]">
        {/* 左：文件树 */}
        <section className="flex min-h-[520px] flex-col overflow-hidden rounded-xl border border-slate-200 bg-white dark:border-slate-800 dark:bg-[#0f172a]">
          <div className="flex items-center gap-2 border-b border-slate-200 px-4 py-2.5 dark:border-slate-800">
            <FolderOpen className="h-4 w-4 text-amber-500 dark:text-amber-400" />
            <span className="font-mono text-sm text-slate-900 dark:text-slate-200">/shared</span>
            <span className="flex items-center gap-1 rounded border border-emerald-200 bg-emerald-50 px-1.5 py-0.5 text-[10px] text-emerald-700 dark:border-emerald-500/30 dark:bg-emerald-500/10 dark:text-emerald-300">
              <Lock className="h-2.5 w-2.5" />
              根目录锁定
            </span>
            <button
              type="button"
              onClick={() => {
                void loadDir("")
                void loadInsight()
              }}
              title="刷新文件树"
              className="ml-auto text-slate-400 hover:text-slate-700 dark:text-slate-500 dark:hover:text-slate-200"
            >
              <RefreshCw className={cn("h-3.5 w-3.5", rootLoading && "animate-spin")} />
            </button>
          </div>

          {/* AI 搜索框（主题改动：亮色浅灰输入底 / 深色夜空底） */}
          <div className="border-b border-slate-200 px-3 py-2 dark:border-slate-800">
            <div className="flex items-center gap-2 rounded-md border border-slate-300 bg-slate-50 px-2.5 py-1.5 dark:border-slate-700 dark:bg-[#0b1120]">
              <FolderSearch className="h-3.5 w-3.5 shrink-0 text-slate-400 dark:text-slate-500" />
              <input
                value={search}
                onChange={(event) => setSearch(event.target.value)}
                placeholder="AI搜索文件:描述要找的文件..."
                className="min-w-0 flex-1 bg-transparent text-xs text-slate-800 placeholder:text-slate-400 outline-none dark:text-slate-200 dark:placeholder:text-slate-600"
              />
              {searching && <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-slate-400 dark:text-slate-500" />}
              {search && !searching && (
                <button
                  type="button"
                  onClick={() => setSearch("")}
                  title="清空"
                  className="shrink-0 text-slate-400 hover:text-slate-700 dark:text-slate-500 dark:hover:text-slate-200"
                >
                  <X className="h-3.5 w-3.5" />
                </button>
              )}
            </div>
            {/* 新建/上传的目标目录提示 */}
            <div
              className="mt-1.5 truncate text-[10px] text-slate-400 dark:text-slate-500"
              title={displayPath(selectedDir)}
            >
              当前目录：{displayPath(selectedDir)}
              {selectedDir && (
                <button
                  type="button"
                  onClick={() => setSelectedDir("")}
                  className="ml-2 text-slate-500 hover:text-slate-700 dark:text-slate-400 dark:hover:text-slate-300"
                >
                  回到根目录
                </button>
              )}
            </div>
          </div>

          <div className="min-h-0 flex-1 overflow-auto py-1">
            {searchResults ? (
              // 搜索结果：扁平展示，文件点击下载，目录点击定位
              <div>
                <div className="px-4 py-1.5 text-[11px] text-slate-500">
                  找到 {searchResults.length} 个结果（已遍历加载的目录，最多 100 条）
                </div>
                {searchResults.map((item) => (
                  <div
                    key={item.path}
                    className="group flex items-center gap-2 px-4 py-1 text-xs hover:bg-slate-100 dark:hover:bg-slate-800/60"
                  >
                    <button
                      type="button"
                      onClick={() => (item.type === "dir" ? void revealDir(item.path) : void handleDownload(item))}
                      className="flex min-w-0 flex-1 items-center gap-1.5 text-left"
                      title={displayPath(item.path)}
                    >
                      {item.type === "dir" ? (
                        <Folder className="h-3.5 w-3.5 shrink-0 text-amber-500 dark:text-amber-400" />
                      ) : (
                        <FileIcon className="h-3.5 w-3.5 shrink-0 text-slate-400 dark:text-slate-500" />
                      )}
                      <span className="truncate text-slate-700 dark:text-slate-300">{item.name}</span>
                      <span className="truncate text-[10px] text-slate-400 dark:text-slate-600">
                        {displayPath(item.path)}
                      </span>
                    </button>
                    <button
                      type="button"
                      onClick={() => void handleDownload(item)}
                      title="下载"
                      className="shrink-0 rounded p-0.5 text-slate-400 hover:bg-slate-200 hover:text-slate-900 dark:text-slate-500 dark:hover:bg-slate-700 dark:hover:text-slate-100"
                    >
                      <Download className="h-3 w-3" />
                    </button>
                  </div>
                ))}
                {searchResults.length === 0 && !searching && (
                  <div className="px-4 py-6 text-center text-xs text-slate-400 dark:text-slate-600">
                    未找到匹配的文件或目录
                  </div>
                )}
              </div>
            ) : (
              <>
                {/* 根节点行：/shared 固定根，支持展开/收起 */}
                <div className="group flex items-center gap-1.5 py-1 pl-3 pr-2 text-xs hover:bg-slate-100 dark:hover:bg-slate-800/60">
                  <button
                    type="button"
                    onClick={() => {
                      setSelectedDir("")
                      setExpanded((prev) => ({ ...prev, "": !(prev[""] ?? true) }))
                    }}
                    className="flex min-w-0 flex-1 items-center gap-1.5 text-left"
                  >
                    {(expanded[""] ?? true) ? (
                      <ChevronDown className="h-3.5 w-3.5 shrink-0 text-slate-400 dark:text-slate-500" />
                    ) : (
                      <ChevronRight className="h-3.5 w-3.5 shrink-0 text-slate-400 dark:text-slate-500" />
                    )}
                    <FolderOpen className="h-3.5 w-3.5 shrink-0 text-amber-500 dark:text-amber-400" />
                    <span className="truncate font-medium text-slate-900 dark:text-slate-200">/shared</span>
                  </button>
                  <span className="hidden shrink-0 items-center gap-0.5 rounded bg-white px-1 py-0.5 shadow-md ring-1 ring-slate-200 group-hover:flex dark:bg-slate-900/90 dark:ring-slate-700">
                    <button
                      type="button"
                      onClick={() => setPrompt({ mode: "file", path: "" })}
                      title="新建文件"
                      className={ACTION_BTN}
                    >
                      <FilePlus2 className="h-3 w-3" />
                    </button>
                    <button
                      type="button"
                      onClick={() => setPrompt({ mode: "mkdir", path: "" })}
                      title="新建子目录"
                      className={ACTION_BTN}
                    >
                      <FolderPlus className="h-3 w-3" />
                    </button>
                  </span>
                </div>

                {(expanded[""] ?? true) && (
                  <div>
                    {(childrenMap[""] ?? []).map((entry) => (
                      <TreeNode key={entry.path} entry={entry} depth={1} ctx={treeCtx} />
                    ))}
                    {!rootLoading && (childrenMap[""] ?? []).length === 0 && (
                      <div className="py-8 text-center text-xs text-slate-400 dark:text-slate-600">
                        共享目录为空，上传文件或新建目录开始协作
                      </div>
                    )}
                    {rootLoading && (
                      <div className="py-4 text-center text-xs text-slate-500">正在加载目录…</div>
                    )}
                  </div>
                )}
              </>
            )}
          </div>
        </section>

        {/* 右：预览占位卡片（本页面不做在线预览） */}
        <section className="flex min-h-[320px] flex-col rounded-xl border border-slate-200 bg-white dark:border-slate-800 dark:bg-[#0f172a]">
          <div className="flex items-center gap-2 border-b border-slate-200 px-4 py-2.5 dark:border-slate-800">
            <ShieldCheck className="h-4 w-4 text-slate-400 dark:text-slate-500" />
            <span className="text-sm font-medium text-slate-900 dark:text-slate-200">文件预览</span>
          </div>
          <div className="flex flex-1 flex-col items-center justify-center gap-3 p-6 text-center">
            <div className="flex h-12 w-12 items-center justify-center rounded-2xl bg-slate-100 text-slate-400 dark:bg-slate-800/70 dark:text-slate-500">
              <FileIcon className="h-6 w-6" />
            </div>
            <div className="text-sm text-slate-500 dark:text-slate-400">选择文件查看内容</div>
            <p className="max-w-xs text-xs leading-relaxed text-slate-400 dark:text-slate-600">
              本页面不提供在线预览能力，文件仅支持下载。点击文件条目即可直接下载到本地。
            </p>
          </div>
        </section>
      </div>

      {/* 输入弹窗 */}
      {prompt && (
        <PromptDialog
          title={prompt.mode === "rename" ? "重命名" : prompt.mode === "mkdir" ? "新建目录" : "新建文件"}
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
