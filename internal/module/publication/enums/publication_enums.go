// Package pubenums 统一管理 publication 模块业务消息。
package pubenums

import "go_wp/pkg/facing"

const (
	ErrRouteOccupied  = "ErrRouteOccupied"  // 目标路径已被其他页面占用
	ErrRouteNotFound  = "ErrRouteNotFound"  // 路径占用不存在
	ErrReceiptPending = "ErrReceiptPending" // 存在未完成的发布回执，请先恢复或标记失败
	// ErrInvalidParam 请求本身不合法（nil 请求、缺少必要字段等），与路径/占用无关。
	ErrInvalidParam = "ErrInvalidParam" // 请求参数无效
	// ErrRouteActiveRename 对已激活（线上）路径执行草稿改名：非法状态转移，
	// 应走页面改 URL（UpdateURL）流程而非直接改 active 行。
	ErrRouteActiveRename = "ErrRouteActiveRename" // 已激活的线上路径不能直接改名，请通过页面改 URL 流程操作
	// ErrAuditFailed SEO 体检本身失败（读不到产物目录、解析异常等）。
	//
	// 存在的理由同 order / user 模块的 ErrInternal：RunSEOAudit 返回的可能是基础设施
	// 错误的原文（带路径甚至 SQL 片段），直接铺给后台页面违反 CQ-009；这里给一条
	// 可翻译的归口文案，原文进日志。
	ErrAuditFailed = "ErrAuditFailed" // SEO 体检失败，请查看服务端日志
	// ErrInternal 未归类的系统错误对外统一文案（AGENTS.md「错误文案三件套」的②）。
	//
	// 与 adminenums.ErrInternal **同 key**（值都是 "ErrInternal"）：这是同一句通用归口文案
	// ——「操作失败，请稍后重试」对哪个模块都一样，共用词条比每个模块各造一条好。
	// 它的用途只有一个：把基础设施错误（SQL 原文 / 约束名 / 路径）挡在响应之外，
	// 原文只进结构化日志（形态参照 admin_err.go 与 order 模块的 orderFacingText）。
	ErrInternal = "ErrInternal" // 操作失败，请稍后重试

)

const (
	MsgPublished   = "MsgPublished"   // 发布成功
	MsgRolledBack  = "MsgRolledBack"  // 回滚成功
	MsgDeactivated = "MsgDeactivated" // 取消激活成功
)

// errFallbacks 错误 key → 中文兜底（**与 sys_i18n 的 zh-CN 词条逐字一致**）。
//
// 兜底跟**声明处**走（pkg/facing.New），出口只调一次 facing.FactOf。词条库读不到时
// businessText 只能退回声明处给的那句话 —— 没有它，客户端拿到的是 `ErrRouteOccupied` 这样的裸 key
// （这批常量的值就是常量名本身，旧式形态）。
//
// 逐字一致的理由：兜底只在词条缺席时可见，与词条不一致会让人以为「词条写坏了」。
var errFallbacks = map[string]string{
	ErrRouteOccupied:     "目标路径已被其他页面占用",
	ErrRouteNotFound:     "路径占用不存在",
	ErrReceiptPending:    "存在未完成的发布回执，请先恢复或标记失败",
	ErrInvalidParam:      "请求参数无效",
	ErrRouteActiveRename: "已激活的线上路径不能直接改名，请通过页面改 URL 流程操作",
	ErrAuditFailed:       "SEO 体检失败，请查看服务端日志",
	ErrInternal:          "操作失败，请稍后重试",
}

// Err 声明一条自带对外文案的业务错误（兜底与本表同源）。
func Err(key string) error { return facing.New(key, errFallbacks[key]) }

// FallbackFor 兜底表取值（没有 error 对象、只有 key 的场景用：逐条原因、页面事实）。
// 未登记返回空串 —— 调用方据此回落自己的最后一句，不要在这里编一句通用文案。
func FallbackFor(key string) string { return errFallbacks[key] }
