// 主题偏好：仅保存在当前浏览器 localStorage，后端不存储。
// 全局通过 <html class="dark"> 生效，Tailwind 使用 darkMode: "class"。

export type Theme = "light" | "dark" | "auto"

const THEME_KEY = "ssedeploy_theme"

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
