import { request } from "./client"
import { getToken } from "@/lib/auth"

// IDE 工作台 Pod 信息（仅当前登录用户自己的 Pod）
export interface IDEPod {
  name: string
  status: "running" | "stopped" | "failed" | "notfound"
  statusText: string
  podName: string
  running: boolean
}

// 工作区目录根：home -> /home/workspace，shared -> /home/workspace/shared
export type IDERoot = "home" | "shared"

// 工作区文件/目录条目（path 为相对目录根的路径）
export interface IDEFileEntry {
  path: string
  type: "file" | "dir"
}

export const ideApi = {
  // 当前用户自己的 Pod 列表：GET /api/v1/ide/pods
  pods: () => request<{ items: IDEPod[] }>({ url: "/ide/pods" }),

  // 列举工作区文件：GET /api/v1/ide/pods/:name/files
  files: (name: string, root: IDERoot) =>
    request<{ items: IDEFileEntry[] }>({
      url: `/ide/pods/${encodeURIComponent(name)}/files`,
      params: { root },
    }),

  // 读取文件内容：GET /api/v1/ide/pods/:name/file
  file: (name: string, root: IDERoot, path: string) =>
    request<{ path: string; content: string }>({
      url: `/ide/pods/${encodeURIComponent(name)}/file`,
      params: { root, path },
    }),

  // 保存文件到工作区（终端立即可见）：PUT /api/v1/ide/pods/:name/file
  saveFile: (name: string, root: IDERoot, path: string, content: string) =>
    request<null>({
      url: `/ide/pods/${encodeURIComponent(name)}/file`,
      method: "put",
      data: { root, path, content },
    }),

  // 新建文件/文件夹：POST /api/v1/ide/pods/:name/files
  // path 为父目录（相对目录根，空表示根目录），name 为单层名称
  createEntry: (name: string, root: IDERoot, path: string, entryName: string, type: "file" | "dir") =>
    request<null>({
      url: `/ide/pods/${encodeURIComponent(name)}/files`,
      method: "post",
      data: { root, path, name: entryName, type },
    }),

  // 重命名：PATCH /api/v1/ide/pods/:name/file
  renameEntry: (name: string, root: IDERoot, path: string, newName: string) =>
    request<null>({
      url: `/ide/pods/${encodeURIComponent(name)}/file`,
      method: "patch",
      data: { root, path, name: newName },
    }),

  // 删除文件/文件夹：DELETE /api/v1/ide/pods/:name/file
  deleteEntry: (name: string, root: IDERoot, path: string) =>
    request<null>({
      url: `/ide/pods/${encodeURIComponent(name)}/file`,
      method: "delete",
      params: { root, path },
    }),
}

/**
 * 下载工作区文件：后端返回文件流，前端生成 Blob 后触发浏览器下载。
 * 下载接口需要携带 JWT，因此使用 fetch 而不是 <a href> 直接访问。
 */
export async function downloadWorkspaceFile(podName: string, root: IDERoot, path: string): Promise<void> {
  const params = new URLSearchParams({ root, path })
  const token = getToken()
  const response = await fetch(`/api/v1/ide/pods/${encodeURIComponent(podName)}/download?${params.toString()}`, {
    headers: token ? { Authorization: `Bearer ${token}` } : undefined,
  })
  if (!response.ok) {
    // 后端错误统一为 {code, message}，优先展示 message（如“不允许访问工作区外目录”）
    let message = "下载失败，请稍后重试"
    try {
      const body = await response.json()
      if (body?.message) message = body.message
    } catch {
      // 非 JSON 响应时使用默认提示
    }
    throw new Error(message)
  }

  const blob = await response.blob()
  const fileName = path.split("/").filter(Boolean).pop() || "download"
  const url = URL.createObjectURL(blob)
  const link = document.createElement("a")
  link.href = url
  link.download = fileName
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
}
