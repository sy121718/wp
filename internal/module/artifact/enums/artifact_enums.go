// Package artifactenums 统一管理 artifact 模块业务消息。
package artifactenums

import "go_wp/pkg/facing"

const (
	ErrArtifactNotFound = "ErrArtifactNotFound" // 构建产物不存在
	ErrArtifactMismatch = "ErrArtifactMismatch" // 产物内容与数据库记录不一致
	ErrInvalidArtifact  = "ErrInvalidArtifact"  // 构建产物不完整
	// ErrInvalidParam 请求本身不合法（nil 请求、缺少必要字段等），与产物存在性无关。
	ErrInvalidParam = "ErrInvalidParam" // 请求参数无效
	// ErrContentRefQueryFailed 内容对象的外部引用来源查询失败。
	// 属于「问不到就不能删」的失败：引用关系不明时宁可少回收一轮，也不能删共享对象。
	ErrContentRefQueryFailed = "ErrContentRefQueryFailed" // 内容对象引用关系查询失败
	// ErrContentObjectDeleteNotApplied 内容对象删除未生效：DELETE 没报错、行也没消失，
	// 删后复查确认它仍然存在且仍然无引用。与「被并发归档重新引用」是两回事 ——
	// 后者是正常赛跑，前者说明删除语句没能落地（约束/触发器/权限），必须可见。
	ErrContentObjectDeleteNotApplied = "ErrContentObjectDeleteNotApplied" // 内容对象删除未生效
)

const (
	MsgArtifactSaved = "MsgArtifactSaved" // 构建产物已归档
	MsgArtifactFound = "MsgArtifactFound" // 构建产物查询成功
)

// errFallbacks 错误 key → 中文兜底（**与 sys_i18n 的 zh-CN 词条逐字一致**）。
//
// 兜底跟**声明处**走（pkg/facing.New），出口只调一次 facing.FactOf。词条库读不到时
// businessText 只能退回声明处给的那句话 —— 没有它，客户端拿到的是 `ErrArtifactNotFound` 这样的裸 key
// （这批常量的值就是常量名本身，旧式形态）。
//
// 逐字一致的理由：兜底只在词条缺席时可见，与词条不一致会让人以为「词条写坏了」。
var errFallbacks = map[string]string{
	ErrArtifactNotFound:              "构建产物不存在",
	ErrArtifactMismatch:              "产物内容与数据库记录不一致",
	ErrInvalidArtifact:               "构建产物不完整",
	ErrInvalidParam:                  "请求参数无效",
	ErrContentRefQueryFailed:         "内容对象引用关系查询失败，为避免误删共享对象已中止本次回收",
	ErrContentObjectDeleteNotApplied: "内容对象删除未生效（删除后复查仍存在），请稍后重试或联系管理员",
}

// Err 声明一条自带对外文案的业务错误（兜底与本表同源）。
func Err(key string) error { return facing.New(key, errFallbacks[key]) }

// FallbackFor 兜底表取值（没有 error 对象、只有 key 的场景用：逐条原因、页面事实）。
// 未登记返回空串 —— 调用方据此回落自己的最后一句，不要在这里编一句通用文案。
func FallbackFor(key string) string { return errFallbacks[key] }
