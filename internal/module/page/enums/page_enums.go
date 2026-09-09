// Package pageenums 管理 page 模块业务消息。
package pageenums

const (
	// ErrInvalidParam 请求本身不合法（nil 请求、空/空白 ID 等），与资源存在性无关。
	ErrInvalidParam         = "ErrInvalidParam"         // 请求参数无效
	ErrPageNotFound         = "ErrPageNotFound"         // 页面不存在
	ErrProjectNotFound      = "ErrProjectNotFound"      // 站点工程不存在
	ErrInvalidKind          = "ErrInvalidKind"          // 页面类型与内容目标不匹配
	ErrInvalidDocument      = "ErrInvalidDocument"      // 页面草稿文档不合法
	ErrInvalidPath          = "ErrInvalidPath"          // 页面访问路径不合法
	ErrDraftVersionConflict = "ErrDraftVersionConflict" // 草稿版本已更新，请刷新后重试
	ErrPathOccupied         = "ErrPathOccupied"         // 页面访问路径已被占用
	ErrNoStagedArtifact     = "ErrNoStagedArtifact"     // 无暂存产物，请先构建
	ErrRollbackTargetMiss   = "ErrRollbackTargetMiss"   // 回滚目标产物不存在
	ErrRebuildRequired      = "ErrRebuildRequired"      // 草稿已变更，请重新构建后再发布
)

const (
	MsgPageCreated     = "MsgPageCreated"     // 页面创建成功
	MsgPageDeleted     = "MsgPageDeleted"     // 页面删除成功
	MsgDraftSaved      = "MsgDraftSaved"      // 草稿保存成功
	MsgPageDetail      = "MsgPageDetail"      // 页面查询成功
	MsgRevisionsListed = "MsgRevisionsListed" // 页面修订查询成功
	MsgBuildReady      = "MsgBuildReady"      // 构建完成，产物已暂存
	MsgPublished       = "MsgPublished"       // 发布成功
	MsgRollbackDone    = "MsgRollbackDone"    // 回滚成功
	MsgURLUpdated      = "MsgURLUpdated"      // 访问路径已更新
)

// MsgInternalError handler 内部错误统一兜底提示（禁止直出 err.Error() 泄露内部细节）。
const MsgInternalError = "MsgInternalError" // 系统内部错误，请稍后重试
