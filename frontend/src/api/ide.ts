import { request } from "./client"

// IDE 工作台 Pod 信息（仅当前登录用户自己的 Pod）
export interface IDEPod {
  name: string
  status: "running" | "stopped" | "failed" | "notfound"
  statusText: string
  podName: string
  running: boolean
}

export const ideApi = {
  // 当前用户自己的 Pod 列表：GET /api/v1/ide/pods
  pods: () => request<{ items: IDEPod[] }>({ url: "/ide/pods" }),
}
