import { request } from "./client"

// 顶部汇总卡片
export interface GPUSummaryData {
  count: number
  avgUtil: number
  memUsedBytes: number
  memTotalBytes: number
  maxTemp: number
  driverVersion: string
}

// 单块 GPU 实时指标
export interface GPUDeviceData {
  id: number
  name: string
  temp: number
  power: number
  powerMax: number
  util: number
  memUsedBytes: number
  memTotalBytes: number
}

// 趋势采样点：util 为百分比，mem 为字节
export interface GPUTrendPoint {
  ts: number
  value: number
}

export interface GPUTrendSeries {
  gpuId: number
  util: GPUTrendPoint[]
  mem: GPUTrendPoint[]
}

// 单卡上的 Pod 占用
export interface GPUPodUsage {
  name: string
  namespace: string
  memUsedBytes: number
  percent: number
  util: number
}

export interface GPUPodAlloc {
  gpuId: number
  pods: GPUPodUsage[]
}

// GET /api/gpu 返回结构
export interface GPUOverviewData {
  collectedAt: number
  summary: GPUSummaryData
  gpuList: GPUDeviceData[]
  trend: { maxPoints: number; series: GPUTrendSeries[] }
  podAlloc: GPUPodAlloc[]
}

export const gpuApi = {
  detail: () => request<GPUOverviewData>({ url: "/gpu" }),
}
