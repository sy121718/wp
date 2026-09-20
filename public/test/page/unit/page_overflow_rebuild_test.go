package unit

// page_overflow_rebuild_test.go — ARCH-04 验收：溢出到构建队列的重建与同步路径语义一致。
//
// 报告的问题：同步 RebuildStale 遍历启用语言并重新发布此前已发布的语言；溢出部分入队后
// executor 只调 Build(ID) —— 没有语言、没有重新发布，同一批第 21 个之后可能停在默认语言
// 的暂存态。本用例构造 21 页 × 两语言（前 20 页走同步、最后一页溢出到队列），断言两条路径
// 的结果逐条一致：两种语言都被构建、此前已上线的语言被重新发布、从未上线的语言仍不上线。

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	buildcontract "go_wp/internal/module/build/contract"
	buildmodel "go_wp/internal/module/build/model"
	buildservice "go_wp/internal/module/build/service"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	projectdto "go_wp/internal/module/project/dto"
	projectservice "go_wp/internal/module/project/service"
)

// twoLocaleProject 把站点语言清单设成「默认 zh-CN + en-US」。
//
// 语言 URL 方案默认就是 default_plain（两语言各有独立路径），显式声明一次以免受其它用例影响。
func twoLocaleProject(t *testing.T, projects *projectservice.Service, projectID string) {
	t.Helper()
	if _, err := projects.SaveLocales(context.Background(), &projectdto.LocalesSaveReq{
		ProjectID: projectID,
		Locales: []projectdto.LocaleItem{
			{Lang: "zh-CN", IsDefault: true},
			{Lang: "en-US"},
		},
	}); err != nil {
		t.Fatalf("保存语言清单失败: %v", err)
	}
}

// newQueueBackedPageService 在 page 服务上接入**真实构建队列**，并按装配层的方式注册执行器。
//
// 执行器那一段与 internal/routers/assembly_publish.go 的接线逐字对应：把任务冻结的
// lang / intent / 输入版本交给页面模块的单页重建编排 —— 这正是报告里被漏掉的那一环。
func newQueueBackedPageService(t *testing.T, db *gorm.DB, svc pagecontract.PageService) *buildservice.Service {
	t.Helper()
	queue := buildservice.NewService(buildmodel.NewModel(db))
	setter, ok := svc.(interface {
		SetBuildQueue(pagecontract.BuildQueueEnqueuer)
	})
	if !ok {
		t.Fatal("页面模块未提供构建队列注入点（SetBuildQueue）")
	}
	setter.SetBuildQueue(queue)
	queue.RegisterExecutor("page", func(ctx context.Context, job *buildcontract.Job) error {
		return svc.RunPageBuildJob(ctx, &pagedto.PageBuildJobReq{
			ID: job.SourceID, Lang: job.Lang, Intent: job.Intent,
			DraftVersion: job.DraftVersion, BuildInputHash: job.BuildInputHash,
		})
	})
	return queue
}

// pageJobRow 队列行里本文件断言相关的列。
type pageJobRow struct {
	ID             int64   `gorm:"column:id"`
	Lang           string  `gorm:"column:lang"`
	Intent         string  `gorm:"column:intent"`
	DraftVersion   int64   `gorm:"column:draft_version"`
	BuildInputHash string  `gorm:"column:build_input_hash"`
	Status         string  `gorm:"column:status"`
	ErrorMessage   *string `gorm:"column:error_message"`
}

// loadPageJobs 读某页面在队列里的全部任务。
func loadPageJobs(t *testing.T, db *gorm.DB, pageID string) []pageJobRow {
	t.Helper()
	var rows []pageJobRow
	if err := db.Raw("SELECT id, lang, intent, draft_version, build_input_hash, status, error_message "+
		"FROM build_jobs WHERE source_id = ? ORDER BY id", pageID).Scan(&rows).Error; err != nil {
		t.Fatalf("读取构建任务失败: %v", err)
	}
	return rows
}

// stagedLangs 页面已产生暂存产物的语言集合（升序）。
func stagedLangs(t *testing.T, db *gorm.DB, pageID string) []string {
	t.Helper()
	var langs []string
	if err := db.Raw("SELECT lang FROM page_stagings WHERE page_id = ? ORDER BY lang", pageID).Scan(&langs).Error; err != nil {
		t.Fatalf("读取 page_stagings 失败: %v", err)
	}
	return langs
}

// publishedLangsPage 页面已上线（存在激活记录）的语言集合（升序）。
func publishedLangsPage(t *testing.T, db *gorm.DB, pageID string) []string {
	t.Helper()
	var langs []string
	if err := db.Raw("SELECT lang FROM page_publications WHERE page_id = ? ORDER BY lang", pageID).Scan(&langs).Error; err != nil {
		t.Fatalf("读取 page_publications 失败: %v", err)
	}
	return langs
}

