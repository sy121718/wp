package commentdto

// comment_resp.go — comment 模块的响应结构。
//
// 时间字段一律 utils.JSONTime（AGENTS.md：对外 JSON 只到秒）。
// 展示用的格式化（"2006-01-02 15:04"）留在页面 / 片段层 —— dto 是数据，不是文案。

import "go_wp/pkg/utils"

// Item 公开列表里的一条评论（含它的一级回复）。
type Item struct {
	// ID 评论 id（**纯内部流水**，见迁移 466 的主键选型）。
	//
	// 片段把它渲染成 data-comment-id 属性（回复表单要提交它），不进任何 URL ——
	// 一旦它出现在 URL 上就必须换成 uuid 并加归属校验。
	ID int64
	// Body 正文（调用方负责转义：Jet 默认转义，禁止标记为安全）。
	Body string
	// 刻意**没有**作者显示名字段：评论者名字在 user 模块手里（访客账号），
	// 而本批没有接入「按 id 批量取显示名」的收窄端口（user 模块现有契约里
	// 没有这一条，CustomerAdminPort 返回的资料含邮箱等不该进公开面的字段）。
	// 展示层因此用一句兜底文案（片段用 site.fragment.comment.author_guest 词条，
	// 后台页显示账号 #id）—— 空字段比错字段好，而「我们不知道是谁」是**事实**。
	// IsReply 这条是回复（渲染时缩进一层）。
	IsReply bool
	// CreatedAt 提交时刻。
	CreatedAt utils.JSONTime
	// Replies 它的一级回复（时间正序）。
	Replies []Item
}

// ListResp 公开列表结果。
type ListResp struct {
	Items []Item
	// Total 已通过的**顶层评论**总数（分页依据；回复不计入 —— 否则「共 N 条」会随回复数跳动）。
	Total int64
	Page  int
	// PageSize 实际生效的每页条数（已归一）。
	PageSize int
	// HasMore 还有下一页。
	HasMore bool
}

// SubmitResp 提交结果。
type SubmitResp struct {
	// ID 新评论的 id（内部流水；片段不用它，接口测试与日志用）。
	ID int64
	// Status 落库后的状态（当前固定 pending —— 先审后发口径）。
	Status string
	// Message 可展示的成品文案 key（"评论已提交，待审核"）。
	Message string
}

// AdminItem 后台审核队列里的一行。
//
// 比公开列表多出「谁在什么实体上评的、什么时候审的」，少掉回复嵌套 ——
// 审核是逐条判断正文，把回复折进父行反而让运营多一次展开。
type AdminItem struct {
	ID         int64
	Body       string
	EntityType string
	EntityID   string
	// UserID 评论者账号 id（后台显示 #id：这是控制面，运营需要据此定位账号）。
	UserID uint64
	Status string
	// StatusLabel 状态展示名（按请求语言取好的成品文案）。
	StatusLabel string
	CreateTime  utils.JSONTime
	// ReviewedAt 审核时刻；未审核为 nil。
	ReviewedAt *utils.JSONTime
	// ReviewerID 审核人；未审核为 nil。
	ReviewerID *uint64
	// IsReply 这条是回复（后台加一个「回复」标记）。
	IsReply bool
}

// AdminListResp 后台审核队列结果。
type AdminListResp struct {
	Items    []AdminItem
	Total    int64
	Page     int
	PageSize int
}

// ReviewResp 批量审核结果。
type ReviewResp struct {
	// Affected 实际被改写的行数（可能小于请求条数：并发的另一次审核已把它改成目标状态，
	// 或者 id 不属于这个工程 —— 两种情况都不该被报成失败，但**必须如实回带**）。
	Affected int64
	// Status 本次设置的目标状态。
	Status string
}

// EntityType 一种可被评论的实体类型（对外暴露的展示形态）。
//
// 取值来自**拥有该实体的模块**（装配期注册，见 contract.EntityType）：
// 后台筛选下拉与页面上显示的名称都从这里来，comment 模块自己不维护取值表。
type EntityType struct {
	// Type 实体类型标识（如 article / product）。
	Type string
	// Label 展示名（已按请求语言取好的成品文案）。
	Label string
}
