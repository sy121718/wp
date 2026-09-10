// Package navigationenums 统一管理 navigation 模块响应消息（0-C）。
package navigationenums

// 成功消息（未接 i18n 前中文常量）。
const (
	MsgCreateSuccess = "MsgCreateSuccess" // 导航项创建成功
	MsgUpdateSuccess = "MsgUpdateSuccess" // 导航项更新成功
	MsgListSuccess   = "MsgListSuccess"   // 导航列表获取成功
	MsgDetailSuccess = "MsgDetailSuccess" // 导航详情获取成功
	MsgDeleteSuccess = "MsgDeleteSuccess" // 导航项删除成功
)

// 错误消息。
const (
	ErrInvalidParam = "ErrInvalidParam" // 参数错误
	ErrNotFound     = "ErrNotFound"     // 导航项不存在
	ErrInvalidKind  = "ErrInvalidKind"  // 非法的导航类型，仅支持 header 或 footer
	ErrPathTaken    = "ErrPathTaken"    // 同工程同类型下已存在相同路径的导航项
	// ErrInvalidSource 非法的菜单项来源（或非 custom 来源未指定来源实体）。
	ErrInvalidSource = "ErrInvalidSource" // 非法的菜单项来源，仅支持 custom/page/article/product/category/block，且非 custom 必须指定来源实体
	// ErrInvalidTarget 非法的打开方式。
	ErrInvalidTarget = "ErrInvalidTarget" // 非法的打开方式，仅支持 self 或 blank
	// ErrInvalidParent 非法的父引用（自引用 / 成环 / 跨工程 / 跨类型 / 父项不存在）。
	ErrInvalidParent = "ErrInvalidParent" // 非法的父引用：父项必须存在、同工程、同类型，且不得形成环
)
