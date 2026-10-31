package pubenums

import (
	"net/http"

	"go_wp/pkg/facing"
)

const (
	ErrRouteOccupied  = "ErrRouteOccupied"
	ErrRouteNotFound  = "ErrRouteNotFound"
	ErrReceiptPending = "ErrReceiptPending"

	ErrInvalidParam = "ErrInvalidParam"

	ErrProjectRequired = "ErrProjectRequired"

	ErrRouteActiveRename = "ErrRouteActiveRename"

	ErrInternal = "ErrInternal"
)

const (
	MsgPublished   = "MsgPublished"
	MsgRolledBack  = "MsgRolledBack"
	MsgDeactivated = "MsgDeactivated"
)

var errFallbacks = map[string]string{
	ErrRouteOccupied:     "目标路径已被其他页面占用",
	ErrRouteNotFound:     "路径占用不存在",
	ErrReceiptPending:    "存在未完成的发布回执，请先恢复或标记失败",
	ErrInvalidParam:      "请求参数无效",
	ErrProjectRequired:   "请先选择要操作的站点工程",
	ErrRouteActiveRename: "已激活的线上路径不能直接改名，请通过页面改 URL 流程操作",
	ErrInternal:          "操作失败，请稍后重试",
}

func Err(key string) error { return facing.New(key, errFallbacks[key]) }

func ErrProjectMissing() error {
	return facing.WithStatus(Err(ErrProjectRequired), http.StatusBadRequest)
}

func FallbackFor(key string) string { return errFallbacks[key] }
