package workbenchhttp

import (
	"github.com/gin-gonic/gin"
	workbenchdto "go_wp/internal/module/workbench/dto"
	"go_wp/pkg/binding"
	"go_wp/pkg/builder/source"
	"go_wp/pkg/response"
)

func previewContext(c *gin.Context) {
	req, err := binding.ParseQuery[workbenchdto.PreviewStateReq](c)
	if err != nil {
		response.ParamError(c)
		c.Abort()
		return
	}
	c.Request = c.Request.WithContext(source.WithPreviewState(c.Request.Context(), req.PreviewState))
	c.Next()
}
