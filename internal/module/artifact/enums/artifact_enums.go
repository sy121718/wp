// Package artifactenums 统一管理 artifact 模块业务消息。
package artifactenums

const (
	ErrArtifactNotFound = "构建产物不存在"
	ErrArtifactMismatch = "产物内容与数据库记录不一致"
	ErrInvalidArtifact  = "构建产物不完整"
)

const (
	MsgArtifactSaved = "构建产物已归档"
	MsgArtifactFound = "构建产物查询成功"
)
