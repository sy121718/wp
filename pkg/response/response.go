package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func Success(c *gin.Context, data ...interface{}) {
	var responseData interface{}
	if len(data) > 0 {
		responseData = data[0]
	}

	c.JSON(http.StatusOK, Response{
		Code:    http.StatusOK,
		Message: "操作成功",
		Data:    responseData,
	})
}

func SuccessWithMessage(c *gin.Context, message string, data ...interface{}) {
	var responseData interface{}
	if len(data) > 0 {
		responseData = data[0]
	}

	c.JSON(http.StatusOK, Response{
		Code:    http.StatusOK,
		Message: message,
		Data:    responseData,
	})
}

func ErrorWithMessage(c *gin.Context, code int, message string) {
	c.JSON(code, Response{
		Code:    code,
		Message: message,
	})
}

func ParamError(c *gin.Context, msg ...string) {
	message := "请求参数错误"
	if len(msg) > 0 {
		message = msg[0]
	}

	ErrorWithMessage(c, http.StatusBadRequest, message)
}

func NotFound(c *gin.Context, msg ...string) {
	message := "请求的资源不存在"
	if len(msg) > 0 {
		message = msg[0]
	}

	ErrorWithMessage(c, http.StatusNotFound, message)
}

func SuccessWithData(c *gin.Context, data interface{}) {
	Success(c, data)
}
