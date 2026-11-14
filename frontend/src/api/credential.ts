import { request } from "./client"

// 平台为单个 Pod 签发的 SSH 连接凭证
export interface SSHCredential {
  id: number
  namespace: string
  podName: string
  host: string
  port: number
  username: string
  password: string
  createdAt: number
}

// 一条 SSH 连接记录，由平台内置网关在认证成功/失败/断开时写入
export interface SSHEvent {
  id: number
  namespace: string
  podName: string
  username: string
  remoteAddr: string
  event: "accepted" | "failed" | "disconnect"
  message: string
  createdAt: number
}

// SSH 凭证接口：创建 Pod 时自动签发，这里只做查询与连通性测试
export const credentialApi = {
  get: (podName: string) => request<SSHCredential>({ url: `/pods/${podName}/credentials` }),

  // 后端使用该凭证实际登录 SSH 网关并执行 whoami && hostname
  test: (podName: string) =>
    request<{ output: string }>({
      url: `/pods/${podName}/credentials/test`,
      method: "post",
    }),

  // 该 Pod 最近的 SSH 连接记录（默认 100 条，后端按时间倒序返回）
  sshEvents: (podName: string, limit = 100) =>
    request<{ items: SSHEvent[] }>({
      url: `/pods/${podName}/ssh-events`,
      params: { limit },
    }),
}
