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
	ID          int64  `json:"id" form:"id"`
	ProviderKey string `json:"providerKey" form:"providerKey"`
	DisplayName string `json:"displayName" form:"displayName"`
	BaseURL     string `json:"baseUrl" form:"baseUrl"`
	Protocol    string `json:"protocol" form:"protocol"`
	APIKey      string `json:"apiKey" form:"apiKey"`
	Status      *int   `json:"status" form:"status"`
	Sort        int    `json:"sort" form:"sort"`
	Version     int64  `json:"version" form:"version"`

	// UpdateBy 由服务端填（当前登录账号），json:"-" 让请求体无法自报操作人。
	UpdateBy int64 `json:"-"`
}

// SetStatusReq 启停一个供应商（卡片上的状态点）。
type SetStatusReq struct {
	ID       int64 `json:"id" form:"id"`
	Status   int   `json:"status" form:"status"`
	Version  int64 `json:"version" form:"version"`
	UpdateBy int64 `json:"-"`
}

// SaveModelsReq 整组保存模型目录（config_data.models 的整组替换）。
//
// 目录是**整组替换**的编辑单元：页面一次性提交全部行，service 一次写回，
// 不做「逐行 diff」—— 行没有自己的身份（id 是模型 ID，由用户填），逐行合并
// 会把「删掉一行」变成无法表达的操作。
type SaveModelsReq struct {
	ProviderID int64        `json:"providerId" form:"providerId"`
	Version    int64        `json:"version" form:"version"`
	Models     []ModelEntry `json:"models" form:"models"`
	UpdateBy   int64        `json:"-"`
}

// ProviderActionReq 「恢复默认模型」「获取可用模型」这类整组动作的入参。
type ProviderActionReq struct {
	ProviderID int64 `json:"providerId" form:"providerId"`
	Version    int64 `json:"version" form:"version"`
	UpdateBy   int64 `json:"-"`
}

// ChatReq 一次最小对话请求。provider_key + model 二元组即「用了哪个第三方」的标识。
type ChatReq struct {
	ProviderKey     string `json:"provider_key" form:"provider_key" binding:"required,max=50"`
	Model           string `json:"model"        form:"model"        binding:"required,max=200"`
	Input           string `json:"input"        form:"input"        binding:"required,max=200000"`
	MaxOutputTokens int64  `json:"max_output_tokens" form:"max_output_tokens"`

	// SessionID / UserID 是这次调用的**归属**，只用于写调用流水（ai_call_log），不进出站请求。
	//
	// 两个都由服务端填，**不接受调用方自报**：会话层填自己正在续写的那条会话与发起人，
	// 对外接口把 UserID 覆盖成当前登录账号、SessionID 清零（否则日志里的「谁调用的」可被伪造）。
	// 0 表示未记录。
	SessionID int64 `json:"session_id" form:"session_id"`
	UserID    int64 `json:"user_id"    form:"user_id"`

	// Messages 完整的出站消息序列（优先于 Input）；为空时按 Input 构造一条 user 消息。
	//
	// 与 Input 并存是**过渡期的两个入口**：一问一答（provider 连通性探测、模型列表校验）
	// 只需要一句文本，而带工具的轮次需要「历史 + 工具往返」这种序列。
	// 让两者都走同一条出站路径，比给带工具的调用另开一个函数更省事 —— 后者会把
	// 协议分派、SSRF 校验、自定义头、密钥解密这一整串再复制一遍。
	//
	// **不接受调用方自报**（同 SessionID/UserID 的纪律）：出站消息由服务端拼，
	// 对外接口不提供注入任意消息的通道。
	Messages []ChatMessage `json:"-" form:"-"`
	// Tools 本次可用的工具声明；为空表示不带工具（服务端填，同上不接受自报）。
	Tools []ToolSpec `json:"-" form:"-"`
}
