package aidto

// AppendEventReq 追加一条事件。
//
// SessionKey 与 SessionID 二选一：给了 SessionKey 走「按客户端的会话标识续写或新建」，
// 给了 SessionID 直接写指定会话。两者都空是参数错误。
type AppendEventReq struct {
	SessionKey string
	SessionID  int64
	Kind       string
	Content    string
	// Tokens <= 0 时按字符数粗估（宁可高估：它是触发折叠的判断依据，低估会让会话涨过头）。
	Tokens int64
	Meta   map[string]any
	UserID int64
	// ProviderKey / ModelID 仅在「因 SessionKey 新建会话」时写入会话头；
	// 命中已有会话时它们参与绑定一致性校验（非空且与库里不一致 → 拒绝，见 ErrSessionProviderMismatch）。
	ProviderKey string
	ModelID     string
	// Title 仅在「因 SessionKey 新建会话」时用作初始标题（通常传首条用户消息的前若干字）。
	Title string
}

// FoldPlanReq 计算折叠建议区间的入参。
type FoldPlanReq struct {
	SessionID int64
	// KeepRecent 保留最近多少条不参与折叠（尾部工作集；<=0 时用默认值）。
	KeepRecent int
}

// FoldReq 提交一次折叠：区间 + 模型写好的摘要。
type FoldReq struct {
	SessionID int64
	FromSeq   int64
	ToSeq     int64
	Summary   string
	// SummaryTokens <= 0 时按字符数粗估。
	SummaryTokens int64
	UserID        int64
}

// RenameSessionReq 改会话标题 / 切换当前模型 / 归档（三件事共用乐观锁版本号）。
type RenameSessionReq struct {
	ID          int64
	Title       string
	ProviderKey string
	ModelID     string
	Status      int16
	Version     int64
	UserID      int64
}

// SendMessageReq 会话页「发消息」：写 user 事件 → 取会话上下文投影 → 打一次模型 → 写 assistant 事件。
//
// SessionID 与 SessionKey 二选一：给 SessionID 就往这条已有会话里发；只给 SessionKey 时
// 走 EnsureSession 续写或新建（ProviderKey / ModelID 同时是新建会话的会话头）。
//
// 这里不带 binding:"required"：空值与「没选模型」都要回可翻译的业务 key（见 service 的校验），
// 交给框架的 required 会变成不可控的绑定错误文本。
type SendMessageReq struct {
	SessionID   int64  `json:"sessionId" form:"sessionId"`
	SessionKey  string `json:"sessionKey" form:"sessionKey"`
	ProviderKey string `json:"providerKey" form:"providerKey" binding:"max=50"`
	Model       string `json:"model" form:"model" binding:"max=200"`
	Input       string `json:"input" form:"input" binding:"max=200000"`
	// UserText 用户真正敲进去的那句话（不含注入的页面上下文）。
	//
	// Input 是**发给模型**的形态：悬浮球会往里拼「（当前页面：仪表盘 /admin）」这类
	// 上下文。那段注记模型需要，界面回填历史时却不该显示 —— 否则面板里每条提问前面
	// 都顶着一行系统注记，看起来像日志而不是对话。留空时按 Input 处理。
	UserText string `json:"-"`
	// Images 本轮随提问一起发出去的图片，每个元素是**完整的 data URI**。
	//
	// 只有当前轮带图：历史那一轮存的是标识（ImageRefs），不是像素 ——
	// 否则每问一句都要把前面所有图重发一遍，上下文与账单都跟着涨。
	Images []string `json:"images,omitempty" form:"images"`
	// ImageLabels 同一批图片的**标识**（文件名），与 Images 一图对一图。
	//
	// 分工：Images 给模型看（data URI —— 上游在另一台机器上，站内相对路径对它毫无意义），
	// ImageLabels 给人看（界面上画「📎 图片：xxx.png」这类标识，也进事件 meta）。
	// 不存站内地址：图片只给模型看，不必进媒体库，于是也没有一个长期有效的地址可存。
	ImageLabels     []string `json:"imageLabels,omitempty" form:"imageLabels"`
	MaxOutputTokens int64    `json:"maxOutputTokens" form:"maxOutputTokens"`
	UserID          int64    `json:"-"`
}
