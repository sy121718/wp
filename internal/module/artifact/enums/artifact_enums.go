// Package artifactenums 统一管理 artifact 模块业务消息。
package artifactenums

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
