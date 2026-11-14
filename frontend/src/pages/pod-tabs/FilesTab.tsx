import { useCallback, useEffect, useMemo, useState } from "react"
import {
  ArrowLeft,
  FilePlus2,
  FileText,
  Folder,
  FolderPlus,
  Loader2,
  RefreshCw,
} from "lucide-react"
import { toast } from "sonner"
import { ideApi, type IDEFileEntry } from "@/api/ide"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { cn } from "@/lib/utils"

// 工作区根目录在容器内的绝对路径；界面统一按真实路径展示
const WORKSPACE_LABEL = "/home/workspace"

// 新建文件/目录时校验单层名称
function validateEntryName(value: string): string | null {
  const name = value.trim()
  if (!name) return "请输入名称"
  if (name === "." || name === "..") return "名称不合法"
  if (/[/\\]/.test(name)) return "名称不能包含路径分隔符"
  return null
}

// 根据扩展名给文件图标上色，.md 与截图中的文档保持一致
function fileIconClass(name: string): string {
  if (name.endsWith(".md")) return "text-amber-500"
  if (/\.(go|ts|tsx|js|jsx|py|java|rs|sh)$/.test(name)) return "text-sky-500"
  if (/\.(json|yaml|yml|toml|lock)$/.test(name)) return "text-violet-500"
  return "text-slate-400"
}

