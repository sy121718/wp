package unit

import (
	"context"
	json "go_wp/pkg/jsonx"
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

func hreflangLinesOf(html string) []string {
	var out []string
	for _, line := range strings.Split(html, "\n") {
		if strings.Contains(line, `rel="alternate"`) {
			out = append(out, strings.TrimSpace(line))
		}
	}
	return out
}

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

func TestPagePublicationPlanFreezesHreflangAcrossConfigChange(t *testing.T) {
	db, svc, projects, projectID := newPageService(t)
	ctx := context.Background()
	withDefaultPlain(t)
	saveSiteLocales(t, projects, projectID, "zh-CN", "en-US")

	page := createPage(t, svc, projectID, "/about", headingDocument)

	buildAndPublishLang(t, svc, page.ID, "zh-CN")
	buildAndPublishLang(t, svc, page.ID, "en-US")
	zhBefore, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID})
	if err != nil {
		t.Fatalf("zh 重建失败: %v", err)
	}
	linesBefore := hreflangLinesOf(artifactIndexHTML(t, zhBefore.StagedHash))

	if len(linesBefore) != 3 ||
		!strings.Contains(strings.Join(linesBefore, "\n"), `hreflang="en-US"`) ||
		!strings.Contains(strings.Join(linesBefore, "\n"), `hreflang="zh-CN"`) {
		t.Fatalf("前置事实不成立：zh 产物应含 zh-CN 与 en-US 两条互指 + x-default，实际 %v", linesBefore)
	}
	enHashBefore := stagedHashOf(t, db, page.ID, "en-US")
	t.Logf("改动前：zh=%s %v / en=%s", zhBefore.StagedHash, linesBefore, enHashBefore)

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

	if _, err = projects.SaveLocales(ctx, &projectdto.LocalesSaveReq{
		ProjectID: projectID,
		Locales: []projectdto.LocaleItem{
			{Lang: "en-US", IsDefault: true},
			{Lang: "zh-CN"},
		},
	}); err != nil {
		t.Fatalf("改站点语言配置失败: %v", err)
	}

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

	if _, lerr := os.Readlink(filepath.Join(activeDir(t), "about")); lerr != nil {
		t.Fatalf("默认语言改配置后 /about 的激活链接不应消失: %v", lerr)
	}

	if _, hashAfter, _ := publicationPlanRowOf(t, db, page.ID, "zh-CN"); hashAfter != planHash {
		t.Fatalf("重建既有产物不应重冻发布计划：%s → %s", planHash, hashAfter)
	}
}

func TestPageFirstPublishBehaviorUnchanged(t *testing.T) {
	db, svc, projects, projectID := newPageService(t)
	withDefaultPlain(t)
	saveSiteLocales(t, projects, projectID, "zh-CN", "en-US")

	page := createPage(t, svc, projectID, "/about", headingDocument)

	buildAndPublishLang(t, svc, page.ID, "zh-CN")
	zhHash := stagedHashOf(t, db, page.ID, "zh-CN")
	if lines := hreflangLinesOf(artifactIndexHTML(t, zhHash)); len(lines) != 0 {
		t.Fatalf("首次发布时另一种语言尚未上线，产物不应含互指（既有行为），实际 %v", lines)
	}

	plan, _, _ := publicationPlanRowOf(t, db, page.ID, "zh-CN")
	if len(plan.SiteLangs) != 2 || plan.DefaultLang != "zh-CN" {
		t.Fatalf("首发布应冻结当时的站点语言输入，实际 %+v", plan)
	}

	buildAndPublishLang(t, svc, page.ID, "en-US")
	enHash := stagedHashOf(t, db, page.ID, "en-US")
	enLines := hreflangLinesOf(artifactIndexHTML(t, enHash))
	if len(enLines) != 3 || !strings.Contains(strings.Join(enLines, "\n"), `hreflang="zh-CN"`) {
		t.Fatalf("en 产物应含 zh-CN 与 en-US 两条互指 + x-default，实际 %v", enLines)
	}
	t.Logf("首次发布行为：zh=%s（%d 条互指） en=%s（%d 条互指）", zhHash, len(hreflangLinesOf(artifactIndexHTML(t, zhHash))), enHash, len(enLines))
}

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

	if built.StagedHash == "" {
		t.Fatal("重建应产出暂存产物")
	}
	t.Logf("重新冻结：%+v（草稿 %d→%d）", after, beforeDraft, afterDraft)
}
