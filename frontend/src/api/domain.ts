import { request } from "./client"

// 一条子域名映射，对应集群中的一个 Ingress
export interface DomainMapping {
  prefix: string
  host: string
  remark: string
  podName: string
  serviceName: string
  port: number
  ingressName: string
  createdAt: number
}

export interface CreateDomainPayload {
  prefix: string
  port?: number
  remark?: string
}

export const domainApi = {
  list: (podName: string) =>
    request<{ items: DomainMapping[]; baseDomain: string }>({
      url: `/pods/${podName}/domains`,
    }),

  create: (podName: string, payload: CreateDomainPayload) =>
    request<DomainMapping>({ url: `/pods/${podName}/domains`, method: "post", data: payload }),

  remove: (podName: string, prefix: string) =>
    request<null>({ url: `/pods/${podName}/domains/${prefix}`, method: "delete" }),
}
