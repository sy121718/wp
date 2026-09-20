package unit

// page_publication_plan_test.go — 发布计划冻结（审计 I18N-01）。
//
// 缺陷现象：手工 Page 的 hreflang 互指与「默认语言是谁」是**构建期**从站点语言清单
// （project_locales）推导出来的，而清单是可编辑配置。产物一旦建成，任何一次重放 / 重建
// 都会用当时那份清单重算一遍：发布前的确定性复构建、组件升级后的批量重建
//（RebuildStale）、灾难恢复的按元数据重建（RebuildArtifact）。于是同一份冻结源文档
// 在配置改动之后产出另一份互指链接 —— 既有产物的 hreflang 凭空变了（少一条、多一条、
// x-default 换人），而线上路径一个都没动，没有任何报错。
//
// 这里钉住三件事：
//  1. 发布时冻结的语言输入落库（page_publication_plans，迁移 308），且内容是「当时」的
//     站点语言表 + 默认语言，不是指向当前配置的某个指针；
//  2. **改站点语言配置之后重建既有产物，产物字节与互指逐字不变**（核心验收）；
//  3. 首次发布行为不变（互指集合仍按访问面推导），且冻结不是一冻永逸 —— 作者改了草稿
//     （新的发布决策）之后会按当时的配置重新冻结。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	projectdto "go_wp/internal/module/project/dto"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/pipeline"

	"gorm.io/gorm"
)

// saveSiteLocales 设置站点语言清单：**第一个语言即默认语言**（其余按顺序启用）。
//
// 刻意走 project.SaveLocales（后台「站点语言配置」的真实入口）而不是直接写表：
// 本条审计的场景就是「运营在后台改了站点语言配置」，绕开用例入口会测不到它的校验与
// 副作用（例如禁用语言时的退役流程）——那正是既有行为必须保持不变的部分。
func saveSiteLocales(t *testing.T, projects *projectservice.Service, projectID string, langs ...string) {
	t.Helper()
	items := make([]projectdto.LocaleItem, 0, len(langs))
	for i, lang := range langs {
		items = append(items, projectdto.LocaleItem{Lang: lang, IsDefault: i == 0})
	}
	if _, err := projects.SaveLocales(context.Background(), &projectdto.LocalesSaveReq{
		ProjectID: projectID, Locales: items,
	}); err != nil {
		t.Fatalf("设置站点语言清单失败: %v", err)
	}
}

// hreflangLinesOf 摘出产物 head 里的 alternate 行（顺序即产物字节里的顺序）。
func hreflangLinesOf(html string) []string {
	var out []string
	for _, line := range strings.Split(html, "\n") {
		if strings.Contains(line, `rel="alternate"`) {
			out = append(out, strings.TrimSpace(line))
		}
	}
	return out
}

// buildAndPublishLang 构建并发布指定语言（多语言逐语言上线）。
func buildAndPublishLang(t *testing.T, svc pagecontract.PageService, pageID, lang string) {
	t.Helper()
	ctx := context.Background()
	if _, err := svc.Build(ctx, &pagedto.BuildReq{ID: pageID, Lang: lang}); err != nil {
		t.Fatalf("%s 构建失败: %v", lang, err)
	}
	if _, err := svc.Publish(ctx, &pagedto.PublishReq{ID: pageID, Lang: lang}); err != nil {
		t.Fatalf("%s 发布失败: %v", lang, err)
	}
}

// stagedHashOf 读取某语言当前暂存产物的 hash（page_stagings 为该语言真源）。
func stagedHashOf(t *testing.T, db *gorm.DB, pageID, lang string) string {
	t.Helper()
	var hash string
	if err := db.Raw("SELECT artifact_hash FROM page_stagings WHERE page_id = ? AND lang = ?", pageID, lang).
		Scan(&hash).Error; err != nil {
		t.Fatalf("读取暂存指针失败: %v", err)
	}
	if hash == "" {
		t.Fatalf("暂存指针缺失（page=%s lang=%s）", pageID, lang)
	}
	return hash
}

// publicationPlanRowOf 读取某语言已冻结的发布计划（真源表 page_publication_plans）。
func publicationPlanRowOf(t *testing.T, db *gorm.DB, pageID, lang string) (plan pipeline.PublicationPlan, planHash string, draftVersion int64) {
	t.Helper()
	var row struct {
		PlanRaw      string
		PlanHash     string
		DraftVersion int64
	}
	if err := db.Raw(`SELECT plan::text AS plan_raw, plan_hash, draft_version
		FROM page_publication_plans WHERE page_id = ? AND lang = ?`, pageID, lang).Scan(&row).Error; err != nil {
		t.Fatalf("读取发布计划失败: %v", err)
	}
	if row.PlanRaw == "" {
		t.Fatalf("发布计划不存在（page=%s lang=%s）：发布必须把语言输入冻结落库", pageID, lang)
	}
	if err := json.Unmarshal([]byte(row.PlanRaw), &plan); err != nil {
		t.Fatalf("解析发布计划失败: %v（原文 %s）", err, row.PlanRaw)
	}
	return plan, row.PlanHash, row.DraftVersion
}

