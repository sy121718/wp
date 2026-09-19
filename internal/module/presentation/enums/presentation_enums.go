// Package presentationenums presentation 模块响应消息。
package presentationenums

// 响应消息（未接 i18n 前中文常量）。
const (
	MsgCreateSuccess  = "MsgCreateSuccess"  // 自动发布实例创建成功
	MsgRebuildSuccess = "MsgRebuildSuccess" // 实例重建成功
	MsgListSuccess    = "MsgListSuccess"    // 实例列表获取成功
	MsgDetailSuccess  = "MsgDetailSuccess"  // 实例详情获取成功
	MsgDeleteSuccess  = "MsgDeleteSuccess"  // 实例已删除
	// MsgPreviewSuccess 预览渲染成功（未落库、未激活，issue #14）。
	MsgPreviewSuccess = "MsgPreviewSuccess"
	// MsgUpdateURLSuccess 实例 URL 修改成功（新路径已激活、旧路径已按策略处置）。
	MsgUpdateURLSuccess = "MsgUpdateURLSuccess"

	ErrInvalidParam  = "ErrInvalidParam"  // 参数错误
	ErrNotFound      = "ErrNotFound"      // 实例不存在
	ErrNoTemplate    = "ErrNoTemplate"    // 该类型无可用内容模板
	ErrEntityMissing = "ErrEntityMissing" // 内容实体不存在
	ErrBuildFailed   = "ErrBuildFailed"   // 实例构建失败
	// ErrProjectRequired 未指定工程且无法从唯一工程推导（0 个或多个工程）。
	ErrProjectRequired = "ErrProjectRequired"
	// ErrProjectNotFound 显式指定的工程不存在。
	ErrProjectNotFound = "ErrProjectNotFound"
	// ErrRegistryMissing 实体类型注册表未装配（装配缺陷，构建期无法解析实体字段）。
	ErrRegistryMissing = "ErrRegistryMissing"
	// ErrTemplateTypeMismatch 指定的模板与内容实体类型不匹配（issue #14）：
	// 多套命名模板之间不允许串用（拿商品模板渲染文章会在构建期产出错误数据）。
	ErrTemplateTypeMismatch = "ErrTemplateTypeMismatch"
	// ErrInvalidPath 目标路径不合法（归一化失败：缺前导斜杠 / 含非法段 / 超长等）。
	ErrInvalidPath = "ErrInvalidPath"
	// ErrSamePath 新路径与实例当前路径相同（改 URL 的空操作）。
	ErrSamePath = "ErrSamePath"
	// ErrPathOccupied 目标路径已被其他页面或展示实例占用。
	// 与 page 侧 ErrPathOccupied 同一文案口径（改 URL 抢路径的失败原因对用户是同一件事）。
	ErrPathOccupied = "ErrPathOccupied"

	// ErrDetachConfirmRequired 转入独立文档需要用户确认（会放弃模板同步）。
	// 服务层在未确认时返回它，前端据它弹确认并带 confirmDetach 重试 ——
	// 不用「先弹窗再请求」是因为判据（文档结构是否真的变了）只有服务端算得准。
	ErrDetachConfirmRequired = "ErrDetachConfirmRequired"
	// ErrRollbackTargetMiss 回滚目标不存在（hash / 快照找不到或产物文件已缺失）。
	ErrRollbackTargetMiss = "ErrRollbackTargetMiss"
	// ErrRollbackFailed 回滚激活失败（线上版本保持不变）。
	ErrRollbackFailed = "ErrRollbackFailed"
	// ErrSnapshotMismatch 目标快照不属于该实例（防止拿别人的快照回滚自己）。
	ErrSnapshotMismatch = "ErrSnapshotMismatch"
)

// 实例发布状态（由指针列推导，非表列）。
const (
	// StatusActive 已上线（active_artifact_id 非空）。
	StatusActive = "active"
	// StatusDraft 仅有草稿/未上线。
	StatusDraft = "draft"
)
