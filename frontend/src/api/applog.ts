import { request } from "./client"

// 日志目录下的一个文件
export interface AppLogFile {
  name: string
  size: number
}

// 日志读取结果：只包含文件末尾 N 行
export interface AppLogResult {
  fileName: string
  filePath: string
  size: number
  lines: string[]
}

// 应用日志接口：日志文件位于 Pod 工作区的 logs 目录
export const appLogApi = {
  files: (podName: string) =>
    request<{ items: AppLogFile[] }>({ url: `/pods/${podName}/app-logs/files` }),

  read: (podName: string, fileName: string, lines = 500) =>
    request<AppLogResult>({ url: `/pods/${podName}/app-logs`, params: { fileName, lines } }),
}
