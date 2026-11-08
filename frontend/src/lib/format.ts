// 通用格式化函数
export function formatBytes(bytes?: number): string {
  if (!bytes || bytes <= 0) return "0 B"
  const units = ["B", "KiB", "MiB", "GiB", "TiB"]
  let value = bytes
  let index = 0
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024
    index++
  }
  return `${value.toFixed(value >= 10 || index === 0 ? 0 : 1)} ${units[index]}`
}

// 毫核转换为可读 CPU 用量
export function formatCPU(milli?: number): string {
  if (!milli || milli <= 0) return "0 m"
  if (milli >= 1000) return `${(milli / 1000).toFixed(2)} 核`
  return `${milli} m`
}

// Unix 秒转本地时间
export function formatTime(ts?: number): string {
  if (!ts) return "-"
  const date = new Date(ts * 1000)
  return date.toLocaleString("zh-CN", { hour12: false })
}

// 只显示时分秒，用于图表 X 轴
export function formatClock(ts: number): string {
  return new Date(ts * 1000).toLocaleTimeString("zh-CN", { hour12: false })
}

// 计算百分比（保留 1 位小数）
export function percent(used?: number, limit?: number): number {
  if (!used || !limit || limit <= 0) return 0
  return Math.min(100, (used / limit) * 100)
}
