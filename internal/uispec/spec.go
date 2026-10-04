// Package uispec 定义「模型输出的展示指令」（spec）及其校验、渲染。
//
// 为什么独立成包：spec 是两个方向共用的协议 ——
//
//	· 生产侧（ai 模块）把模型输出解析成 Spec；
//	· 消费侧（渲染器）把 Spec + 数据源结果渲染成页面片段。
//
// 放在 ai 模块里会让渲染器反向依赖 ai；放在 templates 里会让 ai 依赖渲染细节。
// 本包**零业务依赖**（不 import 任何模块），只描述形状与界限。
//
// 一条硬边界（docs/17 决策 D5）：**spec 不携带数据**。
// 模型只回答「渲染哪个组件、数据从哪个已注册工具取、参数是什么」，数字一律服务端查。
// 理由是后台概览页有过 P0 事故：整页静态演示数字被运营当成真实统计。模型编的数字比静态
// 假数据更难发现 —— 它会长得像真的，而且跟着筛选条件「合理地」变。
package uispec

// 组件类型白名单。首批四种（docs/17 P5）。
//
// 白名单是**封闭**的：不在表里的类型一律拒绝，不做「未知类型当纯文本渲染」的兜底 ——
// 那会把「模型编了个组件」变成「页面上出现一段谁也看不懂的文字」，比直接降级更难查。
const (
	TypeStat      = "stat"      // 一张或多张指标卡（label + value + 可选涨跌）
	TypeTable     = "table"     // 表格（列 + 行）
	TypeList      = "list"      // 项目列表（label + value + 可选说明）
	TypeAccordion = "accordion" // 手风琴（可折叠分组）
)

// spec 的规模上限。
//
// 这些数字不是美学偏好，是**成本边界**：一次回答的块数决定了要跑几次数据源查询
// （每个 block 至少一次），行数决定了渲染出的 DOM 体积。模型没有「页面会不会卡」的概念，
// 只能由这一层替它兜住。
const (
	MaxBlocks     = 8   // 一次回答最多几个块
	MaxTitleRunes = 80  // 块标题
	MaxSourceLen  = 64  // 数据源名（= 已注册工具名）
	MaxParams     = 8   // 每个块最多几个参数
	MaxParamKey   = 32  // 参数名
	MaxParamValue = 128 // 参数值（含逗号分隔的列表）
	MaxWordRunes  = 120 // 块内文案（表头 / 标签 / 提示）
	MaxLimit      = 100 // 行数 / 项数上限
	MinLimit      = 1
)

// Block 一个渲染块。
//
// 字段是**封闭**的：解析时用 DisallowUnknownFields，模型多给一个字段就整条拒绝。
// 为什么这么严：多出来的字段往往是模型「自己发明」的能力（比如 source 之外再带一份
// rows 数据），放过去就等于开了一条绕过「数字必须服务端查」的路。
type Block struct {
	// Type 组件类型，取值见 TypeStat 等常量。
	Type string `json:"type"`
	// Title 块标题（可空；渲染器会按类型给默认标题）。
	Title string `json:"title,omitempty"`
	// Source 数据源 = **已注册的工具名**。渲染器按名查工具并执行，取结果的 Data 渲染。
	// 复用工具注册表而不是另建一份数据源表：权限点、审计、结果剪枝、参数 schema 校验
	// 都已经在工具那条链上，另起一套等于把这四样各写第二遍。
	Source string `json:"source"`
	// Params 数据源参数（字符串值；校验与类型转换交给工具的 JSON Schema）。
	Params map[string]string `json:"params,omitempty"`
	// Limit 行数 / 项数上限（可空表示用数据源默认值）。
	Limit int `json:"limit,omitempty"`
}

// Spec 一次回答的展示部分。
//
// Text 与 Blocks 不是二选一：模型可以「说一段话 + 附一张表」。两个都空才是无效回答。
//
// 待定（不在本批）：docs/17 决策 D6 要求 spec 与 action 同一协议两半（{blocks, actions}）——
// 「出图」与「帮你筛好」是同一个回答的两部分。action 的运行时（前端点一下真的应用筛选）
// 还没做，所以这里先不声明字段：声明了不实现，字段就会被人当已支持的能力用。
type Spec struct {
	// Text 纯文本回答。渲染失败时**它就是降级后的全部内容**。
	Text string `json:"text,omitempty"`
	// Blocks 渲染块（可空：纯文字回答是合法形态）。
	Blocks []Block `json:"blocks,omitempty"`
}
