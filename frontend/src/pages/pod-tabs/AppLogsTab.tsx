import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react"
import { ChevronDown, ExternalLink, FileText, Loader2, RefreshCw, X } from "lucide-react"
import { toast } from "sonner"
import { appLogApi, type AppLogFile, type AppLogResult } from "@/api/applog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { cn } from "@/lib/utils"

// 单次拉取的末尾行数：默认 500 行，既能看最新日志又不会撑爆内存
const TAIL_LINES = 500

// 日志教程：跳转到 K8s 官方日志文档
const TUTORIAL_URL = "https://kubernetes.io/docs/concepts/cluster-administration/logging/"

// 日志级别高亮配色（与终端深色背景搭配）
const LEVEL_COLORS: Record<string, string> = {
  INFO: "text-emerald-400",
  ERROR: "text-red-400",
  WARN: "text-amber-300",
  WARNING: "text-amber-300",
  DEBUG: "text-sky-400",
  TRACE: "text-slate-400",
  FATAL: "text-red-500",
}

// 匹配形如 [INFO] / [ERROR] 的级别标记
const LEVEL_RE = /\[(INFO|ERROR|WARN|WARNING|DEBUG|TRACE|FATAL)\]/gi

// 把一行日志拆成「普通文本 + 级别高亮」片段
function highlightLine(line: string): ReactNode[] {
  const nodes: ReactNode[] = []
  let last = 0
  let matched: RegExpExecArray | null
  LEVEL_RE.lastIndex = 0
  while ((matched = LEVEL_RE.exec(line)) !== null) {
    if (matched.index > last) {
      nodes.push(line.slice(last, matched.index))
    }
    const level = matched[1].toUpperCase()
    nodes.push(
      <span key={matched.index} className={LEVEL_COLORS[level] ?? "text-slate-100"}>
        {matched[0]}
      </span>,
    )
    last = matched.index + matched[0].length
  }
  if (last < line.length) {
    nodes.push(line.slice(last))
  }
  return nodes
}

// 文件大小展示：小文件按截图风格显示为 bytes
function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} bytes`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 / 1024).toFixed(2)} MB`
}

