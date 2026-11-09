import { request } from "./client"

// 概览页数据结构，字段与后端 model.Overview 一一对应
export interface OverviewData {
  updatedAt: number
  pods: {
    running: number
    total: number
    stopped: number
  }
  cpu: {
    load1m: number
    cores: number
    available: boolean
  }
  memory: UsageStat
  disk: UsageStat
  gpu: {
    count: number
    items: GPUCard[]
  }
  cluster: {
    runningPods: number
    totalPods: number
    stoppedPods: number
    groups: number
    nodeCount: number
    readyNodes: number
    version: string
  }
  modules: {
    nodes: number
    nodesOnline: boolean
    agents: number
    agentsOnline: boolean
    attacks: number
    bans: number
  }
}

// 带进度条的用量数据
export interface UsageStat {
  percent: number
  usedBytes: number
  totalBytes: number
  available: boolean
}

// 单张 GPU 卡片数据
export interface GPUCard {
  index: number
  name: string
  utilization: number
  memoryUsed: number
  memoryTotal: number
  temperature: number
}

// 概览接口：3 秒轮询一次
export const overviewApi = {
  get: () => request<OverviewData>({ url: "/overview" }),
}
