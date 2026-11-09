import axios, { type AxiosRequestConfig } from "axios"
import { clearAuth, getToken } from "@/lib/auth"

// 集群页接口：独立 baseURL /api/cluster，后端统一返回 { code, msg, data }
const http = axios.create({ baseURL: "/api/cluster", timeout: 20000 })

http.interceptors.request.use((config) => {
  const token = getToken()
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

http.interceptors.response.use(
  (response) => {
    const body = response.data
    if (body && typeof body.code === "number" && body.code !== 0) {
      return Promise.reject(new Error(body.msg || "请求失败"))
    }
    return body?.data
  },
  (error) => {
    const status = error.response?.status
    if (status === 401) {
      clearAuth()
      if (!location.pathname.startsWith("/login")) {
        location.href = "/login"
      }
    }
    const message = error.response?.data?.msg || error.message || "网络异常，请稍后重试"
    return Promise.reject(new Error(message))
  },
)

async function request<T>(config: AxiosRequestConfig): Promise<T> {
  const data = await http.request(config)
  return data as unknown as T
}

// 时间粒度
export type ClusterRange = "15m" | "1h" | "6h" | "24h" | "7d"

export interface ClusterNode {
  nodeName: string
  status: "online" | "offline"
  cpuPercent: number
  memPercent: number
  load: number
}

export interface ClusterMetricsData {
  timestamps: string[]
  cpu: Record<string, number[]>
  mem: Record<string, number[]>
  load: Record<string, number[]>
}

export interface GpuItem {
  uuid: string
}

export interface GpuMetricsData {
  timestamps: string[]
  util: Record<string, number[]>
  memUsed: Record<string, number[]>
  temp: Record<string, number[]>
}

export const clusterApi = {
  nodes: () => request<ClusterNode[]>({ url: "/nodes" }),
  metrics: (range: ClusterRange) => request<ClusterMetricsData>({ url: "/metrics", params: { range } }),
  gpus: (nodeName: string) =>
    request<GpuItem[]>({ url: `/node/${encodeURIComponent(nodeName)}/gpus` }),
  gpuMetrics: (nodeName: string, range: ClusterRange) =>
    request<GpuMetricsData>({
      url: `/node/${encodeURIComponent(nodeName)}/gpu-metrics`,
      params: { range },
    }),
}