// 应用日志 Tab：选择工作区 logs 目录下的日志文件，按终端样式展示末尾 500 行
export function AppLogsTab({ podName }: { podName: string }) {
  const [files, setFiles] = useState<AppLogFile[]>([])
  const [filesLoading, setFilesLoading] = useState(true)
  const [selected, setSelected] = useState("")

  const [result, setResult] = useState<AppLogResult | null>(null)
  const [loading, setLoading] = useState(false)
  const [refreshing, setRefreshing] = useState(false)

  // 下拉框：展开态与搜索关键字
  const [menuOpen, setMenuOpen] = useState(false)
  const [keyword, setKeyword] = useState("")

  const scrollRef = useRef<HTMLDivElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)

  const loadFiles = useCallback(async () => {
    const { items } = await appLogApi.files(podName)
    setFiles(items)
    return items
  }, [podName])

  // 首次进入：拉文件列表并自动选中第一个文件
  useEffect(() => {
    let alive = true
    setFilesLoading(true)
    setSelected("")
    setResult(null)
    loadFiles()
      .then((items) => {
        if (alive && items.length > 0) {
          setSelected(items[0].name)
        }
      })
      .catch((error) => {
        if (alive) toast.error((error as Error).message)
      })
      .finally(() => {
        if (alive) setFilesLoading(false)
      })
    return () => {
      alive = false
    }
  }, [loadFiles])

  // 选中文件变化：只拉取末尾 N 行
  useEffect(() => {
    if (!selected) {
      setLoading(false)
      setResult(null)
      return
    }
    let alive = true
    setLoading(true)
    appLogApi
      .read(podName, selected, TAIL_LINES)
      .then((data) => {
        if (alive) setResult(data)
      })
      .catch((error) => {
        if (alive) {
          setResult(null)
          toast.error((error as Error).message)
        }
      })
      .finally(() => {
        if (alive) setLoading(false)
      })
    return () => {
      alive = false
    }
  }, [podName, selected])

  // 加载完成后自动滚动到底部，展示最新日志
  useEffect(() => {
    const element = scrollRef.current
    if (element) element.scrollTop = element.scrollHeight
  }, [result])

  // 点击下拉框外部时收起
  useEffect(() => {
    if (!menuOpen) return
    function onPointerDown(event: MouseEvent) {
      if (menuRef.current && !menuRef.current.contains(event.target as Node)) {
        setMenuOpen(false)
      }
    }
    document.addEventListener("mousedown", onPointerDown)
    return () => document.removeEventListener("mousedown", onPointerDown)
  }, [menuOpen])

  // 刷新：同时更新文件列表与当前文件内容
  async function refresh() {
    setRefreshing(true)
    try {
      const items = await loadFiles()
      const target = items.some((item) => item.name === selected) ? selected : items[0]?.name ?? ""
      if (target !== selected) {
        setSelected(target)
      } else if (target) {
        setResult(await appLogApi.read(podName, target, TAIL_LINES))
      } else {
        setResult(null)
      }
      toast.success("已刷新最新日志")
    } catch (error) {
      toast.error((error as Error).message)
    } finally {
      setRefreshing(false)
    }
  }

  // 清空选择：回到未选中状态
  function clearSelection() {
    setSelected("")
    setResult(null)
    setKeyword("")
  }

  const filteredFiles = useMemo(() => {
    const kw = keyword.trim().toLowerCase()
    if (!kw) return files
    return files.filter((file) => file.name.toLowerCase().includes(kw))
  }, [files, keyword])

  // 逐行渲染结果做缓存，避免输入搜索关键字时重复计算高亮
  const renderedLines = useMemo(
    () =>
      (result?.lines ?? []).map((line, index) => (
        <div key={index} className="whitespace-pre hover:bg-white/5">
          {highlightLine(line)}
        </div>
      )),
    [result],
  )

  const selectedMeta = files.find((file) => file.name === selected)
  const displayPath = result?.filePath ?? (selected ? `/home/workspace/logs/${selected}` : "")
  const displaySize = result?.size ?? selectedMeta?.size ?? 0

  return (
    <div className="space-y-3">
      <div className="space-y-1.5">
        <div className="text-xs font-medium text-slate-500">日志文件</div>

        <div className="flex flex-wrap items-center gap-2">
          {/* 可搜索、可清空的文件选择器 */}
          <div ref={menuRef} className="relative min-w-[200px] flex-1">
            <div
              role="button"
              tabIndex={0}
              onClick={() => setMenuOpen((open) => !open)}
              onKeyDown={(event) => {
                if (event.key === "Enter" || event.key === " ") setMenuOpen((open) => !open)
              }}
              className="flex h-10 w-full cursor-pointer items-center justify-between gap-2 rounded-lg border border-slate-200 bg-white px-3 text-sm shadow-sm transition-colors hover:border-slate-300"
            >
              <span className={cn("truncate", selected ? "text-slate-700" : "text-slate-400")}>
                {filesLoading ? "正在加载日志文件..." : selected || "请选择日志文件"}
              </span>
              <span className="flex shrink-0 items-center gap-1">
                {selected && (
                  <button
                    type="button"
                    aria-label="清空选择"
                    title="清空选择"
                    className="rounded p-0.5 hover:bg-slate-100"
                    onClick={(event) => {
                      event.stopPropagation()
                      clearSelection()
                    }}
                  >
                    <X className="h-3.5 w-3.5 text-slate-400" />
                  </button>
                )}
                <ChevronDown
                  className={cn(
                    "h-4 w-4 text-slate-400 transition-transform",
                    menuOpen && "rotate-180",
                  )}
                />
              </span>
            </div>

            {menuOpen && (
              <div className="absolute left-0 right-0 z-20 mt-1 overflow-hidden rounded-lg border border-slate-200 bg-white shadow-lg">
                <div className="border-b border-slate-100 p-2">
                  <Input
                    autoFocus
                    value={keyword}
                    onChange={(event) => setKeyword(event.target.value)}
                    placeholder="搜索日志文件"
                    className="h-8 text-xs"
                  />
                </div>
                <div className="max-h-56 overflow-auto py-1">
                  {filteredFiles.length === 0 ? (
                    <div className="px-3 py-6 text-center text-xs text-slate-400">
                      {files.length === 0 ? "logs 目录下暂无日志文件" : "没有匹配的日志文件"}
                    </div>
                  ) : (
                    filteredFiles.map((file) => (
                      <button
                        key={file.name}
                        type="button"
                        onClick={() => {
                          setSelected(file.name)
                          setKeyword("")
                          setMenuOpen(false)
                        }}
                        className={cn(
                          "flex w-full items-center justify-between gap-2 px-3 py-2 text-left text-xs hover:bg-slate-50",
                          file.name === selected && "bg-sky-50 text-sky-700",
                        )}
                      >
                        <span className="truncate font-mono">{file.name}</span>
                        <span className="shrink-0 text-slate-400">{formatBytes(file.size)}</span>
                      </button>
                    ))
                  )}
                </div>
              </div>
            )}
          </div>

          <Button variant="outline" className="h-10" onClick={refresh} disabled={refreshing}>
            {refreshing ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <RefreshCw className="h-4 w-4" />
            )}
            刷新
          </Button>

          <a href={TUTORIAL_URL} target="_blank" rel="noreferrer">
            <Button variant="outline" className="h-10">
              <ExternalLink className="h-4 w-4" />
              日志教程
            </Button>
          </a>
        </div>

        {/* 当前文件绝对路径与大小 */}
        <div className="flex items-center gap-1.5 text-xs text-slate-400">
          <FileText className="h-3.5 w-3.5 shrink-0" />
          {displayPath ? (
            <>
              <span className="truncate font-mono">{displayPath}</span>
              <span className="shrink-0">· {formatBytes(displaySize)}</span>
            </>
          ) : (
            <span>选择日志文件后展示路径与大小</span>
          )}
        </div>
      </div>

      {/* 终端样式日志区 */}
      <div
        ref={scrollRef}
        className="h-[540px] overflow-auto rounded-xl bg-[#1e1e1e] p-4 font-mono text-xs leading-relaxed text-slate-100"
      >
        {loading ? (
          <div className="flex h-full items-center justify-center gap-2 text-slate-400">
            <Loader2 className="h-4 w-4 animate-spin" />
            正在加载日志...
          </div>
        ) : result && result.lines.length > 0 ? (
          renderedLines
        ) : (
          <div className="flex h-full items-center justify-center text-slate-500">
            {files.length === 0
              ? "logs 目录下暂无日志文件（/home/workspace/logs）"
              : selected
                ? "该日志文件为空"
                : "请选择要查看的日志文件"}
          </div>
        )}
      </div>
    </div>
  )
}
