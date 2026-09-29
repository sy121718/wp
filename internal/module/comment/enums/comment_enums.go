// Package commentenums 评论模块的枚举取值、上限常量与全部响应文案。
//
// # 模块口径（产品默认值，不是技术限制）
//
// 下面四条是**当前产品口径**，写在代码里便于将来改（改的时候要连同注释一起改，
// 而不是让读者去猜「为什么不能匿名评论」）：
//
//  1. **提交需要登录**：匿名不可评。身份来自 user 模块的访客会话（片段层由
//     VisitorIdentityMiddleware 解析后经 Request.UserID 传入）。理由：先审后发虽然能挡住
//     内容风险，但挡不住「同一脚本无限灌库」—— 账号是刷量的成本门槛。
//  2. **先审后发**：新评论一律落 pending，只有 approved 出现在公开列表里。
//     改成立即显示只需把 service 的初始状态常量改成 approved，其余不变。
//  3. **只支持一级回复**：parent_id 自引用，回复的回复由 service 归一到顶层评论之下
//     （见 comment_submit.go 的 normalizeParent）。不做多级嵌套 —— 嵌套深度是展示层的复杂度。
//  4. **列表匿名可读**：未登录也能看已通过的评论（列表片段是 anonymous 能力）。
//
// # 与实体类型的关系
//
// 本包**不定义实体类型白名单**：合法的 entity_type 由**拥有该实体的模块**声明
// （article 来自 content 模块、product 来自 product 模块），装配期经
// contract.EntityType 注册进 service 实例。本包只定义「实体类型」这个概念的承载结构
// 与它的形状约束（长度上限），不抄一份取值表 —— 两份真源必然漂移
// （AGENTS.md §数据库：白名单由拥有该表的实体声明；同型先例 pkg/datarule）。
//
// # 错误文案三件套
//
//   - ① 白名单：CommentFacingMessages（与下面的 Err* 常量一一对应，AST 对账测试钉住）；
//   - ② 归口文案：ErrInternal（未命中时返回的可翻译 key）；
//   - ③ 结构化日志：原文只进日志（见 inbound/http 的 comment_err.go）。
package commentenums

import "strings"

// —— 审核状态（与迁移 466 的 ck_comments_status 一一对应）——

const (
	// StatusPending 待审核：新评论的落库默认值，不进任何公开面。
	StatusPending = "pending"
	// StatusApproved 已通过：**唯一**会出现在公开列表里的状态。
	StatusApproved = "approved"
	// StatusRejected 已驳回：运营判定不合适，保留记录以便复查与申诉。
	StatusRejected = "rejected"
	// StatusSpam 垃圾：机器或人工判定的广告 / 灌水。
	//
	// 与 rejected 分开而不是合并：两者的处置动作不同（垃圾可以批量清理，
	// 驳回要保留给人看），合并后就没有「这批是垃圾」这个可操作的筛选条件了。
	StatusSpam = "spam"
)

// Statuses 状态白名单（后台筛选下拉的顺序即此顺序：待审在最前 —— 它是日常要清的空队列）。
func Statuses() []string {
	return []string{StatusPending, StatusApproved, StatusRejected, StatusSpam}
}

// IsValidStatus 状态是否在白名单内（fail-closed：未知状态一律拒绝）。
func IsValidStatus(status string) bool {
	switch status {
	case StatusPending, StatusApproved, StatusRejected, StatusSpam:
		return true
	default:
		return false
	}
}

// ReviewableStatuses 可以被审核动作设置的状态（审核动作只能设这两个）。
//
// pending 不在其中：审核动作是「做出判断」，把一条评论「改成待审」不是判断结果
// （回退到待审是另一个语义，本批不做）。spam 也不在其中：批量动作只提供
// 通过 / 驳回两个按钮，标垃圾是单条的精修动作，将来需要时再加一条权限点与入口。
func ReviewableStatuses() []string {
	return []string{StatusApproved, StatusRejected}
}

// IsReviewableStatus 状态是否可被审核动作设置。
func IsReviewableStatus(status string) bool {
	switch status {
	case StatusApproved, StatusRejected:
		return true
	default:
		return false
	}
}

// —— 形状与长度上限（服务端校验的真源；客户端传什么都不信任）——

