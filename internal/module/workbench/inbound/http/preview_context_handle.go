package workbenchhttp

import (
	"github.com/gin-gonic/gin"
	"go_wp/pkg/builder/source"
	workbenchdto "go_wp/internal/module/workbench/dto"
	"go_wp/pkg/binding"
)

func previewContext(c *gin.Context) {
	req, ok := binding.Query[workbenchdto.PreviewStateReq](c)
	if !ok {
		c.Abort()
		return
	}
	c.Request = c.Request.WithContext(source.WithPreviewState(c.Request.Context(), req.PreviewState))
	c.Next()
}
