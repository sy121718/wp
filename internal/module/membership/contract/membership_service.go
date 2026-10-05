// Package membershipcontract 定义 membership 模块（BIZ-3 会员等级 + 权益）对外契约。
//
// 本模块**不是** user 的一部分：AGENTS.md 的命名约束把 admin / user 定义为两个独立领域，
// 且等级要被 order（折扣）、cart（运费）、runtimefragment（展示）消费 ——
// 塞进 user 会让片段层拿到含注册 / 改密 / 踢设备的完整 UserService。
//
// 契约按消费方**切成互不相同的形状**（不是一个大接口再让各方自取）：
//
//	Reader               —— 读一条会员身份（片段 / 客户页 / 结算都要它，只读，无写能力）
//	Assigner             —— 改一条归属（后台手工指定 / 取消锁定）
//	AssignmentAdminPort  —— 归属的列表与计数（后台展示）
//	TierAdminPort        —— 等级与权益的 CRUD（后台配置）
//	RecalcPort           —— 归属重算的口子（装配期注入数据源 + 启动日结 + 手工触发）
//
// 这么切的判据是**能力泄漏面**：只读消费方（runtimefragment）拿到的接口里没有任何一个
// 写方法，它就无法「顺手」改会员等级；而等级 CRUD 与归属 CRUD 分开，
// 是因为它们的权限点与审计口径不同（改门槛是全站生效，改某人的等级只影响一个人）。
package membershipcontract

import (
	"context"

	membershipdto "go_wp/internal/module/membership/dto"
)

// Reader 读一条会员身份：生效等级 + 它的权益，展开成消费侧直接可用的形状。
//
// 这是**唯一**该被访问面（片段）与客户页消费的接口：它给不出任何写能力，
// 也不暴露除 user_id 之外的账号事实。
type Reader interface {
	// Resolve 解析某访客在某工程的会员身份。
	//
	// 读路径的三条语义（service 侧实现，消费方按它们推理）：
	//   · 该访客**没有归属行** → 内存里兜底到本工程的默认等级，IsDefaultTier = true；
	//   · 该工程**没有默认等级** → 返回 ErrDefaultTierMissing（错误里带 project_id），
	//     **不**静默归到 sort_order 最小的那一档（它可能是运营已停用的）；
	//   · 本方法**绝不写库** —— 不在读路径上顺手建归属行（AGENTS.md 不变量 1：
	//     访问面写库只有 analytics 打点与访问面守卫读会话两个明文例外）。
	Resolve(ctx context.Context, req *membershipdto.ResolveReq) (res *membershipdto.MembershipResp, err error)
}

// Assigner 改动会员归属（后台手工面）。
type Assigner interface {
	// AssignManual 手工指定某访客在某工程的等级。
	//
	// 两条语义由数据库写入自身承载，不靠调用方记得：
	//   · 覆盖当前归属（含覆盖上一条手工指定的）；
	//   · 把 source 置 manual —— 此后日结自动重算（AssignAuto 的原子 SQL 带
	//     `WHERE source <> 'manual'`）不会动它。
	AssignManual(ctx context.Context, req *membershipdto.AssignManualReq) (res *membershipdto.AssignmentResp, err error)
	// UnlockManual 取消手工锁定，把归属交还自动重算。
	//
	// 只改 source、不动 tier_id：解锁本身不该造成一次可见的等级跳变。
	// 该归属不是 manual 时返回 ErrManualNotLocked（不是 ErrNotFound ——
	// 「已经解锁过了」与「这里本来就没有归属」是两件事）。
	UnlockManual(ctx context.Context, req *membershipdto.UnlockManualReq) (err error)
}

// AssignmentAdminPort 归属的读侧（后台列表）。
type AssignmentAdminPort interface {
	// ListAssignments 列出某工程的归属（分页 + 可选按等级 / 来源 / 访客筛）。
	ListAssignments(ctx context.Context, req *membershipdto.ListAssignmentsReq) (list []*membershipdto.AssignmentResp, err error)
	// CountAssignments 与 ListAssignments 同一组筛选条件下的总数（分页条先要它）。
	CountAssignments(ctx context.Context, req *membershipdto.CountAssignmentsReq) (total int64, err error)
}

