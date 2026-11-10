import type { ReactNode } from "react"
import { Navigate, Route, Routes } from "react-router-dom"
import { Toaster } from "sonner"
import { getToken } from "@/lib/auth"
import AppLayout from "@/components/layout/AppLayout"
import Login from "@/pages/Login"
import Overview from "@/pages/Overview"
import HostHealth from "@/pages/HostHealth"
import GpuMonitor from "@/pages/GpuMonitor"
import Profile from "@/pages/Profile"
import Cluster from "@/pages/Cluster"
import Storage from "@/pages/Storage"
import IDE from "@/pages/IDE"
import Shared from "@/pages/Shared"
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
        {/* IDE 工作台为独立全屏页面，自带深色顶栏，不渲染通用侧边栏 */}
        <Route path="/ide" element={ <RequireAuth> <IDE /> </RequireAuth> } />
        <Route element={ <RequireAuth> <AppLayout /> </RequireAuth> } >
          {/* 默认进入概览页 */}
          <Route index element={<Overview />} />
          <Route path="/overview" element={<Navigate to="/" replace />} />
          <Route path="/hosts" element={<HostHealth />} />
          <Route path="/cluster" element={<Cluster />} />
          <Route path="/gpu" element={<GpuMonitor />} />
          <Route path="/storage" element={<Storage />} />
          <Route path="/shared" element={<Shared />} />
          <Route path="/profile" element={<Profile />} />
          <Route path="/pods" element={<PodList />} />
          <Route path="/pods/:name" element={<PodDetail />} />
        </Route>
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
      <Toaster position="top-center" richColors />
    </>
  )
}
