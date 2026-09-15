package retention

// catalog.go — 全库增长型表的生命周期声明（审计 IDX-019）。
//
// IDX-019 的问题不是「少写了几个清理任务」，而是**没有任何地方承诺过保留期**：
// 哪张表留多久、为什么留这么久、谁负责清，全散在各自模块的注释里，
// 新增一张增长表时没有任何东西提醒作者「你也得声明一个策略」。
//
// 这份目录把那句话变成代码里的可查清单：每张增长型表一条声明，
// 要么给出保留期与清理方式，要么显式写「不清理」并给出理由。
// 纯声明、不执行 —— 真正的执行仍在各模块的 retention.Task 里（表访问权留在模块内部）。
// 两者的一致性由一个测试钉住：声明为 delete 的表必须能在目录里描述清楚谁在清。

import (
	"time"
)

// CleanupKind 清理方式。
type CleanupKind string

const (
	// CleanupDelete 按时间列直接删除（分批）。
	CleanupDelete CleanupKind = "delete"
	// CleanupNone 不清理：要么是业务数据（不属于可回收的运行痕迹），
	// 要么涉及合规留档 —— 后者改保留期需要业务确认，不能由工程侧自行决定。
	CleanupNone CleanupKind = "none"
	// CleanupOrphan 按**引用关系**清理孤儿行，没有时间保留期（审计 I18N-024）。
	//
	// 与 CleanupDelete 的区别是判据：这里「该不该删」不由时间决定，而由「还有没有
	// 源数据引用它」决定（按时间删会删掉仍在用的行）。因此这类条目 Retain 必须为 0，
	// 且执行者必须是**显式触发**的入口（运维命令 / 后台操作），不是每日任务。
	CleanupOrphan CleanupKind = "orphan"
)

// Declaration 一张表的生命周期声明。
type Declaration struct {
	// Table 表名。
	Table string
	// TimeColumn 清理依据的时间列（不清理时可为空）。
	TimeColumn string
	// Retain 保留期；0 表示不清理（此时 Kind 必须是 CleanupNone）。
	Retain time.Duration
	// Executor 谁在清（人读的描述：哪个模块、什么节奏）。
	Executor string
	// Kind 清理方式。
	Kind CleanupKind
	// Note 策略与理由 —— 不清理的条目必须写清为什么。
	Note string
}

// 保留期的天级常量集中在这里，避免「同一个数字在不同模块各写一遍」。
const (
	RetainPageRevisionDays = 90
	RetainArtifactDays     = 30
	RetainMailDays         = 180
)

