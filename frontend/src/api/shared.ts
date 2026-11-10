import { request } from "./client"
import { getToken } from "@/lib/auth"

// 共享目录中的一个节点（path 为相对 /shared 的路径，根目录为空字符串）
export interface SharedEntry {
  name: string
  path: string
  type: "file" | "dir"
  size: number
  modTime: number
}

// 共享目录 AI 洞察摘要
export interface SharedInsight {
  updatedAt: string
  keyStatus: string
  risks: string[]
  suggestions: string[]
  totalFiles: number
  totalDirs: number
  totalSize: number
}

// 共享目录接口封装（统一前缀 /api/v1）
export const sharedApi = {
  // 获取指定目录下的直接子项：GET /shared/list?path=xxx
  list: (path: string) =>
    request<{ root: string; items: SharedEntry[] }>({
      url: "/shared/list",
      params: { path },
    }),

  // 新建目录：POST /shared/mkdir
  mkdir: (path: string, name: string) =>
    request<null>({
      url: "/shared/mkdir",
      method: "post",
      data: { path, name },
    }),

  // 重命名：PATCH /shared/rename
  rename: (path: string, name: string) =>
    request<null>({
      url: "/shared/rename",
      method: "patch",
      data: { path, name },
    }),

  // 删除文件/目录：DELETE /shared/rm?path=xxx
  rm: (path: string) =>
    request<null>({
      url: "/shared/rm",
      method: "delete",
      params: { path },
    }),

  // 上传文件（支持多选）：POST /shared/upload?path=xxx
  upload: (path: string, files: File[]) => {
    const form = new FormData()
    files.forEach((file) => form.append("files", file, file.name))
    return request<{ uploaded: string[] }>({
      url: "/shared/upload",
      method: "post",
      params: { path },
      data: form,
      // 大文件上传不设超时上限，避免内网慢速上传被中断
      timeout: 0,
    })
  },

  // AI 洞察摘要：GET /shared/ai/insight
  insight: () => request<SharedInsight>({ url: "/shared/ai/insight" }),
}

// parseFileName 从 Content-Disposition 中解析文件名（优先 RFC 5987 的 UTF-8 形式）
function parseFileName(header: string | null, fallback: string): string {
  if (!header) return fallback
  const utf8 = /filename\*=UTF-8''([^;]+)/i.exec(header)
  if (utf8?.[1]) {
    try {
      return decodeURIComponent(utf8[1])
    } catch {
      // 解码失败时退回普通 filename
    }
  }
  const plain = /filename="?([^";]+)"?/i.exec(header)
  return plain?.[1] || fallback
}

/**
 * 下载共享目录中的文件或目录（目录由后端打包为 zip 流）。
 * 下载接口需要 JWT，因此用 fetch 取 Blob 后触发浏览器下载。
 */
export async function downloadSharedEntry(path: string): Promise<void> {
  const token = getToken()
  const params = new URLSearchParams({ path })
  const response = await fetch(`/api/v1/shared/download?${params.toString()}`, {
    headers: token ? { Authorization: `Bearer ${token}` } : undefined,
  })
  if (!response.ok) {
    // 后端错误统一为 {code, message}，优先展示 message
    let message = "下载失败，请稍后重试"
    try {
      const body = await response.json()
      if (body?.message) message = body.message
    } catch {
      // 非 JSON 响应时使用默认提示
    }
    throw new Error(message)
  }

  const fallback = path.split("/").filter(Boolean).pop() || "download"
  const fileName = parseFileName(response.headers.get("Content-Disposition"), fallback)
  const blob = await response.blob()
  const url = URL.createObjectURL(blob)
  const link = document.createElement("a")
  link.href = url
  link.download = fileName
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
}

// searchShared 基于现有 list 接口做广度优先遍历，返回名称包含关键字的节点。
// 限制遍历目录数与结果数，避免超大共享目录拖慢页面。
export async function searchShared(keyword: string): Promise<SharedEntry[]> {
  const query = keyword.trim().toLowerCase()
  if (!query) return []

  const queue: string[] = [""]
  const visited = new Set<string>()
  const results: SharedEntry[] = []
  let scannedDirs = 0

  while (queue.length > 0 && scannedDirs < 120 && results.length < 100) {
    const dir = queue.shift() as string
    if (visited.has(dir)) continue
    visited.add(dir)
    scannedDirs++

    let items: SharedEntry[] = []
    try {
      items = (await sharedApi.list(dir)).items
    } catch {
      continue // 单个目录读取失败（例如刚被删除）时跳过
    }
    for (const item of items) {
      if (item.name.toLowerCase().includes(query) || item.path.toLowerCase().includes(query)) {
        results.push(item)
        if (results.length >= 100) break
      }
      if (item.type === "dir") queue.push(item.path)
    }
  }
  return results
}
