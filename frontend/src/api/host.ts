import { request } from "./client"

// 6 个折线图共用的一份采样点
export interface HostSample {
  ts: number
  cpuPercent: number
  memoryPercent: number
  netRxBps: number
  netTxBps: number
  diskReadBps: number
  diskWriteBps: number
  pktRx: number
  pktTx: number
  iopsRead: number
  iopsWrite: number
  rxDropPercent: number
  txDropPercent: number
  diskAwaitMs: number
  diskBusyPercent: number
}

// 系统信息卡片
export interface HostBasic {
  name: string
  hostname: string
  osImage: string
  kernelVersion: string
  architecture: string
  cpuCores: number
  podCount: number
  uptimeSeconds: number
  load1: number
  load5: number
  load15: number
  swapUsedBytes: number
  swapTotalBytes: number
  k3sVersion: string
}

// 实时指标（第一行卡片 + 异常与延迟卡片）
export interface HostRealtime {
  available: boolean
  collectorNode: string
  cpuPercent: number
  memoryPercent: number
  memoryUsedBytes: number
  memoryTotalBytes: number
  diskPercent: number
  diskUsedBytes: number
  diskTotalBytes: number
  netRxBps: number
  netTxBps: number
  diskReadBps: number
  diskWriteBps: number
  pktRx: number
  pktTx: number
  iopsRead: number
  iopsWrite: number
  rxDropPercent: number
  txDropPercent: number
  diskAwaitMs: number
  diskBusyPercent: number
}

// 磁盘使用卡片中的一行
export interface DiskPartition {
  mount: string
  usedBytes: number
  totalBytes: number
  percent: number
}

// K3s 节点列表中的一项
export interface NodeBrief {
  name: string
  ready: boolean
  diskPressure: boolean
  memoryPressure: boolean
  pidPressure: boolean
  version: string
}

// GPU 汇总卡片
export interface HostGPU {
  count: number
  driverVersion: string
  items: Array<{
    index: number
    name: string
    utilization: number
    memoryUsed: number
    memoryTotal: number
    temperature: number
  }>
}

// 主机健康详情接口返回结构
export interface HostDetailData {
  collectedAt: number
  node: HostBasic
  realtime: HostRealtime
  series: { maxPoints: number; samples: HostSample[] }
  partitions: DiskPartition[]
  nodes: NodeBrief[]
  gpu: HostGPU
}

// 主机接口：nodeName 传 self 表示后端所在节点
export const hostApi = {
  detail: (nodeName = "self") => request<HostDetailData>({ url: `/host/${nodeName}` }),
}

// 节点平均负载（/proc/loadavg 前三个数）
export interface NodeHealthData {
  load1: number
  load5: number
  load15: number
}

// 负载接口：前端每 10 秒轮询一次
export const nodeHealthApi = {
  detail: () => request<NodeHealthData>({ url: "/node/health" }),
}
