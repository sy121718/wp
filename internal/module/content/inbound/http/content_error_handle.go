package contenthttp

import (
	view "go_wp/pkg/view"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/backpath"
)

func articleJumpListBack(c *gin.Context) string {
	return backpath.FromRequest(c.Request.URL.Query(), articleListPath, "/admin", "content", "keyword", "limit")
}

func articleJumpEditBack(c *gin.Context, id string) string {
	return backpath.WithParams(articleEditPagePath, map[string]string{"id": strings.TrimSpace(id)})
}

var (
	articleJumpListMsg = articleMsg{Key: articlePageTitle, Fallback: "文章"}
	articleJumpEditMsg = articleMsg{Key: articleEditTitle, Fallback: "编辑文章"}
)

func articlePageJump(c *gin.Context, ok bool, msg articleMsg, _ string, _ articleMsg) {
	if ok {
		view.SuccessWith(c, view.SuccessFact{Key: msg.Key, Fallback: msg.Fallback, Args: msg.Args})
		return
	}
	view.FailMessage(c, articleErrScene, msg.Key, msg.Fallback, msg.Args...)
}

func articleListJump(c *gin.Context, ok bool, msg articleMsg) {
	articlePageJump(c, ok, msg, articleJumpListBack(c), articleJumpListMsg)
}

func articleEditJump(c *gin.Context, ok bool, id string, msg articleMsg) {
	articlePageJump(c, ok, msg, articleJumpEditBack(c, id), articleJumpEditMsg)
}
