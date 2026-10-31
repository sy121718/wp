package buildenums

import "go_wp/pkg/facing"

const (
	ErrInvalidParam    = "ErrInvalidParam"
	ErrJobNotFound     = "ErrJobNotFound"
	ErrExecutorMissing = "ErrExecutorMissing"
	ErrInvalidSource   = "ErrInvalidSource"
	ErrInvalidMenuKind = "ErrInvalidMenuKind"
)

const (
	MsgQueueFound = "MsgQueueFound"
	MsgJobRetried = "MsgJobRetried"
)

var errFallbacks = map[string]string{
	ErrInvalidParam:    "请求参数无效",
	ErrJobNotFound:     "构建任务不存在",
	ErrExecutorMissing: "该来源类型没有注册构建执行器，任务无法执行",
	ErrInvalidSource:   "非法的菜单项来源，仅支持 custom/page/article/product/category/block，且非 custom 必须指定来源实体",
	ErrInvalidMenuKind: "菜单位置无效，仅支持 header/header_mobile/footer/footer_mobile",
}

func Err(key string) error { return facing.New(key, errFallbacks[key]) }

func FallbackFor(key string) string { return errFallbacks[key] }
