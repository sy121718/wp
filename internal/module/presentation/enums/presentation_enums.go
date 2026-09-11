// Package presentationenums presentation 模块响应消息。
package presentationenums

// 响应消息（未接 i18n 前中文常量）。
const (
	MsgCreateSuccess  = "MsgCreateSuccess"  // 自动发布实例创建成功
	MsgRebuildSuccess = "MsgRebuildSuccess" // 实例重建成功
	MsgListSuccess    = "MsgListSuccess"    // 实例列表获取成功
	MsgDetailSuccess  = "MsgDetailSuccess"  // 实例详情获取成功
	MsgDeleteSuccess  = "MsgDeleteSuccess"  // 实例已删除

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
)

// 实例发布状态（由指针列推导，非表列）。
const (
	// StatusActive 已上线（active_artifact_id 非空）。
	StatusActive = "active"
	// StatusDraft 仅有草稿/未上线。
	StatusDraft = "draft"
)
