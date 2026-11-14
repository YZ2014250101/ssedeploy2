package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"ssedeploy/internal/model"
)

// ListDomains 返回某个 Pod 的全部子域名映射，并附带主域名供前端预览。
func (h *Handler) ListDomains(c *gin.Context) {
	podName := c.Param("name")
	if err := h.ensurePodAccess(c, podName); err != nil {
		respondErr(c, err)
		return
	}
	items, err := h.Domains.List(c.Request.Context(), podName)
	if err != nil {
		respondErr(c, err)
		return
	}
	OK(c, gin.H{"items": items, "baseDomain": h.Domains.BaseDomain()})
}

// CreateDomain 创建 / 更新子域名映射：自动生成 Ingress，host 为 <prefix>.<主域名>。
func (h *Handler) CreateDomain(c *gin.Context) {
	podName := c.Param("name")
	if err := h.ensurePodAccess(c, podName); err != nil {
		respondErr(c, err)
		return
	}

	var req model.CreateDomainRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "参数格式不正确")
		return
	}

	mapping, err := h.Domains.Create(c.Request.Context(), podName, currentUserID(c), req)
	if err != nil {
		respondErr(c, err)
		return
	}
	OK(c, mapping)
}

// DeleteDomain 删除子域名映射，同时回收对应的 Ingress。
func (h *Handler) DeleteDomain(c *gin.Context) {
	podName := c.Param("name")
	if err := h.ensurePodAccess(c, podName); err != nil {
		respondErr(c, err)
		return
	}
	if err := h.Domains.Delete(c.Request.Context(), podName, c.Param("prefix")); err != nil {
		respondErr(c, err)
		return
	}
	OK(c, nil)
}
