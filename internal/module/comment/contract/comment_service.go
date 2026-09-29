// Package commentcontract — comment 模块对外暴露的契约。
//
// 本模块是**独立模块**（BIZ-5），不挂在 content / product 之下：评论的横切关注点
// （审核状态机 / 限流 / 防刷 / 审核后台 / i18n / 分页）与实体无关，
// 每模块自带等于把这一整套写 N 遍、运营还要跑 N 个后台页。
//
// 多态挂载的两条边界（都在这一层收口）：
//
//   - **实体类型白名单由拥有该实体的模块声明**：content 说自己的东西叫 "article"、
//     product 说自己的东西叫 "product"，装配期把它们的常量作为 EntityType 注册进来
//     （见 contract.EntityType）。comment 模块**不认识**这些实体的任何细节 ——
//     它只知道「有一类叫 X 的东西可以被评论，展示名是 Y」。
//     同型先例：pkg/datarule 的白名单由拥有该表的实体声明；masterdata 的实体类型白名单。
//
//   - **差异化规则由消费方经收窄端口提供**：例如「商品评论必须买过」，comment 模块
//     不查商品表、也不认识订单 —— 它只问 EntityPolicy。端口未注入时**放行**
//     （理由见 EntityPolicy 的注释）。
//
// 跨模块可传递类型只有本包的接口与 dto/ 下的不可变结构（AGENTS.md §模块现状）。
package commentcontract

import (
	"context"

	commentdto "go_wp/internal/module/comment/dto"
)

// LabelPair 展示名：i18n key + 中文兜底。
//
// 两个值一起给（同 masterdata / user enums 的既有形态）：只给中文 → 英文界面恒中文；
// 只给 key → 词条缺失时页面显示裸 key；两个一起给，调用点 `tr(key, fallback)`
// 命中出译文、未命中出中文兜底。**key 与兜底都由拥有该实体的模块提供** ——
// 那是它的实体，只有它知道该怎么称呼。
type LabelPair struct {
	Key      string
	Fallback string
}

// EntityType 一种可被评论的实体（**由拥有该实体的模块声明取值**）。
type EntityType struct {
	// Type 实体类型标识（与实体模块自己的枚举常量同值，如 content 的 "article"）。
	//
	// 它是**数据**（存进 comments.entity_type 列），改名等于让历史行变成孤儿 ——
	// 所以调用点必须传实体模块的常量，不要在这里手写字面量。
	Type string
	// Label 展示名（i18n key + 中文兜底）。
	Label LabelPair
}

// EntityPolicy 差异化评论规则端口（由**拥有该实体的模块**实现；可缺）。
//
// 收窄到一条方法：comment 只需要一个「能不能评」的判定结果 —— 它拿不到商品表、
// 订单表或任何实体侧的写能力（越权防护靠接口形状，不靠调用方自觉）。
//
// **未注入时的行为是「放行」**，判断与理由：
//
//   - 判据（AGENTS.md §认证与鉴权 / 写操作）：fail-closed 用于**安全边界**。
//     评论提交的安全边界是「必须登录」（硬约束，在 service 里无条件执行），
//     不是这条业务规则 —— 差异化规则是**产品策略**（「买过没」随时会改）。
//   - 反向代价：未注入即拒绝会让「装配漏了一行」直接表现成「整站评论功能废掉」，
//     而运营看到的现象与「这个站不让评论」完全一样，排查成本极高；
//     放行则表现为「规则没生效」，属于策略未启用，且审核队列仍然把关内容。
//   - 需要强约束的站点必须**显式注入**端口（实现方自己保证判定正确）；
//     未注入时启动日志会留一条 Warn（装配层负责，见 wiring.go 的清单条目）。
//
// 返回值分三类，不要混：
//
//   - (nil, nil)      —— 允许；
//   - (denial, nil)   —— 不允许，且 denial 自带**可展示文案**（消费方自证能展示）；
//   - (nil, err)      —— 判定本身失败（查询故障 / 装配问题），comment 记日志并归口，
//     不把消费方的内部错误原文透出去。
type EntityPolicy interface {
	// AllowComment 判定某个访客能否对某个实体发表评论。
	AllowComment(ctx context.Context, req *PolicyReq) (denial *PolicyDenial, err error)
}

