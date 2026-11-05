package mailhttp

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	mailenums "go_wp/internal/module/mail/enums"
	"go_wp/pkg/facing"
	pageview "go_wp/pkg/view"
)

// mailIDOfText 把表单里的数字串转成无符号整数；空串或非数字返回 0。
func mailIDOfText(text string) uint64 {
	value, err := strconv.ParseUint(strings.TrimSpace(text), 10, 64)
	if err != nil {
		return 0
	}
	return value
}

// mailWriteOK 是写操作的统一成功片段：结果事实由 Jet 取词。
func mailWriteOK(c *gin.Context) {
	pageview.Success(c)
}

// mailWriteResult 渲染带业务事实的结果片段。
func mailWriteResult(c *gin.Context, template string, data any) {
	c.HTML(http.StatusOK, template, data)
}

type mailContactBulkResult struct {
	Deleted int
	Skipped int
	Items   []mailContactBulkItem
}

type mailContactTagResult struct {
	Updated int
	Skipped int
	Items   []mailContactTagItem
}

// mailWriteFacts 把业务消息拆成「词条 + 参数」，交给失败片段取词。
func mailWriteFacts(msg mailMsg) (string, bool) {
	if msg.Key == "" {
		return "", false
	}
	key := msg.Key
	if len(msg.Args) > 0 {
		key += "|" + strings.Join(msg.Args, "|")
	}
	return key, true
}

// mailWriteFail 按业务消息渲染失败区；无业务事实时退归口文案。
func mailWriteFail(c *gin.Context, status int, msg mailMsg) {
	if key, ok := mailWriteFacts(msg); ok {
		pageview.FailStatus(c, status, mailErrScene, facing.WithStatus(facing.New(key), status))
		return
	}
	pageview.FailStatus(c, status, mailErrScene, facing.WithStatus(mailenums.Err(mailenums.ErrInternal), status))
}

// mailContactIDs 去掉空白与重复项，保持首次出现顺序。
func mailContactIDs(raw []string) []string {
	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

// mailContactUintIDs 把表单里的 id 串批量转成整数；无效项单独计数。
func mailContactUintIDs(raw []string) (ids []uint64, invalid int) {
	ids = make([]uint64, 0, len(raw))
	for _, item := range raw {
		id := mailIDOfText(item)
		if id == 0 {
			invalid++
			continue
		}
		ids = append(ids, id)
	}
	return ids, invalid
}

// mailContactRowsOf 把逐条删除结果映射成片段行事实。
func mailContactRowsOf(items []mailContactBulkItem) []map[string]any {
	rows := make([]map[string]any, 0, len(items))
	for _, item := range items {
		rows = append(rows, map[string]any{
			"ID":      item.ID,
			"OK":      item.Deleted,
			"Message": item.Message,
		})
	}
	return rows
}

// mailTagRowsOf 把逐条打标签结果映射成片段行事实。
func mailTagRowsOf(items []mailContactTagItem) []map[string]any {
	rows := make([]map[string]any, 0, len(items))
	for _, item := range items {
		rows = append(rows, map[string]any{
			"ID":      item.ID,
			"OK":      item.Updated,
			"Message": item.Message,
		})
	}
	return rows
}