// declarations 全库增长型表的生命周期声明。
//
// 顺序与审计 IDX-019 的清单一致（按预计到达问题规模的时间排序），
// 便于与审计条目逐条对照。
var declarations = []Declaration{
	{
		Table: "page_views", TimeColumn: "viewed_at",
		Retain:   0, // 工程可配：analytics_retention_days（默认 90）
		Executor: "analytics 每日任务（PurgeExpiredViews）",
		Kind:     CleanupDelete,
		Note:     "**已按月分区**（迁移 173，按 viewed_at）：时间窗查询走分区裁剪，过期数据可整块 DETACH 归档；保留期**按工程可配**（projects.analytics_retention_days，默认 90 天，0=不清理），因此本行不给固定天数。这是全库唯一由访客浏览器写入的表",
	},
	{
		Table: "inventory_stock_movements", TimeColumn: "create_time",
		Retain:   0,
		Executor: "（未接入）",
		Kind:     CleanupNone,
		Note:     "**已按月分区**（迁移 173）：将来定下保留期后，清理方式是整块 DETACH 归档而不是逐行删。库存流水是账实相符的依据，属于合规留档 —— 保留期需要业务确认（通常数年），工程侧不应单方面决定删除，因此显式声明不清理而不是留空",
	},
	{
		Table: "mail_campaign_events", TimeColumn: "create_time",
		Retain:   RetainMailDays * 24 * time.Hour,
		Executor: "mail 每日任务（PurgeRetention，先固化汇总再删）",
		Kind:     CleanupDelete,
		Note:     "删除前把「全部事件已过期」的活动的打开/点击数固化到 mail_campaigns（open_count / click_count），报表因此不会因清理而变空",
	},
	{
		Table: "mail_logs", TimeColumn: "create_time",
		Retain:   RetainMailDays * 24 * time.Hour,
		Executor: "mail 每日任务（PurgeRetention）",
		Kind:     CleanupDelete,
		Note:     "逐封发送留档；排障窗口远小于保留期",
	},
	{
		Table: "master_data_changes", TimeColumn: "create_time",
		Retain:   0,
		Executor: "（未接入）",
		Kind:     CleanupNone,
		Note:     "**已按月分区**（迁移 173，append-only 触发器建在父表上并自动下沉到分区）；字段级审计（迁移 111 的触发器在库层禁止 UPDATE/DELETE）；属于合规留档，保留期需业务确认",
	},
	{
		Table: "page_revisions", TimeColumn: "create_time",
		Retain:   RetainPageRevisionDays * 24 * time.Hour,
		Executor: "page 保存草稿时即时收敛 + 每日任务兜底",
		Kind:     CleanupDelete,
		Note:     "两个条件同时满足才删（超出每页 20 份 **且** 早于 90 天）：只按条数删会把刚存的版本删掉，编辑者最不能接受这一种",
	},
	{
		Table: "page_artifacts", TimeColumn: "create_time",
		Retain:   RetainArtifactDays * 24 * time.Hour,
		Executor: "page 每日任务（PurgeRetention → GarbageCollectArtifacts）",
		Kind:     CleanupDelete,
		Note:     "保护集合（页面指针 / 每语言激活 / 路由指向）内的产物一律不回收；同 hash 仍被其他行引用时只标 gc_pending 不删文件；同一趟里清掉孤儿内容对象（IDX-016）",
	},
	{
		Table: "artifacts 磁盘目录", TimeColumn: "（随产物行）",
		Retain:   RetainArtifactDays * 24 * time.Hour,
		Executor: "page 每日任务（与产物行同一趟）",
		Kind:     CleanupDelete,
		Note:     "内容寻址目录 artifacts/<hash>/；删除失败只记日志，孤儿目录由反向对账（IDX-015）暴露",
	},
	{
		Table: "order_status_logs", TimeColumn: "create_time",
		Retain:   0,
		Executor: "（无需清理）",
		Kind:     CleanupNone,
		Note:     "订单流转链是订单详情的一部分，而订单本身不删除 —— 日志因此随订单存续；将来若支持订单归档，应与归档同步搬运而不是单独清理",
	},
	{
		// 审计 I18N-024 的落地（2026-09）：孤儿清理已接入 —— 判据与入口见下。
		// TimeColumn 留空：这里没有时间依据，「多久没更新」不是孤儿判据，
		// 按时间删会把仍在用的译文一起删掉（原文没改、只是暂时没重新保存过）。
		Table: "sys_translation", TimeColumn: "",
		Retain:   0,
		Executor: "i18n 孤儿清理命令（cmd/i18n-orphan，显式触发；默认 dry run，-apply 才删）",
		Kind:     CleanupOrphan,
		Note:     "内容寻址译文（project_id, source_hash, context, lang，迁移 195）：改原文后旧译文成为孤儿。判据是**引用关系**而不是时间 —— (A) hash 与原文不符（永远不可能被命中）、(B) 该工程源文档已不再引用这个 (hash, context)，两条判据与清理入口见 pkg/i18n/content_orphan.go。**不做自动清理**：源数据可能只是暂时缺失，自动删会丢掉人工翻译；只清工程级行，全局行（project_id IS NULL）保留，且输出「检出 N 行、清理 M 行」",
	},

	{
		Table: "mail_automation_node_logs", TimeColumn: "create_time",
		Retain:   RetainMailDays * 24 * time.Hour,
		Executor: "mail 每日任务（PurgeRetention）",
		Kind:     CleanupDelete,
		Note:     "自动化流程逐节点一行（启用后增长最快）；定位是排障视图而非常年留档",
	},
	{
		Table: "product_ratings", TimeColumn: "create_time",
		Retain:   0,
		Executor: "（不清理）",
		Kind:     CleanupNone,
		Note:     "评分明细是**业务数据**（用户产生的内容），不是运行痕迹 —— 评分会进集合排序与最低评分筛选，按时间清理等于悄悄改变前台呈现",
	},

	// 汇总表本身不清理：每天一行 × 路径数，体量比明细小几个数量级，
	// 且明细被清理后它的数字仍要留给历史报表。
	{
		Table: "page_views_daily", TimeColumn: "day",
		Retain:   0,
		Executor: "（不清理）",
		Kind:     CleanupNone,
		Note:     "按天预聚合（DB-005 / IDX-010）：历史窗口的统计读数来源，删了报表就没了；体量可控（天数 × 路径数）",
	},
}

// Declarations 返回全部生命周期声明（只读拷贝）。
func Declarations() []Declaration {
	out := make([]Declaration, len(declarations))
	copy(out, declarations)
	return out
}

// DeclarationOf 按表名查声明。
func DeclarationOf(table string) (Declaration, bool) {
	for _, d := range declarations {
		if d.Table == table {
			return d, true
		}
	}
	return Declaration{}, false
}
