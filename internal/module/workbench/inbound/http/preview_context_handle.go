package workbenchhttp

import (
	"github.com/gin-gonic/gin"
	workbenchdto "go_wp/internal/module/workbench/dto"
	"go_wp/pkg/binding"
	"go_wp/pkg/builder/source"
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
