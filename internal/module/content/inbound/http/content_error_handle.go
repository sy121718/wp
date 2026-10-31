package contenthttp

import (
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/shell"
	"go_wp/pkg/backpath"
)

func articleJumpListBack(c *gin.Context) string {
	return shell.BackPath(c, articleListPath, "keyword", "limit")
}

func articleJumpEditBack(c *gin.Context, id string) string {
	return backpath.WithParams(articleEditPagePath, map[string]string{"id": strings.TrimSpace(id)})
}

var (
	articleJumpListMsg = articleMsg{Key: articlePageTitle, Fallback: "文章"}
	articleJumpEditMsg = articleMsg{Key: articleEditTitle, Fallback: "编辑文章"}
)

func articlePageJump(c *gin.Context, ok bool, msg articleMsg, back string, backText articleMsg) {
	seconds := 0
	if ok {
		seconds = 1
	}
	shell.RenderJump(c, shell.Jump{
		OK:          ok,
		MsgKey:      msg.Key,
		Msg:         msg.Fallback,
		MsgArgs:     msg.Args,
		Back:        back,
		BackTextKey: backText.Key,
		BackText:    backText.Fallback,
		Seconds:     seconds,
	})
}

func articleListJump(c *gin.Context, ok bool, msg articleMsg) {
	articlePageJump(c, ok, msg, articleJumpListBack(c), articleJumpListMsg)
}

func articleEditJump(c *gin.Context, ok bool, id string, msg articleMsg) {
	articlePageJump(c, ok, msg, articleJumpEditBack(c, id), articleJumpEditMsg)
}
