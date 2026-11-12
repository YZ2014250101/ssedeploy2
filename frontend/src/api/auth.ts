import { request } from "./client"
import type { AuthUser } from "@/lib/auth"

// 认证接口：登录、注册、当前用户
export const authApi = {
  // 登录：后端 /api/v1/login，返回 JWT 与用户信息
  login: (username: string, password: string) =>
    request<{ token: string; user: AuthUser }>({
      url: "/login",
      method: "post",
      data: { username, password },
    }),

  // 注册：confirmPassword 仅前端校验，不提交给后端
  register: (username: string, email: string, password: string) =>
    request<{ id: number; username: string; email: string }>({
      url: "/register",
      method: "post",
      data: { username, email, password },
    }),

  me: () => request<AuthUser>({ url: "/auth/me" }),
}
