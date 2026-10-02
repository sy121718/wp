package pageservice

// page_publish_plan.go — 发布计划的读取与冻结（审计 I18N-01）。
//
// 一条判据：**产物一旦产出，它的站点语言输入就不再受配置改动影响**。
//
// 计划的生命周期只有两步：
//
//  1. 构建（或发布）时**冻结**：没有计划、或草稿已被改过（= 新的发布决策）时，
//     按当时的站点语言配置解析一份并落库；已有计划且草稿未变时**原样沿用**。
//  2. 此后一切重编译（发布前的确定性复构建、组件升级后的批量重建、灾难恢复重建）
//     都以冻结值为准 —— 不再回读 project_locales。
//
// 为什么把「草稿版本」当作重新冻结的判据：冻结要挡住的是「既有产物在重建后换了
// hreflang」这一类**无声**的漂移；而作者改了草稿再重新发布本来就产出新产物，
// 这时按当前配置重算才是期望行为。若把「配置变了」本身当判据，就等于没有冻结
//（配置一改，重建立刻跟着变，正是本条审计要消灭的现象）。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	pagemodel "go_wp/internal/module/page/model"
	"go_wp/internal/pipeline"
	"go_wp/pkg/logger"

	"gorm.io/gorm"
)

// frozenPublicationPlan 读取页面某语言**仍然有效**的冻结计划。
//
// 返回 (nil, false) 的三种情况都必须回退现场解析并重新冻结：
//   - 从未冻结（首次构建 / 迁移前的存量页面）；
//   - 计划行损坏（JSON 不可解析 / 语言表为空）—— 与其拿一份半截输入去编译，
//     不如按当前配置重冻一份，并把原因写进日志；
//   - 草稿已变（作者做了新的发布决策）。
//
// 读取失败（数据库错误）同样返回 false：本次按现场配置解析，随后写库那一步若也
// 失败会让整个构建失败 —— 不会留下「产物按新配置、计划还是旧的」这种半截状态。
func (s *Service) frozenPublicationPlan(ctx context.Context, page *pagemodel.PageEntity, lang string) (*pipeline.PublicationPlan, bool) {
	if s.model == nil || page == nil || strings.TrimSpace(lang) == "" {
		return nil, false
	}
	row, err := s.model.GetPublicationPlan(ctx, page.ID, lang)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			logger.Scene("publication").With("pageId", page.ID).With("lang", lang).
				Error(err, "发布计划读取失败：本次按当前站点语言配置解析并重新冻结")
		}
		return nil, false
	}
	var plan pipeline.PublicationPlan
	if uerr := json.Unmarshal(row.Plan, &plan); uerr != nil {
		logger.Scene("publication").With("pageId", page.ID).With("lang", lang).
			Error(uerr, "发布计划解析失败：本次按当前站点语言配置解析并重新冻结")
		return nil, false
	}
	if plan.Empty() {
		return nil, false
	}
	if row.DraftVersion != page.DraftVersion {
		logger.Scene("publication").With("pageId", page.ID).With("lang", lang).
			With("planDraftVersion", row.DraftVersion).With("draftVersion", page.DraftVersion).
			Info("草稿已更新：按当前站点语言配置重新冻结发布计划")
		return nil, false
	}
	// 页面级语言排除（迁移 491）是**第二条**让既有计划失效的判据：计划里若还留着本页
	// 现在排除的语言，继续沿用就会把那批「不产出的语言」原样写回产物（Manifest.SiteLangs、
	// 批次口径的互指）—— 那是错的产物且看起来正常。排除集合变化属于「产出范围变了」，
	// 与草稿变更同级：重新冻结，而不是沿用。
	if planHasExcludedLang(plan, page.ExcludedLangs) {
		logger.Scene("publication").With("pageId", page.ID).With("lang", lang).
			With("excluded", strings.Join(page.ExcludedLangs, ",")).
			Info("本页排除了计划里包含的语言：按当前产出范围重新冻结发布计划")
		return nil, false
	}
	return &plan, true
}

