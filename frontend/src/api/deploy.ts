import { request } from "./client"

// 部署实例状态：与后端 model.DeployStatus* 保持一致
// fatal 表示运行致命错误（服务启动失败、镜像拉取失败、CrashLoop 等）
export type DeployStatus = "pending" | "running" | "stopped" | "fatal" | "succeeded" | "notfound"

// 部署来源：repo = 绑仓库（从 Git 拉取代码），local = 本地内置运行时
export type DeploySource = "repo" | "local"

// 部署类型：常驻服务 / 一次性脚本
export type DeployType = "service" | "job"

export interface DeployView {
  name: string
  status: DeployStatus
  statusText: string
  message: string
  source: DeploySource
  sourceText: string
  repoUrl: string
  branch: string
  subDir: string
  type: DeployType
  typeText: string
  scheduleType: string
  gpu: string
  healthPath: string
  timeoutSec: number
  autoDeploy: boolean
  owner: string
  myRole: string
  createdAt: number
}

export interface CreateDeployPayload {
  source: DeploySource
  name: string
  repoUrl: string
  branch: string
  subDir: string
  type: DeployType
  scheduleType: string
  gpu: string
  token: string
  healthPath: string
  timeoutSec: number
  autoDeploy: boolean
}

export interface UpdateDeployPayload {
  repoUrl: string
  branch: string
  subDir: string
  type: DeployType
  scheduleType: string
  gpu: string
  token: string
  healthPath: string
  timeoutSec: number
  autoDeploy: boolean
}

// 部署相关接口集合
export const deployApi = {
  list: () => request<{ items: DeployView[]; total: number }>({ url: "/deployments" }),

  create: (payload: CreateDeployPayload) =>
    request<{ name: string }>({ url: "/deployments", method: "post", data: payload }),

  update: (name: string, payload: UpdateDeployPayload) =>
    request<null>({ url: `/deployments/${name}`, method: "put", data: payload }),

  remove: (name: string) => request<null>({ url: `/deployments/${name}`, method: "delete" }),

  start: (name: string) => request<null>({ url: `/deployments/${name}/start`, method: "post" }),

  stop: (name: string) => request<null>({ url: `/deployments/${name}/stop`, method: "post" }),

  restart: (name: string) => request<null>({ url: `/deployments/${name}/restart`, method: "post" }),
}
