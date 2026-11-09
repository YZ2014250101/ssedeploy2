import { request } from "./client"

// 管理员存储大盘接口：GET /api/v1/admin/storage
export interface PVItem {
  name: string
  capacity: string
  reclaimPolicy: string // Delete / Retain / Recycle
  status: string // Bound / Available / Released / Failed
  claimName: string // namespace/name，未绑定时为空
}

export interface PVCItem {
  name: string
  namespace: string
  capacity: string
  status: string
}

export interface StorageClassItem {
  name: string
  provisioner: string
}

export interface StorageOverviewData {
  pvs: PVItem[]
  pvcs: PVCItem[]
  storageClasses: StorageClassItem[]
}

export const storageApi = {
  overview: () => request<StorageOverviewData>({ url: "/admin/storage" }),
}
