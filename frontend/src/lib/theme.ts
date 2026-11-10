// 主题偏好：仅保存在当前浏览器 localStorage，后端不存储。
// 全局通过 <html class="dark"> 生效，Tailwind 使用 darkMode: "class"。
import { useEffect, useState } from "react"

export type Theme = "light" | "dark" | "auto"

const THEME_KEY = "ssedeploy_theme"

// 主题变更事件：个人页切换明暗时广播，供需要感知主题的页面（如 IDE 终端配色）实时响应
const THEME_EVENT = "ssedeploy:theme-change"

// getTheme 读取本地主题偏好，非法值回退为白天
export function getTheme(): Theme {
  const value = localStorage.getItem(THEME_KEY)
  if (value === "dark" || value === "auto") {
    return value
  }
  return "light"
}

// resolveTheme 把「跟随浏览器」解析为实际明暗
export function resolveTheme(theme: Theme): "light" | "dark" {
  if (theme === "auto") {
    return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light"
  }
  return theme
}

// applyTheme 把实际主题写到 html 根节点
export function applyTheme(theme: Theme) {
  const resolved = resolveTheme(theme)
  document.documentElement.classList.toggle("dark", resolved === "dark")
  document.documentElement.style.colorScheme = resolved
}

// saveTheme 保存偏好并立即生效
export function saveTheme(theme: Theme) {
  localStorage.setItem(THEME_KEY, theme)
  applyTheme(theme)
  // 广播主题变更，让已挂载的页面同步切换（个人页开关 -> IDE / 共享页）
  window.dispatchEvent(new CustomEvent<Theme>(THEME_EVENT, { detail: theme }))
}

// useResolvedTheme 订阅全局主题，返回当前实际生效的明暗。
// 数据来源与个人页开关一致：localStorage 偏好 + 系统 prefers-color-scheme。
// 存在意义：xterm 终端等基于 JS 配置的组件无法使用 Tailwind dark: 变体，需要真实主题值。
export function useResolvedTheme(): "light" | "dark" {
  const [resolved, setResolved] = useState<"light" | "dark">(() => resolveTheme(getTheme()))

  useEffect(() => {
    const sync = () => setResolved(resolveTheme(getTheme()))
    const media = window.matchMedia("(prefers-color-scheme: dark)")
    window.addEventListener(THEME_EVENT, sync)
    media.addEventListener("change", sync)
    // 兜底：其它途径直接修改 html class 时也保持同步
    const observer = new MutationObserver(sync)
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ["class"] })
    sync()
    return () => {
      window.removeEventListener(THEME_EVENT, sync)
      media.removeEventListener("change", sync)
      observer.disconnect()
    }
  }, [])

  return resolved
}

// initTheme 在应用挂载前执行；「跟随浏览器」时监听系统主题变化
export function initTheme() {
  applyTheme(getTheme())
  const media = window.matchMedia("(prefers-color-scheme: dark)")
  media.addEventListener("change", () => {
    if (getTheme() === "auto") {
      applyTheme("auto")
    }
  })
}
