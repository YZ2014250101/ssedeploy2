import { request } from "./client"

// 个人管理页账户信息（GET /api/user/profile）
export interface UserProfileData {
  uid: string
  username: string
  role: string
  permissionCount: number
  displayName: string
  email: string
  theme: "light" | "dark" | "auto"
}

export const profileApi = {
  detail: () => request<UserProfileData>({ url: "/user/profile" }),
}
