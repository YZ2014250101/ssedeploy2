import { request } from "./client"
import type { AuthUser } from "@/lib/auth"

// 登录接口返回令牌与用户信息
export const authApi = {
  login: (username: string, password: string) =>
    request<{ token: string; user: AuthUser }>({
      url: "/auth/login",
      method: "post",
      data: { username, password },
    }),

  me: () => request<AuthUser>({ url: "/auth/me" }),
}
