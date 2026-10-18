// Package analyticsenums 统一管理 analytics 模块响应消息。
package analyticsenums

import (
	"fmt"
	"strings"

	"go_wp/pkg/facing"
)

const (
	// ErrInvalidParam 查询参数无效（工程未指定 / 时间范围不合法）。
	ErrInvalidParam = "ErrInvalidParam" // 请求参数无效
	// ErrInvalidRange 时间范围不合法（起止颠倒或超过上限天数）。
	ErrInvalidRange = "ErrInvalidRange" // 时间范围不合法：起始日期不能晚于结束日期，且跨度不超过 366 天
	// ErrAnalyticsInternal 统计服务内部错误（基础设施故障兜底，不向客户端泄漏内部细节）。
	ErrAnalyticsInternal = "ErrAnalyticsInternal" // 统计服务内部错误
)

// errFallbacks 错误兜底（对外文案的中文那一半）：与常量同处声明，出口只调一次 pkg/facing.FactOf。
//
// 为什么不再有白名单（`var analyticsFacingMessages []string` + 逐条相等比较）：
// 那是第二份真相 —— service 换措辞、或某条错误改由别的模块提供时白名单静默失配，
// 症状是把业务文案吞成归口、或把内部错误当文案透出，两边都不报错。
var errFallbacks = map[string]string{
	ErrInvalidParam:      "请求参数无效",
	ErrInvalidRange:      "时间范围不合法：起始日期不能晚于结束日期，且跨度不超过 366 天",
	ErrAnalyticsInternal: "统计服务内部错误",
}

// Err 声明一条自带对外文案的统计业务错误（兜底与本表同源）。
func Err(key string) error { return facing.New(key, errFallbacks[key]) }

// ErrDetail 把业务错误与定位数据拼成一条（key + 明细，分隔符必须 ASCII「: 」）。
func ErrDetail(key, detail string) error {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return Err(key)
	}
	return fmt.Errorf("%w: %s", Err(key), detail)
}

// FallbackFor 兜底表取值（没有 error 对象、只有 key 的场景用）。
func FallbackFor(key string) string { return errFallbacks[key] }

const (
	// MsgCollectAccepted 打点已受理（公开端点实际返回 204 空响应，此文案供日志与测试引用）。
	MsgCollectAccepted = "MsgCollectAccepted" // 打点已受理
)