// TierAdminPort 等级与权益的配置面。
type TierAdminPort interface {
	// ListTiers 列出某工程的等级（按 sort_order 降序，含各自权益）。
	ListTiers(ctx context.Context, req *membershipdto.ListTiersReq) (list []*membershipdto.TierResp, err error)
	// GetTier 取单个等级详情（含权益）。
	GetTier(ctx context.Context, req *membershipdto.GetTierReq) (res *membershipdto.TierResp, err error)
	// CreateTier 新建等级（可一次带上权益）。
	//
	// 三处唯一键冲突各自返回可辨识的业务错误：等级名 → ErrTierNameTaken、
	// 非默认档门槛 → ErrThresholdTaken、默认等级 → ErrDefaultTierExists。
	// 一律打回给人，不自动加后缀、不静默合并。
	CreateTier(ctx context.Context, req *membershipdto.CreateTierReq) (res *membershipdto.TierResp, err error)
	// UpdateTier 更新等级（指针字段 = 只在非 nil 时改）。
	UpdateTier(ctx context.Context, req *membershipdto.UpdateTierReq) (res *membershipdto.TierResp, err error)
	// DeleteTier 删除等级（软删）。
	//
	// 仍挂着会员归属时被拒（ErrTierInUse，错误里带归属条数）——
	// 静默删除会让那些访客在下一次解析时突然回落到默认等级，而页面上看不出发生过什么。
	DeleteTier(ctx context.Context, req *membershipdto.DeleteTierReq) (err error)
	// SaveEntitlements 全量保存某等级的权益（清单即最终状态，没列出的 kind 会被删掉）。
	SaveEntitlements(ctx context.Context, req *membershipdto.SaveEntitlementsReq) (res *membershipdto.SaveEntitlementsResp, err error)
}

// PurchaseSource 消费额批量只读端口（**订单侧实现**；装配期注入）。
//
// 为什么是端口而不是直接读表：membership 模块读不到 users 表，也不该读 orders 表
// （表隔离 + 消费额口径属于订单域）。「谁是本工程的会员候选」这个问题只有订单侧能回答。
//
// 未注入时：归属重算**整体不可用**（调度器禁用 + RecalcProject 返回 ErrProjectRequired
// 之外的显式失败），而不是「扫到 0 个人」——后者看起来像「大家都没消费」，
// 而这恰恰是要靠 Scanned 计数分辨的那一类静默故障。
type PurchaseSource interface {
	// SpentTotalsByUser 返回本工程「有可计入消费」的用户 → 消费总额（**分**）。
	//
	// 口径（哪些订单状态计入、是否减去退款）由订单侧定义并负责 —— 它才是那个知道
	// 「什么算消费」的领域。membership 侧只做「消费额 ≥ 门槛」的分档。
	SpentTotalsByUser(ctx context.Context, projectID string) (totals map[uint64]int64, err error)
}

// RecalcPort 归属重算的口子。
type RecalcPort interface {
	// SetPurchaseSource 注入消费额批量只读端口（装配期在订单侧就绪后调用一次）。
	SetPurchaseSource(port PurchaseSource)
	// StartRecalcScheduler 启动日结重算（幂等；测试进程或端口未注入时空操作）。
	StartRecalcScheduler()
	// RecalcProject 立即重算某工程的归属（后台「立即重算」与排障用）。
	RecalcProject(ctx context.Context, req *membershipdto.RecalcProjectReq) (res *membershipdto.RecalcResult, err error)
}

// FacingTexter 把本模块的业务错误转成「指定语言下可直接展示的一句话」。
//
// 为什么契约里要有它：消费方（结算页 / 客户页 / 工作台）只依赖 contract 与不可变 dto，
// 拿不到本模块的 enums 白名单。没有这个出口，消费方只剩两条路 ——
// 直出 err.Error()（把 PostgreSQL 原文漏出去）或一律通用提示
// （把「这个工程还没配默认等级」这类可行动差异吞掉）；两条在本仓都踩过。
//
// 实现必须与 inbound/http 的出口同源（命中白名单 → 业务文案；未命中 → 归口文案，原文只进日志）。
type FacingTexter interface {
	FacingText(lang string, err error) string
}

// MembershipService 会员等级与权益的完整对外契约。
//
// 消费方**不要**直接依赖它 —— 按需要取上面那几个收窄接口之一
// （装配层可以用本接口做一次断言，确认实现完整）。
type MembershipService interface {
	Reader
	Assigner
	AssignmentAdminPort
	TierAdminPort
	RecalcPort
}

// QueryReader 会员等级与归属的只读视图：给 AI 工具的窄门。
//
// 为什么不直接用 MembershipService：那个接口上有等级增删改、权益保存、
// 归属重算 —— 工具由模型驱动，给它写能力意味着「AI 顺手改了一个等级的升级门槛」
// 在某次无关改动里变得可能，而门槛一改会牵动后续所有人的升降级。
//
// 两个方法对应两类用户提问：
//   - ListTiers：「我们有几个会员等级」「金卡要花多少钱」；
//   - ListAssignments：「这个等级里有多少人」「某人是不是会员」。
type QueryReader interface {
	// ListTiers 按工程列等级（含门槛金额与权益），按高低排序。
	ListTiers(ctx context.Context, req *membershipdto.ListTiersReq) (list []*membershipdto.TierResp, err error)
	// ListAssignments 按工程 / 等级 / 来源 / 客户列归属记录。
	ListAssignments(ctx context.Context, req *membershipdto.ListAssignmentsReq) (list []*membershipdto.AssignmentResp, err error)
}