// TestPagePublicationPlanFreezesHreflangAcrossConfigChange 核心验收：
// 发布 → 改站点语言配置 → 重建既有产物，hreflang 与产物字节不变。
func TestPagePublicationPlanFreezesHreflangAcrossConfigChange(t *testing.T) {
	db, svc, projects, projectID := newPageService(t)
	ctx := context.Background()
	withDefaultPlain(t)
	saveSiteLocales(t, projects, projectID, "zh-CN", "en-US")

	page := createPage(t, svc, projectID, "/about", headingDocument)

	// 常规多语言上线：先构建两种语言、再逐个发布；最后重建一次 zh，
	// 让它的互指看见已上线的 en（互指集合按访问面推导，先发布者需重建一次才互指）。
	buildAndPublishLang(t, svc, page.ID, "zh-CN")
	buildAndPublishLang(t, svc, page.ID, "en-US")
	zhBefore, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID})
	if err != nil {
		t.Fatalf("zh 重建失败: %v", err)
	}
	linesBefore := hreflangLinesOf(artifactIndexHTML(t, zhBefore.StagedHash))
	// 两种语言 + 一条 x-default = 三条 alternate 行。
	if len(linesBefore) != 3 ||
		!strings.Contains(strings.Join(linesBefore, "\n"), `hreflang="en-US"`) ||
		!strings.Contains(strings.Join(linesBefore, "\n"), `hreflang="zh-CN"`) {
		t.Fatalf("前置事实不成立：zh 产物应含 zh-CN 与 en-US 两条互指 + x-default，实际 %v", linesBefore)
	}
	enHashBefore := stagedHashOf(t, db, page.ID, "en-US")
	t.Logf("改动前：zh=%s %v / en=%s", zhBefore.StagedHash, linesBefore, enHashBefore)

	// 计划已落库：冻结的是「发布那一刻」的站点语言表与默认语言。
	plan, planHash, draftVersion := publicationPlanRowOf(t, db, page.ID, "zh-CN")
	if len(plan.SiteLangs) != 2 || plan.SiteLangs[0] != "zh-CN" || plan.SiteLangs[1] != "en-US" || plan.DefaultLang != "zh-CN" {
		t.Fatalf("冻结的发布计划不符：%+v", plan)
	}
	if planHash != plan.Hash() {
		t.Fatalf("计划指纹与内容不一致：%s vs %s", planHash, plan.Hash())
	}
	if draftVersion <= 0 {
		t.Fatalf("计划应记录冻结时的草稿版本（重新冻结的判据），实际 %d", draftVersion)
	}

	// 改站点语言配置：把默认语言换成 en-US（两种语言仍全部启用 → 不触发语言退役，
	// 访问面一个字节都没动）。这是**纯粹的配置变化**，正是本条审计的场景。
	if _, err = projects.SaveLocales(ctx, &projectdto.LocalesSaveReq{
		ProjectID: projectID,
		Locales: []projectdto.LocaleItem{
			{Lang: "en-US", IsDefault: true},
			{Lang: "zh-CN"},
		},
	}); err != nil {
		t.Fatalf("改站点语言配置失败: %v", err)
	}

	// 重建既有产物：组件升级后的批量重建入口 + 显式重建入口各走一次。
	if err = svc.RebuildStale(ctx, []string{page.ID}); err != nil {
		t.Fatalf("批量重建失败: %v", err)
	}
	zhAfter, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID})
	if err != nil {
		t.Fatalf("改配置后重建 zh 失败: %v", err)
	}
	if zhAfter.StagedHash != zhBefore.StagedHash {
		t.Fatalf("改站点语言配置后重建既有产物，hash 变了：%s → %s（发布计划没有生效）",
			zhBefore.StagedHash, zhAfter.StagedHash)
	}
	linesAfter := hreflangLinesOf(artifactIndexHTML(t, zhAfter.StagedHash))
	if strings.Join(linesAfter, "\n") != strings.Join(linesBefore, "\n") {
		t.Fatalf("改站点语言配置后重建产物，互指变了：改动前 %v / 改动后 %v", linesBefore, linesAfter)
	}
	if enHashAfter := stagedHashOf(t, db, page.ID, "en-US"); enHashAfter != enHashBefore {
		t.Fatalf("非默认语言的既有产物同样必须不变：%s → %s", enHashBefore, enHashAfter)
	}
	// 默认语言换人不得让既有产物换 URL：/about 仍是默认语言的激活路径。
	if _, lerr := os.Readlink(filepath.Join(activeDir(t), "about")); lerr != nil {
		t.Fatalf("默认语言改配置后 /about 的激活链接不应消失: %v", lerr)
	}
	// 计划是**沿用**而不是重冻：重建既有产物不产生新的发布决策。
	if _, hashAfter, _ := publicationPlanRowOf(t, db, page.ID, "zh-CN"); hashAfter != planHash {
		t.Fatalf("重建既有产物不应重冻发布计划：%s → %s", planHash, hashAfter)
	}
}