const (
	// MaxBodyLen 评论正文长度上限（字符数，不是字节数）。
	//
	// 与迁移 466 的 ck_comments_body_len **必须一致**：两处不一致时的表现是
	// 「service 放行、数据库报错」，而那条报错会被归口成「系统内部错误」——
	// 用户看到的是「评论发不出去」，无从知道是长度问题。
	MaxBodyLen = 2000
	// MinBodyLen 正文下限（trim 之后必须至少这么长；空评论没有意义）。
	MinBodyLen = 1
	// MaxEntityTypeLen 实体类型标识的长度上限（与迁移 466 的 varchar(40) 一致）。
	MaxEntityTypeLen = 40
	// MaxEntityIDLen 实体 id 的长度上限（与迁移 466 的 varchar(64) 一致）。
	//
	// 取值依据：uuid 是 36 字符；将来若出现雪花 id / 复合键也在这个量级内。
	MaxEntityIDLen = 64
	// MaxKeywordLen 后台关键词筛选的长度上限（超长一律截断为不匹配，而不是查库）。
	MaxKeywordLen = 64
	// MaxAuthorIPHashLen 来源 IP 哈希列长度（sha256 hex = 64）。
	MaxAuthorIPHashLen = 64
)

// —— 分页 ——

const (
	// DefaultPageSize 公开列表与后台列表的默认每页条数。
	DefaultPageSize = 20
	// MaxPageSize 单页上限（超过一律压到这个值：分页参数来自请求方，
	// 不设上限等于把「一次查多少行」交给请求方决定）。
	MaxPageSize = 50
	// MaxReviewIDs 单次批量审核的 id 数量上限。
	//
	// 与 web/shell 的 MaxBulkIDs（200）同值：那边的额度是**页面**的读取上限
	// （表单多选后提交的规模），这边的额度是**契约**的防御上限（任何调用点都受它约束）。
	// service 不 import web/shell（依赖方向：模块不依赖 web 层），所以这里独立声明一份，
	// 两处不一致时的表现是「页面上能勾 200 条，提交后报参数不合法」—— 真出现时改这里对齐。
	MaxReviewIDs = 200
)

// —— API 响应文案（i18n key，形态 `模块.类别.语义`）——
//
// 值是 i18n key，文案落在 sys_i18n（迁移 466a）；响应层 pkg/response.translate 按请求语言
// 查表，未命中时**原样返回 key**（可见的降级，不是空白）。
//
// 为什么必须是 key 形态：错误出口统一到 response.ErrorAuto，它的判据 IsBusinessError
// 只认两种**形态确定**的模式 —— key 形态与 enums 常量名形态（`ErrXxx`）。
// 值写成中文原文会让业务错误被判成内部错误、对外统一 500（同 masterdata 的 CQ-010 教训）。
const (
	// ErrInvalidParam 参数不合法（形状 / 长度 / 枚举都不对）。
	ErrInvalidParam = "comment.err.invalidParam"
	// ErrProjectRequired 未指定工程（评论按工程隔离，没有工程就没有可查的范围）。
	ErrProjectRequired = "comment.err.projectRequired"
	// ErrEntityTypeUnknown 实体类型不在白名单内。
	//
	// 与 ErrInvalidParam 分开的理由：它是**装配 / 页面配置**问题（页面用了没注册的类型），
	// 消费方据此要做的事与「参数填错了」不同 —— 合并后运维只能收到一句「参数不合法」。
	ErrEntityTypeUnknown = "comment.err.entityTypeUnknown"
	// ErrEntityIDInvalid 实体 id 缺失或形状非法。
	ErrEntityIDInvalid = "comment.err.entityIDInvalid"
	// ErrBodyRequired 评论内容为空（trim 之后）。
	ErrBodyRequired = "comment.err.bodyRequired"
	// ErrBodyTooLong 评论内容超过 MaxBodyLen。
	ErrBodyTooLong = "comment.err.bodyTooLong"
	// ErrRateLimited 触发限流（同身份或同来源的提交频率超限）。
	ErrRateLimited = "comment.err.rateLimited"
	// ErrNotAllowed 被实体的差异化规则拒绝（如「商品评论必须买过」）。
	//
	// 由**拥有该实体的模块**经 contract.EntityPolicy 端口判定，comment 模块不认识业务细节。
	ErrNotAllowed = "comment.err.notAllowed"
	// ErrLoginRequired 未登录（提交需要登录身份）。
	ErrLoginRequired = "comment.err.loginRequired"
	// ErrCSRFRequired 缺少或校验不通过的访客 CSRF token。
	ErrCSRFRequired = "comment.err.csrfRequired"
	// ErrUnavailable 评论能力未接入（端口未装配）。
	ErrUnavailable = "comment.err.unavailable"
	// ErrNothingSelected 批量审核没有选中任何评论。
	ErrNothingSelected = "comment.err.nothingSelected"
	// ErrParentInvalid 回复目标不合法（不存在 / 不属于同一实体）。
	ErrParentInvalid = "comment.err.parentInvalid"
	// ErrInternal 未归类的内部错误（SQL / 表名 / 约束名等）对外归口文案。
	//
	// 值带模块前缀（点分）：裸 "ErrInternal" 已被 admin 批占用（迁移 268）。
	ErrInternal = "comment.err.internal"
)

