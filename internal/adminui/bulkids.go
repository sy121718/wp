package adminui

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	bulkids "go_wp/pkg/bulkids"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

const bulkIDsTooManyTemplate = bulkids.TooManyTemplate

func BulkIDsFacingText(c *gin.Context, err error) string {
	var tooMany *bulkids.IDsError
	if errors.As(err, &tooMany) {
		logger.Scene("adminui").
			With("path", requestURIOf(c)).
			With("user_id", builtin.CurrentUserID(c)).
			With("count", tooMany.Count).
			With("max", tooMany.Max).
			Warn("批量操作被拒：提交的 id 数量超过上限")
		tpl := translateFor(c)(bulkids.MsgTooMany, bulkIDsTooManyTemplate)

		if !i18n.HasStringPlaceholdersOnly(tpl) {
			tpl = bulkIDsTooManyTemplate
		}
		return fmt.Sprintf(tpl, strconv.Itoa(tooMany.Max), strconv.Itoa(tooMany.Count))
	}
	if err != nil {
		logger.Scene("adminui").
			With("path", requestURIOf(c)).
			Error(err, "批量 id 读取失败：未命中受控错误，只对外给归口文案")
	}
	return PageInternalText(c)
}
