package shell

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/pkg/reqparams"
	"go_wp/pkg/safepath"
)

type Jump struct {
	OK bool

	MsgKey string

	Msg string

	MsgArgs []string

	Back string

	BackTextKey string

	BackText string

	Seconds int
}

func RenderJump(c *gin.Context, j Jump) {
	if c == nil {
		return
	}

	back := strings.TrimSpace(j.Back)
	if !safepath.IsSameOriginPath(back) {
		back = adminHomePath
	}

	if reqparams.IsHXRequest(c) {
		c.Header("HX-Redirect", back)
		c.Status(http.StatusOK)
		return
	}

	adminui.RenderJumpPage(c, http.StatusOK, adminui.JumpView{
		OK:          j.OK,
		MsgKey:      strings.TrimSpace(j.MsgKey),
		Msg:         strings.TrimSpace(j.Msg),
		MsgArgs:     j.MsgArgs,
		Back:        back,
		BackTextKey: strings.TrimSpace(j.BackTextKey),
		BackText:    strings.TrimSpace(j.BackText),
		Seconds:     j.Seconds,
	})
}