// TestPageFirstPublishBehaviorUnchanged 既有 Page 的首次发布行为不变：
// 互指集合仍按访问面推导（另一种语言没上线就没有互指），冻结不改变这次发布的产物。
func TestPageFirstPublishBehaviorUnchanged(t *testing.T) {
	db, svc, projects, projectID := newPageService(t)
	withDefaultPlain(t)
	saveSiteLocales(t, projects, projectID, "zh-CN", "en-US")

	page := createPage(t, svc, projectID, "/about", headingDocument)

	// 首次发布：只有默认语言上线，另一种语言还在配置里但没构建/发布。
	buildAndPublishLang(t, svc, page.ID, "zh-CN")
	zhHash := stagedHashOf(t, db, page.ID, "zh-CN")
	if lines := hreflangLinesOf(artifactIndexHTML(t, zhHash)); len(lines) != 0 {
		t.Fatalf("首次发布时另一种语言尚未上线，产物不应含互指（既有行为），实际 %v", lines)
	}
	// 首发布即冻结计划 —— 冻结本身不改变这次发布的产物，只是把输入记下来。
	plan, _, _ := publicationPlanRowOf(t, db, page.ID, "zh-CN")
	if len(plan.SiteLangs) != 2 || plan.DefaultLang != "zh-CN" {
		t.Fatalf("首发布应冻结当时的站点语言输入，实际 %+v", plan)
	}

	// 再发布 en：en 的产物含两条互指（既有行为：后发布者能看见先发布者）。
	buildAndPublishLang(t, svc, page.ID, "en-US")
	enHash := stagedHashOf(t, db, page.ID, "en-US")
	enLines := hreflangLinesOf(artifactIndexHTML(t, enHash))
	if len(enLines) != 3 || !strings.Contains(strings.Join(enLines, "\n"), `hreflang="zh-CN"`) {
		t.Fatalf("en 产物应含 zh-CN 与 en-US 两条互指 + x-default，实际 %v", enLines)
	}
	t.Logf("首次发布行为：zh=%s（%d 条互指） en=%s（%d 条互指）", zhHash, len(hreflangLinesOf(artifactIndexHTML(t, zhHash))), enHash, len(enLines))
}

// TestPagePublicationPlanRefreezesOnNewDraft 冻结不是一冻永逸：
// 作者改了草稿（新的发布决策）之后，按当时的站点配置重新冻结。
func TestPagePublicationPlanRefreezesOnNewDraft(t *testing.T) {
	db, svc, projects, projectID := newPageService(t)
	ctx := context.Background()
	withDefaultPlain(t)
	saveSiteLocales(t, projects, projectID, "zh-CN", "en-US")

	page := createPage(t, svc, projectID, "/about", headingDocument)
	if _, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID}); err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	before, beforeHash, beforeDraft := publicationPlanRowOf(t, db, page.ID, "zh-CN")
	if before.DefaultLang != "zh-CN" {
		t.Fatalf("首次冻结应取当时的默认语言，实际 %+v", before)
	}

	// 改配置（换默认语言）后保存草稿：新草稿 = 新的发布决策，重建按当前配置重新冻结。
	if _, err := projects.SaveLocales(ctx, &projectdto.LocalesSaveReq{
		ProjectID: projectID,
		Locales: []projectdto.LocaleItem{
			{Lang: "en-US", IsDefault: true},
			{Lang: "zh-CN"},
		},
	}); err != nil {
		t.Fatalf("改站点语言配置失败: %v", err)
	}
	if _, err := svc.SaveDraft(ctx, &pagedto.SaveDraftReq{
		ID: page.ID, ExpectedVersion: page.DraftVersion,
		DraftPath: "/about", DraftDocument: []byte(headingDocument),
	}); err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}
	built, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID})
	if err != nil {
		t.Fatalf("改草稿后重建失败: %v", err)
	}
	after, afterHash, afterDraft := publicationPlanRowOf(t, db, page.ID, "zh-CN")
	if after.DefaultLang != "en-US" {
		t.Fatalf("改了草稿的新发布决策应按当前配置重冻（默认语言 en-US），实际 %+v", after)
	}
	if afterHash == beforeHash || afterDraft == beforeDraft {
		t.Fatalf("重新冻结应产生新指纹与新草稿版本：hash %s→%s draft %d→%d",
			beforeHash, afterHash, beforeDraft, afterDraft)
	}
	// 默认语言换人后，本语言不再是无前缀的默认语言 —— 这是**新产物**的路径，
	// 与「既有产物不变」不矛盾（既有产物仍由它的冻结计划守着）。
	if built.StagedHash == "" {
		t.Fatal("重建应产出暂存产物")
	}
	t.Logf("重新冻结：%+v（草稿 %d→%d）", after, beforeDraft, afterDraft)
}
