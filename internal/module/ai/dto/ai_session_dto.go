package aidto

import "go_wp/pkg/utils"

// ai_session_dto.go — AI 会话层的请求 / 响应形状（会话头、投影项、追加与折叠）。
//
// 时间字段一律 utils.JSONTime（对外 JSON 只到秒，AGENTS.md「时间列」口径）。
// 投影项不借用 model 实体：model 只回实体，service 负责翻译成这里的形状。

// Session 会话头（列表项与详情共用）。
type Session struct {
	ID          int64  `json:"id"`
	SessionKey  string `json:"sessionKey"`
	Title       string `json:"title"`
	ProviderKey string `json:"providerKey"`
	ModelID     string `json:"modelId"`
	Status      int16  `json:"status"`
	// EventCount = 已分配的最大事件序号（= 事件条数，序号无洞）。
	EventCount    int64          `json:"eventCount"`
	CompactCount  int            `json:"compactCount"`
	ContextTokens int64          `json:"contextTokens"`
	Version       int64          `json:"version"`
	CreateTime    utils.JSONTime `json:"createTime"`
	UpdateTime    utils.JSONTime `json:"updateTime"`
}

// SessionItem 投影里的一项：要么是一条事件，要么是一次折叠留下的摘要块。
//
// Folded=true 时 Content 是模型写的摘要，FoldedFrom/FoldedTo 是它替换掉的序号区间
// （前端据此渲染成可展开的折叠块；展开读原文走事件日志接口，不在这里展开）。
type SessionItem struct {
	Seq        int64  `json:"seq"`
	Kind       string `json:"kind"`
	Content    string `json:"content"`
	Tokens     int64  `json:"tokens"`
	Folded     bool   `json:"folded"`
	FoldedFrom int64  `json:"foldedFrom"`
	FoldedTo   int64  `json:"foldedTo"`
}

// SessionDetail 会话详情 = 会话头 + 当前投影 + 计量。
//
// VisibleTokens 是投影重算出来的可见 token 数（与 ContextTokens 的区别：前者实时算，
// 后者是最后一次写入时落库的值；两者不一致说明有人在两次写入之间直接改了表）。
type SessionDetail struct {
	Session
	Items         []SessionItem `json:"items"`
	VisibleTokens int64         `json:"visibleTokens"`
	VisibleCount  int           `json:"visibleCount"`
}

// SessionEventItem 事件日志里的一条原始记录（运维视图：压缩后原文仍在这里）。
type SessionEventItem struct {
	Seq            int64          `json:"seq"`
	Kind           string         `json:"kind"`
	SurfaceOp      string         `json:"surfaceOp"`
	ReplaceFromSeq int64          `json:"replaceFromSeq"`
	ReplaceToSeq   int64          `json:"replaceToSeq"`
	Content        string         `json:"content"`
	ContentTokens  int64          `json:"contentTokens"`
	CreateTime     utils.JSONTime `json:"createTime"`
	// Meta 事件的附加结构；展示指令交给页面渲染的视图在 meta.render 里。
	//
	// omitempty：没有附加结构的事件保持原样，客户端不必区分「空对象」与「没有」。
	Meta map[string]any `json:"meta,omitempty"`
}

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

// AppendEventResult 追加结果：分配到的序号 + 追加后的会话头。
type AppendEventResult struct {
	Seq     int64   `json:"seq"`
	Session Session `json:"session"`
}

// FoldPlanReq 计算折叠建议区间的入参。
type FoldPlanReq struct {
	SessionID int64
	// KeepRecent 保留最近多少条不参与折叠（尾部工作集；<=0 时用默认值）。
	KeepRecent int
}