// 成功回执（同样是 i18n key）。
const (
	// MsgSubmitPending 提交成功、待审核（先审后发口径下的「成功」就是这句）。
	MsgSubmitPending = "comment.msg.submitPending"
	// MsgReviewApproved 批量审核：通过。
	MsgReviewApproved = "comment.msg.reviewApproved"
	// MsgReviewRejected 批量审核：驳回。
	MsgReviewRejected = "comment.msg.reviewRejected"
)

// CommentFacingMessages 可以原样展示给前端的评论业务文案（**白名单**）。
//
// 命中 → 原样透出（前端据此提示「这个类型的评论不支持」「评论太长了」「发太快了」）；
// 未命中 → inbound/http 的 commentErrText 记结构化日志并返回 ErrInternal。
// 方向是安全的：漏写一条只会让前端看到一句通用提示（一眼可见），
// 而不会把 service 上抛的 PostgreSQL 原文（表名 / 约束名 / SQLSTATE）透出去。
//
// ErrInternal 本身不进白名单：它是未命中时的返回值，不是业务文案。
// Msg* 也不进：它们是成功回执，永远不会作为错误响应返回（对账测试里逐条写明理由）。
var CommentFacingMessages = []string{
	ErrInvalidParam, ErrProjectRequired, ErrEntityTypeUnknown, ErrEntityIDInvalid,
	ErrBodyRequired, ErrBodyTooLong, ErrRateLimited, ErrNotAllowed, ErrLoginRequired,
	ErrCSRFRequired, ErrUnavailable, ErrNothingSelected, ErrParentInvalid,
}

// HitFacingMessage 判断一条错误文案是否命中白名单（整串相等）。
//
// 只做**整串相等**判定，不像 membership 那样支持 `key|param` / `key：定位` 两种附加形态：
// 评论的业务错误不带定位信息（哪条评论被拒是日志的事，页面上不需要逐条回带）。
func HitFacingMessage(msg string) (string, bool) {
	msg = strings.TrimSpace(msg)
	for _, m := range CommentFacingMessages {
		if msg == m {
			return msg, true
		}
	}
	return "", false
}

// LabelPair 一组展示名取值：i18n key + 中文兜底。
//
// 形态是**两个值一起给**（同 masterdata / user enums 的既有形态）：只给中文 → 英文界面恒中文；
// 只给 key → 词条缺失时页面显示裸 key；两个一起给，调用点 `tr(key, fallback)` 命中出译文、
// 未命中出中文兜底。enums 零依赖（只 import strings），页面层与 service 层都能 import。
type LabelPair struct {
	Key      string
	Fallback string
}

// —— 状态展示名（后台审核页）——

const (
	LabelKeyStatusPending  = "admin.comment.status.pending"
	LabelKeyStatusApproved = "admin.comment.status.approved"
	LabelKeyStatusRejected = "admin.comment.status.rejected"
	LabelKeyStatusSpam     = "admin.comment.status.spam"

	LabelStatusPending  = "待审核"
	LabelStatusApproved = "已通过"
	LabelStatusRejected = "已驳回"
	LabelStatusSpam     = "垃圾"
)

// StatusLabel 状态 → (i18n key, 中文兜底)；未知状态返回空 key + 原值。
//
// 未知状态原样回显而不是显示「未知」：状态是**数据**，看到陌生的英文值比看到一句
// 抹掉信息的「未知」更容易定位（同 masterdata.EntityTypeLabel 的取舍）。
func StatusLabel(status string) LabelPair {
	switch status {
	case StatusPending:
		return LabelPair{LabelKeyStatusPending, LabelStatusPending}
	case StatusApproved:
		return LabelPair{LabelKeyStatusApproved, LabelStatusApproved}
	case StatusRejected:
		return LabelPair{LabelKeyStatusRejected, LabelStatusRejected}
	case StatusSpam:
		return LabelPair{LabelKeyStatusSpam, LabelStatusSpam}
	default:
		return LabelPair{"", status}
	}
}