// 文件 Tab：以面包屑 + 列表的方式浏览 Pod 工作区，点击文件查看内容
export function FilesTab({ podName }: { podName: string }) {
  // 当前目录（相对工作区根，空表示根目录）
  const [dir, setDir] = useState("")
  const [entries, setEntries] = useState<IDEFileEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState("")

  // 文件预览
  const [preview, setPreview] = useState<{ path: string; content: string | null; error?: string } | null>(
    null,
  )
  const [previewLoading, setPreviewLoading] = useState(false)

  // 新建文件/目录弹窗
  const [createType, setCreateType] = useState<"file" | "dir" | null>(null)
  const [createName, setCreateName] = useState("")
  const [creating, setCreating] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    setLoadError("")
    try {
      const { items } = await ideApi.files(podName, "home", dir)
      // 接口返回当前目录下的递归条目，这里只保留直接子项
      const direct = items
        .filter((item) => !item.path.includes("/"))
        .sort((a, b) =>
          a.type === b.type ? a.path.localeCompare(b.path) : a.type === "dir" ? -1 : 1,
        )
      setEntries(direct)
    } catch (error) {
      setLoadError((error as Error).message)
    } finally {
      setLoading(false)
    }
  }, [podName, dir])

  useEffect(() => {
    void load()
  }, [load])

  // 面包屑：/home/workspace + 当前相对路径各层级
  const segments = useMemo(() => (dir ? dir.split("/").filter(Boolean) : []), [dir])

  async function openFile(relativePath: string) {
    setPreview({ path: relativePath, content: null })
    setPreviewLoading(true)
    try {
      const data = await ideApi.file(podName, "home", relativePath)
      setPreview({ path: relativePath, content: data.content })
    } catch (error) {
      setPreview({ path: relativePath, content: null, error: (error as Error).message })
    } finally {
      setPreviewLoading(false)
    }
  }

  function parentDir(): string {
    const parts = dir.split("/").filter(Boolean)
    parts.pop()
    return parts.join("/")
  }

  async function submitCreate() {
    if (!createType) return
    const error = validateEntryName(createName)
    if (error) {
      toast.error(error)
      return
    }
    setCreating(true)
    try {
      await ideApi.createEntry(podName, "home", dir, createName.trim(), createType)
      toast.success(createType === "dir" ? "目录已创建" : "文件已创建")
      setCreateType(null)
      setCreateName("")
      await load()
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setCreating(false)
    }
  }

  return (
    <div className="space-y-3">
      {/* 顶部：返回 + 面包屑 + 操作按钮 */}
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2">
          <Button
            variant="outline"
            size="icon"
            className="h-8 w-8 shrink-0"
            disabled={dir === ""}
            onClick={() => setDir(parentDir())}
            title="返回上一级"
          >
            <ArrowLeft className="h-4 w-4" />
          </Button>

          <div className="flex min-w-0 items-center gap-1 overflow-x-auto text-sm">
            <button
              type="button"
              onClick={() => setDir("")}
              className={cn(
                "shrink-0 rounded px-1 py-0.5 font-mono hover:bg-slate-100",
                segments.length === 0 ? "font-medium text-slate-900" : "text-slate-500",
              )}
            >
              {WORKSPACE_LABEL}
            </button>
            {segments.map((segment, index) => {
              const target = segments.slice(0, index + 1).join("/")
              const isLast = index === segments.length - 1
              return (
                <span key={target} className="flex shrink-0 items-center gap-1">
                  <span className="text-slate-300">/</span>
                  <button
                    type="button"
                    onClick={() => setDir(target)}
                    className={cn(
                      "rounded px-1 py-0.5 font-mono hover:bg-slate-100",
                      isLast ? "font-medium text-slate-900" : "text-slate-500",
                    )}
                  >
                    {segment}
                  </button>
                </span>
              )
            })}
          </div>
        </div>

        <div className="flex shrink-0 items-center gap-2">
          <Button
            variant="outline"
            size="icon"
            className="h-8 w-8"
            onClick={() => void load()}
            disabled={loading}
            title="刷新"
          >
            <RefreshCw className={cn("h-4 w-4", loading && "animate-spin")} />
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              setCreateName("")
              setCreateType("file")
            }}
          >
            <FilePlus2 className="h-3.5 w-3.5" />
            新文件
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              setCreateName("")
              setCreateType("dir")
            }}
          >
            <FolderPlus className="h-3.5 w-3.5" />
            新目录
          </Button>
        </div>
      </div>

      {/* 文件列表 */}
      <div className="overflow-hidden rounded-xl border border-slate-200 bg-white">
        {loading ? (
          <div className="flex h-56 items-center justify-center gap-2 text-sm text-slate-400">
            <Loader2 className="h-4 w-4 animate-spin" />
            正在读取目录...
          </div>
        ) : loadError ? (
          <div className="flex h-56 flex-col items-center justify-center gap-3 text-sm text-slate-500">
            <span>{loadError}</span>
            <Button variant="outline" size="sm" onClick={() => void load()}>
              重试
            </Button>
          </div>
        ) : entries.length === 0 ? (
          <div className="flex h-56 flex-col items-center justify-center gap-1 text-sm text-slate-400">
            <Folder className="h-8 w-8 text-slate-300" />
            当前目录为空
          </div>
        ) : (
          <div className="divide-y divide-slate-100">
            {entries.map((entry) => {
              const fullPath = dir ? `${dir}/${entry.path}` : entry.path
              const isDir = entry.type === "dir"
              return (
                <button
                  key={`${entry.type}:${entry.path}`}
                  type="button"
                  onClick={() => (isDir ? setDir(fullPath) : void openFile(fullPath))}
                  className="flex w-full items-center gap-3 px-4 py-2.5 text-left text-sm text-slate-700 transition-colors hover:bg-slate-50"
                >
                  {isDir ? (
                    <Folder className="h-4 w-4 shrink-0 fill-sky-50 text-sky-500" />
                  ) : (
                    <FileText className={cn("h-4 w-4 shrink-0", fileIconClass(entry.path))} />
                  )}
                  <span className="truncate font-mono">{entry.path}</span>
                </button>
              )
            })}
          </div>
        )}
      </div>

      {/* 文件内容预览 */}
      <Dialog open={Boolean(preview)} onOpenChange={(open) => !open && setPreview(null)}>
        <DialogContent className="max-w-4xl">
          <DialogHeader>
            <DialogTitle className="truncate font-mono text-sm">
              {preview ? `${WORKSPACE_LABEL}/${preview.path}` : ""}
            </DialogTitle>
          </DialogHeader>
          {previewLoading ? (
            <div className="flex h-64 items-center justify-center gap-2 text-sm text-slate-400">
              <Loader2 className="h-4 w-4 animate-spin" />
              正在读取文件...
            </div>
          ) : preview?.error ? (
            <div className="flex h-64 items-center justify-center text-sm text-slate-500">
              {preview.error}
            </div>
          ) : (
            <pre className="max-h-[60vh] overflow-auto rounded-lg bg-slate-950 p-4 font-mono text-xs leading-relaxed whitespace-pre-wrap text-slate-100">
              {preview?.content ?? ""}
            </pre>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => setPreview(null)}>
              关闭
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 新建文件 / 目录 */}
      <Dialog
        open={createType !== null}
        onOpenChange={(open) => {
          if (!open) setCreateType(null)
        }}
      >
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>{createType === "dir" ? "新建目录" : "新建文件"}</DialogTitle>
          </DialogHeader>
          <div className="space-y-2">
            <p className="break-all font-mono text-xs text-slate-400">
              位置：{WORKSPACE_LABEL}
              {dir ? `/${dir}` : ""}
            </p>
            <Input
              autoFocus
              value={createName}
              onChange={(event) => setCreateName(event.target.value)}
              placeholder={createType === "dir" ? "目录名称" : "文件名称，例如 app.log"}
              onKeyDown={(event) => {
                if (event.key === "Enter") void submitCreate()
              }}
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCreateType(null)} disabled={creating}>
              取消
            </Button>
            <Button onClick={() => void submitCreate()} disabled={creating}>
              {creating && <Loader2 className="h-4 w-4 animate-spin" />}
              创建
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
