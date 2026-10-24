package sysconfigdto

// SetGroupsReq 多组一次保存（同一事务）。
//
// 为什么不是「循环调 SetGroup」：系统设置页一张表单同时改 i18n 与 trade 两组，
// 逐组独立提交会出现「语言改了、货币没改」的半截状态，而界面上它们是一次提交。
type SetGroupsReq struct {
	// Groups 要保存的组（每组自带 version 乐观锁前置条件与完整数据）。
	Groups []SetGroupReq
	// UpdateBy 操作人 ID（0 = 系统 / 未登录）。
	UpdateBy int64
}

// SetGroupReq 整组保存请求。
//
// Version 是**必填**：不带版本的整组保存等于「用我手里这份覆盖别人的改动」，
// 而这正是乐观锁要拦的事 —— 缺失即拒绝，不给「省略即强制覆盖」留口子。
type SetGroupReq struct {
	// GroupKey 分组键。
	GroupKey string `json:"groupKey" form:"groupKey"`
	// Data 新的整组配置（整体替换，不是合并）。
	Data map[string]any `json:"data" form:"data"`
	// Version 调用方读到的版本号（乐观锁前置条件）。
	Version int64 `json:"version" form:"version"`
	// UpdateBy 操作人 ID（0 = 系统 / 未登录）。
	UpdateBy int64 `json:"updateBy" form:"updateBy"`
}
