package model

// LogFile 是工作区日志目录下的一个日志文件。
type LogFile struct {
	Name string `json:"name"`
	Size int64  `json:"size"` // 文件大小（Bytes）
}

// AppLog 是日志文件读取结果。
// Lines 只包含文件末尾 N 行，避免大文件整体返回撑爆内存。
type AppLog struct {
	FileName string   `json:"fileName"` // 文件名称，例如 app.log
	FilePath string   `json:"filePath"` // 容器内绝对路径，例如 /home/workspace/logs/app.log
	Size     int64    `json:"size"`     // 文件大小（Bytes）
	Lines    []string `json:"lines"`    // 末尾 N 行内容
}
