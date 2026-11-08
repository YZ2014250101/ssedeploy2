import type { ReactNode } from "react"
import { Navigate, Route, Routes } from "react-router-dom"
import { Toaster } from "sonner"
import { getToken } from "@/lib/auth"
import AppLayout from "@/components/layout/AppLayout"
import Login from "@/pages/Login"
import PodList from "@/pages/PodList"
import PodDetail from "@/pages/PodDetail"

// 路由守卫：未登录直接跳转登录页
function RequireAuth({ children }: { children: ReactNode }) {
  if (!getToken()) {
    return <Navigate to="/login" replace />
  }
  return <>{children}</>
}

export default function App() {
  return (
    <>
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route
          element={
            <RequireAuth>
              <AppLayout />
            </RequireAuth>
          }
        >
          <Route index element={<Navigate to="/pods" replace />} />
          <Route path="/pods" element={<PodList />} />
          <Route path="/pods/:name" element={<PodDetail />} />
        </Route>
        <Route path="*" element={<Navigate to="/pods" replace />} />
      </Routes>
      <Toaster position="top-center" richColors />
    </>
  )
}