// stagingUpdatedAt 读某语言暂存行的 update_time（无行返回零值）。
func stagingUpdatedAt(t *testing.T, db *gorm.DB, pageID, lang string) time.Time {
	t.Helper()
	var at *time.Time
	if err := db.Raw("SELECT update_time FROM page_stagings WHERE page_id = ? AND lang = ?", pageID, lang).Scan(&at).Error; err != nil {
		t.Fatalf("读取 page_stagings.update_time 失败: %v", err)
	}
	if at == nil {
		return time.Time{}
	}
	return *at
}

// pageColumnOf 读 pages 行的某一列（本文件用它读 staged_artifact_id）。
//
// 与 page_publish_ledger_test.go 的 activeColumnOf 区分：那个读的是 page_publications。
func pageColumnOf(t *testing.T, db *gorm.DB, pageID, column string) string {
	t.Helper()
	var row struct {
		Value *string `gorm:"column:value"`
	}
	if err := db.Raw("SELECT "+column+"::text AS value FROM pages WHERE id = ?", pageID).Scan(&row).Error; err != nil {
		t.Fatalf("读取 pages.%s 失败: %v", column, err)
	}
	if row.Value == nil {
		return ""
	}
	return *row.Value
}

// assertRebuildOutcome 断言一次「依赖失效重建」后的结果形状：两种语言都有暂存产物，
// 只有先前已上线的 zh-CN 处于上线态。
func assertRebuildOutcome(t *testing.T, db *gorm.DB, pageID, stage string) {
	t.Helper()
	if got := stagedLangs(t, db, pageID); !equalStrings(got, []string{"en-US", "zh-CN"}) {
		t.Fatalf("%s：两种语言都应产生暂存产物，实际 %v", stage, got)
	}
	if got := publishedLangsPage(t, db, pageID); !equalStrings(got, []string{"zh-CN"}) {
		t.Fatalf("%s：只有先前已上线的 zh-CN 应处于上线态（en-US 从未上线，不得自动上线），实际 %v", stage, got)
	}
}

