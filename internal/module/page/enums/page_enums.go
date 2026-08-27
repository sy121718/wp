// Package pageenums 管理 page 模块业务消息。
package pageenums

const (
	ErrPageNotFound         = "页面不存在"
	ErrProjectNotFound      = "站点工程不存在"
	ErrInvalidKind          = "页面类型与内容目标不匹配"
	ErrInvalidDocument      = "页面草稿文档不合法"
	ErrInvalidPath          = "页面访问路径不合法"
	ErrDraftVersionConflict = "草稿版本已更新，请刷新后重试"
	ErrPathOccupied         = "页面访问路径已被占用"
)

const (
	MsgPageCreated     = "页面创建成功"
	MsgDraftSaved      = "草稿保存成功"
	MsgPageDetail      = "页面查询成功"
	MsgRevisionsListed = "页面修订查询成功"
)
