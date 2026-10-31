package adminui

import (
	"github.com/gin-gonic/gin"

	"go_wp/pkg/facing"
	"go_wp/pkg/response"
)

type Msg = facing.Msg

func MsgText(c *gin.Context, m Msg) string {
	return response.MsgText(c, m)
}

func InternalMsg() Msg {
	f := InternalErrorFact()
	return Msg{Key: f.Key, Fallback: f.Fallback}
}
