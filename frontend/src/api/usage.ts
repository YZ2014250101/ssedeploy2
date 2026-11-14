import { request } from "./client"

// 用量面板支持的时间范围
export type UsageRange = "24h" | "7d" | "30d"

// 顶部概览卡片
export interface UsageCard {
  key: string
  title: string
  value: number
  unit: string
  // 仅 CPU 利用率卡片返回，单位百分比
  utilization?: number
}

// 单个聚合时段的用量数据，图表与明细表共用
export interface UsagePoint {
  // 聚合时间戳（秒），用于按时间正确排序
  ts: number
  // 展示用时间标签：小时范围为 HH:mm，30 天范围为 MM-DD
  time: string
  cpuUsed: number
  cpuRequested: number
  gpuMem: number
  gpuCardHours: number
  memory: number
  storageDir: number
  storageBucket: number
}

// 用量面板完整响应
export interface UsageData {
  range: UsageRange
  cards: UsageCard[]
  points: UsagePoint[]
}

export const usageApi = {
  get: (podName: string, range: UsageRange) =>
    request<UsageData>({
      url: `/pods/${podName}/usage`,
      params: { range },
    }),
}
