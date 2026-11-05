package adminui

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/internal/text"
	"go_wp/pkg/response"
)

const MsgInternalError = text.InternalKey

func PageErrorBadRequest(c *gin.Context, scene string, err error) {
	if err != nil {
		response.LogInternalErrorf(c, scene, "后台页面参数校验失败", err)
	}
	response.ErrorWithMessage(c, http.StatusBadRequest, MsgInternalError)
}

func PageErrorInternal(c *gin.Context, scene string, err error) {
	if err != nil {

		response.LogInternalErrorf(c, scene, "后台页面处理失败", err)
	}
	response.ErrorWithMessage(c, http.StatusInternalServerError, MsgInternalError)
}

func PageInternalText(c *gin.Context) string {
	return response.InternalText(c)
}

const internalErrorFallback = response.InternalFallback
