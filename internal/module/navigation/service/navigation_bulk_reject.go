package navigationservice

import (
	"net/http"

	navigationenums "go_wp/internal/module/navigation/enums"
	"go_wp/pkg/facing"
)

// BulkRejectedError 是一次导航批量写被整体拒绝时的事实：
// 多项请求给公共整批主消息，单条请求给该条的具体业务原因；
// 逐条 id / 名称 / 原因始终由共享失败出口放进 data.items。
type BulkRejectedError struct {
	key   string
	items []facing.BlockedItem
}

// RejectBatch 构造整批拒绝事实。requested 必须是「按 uuid 身份去重后的请求项数」，
// 因此合法重复不会把单条请求误判成多项。
func RejectBatch(requested int, items []facing.BlockedItem) error {
	if len(items) == 0 {
		return nil
	}
	key := navigationenums.ErrBatchReject
	if requested == 1 && len(items) == 1 {
		key = items[0].ReasonKey
	}
	return &BulkRejectedError{key: key, items: items}
}

func (e *BulkRejectedError) Error() string { return e.key }

func (e *BulkRejectedError) Facing() (string, string) { return e.key, "" }

// HTTPStatus 从拒绝原因推导：冲突优先于缺失，其余按参数错误。
func (e *BulkRejectedError) HTTPStatus() int {
	if e.key != navigationenums.ErrBatchReject {
		return navigationenums.StatusFor(e.key)
	}
	status := http.StatusBadRequest
	for _, item := range e.items {
		switch navigationenums.StatusFor(item.ReasonKey) {
		case http.StatusConflict:
			return http.StatusConflict
		case http.StatusNotFound:
			status = http.StatusNotFound
		}
	}
	return status
}

func (e *BulkRejectedError) BlockedItems() []facing.BlockedItem { return e.items }

var _ facing.BlockedError = (*BulkRejectedError)(nil)
