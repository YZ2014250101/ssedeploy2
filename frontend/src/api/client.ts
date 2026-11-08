import axios, { type AxiosRequestConfig } from "axios"
import { clearAuth, getToken } from "@/lib/auth"

// 所有接口统一前缀 /api/v1，生产环境由 Nginx 反向代理到后端
export const http = axios.create({ baseURL: "/api/v1", timeout: 20000 })

http.interceptors.request.use((config) => {
  const token = getToken()
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

http.interceptors.response.use(
  (response) => {
    // 后端统一结构 { code, message, data }，这里直接返回 data
    const body = response.data
    if (body && typeof body.code === "number" && body.code !== 0) {
      return Promise.reject(new Error(body.message || "请求失败"))
    }
    return body?.data
  },
  (error) => {
    const status = error.response?.status
    // 登录失效时清理本地态并回到登录页
    if (status === 401) {
      clearAuth()
      if (!location.pathname.startsWith("/login")) {
        location.href = "/login"
      }
    }
    const message = error.response?.data?.message || error.message || "网络异常，请稍后重试"
    return Promise.reject(new Error(message))
  },
)

// request 泛型封装，返回值已经是业务 data
export async function request<T>(config: AxiosRequestConfig): Promise<T> {
  const data = await http.request(config)
  return data as unknown as T
}

// 构造 WebSocket 地址（开发环境由 Vite 代理，生产环境由 Nginx 代理）
export function wsUrl(path: string): string {
  const protocol = location.protocol === "https:" ? "wss" : "ws"
  return `${protocol}://${location.host}/api/v1${path}`
}