// equalStrings 比较两个字符串切片是否逐元素相等（内部排序，顺序无关）。
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sort.Strings(a)
	sort.Strings(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestOverflowRebuildMatchesSyncPathSemantics 21 页 × 两语言：前 20 页同步、第 21 页经队列，
// 两条路径的结果必须一致。
func TestOverflowRebuildMatchesSyncPathSemantics(t *testing.T) {
	db, svc, projects, projectID := newPageService(t)
	ctx := context.Background()
	twoLocaleProject(t, projects, projectID)
	queue := newQueueBackedPageService(t, db, svc)

	const total = 21
	entityID := uuid.NewString()
	ids := make([]string, 0, total)
	for i := 0; i < total; i++ {
		ids = append(ids, contentBoundPage(t, svc, projectID, fmt.Sprintf("/overflow/p%02d", i), entityID))
	}
	// 21 页 × 两语言：两种语言都构建，但**只发布 zh-CN** —— en-US 从未上线，
	// 这正是验收里「未上线的语言仍不自动上线」要守住的边界。
	for _, id := range ids {
		for _, lang := range []string{"zh-CN", "en-US"} {
			if _, err := svc.Build(ctx, &pagedto.BuildReq{ID: id, Lang: lang}); err != nil {
				t.Fatalf("预构建 %s/%s 失败: %v", id, lang, err)
			}
		}
		if _, err := svc.Publish(ctx, &pagedto.PublishReq{ID: id, Lang: "zh-CN"}); err != nil {
			t.Fatalf("预发布 %s/zh-CN 失败: %v", id, err)
		}
	}
	syncPage := ids[total-2] // 同步批次的最后一页（第 20 页）
	overflowPage := ids[total-1]
	beforeSync := publicationUpdatedAt(t, db, syncPage, "zh-CN")
	beforeOverflow := publicationUpdatedAt(t, db, overflowPage, "zh-CN")
	// 非默认语言的暂存时间：它是「语言集合真的被逐语言重建」的直接证据
	//（旧实现只按默认语言构建，这一行永远不会前进）。
	beforeOverflowEn := stagingUpdatedAt(t, db, overflowPage, "en-US")
	if beforeOverflowEn.IsZero() {
		t.Fatal("预构建后 en-US 应有暂存行")
	}

	// 一批 21 个页面的依赖失效重建：前 20 页同步重建，第 21 页交给队列。
	if err := svc.RebuildStale(ctx, ids); err != nil {
		t.Fatalf("RebuildStale 失败: %v", err)
	}

	// 1) 溢出页入队两条待办：每种语言一条，且任务上下文三要素齐备。
	jobs := loadPageJobs(t, db, overflowPage)
	if len(jobs) != 2 {
		t.Fatalf("溢出页每语言应各入队一条，实际 %d 条: %+v", len(jobs), jobs)
	}
	seen := map[string]bool{}
	for _, job := range jobs {
		if job.Lang == "" {
			t.Fatalf("任务必须冻结构建语言（空 lang 曾是「只重建默认语言」的根因）: %+v", job)
		}
		if job.Intent != buildmodel.IntentDependency {
			t.Fatalf("溢出重建的意图应为 dependency，实际 %q", job.Intent)
		}
		if job.DraftVersion == 0 || strings.TrimSpace(job.BuildInputHash) == "" {
			t.Fatalf("任务必须冻结输入版本（草稿版本 + 输入摘要，不能只用空 hash 占位）: %+v", job)
		}
		if job.Status != buildmodel.StatusPending {
			t.Fatalf("未消费前应为 pending，实际 %q", job.Status)
		}
		seen[job.Lang] = true
	}
	if !seen["zh-CN"] || !seen["en-US"] {
		t.Fatalf("两条待办应分别覆盖 zh-CN 与 en-US，实际 %v", seen)
	}
	// 同步批次里的页面不入队（它们已经同步重建完）。
	if n := len(loadPageJobs(t, db, syncPage)); n != 0 {
		t.Fatalf("同步批次内的页面不应入队，实际 %d 条", n)
	}

	// 2) 消费队列：worker 走的是与同步路径同一份单页重建编排。
	for i := 0; i < 10; i++ {
		processed, err := queue.RunOnce(ctx)
		if err != nil {
			t.Fatalf("消费队列失败: %v", err)
		}
		if !processed {
			break
		}
	}
	for _, job := range loadPageJobs(t, db, overflowPage) {
		if job.Status != buildmodel.StatusSucceeded {
			msg := ""
			if job.ErrorMessage != nil {
				msg = *job.ErrorMessage
			}
			t.Fatalf("任务应成功结案（失败必须准确反映到任务上），实际 status=%q err=%q", job.Status, msg)
		}
	}

	// 3) 结果与同步路径逐条一致。
	assertRebuildOutcome(t, db, syncPage, "同步路径（第 20 页）")
	assertRebuildOutcome(t, db, overflowPage, "队列异步路径（第 21 页）")

	// 4) 「重新发布」确实发生过：已上线的 zh-CN 激活时间在重建后前进（这正是旧实现漏掉的），
	//    未上线的 en-US 一条激活记录都不该出现。
	if now := publicationUpdatedAt(t, db, overflowPage, "zh-CN"); !now.After(beforeOverflow) {
		t.Fatalf("溢出页的 zh-CN 应被重新发布（激活时间前进），实际 %v → %v", beforeOverflow, now)
	}
	if now := publicationUpdatedAt(t, db, syncPage, "zh-CN"); !now.After(beforeSync) {
		t.Fatalf("同步页的 zh-CN 应被重新发布，实际 %v → %v", beforeSync, now)
	}
	// 5) 语言集合真的被逐语言执行：en-US 的暂存行也在这次重建里刷新了
	//   （旧实现只构建默认语言，这一行会停在预构建时刻）。
	if now := stagingUpdatedAt(t, db, overflowPage, "en-US"); !now.After(beforeOverflowEn) {
		t.Fatalf("溢出页的 en-US 应在本次重建中被构建（暂存时间前进），实际 %v → %v", beforeOverflowEn, now)
	}
}

// TestManualBuildJobOnlyBuilds 手工构建任务只构建、不回写线上（manual 意图的执行语义）。
//
// 这是「手工构建 → manual」通路在契约 / DTO / 执行体三处的行为覆盖：本轮没有真实手工入口，
// 通路本身必须被钉住，否则将来接手工入口时语义无人可依。
func TestManualBuildJobOnlyBuilds(t *testing.T) {
	db, svc, projects, projectID := newPageService(t)
	ctx := context.Background()
	twoLocaleProject(t, projects, projectID)

	pageID := createPage(t, svc, projectID, "/manual/only", pageDocument).ID
	buildAndPublish(t, svc, pageID) // 页面此前已上线（zh-CN）
	activeBefore := activeHashOf(t, db, pageID)
	stagedBefore := pageColumnOf(t, db, pageID, "staged_artifact_id")

	// 改草稿（换一个会改变产物字节的文档），再以 manual 意图执行构建任务。
	if _, err := svc.SaveDraft(ctx, &pagedto.SaveDraftReq{
		ID: pageID, ExpectedVersion: 1, DraftPath: "/manual/only",
		DraftDocument: []byte(headingDocument),
	}); err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}
	if err := svc.RunPageBuildJob(ctx, &pagedto.PageBuildJobReq{
		ID: pageID, Lang: "zh-CN", Intent: pagecontract.BuildIntentManual,
		DraftVersion: 2, BuildInputHash: "manual-job-hash",
	}); err != nil {
		t.Fatalf("手工构建任务执行失败: %v", err)
	}

	// 暂存指针换到新产物（构建确实发生了）……
	if stagedAfter := pageColumnOf(t, db, pageID, "staged_artifact_id"); stagedAfter == stagedBefore || stagedAfter == "" {
		t.Fatalf("手工构建应产生新的暂存产物，实际 %q → %q", stagedBefore, stagedAfter)
	}
	// ……但线上仍是旧产物（manual 不回写线上）。
	if activeAfter := activeHashOf(t, db, pageID); activeAfter != activeBefore {
		t.Fatalf("手工构建不得回写线上（线上 hash 应保持 %q），实际 %q", activeBefore, activeAfter)
	}
}
