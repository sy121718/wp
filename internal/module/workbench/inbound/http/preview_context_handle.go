package workbenchhttp

import (
	"github.com/gin-gonic/gin"
	"go_wp/internal/builder/source"
	workbenchdto "go_wp/internal/module/workbench/dto"
	"go_wp/pkg/binding"
)

// previewContext 只转交已绑定的临时展示事实，不持久化或授权私人数据访问。
func previewContext(c *gin.Context) {
	req, ok := binding.Query[workbenchdto.PreviewStateReq](c)
	if !ok {
		c.Abort()
		return
	}
	c.Request = c.Request.WithContext(source.WithPreviewState(c.Request.Context(), req.PreviewState))
	c.Next()
}
