package pageenums

import "strconv"

// page_label_enums.go — page 模块的「值 → 文案」映射（internal/module/CLAUDE.md §enums）。
//
// 为什么这些 switch 住在这里而不是控制器：值的域属于本模块的业务（重建阶段、语言行状态、
// 重定向结果码、块内文本来源），它的展示文案就是本模块 enums 的事。放在控制器里的代价不是
// 「不好看」，而是加一门语言要回头改控制器，且同一个值在多处各抄一份映射。
//
// 三个函数的形状统一为 `XxxLabel(value) (key, fallback string)`：i18n key + 中文兜底成对返回，
// 控制器只做一次调用与赋值。**未知值的选择写在各自注释里** —— 不返回空 key + 空兜底那种
// 「没有这条文案」的伪信号（调用点会把空当成「不用提示」，于是未知值被静默吞掉）。

// —— 自动重建失败的阶段（pages.rebuild_failed_stage，迁移 474）——
//
// 取值即 service 写进那一列的值（page/service/page_stale.go 的标记点）。
const (
	// RebuildStagePlan 计划阶段失败：站点语言清单或旧发布范围读不到。
	RebuildStagePlan = "plan"
	// RebuildStageBuild 构建 / 发布阶段失败。
	RebuildStageBuild = "build"
)

// RebuildStageLabel 重建失败阶段 → 文案事实（前缀词条 + 阶段词条由模板组合）。
//
// 未知阶段回落到**原值**而不是吞掉：service 新加了阶段而这里没跟上时，页面上显示 plan2
// 至少能让人去查，一句通用话不能。空串也是未知值 —— 调用点先判「有没有失败痕迹」，
// 走到这里说明有失败时刻但没有阶段（页面上只剩时刻）。
func RebuildStageLabel(stage string) (key, fallback string) {
	switch stage {
	case RebuildStagePlan:
		return "admin.pages.impact.stage_plan", "计划阶段（站点语言清单或旧发布范围读不到）"
	case RebuildStageBuild:
		return "admin.pages.impact.stage_build", "构建 / 发布阶段"
	default:
		return "", stage
	}
}

// —— 页面语言行的三档状态（page-langs 面板）——

// 状态取值：值域封闭，由两个事实（是否被本页排除、是否有已激活产物）派生。
const (
	LangStatusExcluded  = "excluded"
	LangStatusPublished = "published"
	LangStatusDraft     = "draft"
)

// LangStatusOf 由行上的两个事实派生状态取值（优先级：已排除 > 已发布 > 未发布）。
//
// 优先级就是语义：被排除的语言即使还留着旧产物也不该显示「已发布」——
// 那会让人以为线上仍会产出它（排除的同时会下线产物，两者不是同一时刻的状态）。
func LangStatusOf(excluded, published bool) string {
	switch {
	case excluded:
		return LangStatusExcluded
	case published:
		return LangStatusPublished
	default:
		return LangStatusDraft
	}
}

// LangStatusLabel 语言行状态 → 文案事实。
//
// 未知状态回落到「未发布」那一档：三档里它是唯一不诱导操作的说法 ——
// 「已发布」会让人以为线上已有产物、「已排除」会让人以为不必处理，两者都会让人少做一步。
func LangStatusLabel(status string) (key, fallback string) {
	switch status {
	case LangStatusExcluded:
		return "admin.page.langs.status.excluded", "已排除（不产出）"
	case LangStatusPublished:
		return "admin.page.langs.status.published", "已发布"
	default:
		return "admin.page.langs.status.draft", "未发布"
	}
}

// —— 重定向写动作的结果码（`admin.redirect.ok.*`）——

// 结果取值：调用点只传本包的常量（控制器里不再出现字符串字面量）。
const (
	RedirectResultCreated = "created"
	RedirectResultDeleted = "deleted"
	RedirectResultMerged  = "merged"
)

// RedirectResultLabel 重定向写动作的结果码 → 成功回执。
//
// 未知码回落到**原值**：本包的常量是唯一取值来源，未知值不可达（防御性分支）；
// 真出现时页面上显示原值，能直接指认是哪个调用点传错了，而不是一句「操作完成」。
func RedirectResultLabel(code string) (key, fallback string) {
	switch code {
	case RedirectResultCreated:
		return "admin.redirect.ok.created", "重定向已新增。"
	case RedirectResultDeleted:
		return "admin.redirect.ok.deleted", "重定向已删除。"
	case RedirectResultMerged:
		return "admin.redirect.ok.merged", "重定向链已合并为直达。"
	default:
		return "", code
	}
}

