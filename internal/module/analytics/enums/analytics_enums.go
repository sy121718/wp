package analyticsenums

import (
	"fmt"
	"strings"

	"go_wp/pkg/facing"
)

const (
	ErrInvalidParam = "ErrInvalidParam"

	ErrInvalidRange = "ErrInvalidRange"

	ErrAnalyticsInternal = "ErrAnalyticsInternal"
)

var errFallbacks = map[string]string{
	ErrInvalidParam:      "请求参数无效",
	ErrInvalidRange:      "时间范围不合法：起始日期不能晚于结束日期，且跨度不超过 366 天",
	ErrAnalyticsInternal: "统计服务内部错误",
}

func Err(key string) error { return facing.New(key, errFallbacks[key]) }

func ErrDetail(key, detail string) error {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return Err(key)
	}
	return fmt.Errorf("%w: %s", Err(key), detail)
}

func FallbackFor(key string) string { return errFallbacks[key] }

const (
	MsgCollectAccepted = "MsgCollectAccepted"
)
