package workbenchenums

import (
	"net/http"

	"go_wp/pkg/facing"
)

// PreviewError 声明草稿预览失败的状态，字节流出口与 JSON 出口共用错误事实。
func PreviewError(key string) error {
	switch key {
	case ErrBlockNotFound:
		return facing.WithStatus(Err(key), http.StatusNotFound)
	case ErrDraftVersionStale:
		return facing.WithStatus(Err(key), http.StatusConflict)
	default:
		return facing.WithStatus(Err(key), http.StatusBadRequest)
	}
}