// FoldPlanResult 折叠建议：从哪折到哪、折叠前多少 token、折完大概剩多少、值不值得。
//
// Excerpt 是待折叠段的纯文本（供写摘要用）；Worthwhile=false 表示净收益为负或区间不足，
// 调用方应当放弃这次折叠（口径：短会话不值得压，见 docs/16 §3.1）。
type FoldPlanResult struct {
	SessionID      int64  `json:"sessionId"`
	FromSeq        int64  `json:"fromSeq"`
	ToSeq          int64  `json:"toSeq"`
	SegmentTokens  int64  `json:"segmentTokens"`
	EstimatedAfter int64  `json:"estimatedAfter"`
	EstimatedSave  int64  `json:"estimatedSave"`
	Excerpt        string `json:"excerpt"`
	Worthwhile     bool   `json:"worthwhile"`
	Reason         string `json:"reason"`
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

// FoldResult 折叠结果：三条事件（开始 / 摘要 / 结束）的序号 + 折叠后的会话头。
type FoldResult struct {
	StartSeq   int64   `json:"startSeq"`
	SummarySeq int64   `json:"summarySeq"`
	EndSeq     int64   `json:"endSeq"`
	Session    Session `json:"session"`
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
	UserText        string `json:"-"`
	MaxOutputTokens int64  `json:"maxOutputTokens" form:"maxOutputTokens"`
	UserID          int64  `json:"-"`
}

// SendMessageResult 一次「发消息」的结果：会话头 + 落下来的两条事件（用户输入 / 模型回复）。
//
// 两条事件都带分配到的序号与内容 token，前端据此原地渲染，不必再回查事件日志。
type SendMessageResult struct {
	Session        Session          `json:"session"`
	UserEvent      SessionEventItem `json:"userEvent"`
	AssistantEvent SessionEventItem `json:"assistantEvent"`
	// ToolEvents 本轮的工具调用与结果（按发生顺序成对出现：调用一条、结果一条）。
	//
	// 单独成一列而不是塞进 AssistantEvent 的正文：它们在事件日志里各有自己的 seq，
	// 是**并列的独立事件**。合并会让「模型说的话」与「工具回的数据」再也分不开，
	// 而页面要能分别渲染（前者是回答，后者是可展开的取数证据）。
	ToolEvents []SessionEventItem `json:"toolEvents,omitempty"`
}

// —— 会话页的用量统计与多维筛选（后台看板）——

// SessionQuery 会话列表 / 统计的筛选条件（页面 query 解析后的形状）。
//
// 时间保持字符串（yyyy-mm-dd）而不在这里转成 time.Time：它直接来自 URL，解析放在 service，
// 解析失败按「不限」处理 —— 一个手改的脏日期不该把整页打成 500。
// Status 用 -1 表达「不限状态」：0 是「已归档」这一真实取值，不能让 0 兼任「不限」。
type SessionQuery struct {
	Keyword     string
	Status      int
	ProviderKey string
	ModelID     string
	CreateBy    int64
	From        string
	To          string
}

// SessionUsage 会话用量指标卡。
//
// 口径：Tokens 是**事件正文估算 token 之和**（ai_event.content_tokens），与会话头上的
// context_tokens 不是一回事 —— 前者是这段时间写进来多少，后者是此刻还留在上下文里多少。
// AvgTokens = Tokens / Sessions（Sessions 为 0 时是 0，不产生 NaN）。
//
// *Text 是展示用的短文本（12345678 → "12.3M"），与对应整数同源、由 service 生成：
// 缩小单位是展示决策，模板不做算术（Jet 没有浮点格式化），而每个用到的地方各拼一遍
// 必然出现两种写法。原始数字照样给出，title / 详情用得上。
type SessionUsage struct {
	Sessions      int64  `json:"sessions"`
	Events        int64  `json:"events"`
	Tokens        int64  `json:"tokens"`
	Compacts      int64  `json:"compacts"`
	AvgTokens     int64  `json:"avgTokens"`
	TokensText    string `json:"tokensText"`
	AvgTokensText string `json:"avgTokensText"`

	// —— 下面三组是 docs/16 §3.1 的验收数字（命中率 / 压缩开销 / 折叠净收益）——
	//
	// 它们与上面的 Tokens 口径**不同**，别当成同一件事：
	//   Tokens 是**事件**的 content_tokens 之和（上下文投影的占用）；
	//   命中率来自**调用流水**（ai_call_log）里上游上报的 input/cached。
	// 一个衡量「上下文有多大」，一个衡量「发出去时省了多少」。
	//
	// HitRateReady=false 时 HitRateText 是「—」而不是「0%」：这家供应商没报缓存字段时
	// 那个 0 什么也不说明，显示成 0% 会让人以为前缀纪律失效了。
	HitRateReady bool    `json:"hitRateReady"`
	HitRatePct   float64 `json:"hitRatePct"`
	HitRateText  string  `json:"hitRateText"`
	// CachedCalls / UsageReportedCalls 支撑命中率的分母构成（「几次调用里几次报了缓存」）。
	CachedCalls        int64 `json:"cachedCalls"`
	UsageReportedCalls int64 `json:"usageReportedCalls"`
	// CompactCostPct 折叠摘要占整个上下文的比重（%）。
	//
	// **口径**：Σ(compact_summary 事件的 content_tokens) / Σ(全部事件 content_tokens)。
	// docs/16 §3.1 的原始表述含「摘要那次模型调用 + 重写前缀」的成本，而本仓当前的
	// 摘要由人填进表单（FoldReq.Summary），**不产生模型调用** —— 所以这里记的是
	// 纯上下文口径。哪天摘要改成模型生成，这里要一并改成含模型成本，否则这个数会
	// 恒偏小，而它恰恰是用来判断「压得值不值」的。
	CompactCostPct  float64 `json:"compactCostPct"`
	CompactCostText string  `json:"compactCostText"`
}

// TrendSeries 折线图上的一条线（一个「供应商 + 模型」组合）。
//
// Points 是 SVG <polyline points="…"> 的现成内容（"x,y x,y …"，坐标系 0..1000 / 0..200）：
// 坐标换算留在 service，模板只贴字符串 —— 与柱状图那版「模板不做算术」同一条规矩。
type TrendSeries struct {
	ProviderKey string `json:"providerKey"`
	ModelID     string `json:"modelId"`
	// OtherCount > 0 表示这是把超上限的几家并起来的「其他」线，值是并入的家数。
	OtherCount int    `json:"otherCount"`
	Total      int64  `json:"total"`
	TotalText  string `json:"totalText"`
	// Color 是 1..8 的调色板序号，模板映射到 --chart-cN。
	Color  int    `json:"color"`
	Points string `json:"points"`
	// Single 表示这条线只有一个数据点：polyline 画不出一个点，模板改用 DotX/DotY 画圆。
	Single bool `json:"single"`
	DotX   int  `json:"dotX"`
	DotY   int  `json:"dotY"`
}

// SessionTrend 折线图的全部数据：窗口两端、峰值、若干条线。
type SessionTrend struct {
	From     string        `json:"from"`
	To       string        `json:"to"`
	Peak     int64         `json:"peak"`
	PeakText string        `json:"peakText"`
	Series   []TrendSeries `json:"series"`
}

// SessionFilterOptions 筛选下拉的候选值。
//
// 取**全量**而不是当前结果集的取值：筛成 A 供应商后再想切到 B，下拉里必须还有 B，
// 否则筛一次就再也回不去。
type SessionFilterOptions struct {
	Providers []string `json:"providers"`
	Models    []string `json:"models"`
	Creators  []int64  `json:"creators"`
}

// SessionTokenUsage 一个会话的 token 消耗快照（详情抽屉顶部的三个数 + 明细表）。
//
// 两个口径刻意分开放：ContextTokens 是**下一轮真的发给模型**的量（折叠后变小），
// Total 是这个会话从建立到现在写过的事件正文总量（含已折叠的，折叠不会让它变小）。
// 混成一个数会让人以为折叠没生效。
type SessionTokenUsage struct {
	ContextTokens int64  `json:"contextTokens"`
	Total         int64  `json:"total"`
	TotalText     string `json:"totalText"`
	Compacts      int64  `json:"compacts"`
	// Rows 按事件类型拆开的消耗；HasBreakdown 为 false 时模板不渲染明细表
	// （一个空表头比没有表更难看，而没有事件本身是正常状态）。
	Rows         []SessionTokenRow `json:"rows"`
	HasBreakdown bool              `json:"hasBreakdown"`
}

// SessionTokenRow 明细里的一行：某类事件贡献了多少 token。
//
// 只给 kind 原文，不给中文标签：文案是展示层的事（模板拼 admin.ai.session.kind.<kind>
// 词条），service 里硬编码一次就要在 i18n 里再维护第二份。
type SessionTokenRow struct {
	Kind   string `json:"kind"`
	Events int64  `json:"events"`
	Tokens int64  `json:"tokens"`
}

// SessionModelUsage 一个会话按 (供应商, 模型) 拆开的 token 消耗 —— 列表行悬浮卡的内容。
//
// 拆到这一层才有用：「这个会话一共 15 token」看不出钱花在哪，而「opencode-go / muse-spark
// 12 token、deepseek / chat 3 token」才回答得了「谁在消耗」。
// SessionCallRow 一次上游调用在会话行悬浮卡里的展示形状（ai_call_log 的一行）。
//
// 与 SessionModelUsage 的分工：那个是**聚合**（这条会话在某家模型上总共烧了多少），
// 这个是**流水**（每一次调用各自多久、多少 token、成没成）。两者都要 ——
// 聚合回答「钱花在哪家」，流水回答「哪一次特别慢 / 哪一次失败了」。
type SessionCallRow struct {
	// Time 已格式化的「01-02 15:04」：格式化留在 service，模板不做时间算术。
	Time        string `json:"time"`
	ProviderKey string `json:"providerKey"`
	ModelID     string `json:"modelId"`
	// LatencyText 已格式化的耗时（「2.9s」/「480ms」）。
	LatencyText string `json:"latencyText"`
	Tokens      int64  `json:"tokens"`
	TokensText  string `json:"tokensText"`
	OK          bool   `json:"ok"`
	// ErrorKey 是失败时的 i18n key（与 enums 哨兵同源），ErrorText 是它未接词条时的中文兜底
	// （取自 aienums.FacingMessages）。模板写 tr(c.ErrorKey, c.ErrorText)：有词条走词条、
	// 没词条回落中文 —— 与页面其它错误提示同一口径，失败原因不会退化成一句「失败」。
	ErrorKey  string `json:"errorKey"`
	ErrorText string `json:"errorText"`
}

// SessionCalls 一条会话的调用流水预览：最近若干条 + 总条数。
//
// 只给「最近 N 条」而不是全量：悬浮卡是鼠标一停就要出来的东西，跑了三个月的会话
// 可能有几千次调用。总条数一并给出 —— 读的人得知道自己看到的是不是全部。
type SessionCalls struct {
	Total int64            `json:"total"`
	Rows  []SessionCallRow `json:"rows"`
}

type SessionModelUsage struct {
	ProviderKey string `json:"providerKey"`
	ModelID     string `json:"modelId"`
	Events      int64  `json:"events"`
	Tokens      int64  `json:"tokens"`
	TokensText  string `json:"tokensText"`
	// Unrecorded 为 true 表示这一组来自 529 之前的历史事件（当时没记来源）。
	// 必须与真实值分开显示：否则空串会被当成「某个名字为空的供应商」。
	Unrecorded bool `json:"unrecorded"`
}

// DialogueTurn 一轮对话里的一个角色发言。
//
// 只带「谁说的 + 说了什么 + 当时的思考过程」三样：回填历史是为了让用户看见
// 「这是一段对话」而不是「一个一次性的搜索框」，展示指令（render）不进历史 ——
// 那一轮当时的卡片是那一轮的产物，重放它们会把旧的数字当成现在的。
type DialogueTurn struct {
	Role      string `json:"role"` // user | assistant
	Text      string `json:"text"`
	Reasoning string `json:"reasoning,omitempty"`
}
