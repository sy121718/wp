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
		return *frozen, false, nil
	}
	inputs, err := pipeline.ResolveSiteLangInputs(ctx, s.project, page.ProjectID, pipeline.LangFallbackForbidden)
	if err != nil {
		return pipeline.PublicationPlan{}, false, err
	}
	plan := pipeline.PlanOfSiteLangInputs(inputs)
	if plan.Empty() {
		return pipeline.PublicationPlan{}, false, pipeline.ErrLangTableUnavailable
	}
	return plan, true, nil
}
