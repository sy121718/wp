package aidto

// ai_req.go — ai 模块的请求形状（inbound → service）。
//
// 形状照 internal/module/CLAUDE.md 的约定：请求结构体只描述「调用方要什么」，
// 不含 JSON 标签以外的绑定逻辑（绑定在 handle 层，Query 参数 → 结构体）。

// SaveProviderReq 新建 / 更新一个供应商。
//
// ID == 0 表示新建；否则按版本号更新（独占字段：Version 必填，见 service 的乐观锁口径）。
// APIKey 是**明文**，只在本次调用内存里存在：非空则加密后写入 api_key_cipher，
// 空表示「不改动已存的密钥」（页面占位文案「已配置 —— 输入新值可替换」即此语义）。
// ProviderKey 只在新建时生效，更新时忽略（内置默认模型清单按它索引，见 service 注释）。
//
// Status 是**指针**：nil 表示调用方没表态，新建按「启用」落库（与表的 DEFAULT 1 一致）。
// 用零值 int 会让「没提交状态字段」被读成「停用」—— 新建抽屉里没有状态字段，
// 那样建出来的供应商一出生就是停用，且看不出哪里错了。
type SaveProviderReq struct {
	ID          int64
	ProviderKey string
	DisplayName string
	BaseURL     string
	Protocol    string
	APIKey      string
	Status      *int
	Sort        int
	Version     int64
	UpdateBy    int64
}

// SetStatusReq 启停一个供应商（卡片上的状态点）。
type SetStatusReq struct {
	ID       int64
	Status   int
	Version  int64
	UpdateBy int64
}

// SaveModelsReq 整组保存模型目录（config_data.models 的整组替换）。
//
// 目录是**整组替换**的编辑单元：页面一次性提交全部行，service 一次写回，
// 不做「逐行 diff」—— 行没有自己的身份（id 是模型 ID，由用户填），逐行合并
// 会把「删掉一行」变成无法表达的操作。
type SaveModelsReq struct {
	ProviderID int64
	Version    int64
	Models     []ModelEntry
	UpdateBy   int64
}

// ProviderActionReq 「恢复默认模型」「获取可用模型」这类整组动作的入参。
type ProviderActionReq struct {
	ProviderID int64
	Version    int64
	UpdateBy   int64
}
