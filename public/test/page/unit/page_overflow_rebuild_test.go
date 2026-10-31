package unit

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

type pageJobRow struct {
	ID             int64   `gorm:"column:id"`
	Lang           string  `gorm:"column:lang"`
	Intent         string  `gorm:"column:intent"`
	DraftVersion   int64   `gorm:"column:draft_version"`
	BuildInputHash string  `gorm:"column:build_input_hash"`
	Status         string  `gorm:"column:status"`
	ErrorMessage   *string `gorm:"column:error_message"`
}

func loadPageJobs(t *testing.T, db *gorm.DB, pageID string) []pageJobRow {
	t.Helper()
	var rows []pageJobRow
	if err := db.Raw("SELECT id, lang, intent, draft_version, build_input_hash, status, error_message "+
		"FROM build_jobs WHERE source_id = ? ORDER BY id", pageID).Scan(&rows).Error; err != nil {
		t.Fatalf("读取构建任务失败: %v", err)
	}
	return rows
}

func stagedLangs(t *testing.T, db *gorm.DB, pageID string) []string {
	t.Helper()
	var langs []string
	if err := db.Raw("SELECT lang FROM page_stagings WHERE page_id = ? ORDER BY lang", pageID).Scan(&langs).Error; err != nil {
		t.Fatalf("读取 page_stagings 失败: %v", err)
	}
	return langs
}

func publishedLangsPage(t *testing.T, db *gorm.DB, pageID string) []string {
	t.Helper()
	var langs []string
	if err := db.Raw("SELECT lang FROM page_publications WHERE page_id = ? ORDER BY lang", pageID).Scan(&langs).Error; err != nil {
		t.Fatalf("读取 page_publications 失败: %v", err)
	}
	return langs
}

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

func assertRebuildOutcome(t *testing.T, db *gorm.DB, pageID, stage string) {
	t.Helper()
	if got := stagedLangs(t, db, pageID); !equalStrings(got, []string{"en-US", "zh-CN"}) {
		t.Fatalf("%s：两种语言都应产生暂存产物，实际 %v", stage, got)
	}
	if got := publishedLangsPage(t, db, pageID); !equalStrings(got, []string{"zh-CN"}) {
		t.Fatalf("%s：只有先前已上线的 zh-CN 应处于上线态（en-US 从未上线，不得自动上线），实际 %v", stage, got)
	}
}

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
	syncPage := ids[total-2]
	overflowPage := ids[total-1]
	beforeSync := publicationUpdatedAt(t, db, syncPage, "zh-CN")
	beforeOverflow := publicationUpdatedAt(t, db, overflowPage, "zh-CN")

	beforeOverflowEn := stagingUpdatedAt(t, db, overflowPage, "en-US")
	if beforeOverflowEn.IsZero() {
		t.Fatal("预构建后 en-US 应有暂存行")
	}

	if err := svc.RebuildStale(ctx, ids); err != nil {
		t.Fatalf("RebuildStale 失败: %v", err)
	}

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

	if n := len(loadPageJobs(t, db, syncPage)); n != 0 {
		t.Fatalf("同步批次内的页面不应入队，实际 %d 条", n)
	}

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

	assertRebuildOutcome(t, db, syncPage, "同步路径（第 20 页）")
	assertRebuildOutcome(t, db, overflowPage, "队列异步路径（第 21 页）")

	if now := publicationUpdatedAt(t, db, overflowPage, "zh-CN"); !now.After(beforeOverflow) {
		t.Fatalf("溢出页的 zh-CN 应被重新发布（激活时间前进），实际 %v → %v", beforeOverflow, now)
	}
	if now := publicationUpdatedAt(t, db, syncPage, "zh-CN"); !now.After(beforeSync) {
		t.Fatalf("同步页的 zh-CN 应被重新发布，实际 %v → %v", beforeSync, now)
	}

	if now := stagingUpdatedAt(t, db, overflowPage, "en-US"); !now.After(beforeOverflowEn) {
		t.Fatalf("溢出页的 en-US 应在本次重建中被构建（暂存时间前进），实际 %v → %v", beforeOverflowEn, now)
	}
}

func TestManualBuildJobOnlyBuilds(t *testing.T) {
	db, svc, projects, projectID := newPageService(t)
	ctx := context.Background()
	twoLocaleProject(t, projects, projectID)

	pageID := createPage(t, svc, projectID, "/manual/only", pageDocument).ID
	buildAndPublish(t, svc, pageID)
	activeBefore := activeHashOf(t, db, pageID)
	stagedBefore := pageColumnOf(t, db, pageID, "staged_artifact_id")

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

	if stagedAfter := pageColumnOf(t, db, pageID, "staged_artifact_id"); stagedAfter == stagedBefore || stagedAfter == "" {
		t.Fatalf("手工构建应产生新的暂存产物，实际 %q → %q", stagedBefore, stagedAfter)
	}

	if activeAfter := activeHashOf(t, db, pageID); activeAfter != activeBefore {
		t.Fatalf("手工构建不得回写线上（线上 hash 应保持 %q），实际 %q", activeBefore, activeAfter)
	}
}
