package handler

import (
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
)

// IDEPods 返回当前用户自己的 Pod 列表，供 IDE 工作台的 Pod 下拉选择器使用。
// 路由：GET /api/v1/ide/pods
func (h *Handler) IDEPods(c *gin.Context) {
	items, err := h.Pods.ListOwned(c.Request.Context(), currentUserID(c))
	if err != nil {
		respondErr(c, err)
		return
	}
	OK(c, gin.H{"items": items})
}

// IDEFiles 列举工作区文件树。
// 路由：GET /api/v1/ide/pods/:name/files?root=home|shared&path=
// 文件与终端读写的是同一份 PVC（挂载在 /home/workspace）。
func (h *Handler) IDEFiles(c *gin.Context) {
	name := c.Param("name")
	// 越权校验：只能访问自己创建的 Pod
	if err := h.Pods.EnsureOwner(c.Request.Context(), name, currentUserID(c)); err != nil {
		respondErr(c, err)
		return
	}

	items, err := h.Pods.ListFiles(c.Request.Context(), name, c.Query("root"), c.Query("path"))
	if err != nil {
		respondErr(c, err)
		return
	}
	OK(c, gin.H{"items": items})
}

// IDEFile 读取单个文件内容。
// 路由：GET /api/v1/ide/pods/:name/file?root=home|shared&path=main.go
func (h *Handler) IDEFile(c *gin.Context) {
	name := c.Param("name")
	if err := h.Pods.EnsureOwner(c.Request.Context(), name, currentUserID(c)); err != nil {
		respondErr(c, err)
		return
	}

	rel := c.Query("path")
	content, err := h.Pods.ReadFile(c.Request.Context(), name, c.Query("root"), rel)
	if err != nil {
		respondErr(c, err)
		return
	}
	OK(c, gin.H{"path": rel, "content": content})
}

// ideSaveRequest 是保存文件请求体。
type ideSaveRequest struct {
	Root    string `json:"root"`    // home / shared
	Path    string `json:"path"`    // 相对工作区的文件路径
	Content string `json:"content"` // 完整文件内容
}

// IDEFileSave 保存文件到工作区（终端立即可见）。
// 路由：PUT /api/v1/ide/pods/:name/file
func (h *Handler) IDEFileSave(c *gin.Context) {
	name := c.Param("name")
	if err := h.Pods.EnsureOwner(c.Request.Context(), name, currentUserID(c)); err != nil {
		respondErr(c, err)
		return
	}

	var req ideSaveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "请求参数不合法")
		return
	}
	if req.Path == "" {
		Fail(c, http.StatusBadRequest, "请指定文件路径")
		return
	}
	if err := h.Pods.WriteFile(c.Request.Context(), name, req.Root, req.Path, req.Content); err != nil {
		respondErr(c, err)
		return
	}
	OK(c, nil)
}

// ideCreateRequest 是新建文件/文件夹请求体。
type ideCreateRequest struct {
	Root string `json:"root"` // home / shared
	Path string `json:"path"` // 父目录（相对目录根，空表示根目录）
	Name string `json:"name"` // 新建的文件/目录名（单层名称）
	Type string `json:"type"` // file / dir
}

// IDECreateEntry 新建文件或文件夹。
// 路由：POST /api/v1/ide/pods/:name/files
func (h *Handler) IDECreateEntry(c *gin.Context) {
	name := c.Param("name")
	if err := h.Pods.EnsureOwner(c.Request.Context(), name, currentUserID(c)); err != nil {
		respondErr(c, err)
		return
	}

	var req ideCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "请求参数不合法")
		return
	}
	if req.Type != "file" && req.Type != "dir" {
		Fail(c, http.StatusBadRequest, "类型必须是 file 或 dir")
		return
	}
	if err := h.Pods.CreateEntry(c.Request.Context(), name, req.Root, req.Path, req.Name, req.Type == "dir"); err != nil {
		respondErr(c, err)
		return
	}
	OK(c, nil)
}

// ideRenameRequest 是重命名请求体。
type ideRenameRequest struct {
	Root string `json:"root"` // home / shared
	Path string `json:"path"` // 原路径（相对目录根）
	Name string `json:"name"` // 新名称（单层名称）
}

// IDERenameEntry 重命名文件或文件夹。
// 路由：PATCH /api/v1/ide/pods/:name/file
func (h *Handler) IDERenameEntry(c *gin.Context) {
	name := c.Param("name")
	if err := h.Pods.EnsureOwner(c.Request.Context(), name, currentUserID(c)); err != nil {
		respondErr(c, err)
		return
	}

	var req ideRenameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "请求参数不合法")
		return
	}
	if req.Path == "" {
		Fail(c, http.StatusBadRequest, "请指定要重命名的路径")
		return
	}
	if err := h.Pods.RenameEntry(c.Request.Context(), name, req.Root, req.Path, req.Name); err != nil {
		respondErr(c, err)
		return
	}
	OK(c, nil)
}

// IDEDeleteEntry 删除文件或文件夹（目录递归删除）。
// 路由：DELETE /api/v1/ide/pods/:name/file?root=home|shared&path=xxx
func (h *Handler) IDEDeleteEntry(c *gin.Context) {
	name := c.Param("name")
	if err := h.Pods.EnsureOwner(c.Request.Context(), name, currentUserID(c)); err != nil {
		respondErr(c, err)
		return
	}

	rel := c.Query("path")
	if rel == "" {
		Fail(c, http.StatusBadRequest, "请指定要删除的路径")
		return
	}
	if err := h.Pods.DeleteEntry(c.Request.Context(), name, c.Query("root"), rel); err != nil {
		respondErr(c, err)
		return
	}
	OK(c, nil)
}

// IDEDownload 下载工作区文件：后端返回文件流，前端触发浏览器下载。
// 路由：GET /api/v1/ide/pods/:name/download?root=home|shared&path=main.go
func (h *Handler) IDEDownload(c *gin.Context) {
	name := c.Param("name")
	if err := h.Pods.EnsureOwner(c.Request.Context(), name, currentUserID(c)); err != nil {
		respondErr(c, err)
		return
	}

	rel := c.Query("path")
	file, err := h.Pods.OpenFile(c.Request.Context(), name, c.Query("root"), rel)
	if err != nil {
		respondErr(c, err)
		return
	}
	defer file.Close()

	// 文件名只取最后一段并去掉特殊字符，避免响应头注入
	fileName := strings.NewReplacer(`"`, "", `\`, "", "\r", "", "\n", "").Replace(path.Base(rel))
	if fileName == "" || fileName == "." || fileName == "/" {
		fileName = "download"
	}
	c.Header("Content-Disposition", `attachment; filename="`+fileName+`"`)
	c.Header("Content-Type", "application/octet-stream")
	c.Header("X-Content-Type-Options", "nosniff")

	// 直接输出文件流；客户端主动断开时不视为服务端错误
	_, _ = io.Copy(c.Writer, file)
}
