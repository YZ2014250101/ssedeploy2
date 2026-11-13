import { request } from "./client"

// 中间件类型：与后端 model.Middleware 中的常量保持一致
export type MiddlewareType = "mysql" | "minio"
export type MiddlewareStatus = "running" | "creating" | "failed" | "notfound"

// 一条中间件实例：既包含连接信息，也包含 K8s 实际状态
export interface MiddlewareView {
  id: number
  namespace: string
  podName: string
  type: MiddlewareType
  name: string
  version: string
  image: string
  storage: string
  username: string
  password: string
  database: string
  port: number
  host: string
  status: MiddlewareStatus
  createdAt: number
}

export interface CreateMiddlewarePayload {
  type: MiddlewareType
  name: string
  version?: string
  storage?: string
  database?: string
}

// 中间件接口：归属某个 Pod，路径与部署实例保持一致
export const middlewareApi = {
  list: (podName: string) =>
    request<{ items: MiddlewareView[] }>({ url: `/pods/${podName}/middlewares` }),

  create: (podName: string, payload: CreateMiddlewarePayload) =>
    request<MiddlewareView>({ url: `/pods/${podName}/middlewares`, method: "post", data: payload }),

  // keepPvc 默认 true：保留数据盘，避免误删导致数据丢失
  remove: (podName: string, name: string, keepPvc = true) =>
    request<null>({
      url: `/pods/${podName}/middlewares/${name}`,
      method: "delete",
      params: { keepPvc },
    }),
}
