package mailhttp

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	mailenums "go_wp/internal/module/mail/enums"
	mailservice "go_wp/internal/module/mail/service"
	"go_wp/pkg/facing"
	pageview "go_wp/pkg/view"
)

// mailInvalidBatchError 是表单里含非法 id 时的整批拒绝事实：
// 主消息只取一条公共文案，逐条非法 id 交给共享出口放进 data.items，服务层不会被调用。
type mailInvalidBatchError struct {
	ids []string
}

func (e *mailInvalidBatchError) Error() string { return mailenums.ErrBatchReject }

func (e *mailInvalidBatchError) Facing() (string, string) { return mailenums.ErrBatchReject, "" }

func (e *mailInvalidBatchError) HTTPStatus() int { return http.StatusBadRequest }

func (e *mailInvalidBatchError) BlockedItems() []facing.BlockedItem {
	items := make([]facing.BlockedItem, 0, len(e.ids))
	for _, id := range e.ids {
		items = append(items, facing.BlockedItem{ID: id, ReasonKey: mailenums.ErrInvalidParam})
	}
	return items
}

// mailBatchIDs 把已去重的 id 串转成整数，并返回非法项；非法项存在时整批拒绝。
func mailBatchIDs(raw []string) (ids []uint64, invalid []string) {
	ids = make([]uint64, 0, len(raw))
	for _, item := range raw {
		id := mailIDOfText(item)
		if id == 0 {
			invalid = append(invalid, strings.TrimSpace(item))
			continue
		}
		ids = append(ids, id)
	}
	return ids, invalid
}

// mailBatchIDsOrFail 把归一交给 service（去重、上限与解析都是业务规则），
// 这里只负责把失败事实交给共享出口。
func mailBatchIDsOrFail(c *gin.Context, raw []string) ([]uint64, bool) {
	ids, err := mailservice.NormalizeBatchIDs(raw)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, mailErrScene, err)
		return nil, false
	}
	return ids, true
}