// publicationPlanFor 解析本次构建 / 发布依据的发布计划（审计 I18N-01）。
//
// 第二个返回值是「是否需要落库」：沿用已冻结计划时为 false（不改库，避免无谓写入
// 与 update_time 抖动）；现场解析时一定为 true，由调用方在**写暂存指针的同一事务**
// 里落库 —— 产物与它依据的语言输入必须同生共死。
//
// 现场解析一律走发布口径（LangFallbackForbidden）：冻结动作本身就是发布决策，
// 语言表读不到时降级成「只有默认语言」会在库里留下一份**错的**冻结事实，
// 此后每次重建都忠实地复现它，且没有任何报错。
func (s *Service) publicationPlanFor(ctx context.Context, page *pagemodel.PageEntity, lang string) (pipeline.PublicationPlan, bool, error) {
	if frozen, ok := s.frozenPublicationPlan(ctx, page, lang); ok {
		// 沿用冻结值 —— 同时把「站点语言清单已经与这份冻结输入不一致」记成可见日志
		// （审计 I18N-01 续第 3 条）：不改变本次行为，只是不让人靠猜。
		s.warnPlanDrift(ctx, page, lang, *frozen)
		return *frozen, false, nil
	}
	inputs, err := pipeline.ResolveSiteLangInputs(ctx, s.project, page.ProjectID, pipeline.LangFallbackForbidden)
	if err != nil {
		return pipeline.PublicationPlan{}, false, err
	}
	// 页面级语言排除（迁移 491）在**冻结时**扣掉，而不是等发布循环里逐个跳过。
	//
	// 两条路径的差别不是风格，而是产物对不对：
	//   · 只在循环里跳过 → 计划（以及写进 Manifest 的 SiteLangs）仍声称「这次发布了该语言」，
	//     而产物根本不存在；批次口径下产物的互指直接按这份集合生成，于是出现
	//     「hreflang 指向一条本站永远不会有产物的路径」——产物自己说的话是错的。
	//   · 冻结时扣掉 → 产物只依赖「确实会产出的语言集合」，站点语言清单改了、排除集合
	//     改了都通过重新冻结体现，不存在半截冻结。
	//
	// 由此也定了「改排除后旧计划要不要重新冻结」：**要**（见 frozenPublicationPlan 的
	// 含排除语言即失效），否则旧计划里那份含被排除语言的集合会被后续重建忠实复现。
	inputs = dropExcludedLangs(inputs, page.ExcludedLangs)
	plan := pipeline.PlanOfSiteLangInputs(inputs)
	if plan.Empty() {
		return pipeline.PublicationPlan{}, false, pipeline.ErrLangTableUnavailable
	}
	// 重新冻结前先说清楚「为什么要按当前配置重算」：这是唯一会让既有产物的语言输入
	// 发生变化的入口（作者改了草稿 = 新的发布决策），日志里必须能看出这一点。
	logger.Scene("publication").With("pageId", page.ID).With("lang", lang).
		With("planHash", plan.Hash()).Info("按当前站点语言配置冻结发布计划")
	return plan, true, nil
}

// planHasLang 语言是否在计划的参与集合里（互指刷新据此把范围收在本页的发布范围内）。
func planHasLang(plan pipeline.PublicationPlan, lang string) bool {
	for _, l := range plan.SiteLangs {
		if l == lang {
			return true
		}
	}
	return false
}

// warnPlanDrift 冻结计划的语言集合与当前站点语言集合不一致时，记一条可见日志。
//
// 判据与「是否重冻」无关（重冻只由草稿变更触发，见 PagePublicationPlanEntity.DraftVersion）：
// 这里只是把「这份产物的互指不会包含新语言」这件事**变得可见** —— 否则运营加了语言、
// 页面却一直只有旧语言互指，全靠人猜（审计 I18N-01 续第 3 条）。
//
// 分等级：站点语言是**增加**时记 Info（正常演进，既有产物本就不该被改写）；
// 出现**移除/禁用**时记 Warn（那些语言的既有产物已经被 I18N-017 的退役流程下线，
// 而冻结计划仍留着它们，属于需要人工确认的漂移）。读取失败什么都不记 ——
// 诊断日志不该因为一次读库抖动就产出一条假的「配置不一致」。
func (s *Service) warnPlanDrift(ctx context.Context, page *pagemodel.PageEntity, lang string, plan pipeline.PublicationPlan) {
	if s == nil || s.project == nil || page == nil || plan.Empty() {
		return
	}
	current, err := s.project.EnabledLangs(ctx, page.ProjectID)
	if err != nil || len(current) == 0 {
		return
	}
	// 被本页排除的语言不在「应当产出」的集合里，必须一起扣掉：否则每次发布都会记一条
	// 「站点移除了这些语言」的假警告（真实成因是这一页主动排除），把告警噪音当信号用。
	current = dropExcludedLangs(pipeline.SiteLangInputs{SiteLangs: current}, page.ExcludedLangs).SiteLangs
	added, removed := langSetDiff(plan.SiteLangs, current)
	if added == nil && removed == nil {
		return
	}
	sc := logger.Scene("publication")
	ev := sc.With("pageId", page.ID).With("lang", lang).
		With("frozen", strings.Join(plan.SiteLangs, ",")).
		With("current", strings.Join(current, ","))
	// 只记「加进来的」（此时也在说明「为什么既有产物没有它们」）；移除的另记一条警告。
	if len(added) > 0 {
		ev.With("added", strings.Join(added, ",")).
			Info("站点语言清单新增了语言，但本页的冻结发布计划不含它：既有产物按冻结值发布，新语言需在草稿改动后的重新发布中生效")
	}
	if len(removed) > 0 {
		ev.With("removed", strings.Join(removed, ",")).
			Warn("站点语言清单已移除本页冻结计划里的语言：既有产物仍按冻结值声明互指，请确认该语言的路由已下线或安排重新发布")
	}
}

// langSetDiff 以**集合**语义比较两份语言表（顺序无关：默认语言在前是实现细节，
// is_default 换人不应被误报成「语言集合变了」）。
// 返回 (仅出现在 current 的、仅出现在 frozen 的)；两者都为空时返回 (nil, nil)。
func langSetDiff(frozen, current []string) (added, removed []string) {
	frozenSet := make(map[string]bool, len(frozen))
	for _, l := range frozen {
		frozenSet[strings.TrimSpace(l)] = true
	}
	currentSet := make(map[string]bool, len(current))
	for _, l := range current {
		currentSet[strings.TrimSpace(l)] = true
	}
	for _, l := range current {
		if l = strings.TrimSpace(l); l != "" && !frozenSet[l] {
			added = append(added, l)
		}
	}
	for _, l := range frozen {
		if l = strings.TrimSpace(l); l != "" && !currentSet[l] {
			removed = append(removed, l)
		}
	}
	if len(added) == 0 && len(removed) == 0 {
		return nil, nil
	}
	return added, removed
}
