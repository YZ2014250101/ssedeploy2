// 登录态存储：token 与用户信息保存在 localStorage
export interface AuthUser {
  id: number
  username: string
  displayName: string
  role: string
  createdAt: number
}

const TOKEN_KEY = "ssd_token"
const USER_KEY = "ssd_user"

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY)
}

export function getUser(): AuthUser | null {
  const raw = localStorage.getItem(USER_KEY)
  if (!raw) return null
  try {
    return JSON.parse(raw) as AuthUser
  } catch {
    return null
  }
}

export function saveAuth(token: string, user: AuthUser) {
  localStorage.setItem(TOKEN_KEY, token)
  localStorage.setItem(USER_KEY, JSON.stringify(user))
}

export function clearAuth() {
  localStorage.removeItem(TOKEN_KEY)
  localStorage.removeItem(USER_KEY)
}
