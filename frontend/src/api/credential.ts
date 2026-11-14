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

// SSH 凭证接口：创建 Pod 时自动签发，这里只做查询与连通性测试
export const credentialApi = {
  get: (podName: string) => request<SSHCredential>({ url: `/pods/${podName}/credentials` }),

  // 后端使用该凭证实际登录 SSH 网关并执行 whoami && hostname
  test: (podName: string) =>
    request<{ output: string }>({
      url: `/pods/${podName}/credentials/test`,
      method: "post",
    }),
}
