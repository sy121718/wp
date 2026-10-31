package blueprintenums

import "go_wp/pkg/facing"

const (
	MsgCreateSuccess  = "MsgCreateSuccess"
	MsgUpdateSuccess  = "MsgUpdateSuccess"
	MsgPublishSuccess = "MsgPublishSuccess"
	MsgListSuccess    = "MsgListSuccess"
	MsgDetailSuccess  = "MsgDetailSuccess"
	MsgDeleteSuccess  = "MsgDeleteSuccess"

	ErrInvalidParam = "ErrInvalidParam"
	ErrNotFound     = "ErrNotFound"
	ErrInvalidKind  = "ErrInvalidKind"
	ErrDataInvalid  = "ErrDataInvalid"
)

var errFallbacks = map[string]string{
	ErrInvalidParam: "请求参数无效",
	ErrNotFound:     "请求资源不存在",
	ErrInvalidKind:  "不支持的页面类型",
	ErrDataInvalid:  "Blueprint 文档格式非法",
}

func Err(key string) error { return facing.New(key, errFallbacks[key]) }

func FallbackFor(key string) string { return errFallbacks[key] }