// —— 块内文本的来源标签（翻译工作台）——
//
// 标签值是**事实**（页面文档里这段文字来自哪个槽位），取值即模板
// internal/templates/admin/page/page_translations.html 里 `admin.page_translations.origin.{…}`
// 的词条后缀；展示 key 与中文兜底在模板里（`{{tr}}` 取词）—— 值域与词条归属在本包，
// 取词时机（请求语言）在渲染期。
const (
	// TranslationOriginHeader 页眉槽位绑定的块。
	TranslationOriginHeader = "header"
	// TranslationOriginFooter 页脚槽位绑定的块。
	TranslationOriginFooter = "footer"
	// TranslationOriginBlock 其它（正文引用 / 未知槽位）—— 即「全局块」。
	TranslationOriginBlock = "block"
)

// 槽位键：与 builder.SlotHeader / builder.SlotFooter 同值。本包不 import builder
// （enums 是纯文案的叶子包，不该把构建内核拖进每一个引用它的模块），改成在这里
// 声明一份**由测试对账**的镜像 —— 见 page_label_enums_test.go 的
// TestSlotTokensMirrorBuilder：builder 改了槽位键而这里没跟上时，那条测试会变红。
const (
	slotKeyHeader = "header"
	slotKeyFooter = "footer"
)

// TranslationOriginOfSlot 槽位键 → 块内文本来源标签。
//
// 只认页眉 / 页脚两个槽位，其余（含未知槽位）一律「全局块」—— 与迁入前控制器里那个
// switch 逐字同判据：未知槽位不是错误，只是没有更细的来源可标。
func TranslationOriginOfSlot(slot string) string {
	switch slot {
	case slotKeyHeader:
		return TranslationOriginHeader
	case slotKeyFooter:
		return TranslationOriginFooter
	default:
		return TranslationOriginBlock
	}
}

// —— 批量删除的结论文案（按计数选整句）——
//
// 形状比 `(key, fallback)` 多一个 `args`：候选句**本身是常量**（见下方常量组），
// 控制器要选的只是「哪一条 + 带哪几个计数」。这类「按计数选整句」的判据属于值的域
// （「成功几个 / 跳过几个」是批量操作的结果语义），因此也住在本包，控制器只调用一行 ——
// 留在控制器里就是「挑文案的 switch」，加一门语言要回头改控制器。
type Outcome struct {
	// Key 词条 key（sys_i18n 的 item_key）。
	Key string
	// Fallback 中文兜底（词条缺失时原样输出，绝不报错）。
	Fallback string
	// Args 已 strconv 的占位符实参（模板 `%s` 按顺序填）。
	Args []string
}

// PagesBulkDeleteOutcome 页面批量删除的结论：成功几个、跳过几个都要说清楚 ——
// 只报「操作完成」会把部分成功静默成全部成功，用户不会再去看剩下那几个。
func PagesBulkDeleteOutcome(deleted, skipped int) Outcome {
	switch {
	case deleted == 0 && skipped == 0:
		return Outcome{Key: BulkPageNoneSelected, Fallback: "没有勾选任何页面，列表未改动。"}
	case skipped == 0:
		return Outcome{Key: BulkPageAllDeleted, Fallback: "已删除 %s 个页面。", Args: []string{itoa(deleted)}}
	case deleted == 0:
		return Outcome{Key: BulkPageAllSkipped, Fallback: "%s 个页面都未能删除，列表未改动。", Args: []string{itoa(skipped)}}
	default:
		return Outcome{Key: BulkPagePartial, Fallback: "已删除 %s 个，%s 个未能删除（可能已被删除或路径清理失败）。",
			Args: []string{itoa(deleted), itoa(skipped)}}
	}
}

// RedirectBulkDeleteOutcome 重定向批量删除的结论（判据同上，文案域不同）。
func RedirectBulkDeleteOutcome(deleted, skipped int) Outcome {
	switch {
	case deleted == 0 && skipped == 0:
		return Outcome{Key: BulkRedirectNoneSelected, Fallback: "没有勾选任何重定向，列表未改动。"}
	case skipped == 0:
		return Outcome{Key: BulkRedirectAllDeleted, Fallback: "已删除 %s 条重定向。", Args: []string{itoa(deleted)}}
	case deleted == 0:
		return Outcome{Key: BulkRedirectAllSkipped, Fallback: "%s 条重定向都未能删除，列表未改动。", Args: []string{itoa(skipped)}}
	default:
		return Outcome{Key: BulkRedirectPartial, Fallback: "已删除 %s 条，%s 条未能删除（可能已不存在或访问面不可用）。",
			Args: []string{itoa(deleted), itoa(skipped)}}
	}
}

// itoa 计数转占位符实参（本包不引 strconv 之外的东西，也不把数字格式留给调用方）。
func itoa(n int) string { return strconv.Itoa(n) }
