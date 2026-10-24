// Package buildenums 统一管理 build 模块业务消息。
package buildenums

import "go_wp/pkg/facing"

const (
	ErrInvalidParam    = "ErrInvalidParam"    // 请求参数无效
	ErrJobNotFound     = "ErrJobNotFound"     // 构建任务不存在
	ErrExecutorMissing = "ErrExecutorMissing" // 该来源类型没有注册执行器
	ErrInvalidSource   = "ErrInvalidSource"   // 来源类型不在白名单内
)

const (
	MsgQueueFound = "MsgQueueFound" // 队列状态查询成功
	MsgJobRetried = "MsgJobRetried" // 失败任务已重新入队
)

// errFallbacks 错误 key → 中文兜底（**与 sys_i18n 的 zh-CN 词条逐字一致**）。
//
// 兜底跟**声明处**走（pkg/facing.New），出口只调一次 facing.FactOf。词条库读不到时
// businessText 只能退回声明处给的那句话 —— 没有它，客户端拿到的是 `ErrInvalidParam` 这样的裸 key
// （这批常量的值就是常量名本身，旧式形态）。
//
// 逐字一致的理由：兜底只在词条缺席时可见，与词条不一致会让人以为「词条写坏了」。
var errFallbacks = map[string]string{
	ErrInvalidParam:    "请求参数无效",
	ErrJobNotFound:     "构建任务不存在",
	ErrExecutorMissing: "该来源类型没有注册构建执行器，任务无法执行",
	ErrInvalidSource:   "非法的菜单项来源，仅支持 custom/page/article/product/category/block，且非 custom 必须指定来源实体",
}

// Err 声明一条自带对外文案的业务错误（兜底与本表同源）。
func Err(key string) error { return facing.New(key, errFallbacks[key]) }

// FallbackFor 兜底表取值（没有 error 对象、只有 key 的场景用：逐条原因、页面事实）。
// 未登记返回空串 —— 调用方据此回落自己的最后一句，不要在这里编一句通用文案。
func FallbackFor(key string) string { return errFallbacks[key] }
