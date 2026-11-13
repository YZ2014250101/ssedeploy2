import { request } from "./client"

// Pod 状态码与后端保持一致
export type PodStatus = "running" | "stopped" | "failed" | "notfound"

export interface PodView {
  name: string
  status: PodStatus
  statusText: string
  spec: string
  image: string
  owner: string
  myRole: string
  replicas: number
  readyReplicas: number
  createdAt: number
}

export interface PodDetail extends PodView {
  namespace: string
  cpu: string
  memory: string
  disk: string
  env: EnvVar[]
  command: string[]
  podName: string
  // 创建 Pod 时自动生成的外部访问 Service
  serviceName: string
  servicePort: number
  nodePort: number
}

export interface EnvVar {
  key: string
  value: string
}

export interface PodStats {
  running: number
  stopped: number
  failed: number
  mine: number
}

export interface CreatePodPayload {
  name: string
  image: string
  cpu: string
  memory: string
  disk: string
  replicas: number
  env: EnvVar[]
  command: string[]
}

export interface UpdatePodPayload {
  image: string
  cpu: string
  memory: string
  env: EnvVar[]
  command: string[]
}

export interface MetricPoint {
  ts: number
  cpuMilli: number
  memBytes: number
}

export interface PodMetrics {
  available: boolean
  cpuMilli: number
  memBytes: number
  cpuLimitMilli: number
  memLimitBytes: number
  series: MetricPoint[]
}

export interface PodEvent {
  type: string
  reason: string
  message: string
  count: number
  lastTime: number
}

// Pod 相关接口集合
export const podApi = {
  list: (params?: { keyword?: string; status?: string }) =>
    request<{ items: PodView[]; total: number }>({ url: "/pods", params }),

  stats: () => request<PodStats>({ url: "/pods/stats" }),

  get: (name: string) => request<PodDetail>({ url: `/pods/${name}` }),

  create: (payload: CreatePodPayload) =>
    request<{ name: string }>({ url: "/pods", method: "post", data: payload }),

  update: (name: string, payload: UpdatePodPayload) =>
    request<null>({ url: `/pods/${name}`, method: "put", data: payload }),

  remove: (name: string) => request<null>({ url: `/pods/${name}`, method: "delete" }),

  start: (name: string) => request<null>({ url: `/pods/${name}/start`, method: "post" }),

  stop: (name: string) => request<null>({ url: `/pods/${name}/stop`, method: "post" }),

  restart: (name: string) => request<null>({ url: `/pods/${name}/restart`, method: "post" }),

  metrics: (name: string, minutes = 30) =>
    request<PodMetrics>({ url: `/pods/${name}/metrics`, params: { minutes } }),

  events: (name: string) => request<{ items: PodEvent[] }>({ url: `/pods/${name}/events` }),

  logs: (name: string, tailLines = 500) =>
    request<{ content: string }>({ url: `/pods/${name}/logs`, params: { tailLines } }),
}

// 规格预设：与界面下拉保持一致
export const SPEC_PRESETS = [
  { label: "1C · 2GiB · 5GiB", cpu: "1", memory: "2Gi", disk: "5Gi" },
  { label: "2C · 4GiB · 5GiB", cpu: "2", memory: "4Gi", disk: "5Gi" },
  { label: "4C · 8GiB · 20GiB", cpu: "4", memory: "8Gi", disk: "20Gi" },
  { label: "8C · 16GiB · 50GiB", cpu: "8", memory: "16Gi", disk: "50Gi" },
]