// PolicyReq 差异化规则的判定入参（不可变）。
type PolicyReq struct {
	ProjectID  string
	EntityType string
	EntityID   string
	// UserID 评论者（访客账号 id）；恒非零（调用方在登录校验之后才问规则）。
	UserID uint64
}

// PolicyDenial 被差异化规则拒绝的结果。
//
// 文案做成 **key + 兜底** 一对（而不是一个成品字符串）：
// 拒绝文案是**消费方**的词条（如 product 的 `product.err.commentPurchaseRequired`），
// 而请求语言只有 comment 侧知道（片段的 ?lang / 后台的请求语言）——
// 只给中文成品的话，英文站点会看到一句中文。做成 key + 中文兜底之后，
// comment 侧在出口用 `i18n.Translate(key, fallback, lang)` 取词：
// 命中全局 sys_i18n 出译文，未命中出中文兜底（绝不输出裸 key，也绝不输出空串）。
//
// 做成独立返回值而不是 error 的理由：error 在 comment 侧的语义是「出了故障」，
// 而「买过才能评」不是故障 —— 它是**判定结果**，只是结果是「不允许」。
type PolicyDenial struct {
	// Message 拒绝原因，语义是 **i18n key**（消费方自己的词条 key）。
	//
	// 也可以直接给成品文案（那就把它同时填进 Fallback）—— 契约只要求
	//「Message 与 Fallback 合起来能产出**当前语言**下的一句人话」。
	Message string
	// Fallback Message 未命中词条时的**中文原文**（消费方提供）。
	//
	// 为空时 comment 侧回落本模块的归口业务文案（「当前不满足这条内容的评论条件」）——
	// 页面上绝不出现空白（同 enums 的 LabelPair 形态：key 与兜底一起给）。
	Fallback string
}

// FragmentPort 访问面片段层消费的**收窄**契约。
//
// 只有两条只读 / 写入方法，没有审核、没有后台列表：片段层拿到它就没有
// 「替别人通过审核」的能力（同 membershipcontract.Reader 的取舍）。
type FragmentPort interface {
	// ListApproved 按 (工程, 实体类型, 实体 id) 列出**已通过**的评论（含一级回复）。
	ListApproved(ctx context.Context, req *commentdto.ListReq) (res *commentdto.ListResp, err error)
	// Submit 提交一条评论（落 pending，等审核）。
	//
	// 前置条件（调用方必须先做）：登录身份 UserID 非零、访问面 CSRF 已校验。
	// service 会再校验一次登录（不信任调用方），但 CSRF 是访问面协议，不在这里判。
	Submit(ctx context.Context, req *commentdto.SubmitReq) (res *commentdto.SubmitResp, err error)
}

// FacingTexter 把本模块的业务错误转成一句**可展示**的文案。
//
// 为什么需要它：片段层 / 后台页只依赖 contract 与不可变 dto，**拿不到本模块 enums
// 的白名单**（enums 是模块内部实现）。没有这个出口，消费方要么直出 err.Error()
// （把 PostgreSQL 原文漏到页面上），要么一律通用提示（吞掉「这个类型不支持评论」
// 这类可行动差异）。形态与 membershipcontract.FacingTexter 一致。
type FacingTexter interface {
	// FacingText 命中白名单 → 按 lang 取词的成品文案；未命中 → 记日志 + 归口文案。
	// err 为 nil 时返回空串（调用方不必先判空）。
	FacingText(lang string, err error) string
}

// CommentService comment 模块的完整契约（装配层持有；片段层只拿 FragmentPort）。
type CommentService interface {
	FragmentPort

	// AdminList 后台审核队列（按工程 + 状态 / 实体类型 / 关键词筛选 + 分页）。
	AdminList(ctx context.Context, req *commentdto.AdminListReq) (res *commentdto.AdminListResp, err error)
	// Review 批量通过 / 驳回（单条 UPDATE，原子）。
	Review(ctx context.Context, req *commentdto.ReviewReq) (res *commentdto.ReviewResp, err error)
	// EntityTypeLabels 已注册的实体类型与展示名（后台筛选下拉用；顺序即注册顺序）。
	EntityTypeLabels(tr func(key, fallback string) string) []commentdto.EntityType
	// IsRegisteredEntityType 实体类型是否已注册（后台筛选参数校验用，fail-closed）。
	IsRegisteredEntityType(entityType string) bool

	// FacingText 见 FacingTexter。
	FacingText(lang string, err error) string
}
