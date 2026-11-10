package handler

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
)

// maxUploadBytes 单次上传请求的总体积上限（512MB）。
const maxUploadBytes = 512 << 20

// sharedMkdirRequest 新建目录请求体。
type sharedMkdirRequest struct {
	Path string `json:"path"` // 父目录（相对 /shared，空表示根目录）
	Name string `json:"name"` // 新目录名称（单层名称）
}

// sharedRenameRequest 重命名请求体。
type sharedRenameRequest struct {
	Path string `json:"path"` // 原路径（相对 /shared）
	Name string `json:"name"` // 新名称（单层名称）
}

// SharedList 获取指定目录下的文件列表（单层，前端按需懒加载实现无限层级）。
// 路由：GET /api/v1/shared/list?path=xxx
func (h *Handler) SharedList(c *gin.Context) {
	items, err := h.SharedSvc.List(c.Query("path"))
	if err != nil {
		respondErr(c, err)
		return
	}
	OK(c, gin.H{"root": "/shared", "items": items})
}

// SharedMkdir 新建目录。
// 路由：POST /api/v1/shared/mkdir
func (h *Handler) SharedMkdir(c *gin.Context) {
	var req sharedMkdirRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "请求参数不合法")
		return
	}
	if err := h.SharedSvc.Mkdir(req.Path, req.Name); err != nil {
		respondErr(c, err)
		return
	}
	OK(c, nil)
}

// SharedUpload 上传文件（支持一次选择多个文件）。
// 表单字段名兼容 files / file，父目录由 query path 指定。
// 路由：POST /api/v1/shared/upload?path=xxx
func (h *Handler) SharedUpload(c *gin.Context) {
	// 限制请求体大小，防止超大文件打满磁盘或内存
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadBytes)
	if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
		Fail(c, http.StatusBadRequest, "上传内容过大或格式不正确")
		return
	}
	form := c.Request.MultipartForm
	if form == nil {
		Fail(c, http.StatusBadRequest, "请选择要上传的文件")
		return
	}

	files := form.File["files"]
	if len(files) == 0 {
		files = form.File["file"] // 兼容单文件字段名
	}
	if len(files) == 0 {
		Fail(c, http.StatusBadRequest, "请选择要上传的文件")
		return
	}

	parent := c.Query("path")
	uploaded := make([]string, 0, len(files))
	for _, header := range files {
		// 浏览器可能携带完整路径，这里只保留最后一段文件名
		name := path.Base(strings.ReplaceAll(header.Filename, "\\", "/"))
		if name == "" || name == "." || name == "/" {
			Fail(c, http.StatusBadRequest, "文件名不合法")
			return
		}
		file, err := header.Open()
		if err != nil {
			Fail(c, http.StatusBadRequest, "读取上传文件失败")
			return
		}
		err = h.SharedSvc.SaveUpload(parent, name, file)
		_ = file.Close()
		if err != nil {
			respondErr(c, err)
			return
		}
		uploaded = append(uploaded, name)
	}
	OK(c, gin.H{"uploaded": uploaded})
}

// SharedRemove 删除文件或目录（目录递归删除）。
// 路由：DELETE /api/v1/shared/rm?path=xxx
func (h *Handler) SharedRemove(c *gin.Context) {
	if err := h.SharedSvc.Remove(c.Query("path")); err != nil {
		respondErr(c, err)
		return
	}
	OK(c, nil)
}

// SharedRename 重命名文件或目录。
// 路由：PATCH /api/v1/shared/rename
func (h *Handler) SharedRename(c *gin.Context) {
	var req sharedRenameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "请求参数不合法")
		return
	}
	if err := h.SharedSvc.Rename(req.Path, req.Name); err != nil {
		respondErr(c, err)
		return
	}
	OK(c, nil)
}

// SharedDownload 下载文件或目录（目录自动打包为 zip 流）。
// 路由：GET /api/v1/shared/download?path=xxx
func (h *Handler) SharedDownload(c *gin.Context) {
	stream, fileName, isDir, err := h.SharedSvc.OpenDownload(c.Query("path"))
	if err != nil {
		respondErr(c, err)
		return
	}
	defer stream.Close()

	if isDir {
		fileName += ".zip"
	}
	// 同时给出 ASCII 与 UTF-8 文件名，兼容中文名与旧浏览器
	safeName := strings.NewReplacer(`"`, "", `\`, "", "\r", "", "\n", "").Replace(fileName)
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, safeName, url.PathEscape(fileName)))
	c.Header("Content-Type", "application/octet-stream")
	c.Header("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(c.Writer, stream)
}

// SharedInsight 返回共享目录的 AI 洞察摘要。
// 路由：GET /api/v1/shared/ai/insight
func (h *Handler) SharedInsight(c *gin.Context) {
	insight, err := h.SharedSvc.Insight()
	if err != nil {
		respondErr(c, err)
		return
	}
	OK(c, insight)
}
