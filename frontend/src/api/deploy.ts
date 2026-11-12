import { request } from "./client"

// 部署实例状态：与后端 model.DeployStatus* 保持一致
// fatal 表示部署脚本执行失败（非 0 退出码）
export type DeployStatus = "pending" | "running" | "stopped" | "fatal" | "succeeded" | "notfound"

// 部署来源：repo = 绑仓库（从 Git 拉取代码），local = 本地（不拉代码，直接执行脚本）
export type DeploySource = "repo" | "local"

// 部署类型：常驻服务 / 一次性脚本
export type DeployType = "service" | "job"

export interface DeployView {
  podName: string
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
  // push 自动部署回调：路径 + 仅负责人/管理员可见的密钥
  webhookPath: string
  webhookToken: string
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

// 部署相关接口：全部以 Pod 为作用域，部署脚本在目标 Pod 内执行
export const deployApi = {
  list: (podName: string) =>
    request<{ items: DeployView[]; total: number }>({ url: `/pods/${podName}/deployments` }),

  create: (podName: string, payload: CreateDeployPayload) =>
    request<{ name: string }>({ url: `/pods/${podName}/deployments`, method: "post", data: payload }),

  update: (podName: string, name: string, payload: UpdateDeployPayload) =>
    request<null>({ url: `/pods/${podName}/deployments/${name}`, method: "put", data: payload }),

  remove: (podName: string, name: string) =>
    request<null>({ url: `/pods/${podName}/deployments/${name}`, method: "delete" }),

  start: (podName: string, name: string) =>
    request<null>({ url: `/pods/${podName}/deployments/${name}/start`, method: "post" }),

  stop: (podName: string, name: string) =>
    request<null>({ url: `/pods/${podName}/deployments/${name}/stop`, method: "post" }),

  restart: (podName: string, name: string) =>
    request<null>({ url: `/pods/${podName}/deployments/${name}/restart`, method: "post" }),
}
