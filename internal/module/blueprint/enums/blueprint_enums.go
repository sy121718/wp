// Package blueprintenums blueprint 模块响应消息。
package blueprintenums

import "go_wp/pkg/facing"

// 响应消息（未接 i18n 前中文常量）。
const (
	MsgCreateSuccess  = "MsgCreateSuccess"  // Blueprint 创建成功
	MsgUpdateSuccess  = "MsgUpdateSuccess"  // Blueprint 更新成功
	MsgPublishSuccess = "MsgPublishSuccess" // Blueprint 发布成功
	MsgListSuccess    = "MsgListSuccess"    // Blueprint 列表获取成功
	MsgDetailSuccess  = "MsgDetailSuccess"  // Blueprint 详情获取成功
	MsgDeleteSuccess  = "MsgDeleteSuccess"  // Blueprint 已删除

	ErrInvalidParam = "ErrInvalidParam" // 参数错误
	ErrNotFound     = "ErrNotFound"     // Blueprint 不存在
	ErrInvalidKind  = "ErrInvalidKind"  // 不支持的页面类型
	ErrDataInvalid  = "ErrDataInvalid"  // Blueprint 文档格式非法
)

// errFallbacks 错误 key → 中文兜底（**与 sys_i18n 的 zh-CN 词条逐字一致**）。
//
// 兜底跟**声明处**走（pkg/facing.New），出口只调一次 facing.FactOf。词条库读不到时
// businessText 只能退回声明处给的那句话 —— 没有它，客户端拿到的是 `ErrInvalidParam` 这样的裸 key
// （这批常量的值就是常量名本身，旧式形态）。
//
// 逐字一致的理由：兜底只在词条缺席时可见，与词条不一致会让人以为「词条写坏了」。
var errFallbacks = map[string]string{
	ErrInvalidParam: "请求参数无效",
	ErrNotFound:     "请求资源不存在",
	ErrInvalidKind:  "不支持的页面类型",
	ErrDataInvalid:  "Blueprint 文档格式非法",
}

// Err 声明一条自带对外文案的业务错误（兜底与本表同源）。
func Err(key string) error { return facing.New(key, errFallbacks[key]) }

// FallbackFor 兜底表取值（没有 error 对象、只有 key 的场景用：逐条原因、页面事实）。
// 未登记返回空串 —— 调用方据此回落自己的最后一句，不要在这里编一句通用文案。
func FallbackFor(key string) string { return errFallbacks[key] }