// —— 后台审核页的固定词条（页面 / 模板用 tr(key, fallback) 取）——

const (
	// MsgPageTitle 页面标题与菜单标题共用一条词条（同一个东西不该在两个地方各写一份）。
	MsgPageTitle = "admin.comment.menu"

	LabelKeyColBody     = "admin.comment.col.body"
	LabelKeyColEntity   = "admin.comment.col.entity"
	LabelKeyColEntityID = "admin.comment.col.entity_id"
	LabelKeyColAuthor   = "admin.comment.col.author"
	LabelKeyColStatus   = "admin.comment.col.status"
	LabelKeyColTime     = "admin.comment.col.time"
	LabelKeyColActions  = "admin.comment.col.actions"

	LabelKeyFilterStatus     = "admin.comment.filter.status"
	LabelKeyFilterEntityType = "admin.comment.filter.entity_type"
	LabelKeyFilterKeyword    = "admin.comment.filter.keyword"
	LabelKeyFilterAll        = "admin.comment.filter.all"
	LabelKeyFilterSearch     = "admin.comment.filter.search"
	LabelKeyFilterReset      = "admin.comment.filter.reset"

	LabelKeyActionApprove = "admin.comment.action.approve"
	LabelKeyActionReject  = "admin.comment.action.reject"

	LabelKeyEmpty     = "admin.comment.empty"
	LabelKeyEmptyHint = "admin.comment.empty_hint"
	LabelKeyHint      = "admin.comment.hint"

	LabelKeyDoneApproved = "admin.comment.done.approved"
	LabelKeyDoneRejected = "admin.comment.done.rejected"

	LabelKeyErrNothingSelected = "admin.comment.err.nothing_selected"
	LabelKeyProjectRequired    = "admin.comment.project_required"
	LabelKeyNoProject          = "admin.comment.no_project"
	LabelKeyLoadFailed         = "admin.comment.load_failed"
)

// PageTitle 审核页标题（key + 中文兜底）。
//
// handler 先取词再把成品文案交给 shell.Prepare —— Prepare 内部是 `t(title, title)`，
// 兜底就是 key 本身，词条缺失时页面标题会显示 `admin.comment.menu` 这样的裸 key，
// 所以这里给出 LabelPair 让调用点用 `tr(key, fallback)`。
var PageTitle = LabelPair{MsgPageTitle, "评论审核"}

// 后台页各固定词条的中文兜底（与库内 zh-CN 值逐字一致）。
//
// 为什么兜底留在代码里：词条缺失时展示的是中文原文，而不是裸 key
// （`admin.comment.col.body` 摆到页面上，运营只会来报「页面坏了」）。
const (
	LabelColBody     = "内容"
	LabelColEntity   = "评论对象"
	LabelColEntityID = "对象 id"
	LabelColAuthor   = "评论人"
	LabelColStatus   = "状态"
	LabelColTime     = "提交时间"
	LabelColActions  = "操作"

	LabelFilterStatus     = "状态"
	LabelFilterEntityType = "实体类型"
	LabelFilterKeyword    = "关键词"
	LabelFilterAll        = "全部"
	LabelFilterSearch     = "筛选"
	LabelFilterReset      = "重置"

	LabelActionApprove = "通过"
	LabelActionReject  = "驳回"

	LabelEmpty     = "没有符合条件的评论。"
	LabelEmptyHint = "换一个筛选条件，或等访客提交新的评论。"
	LabelHint      = "评论按「站点工程 + 实体」隔离；只有「已通过」的评论会出现在公开页面上。"

	// LabelDoneApprovedTemplate / LabelDoneRejectedTemplate 带一个 %d（本次处理的条数）。
	LabelDoneApprovedTemplate = "已通过 %d 条评论。"
	LabelDoneRejectedTemplate = "已驳回 %d 条评论。"

	LabelErrNothingSelected = "没有选择任何评论。"
	LabelProjectRequired    = "请先选择站点工程：评论按工程隔离。"
	LabelNoProject          = "还没有站点工程：评论按工程隔离，先建一个工程再看这一页。"
	LabelLoadFailed         = "评论列表暂时读不出来 —— 稍后重试。"
)

// DoneTemplate 审核批次完成的文案模板（key + 中文兜底，调用点自己 Sprintf 条数）。
func DoneTemplate(approved bool) LabelPair {
	if approved {
		return LabelPair{LabelKeyDoneApproved, LabelDoneApprovedTemplate}
	}
	return LabelPair{LabelKeyDoneRejected, LabelDoneRejectedTemplate}
}
