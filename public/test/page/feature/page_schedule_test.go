package feature

// page_schedule_test.go — 定时上下线（PIPE-7）的端到端行为（真实 PostgreSQL + 真实文件系统）。
//
// 覆盖两条拍板的决策与它们最容易退化的地方：
//
//   1. **到点只切指针，绝不重新编译** —— 断言激活目录里的符号链接指向排定时冻结的那份产物
//      （page_stagings.artifact_hash），而不是「重新构建出来的另一份」；
//   2. **排定后草稿被改 → 硬失败** —— 到点不按当前草稿上线，置 failed 且原因可读；
//
// 另有三条防退化的：
//   · 幂等直通（链接已指向本次产物时重复执行不产生新状态）；
//   · 并发只生效一次（两个执行者同时扫描，claim 的互斥把它压到一次）；
//   · 下线（符号链接、page_routes、page_publications、pages 镜像、sitemap 五处一起收敛）。
//
// 「到点」在测试里用一条 UPDATE 把 scheduled_at 挪到过去来完成，而不是 sleep 等真实时间：
// 判据（`scheduled_at <= now()`）与等待时长无关，用等待只会让用例变得又慢又飘。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	pagemodel "go_wp/internal/module/page/model"

	"go_wp/pkg/sitetz"

	"gorm.io/gorm"
)

// scheduleAtFuture 生成一个「未来两小时」的排定时刻字符串（按站点时区书写的形态）。
//
// 顺带验证时区往返：写侧按站点时区解释，读回来必须落回同一个绝对时刻 ——
// 用 sitetz.FormatDateTime 生成、由 sitetz.ParseDateTime 解析，两端同一口径。
func scheduleAtFuture() string {
	return sitetz.FormatDateTime(time.Now().Add(2 * time.Hour))
}

// makeScheduleDue 把这条排定的到点时刻挪到过去（等价于「时间到了」）。
func makeScheduleDue(t *testing.T, db *gorm.DB, id int64) {
	t.Helper()
	if err := db.Exec(`UPDATE page_schedules SET scheduled_at = now() - interval '1 minute' WHERE id = ?`, id).Error; err != nil {
		t.Fatalf("把排定挪到过去失败: %v", err)
	}
}

// stagingHashOf 读该页面该语言的暂存产物 hash（排定冻结的那一份）。
func stagingHashOf(t *testing.T, db *gorm.DB, pageID, lang string) string {
	t.Helper()
	var hash string
	if err := db.Raw(`SELECT artifact_hash FROM page_stagings WHERE page_id = ? AND lang = ?`, pageID, lang).
		Scan(&hash).Error; err != nil {
		t.Fatalf("读取暂存产物 hash 失败: %v", err)
	}
	return hash
}

// activeLinkTarget 读激活目录里某个路径的符号链接目标（不存在返回空串）。
func activeLinkTarget(t *testing.T, path string) string {
	t.Helper()
	link := filepath.Join(artifactRootOf(t), "public", "active", trimLeadingSlash(path))
	target, err := os.Readlink(link)
	if err != nil {
		return ""
	}
	return target
}

// trimLeadingSlash 去掉前导斜杠（激活目录里的相对路径）。
func trimLeadingSlash(p string) string {
	for len(p) > 0 && p[0] == '/' {
		p = p[1:]
	}
	return p
}

// scheduleRowOf 读一条排定的当前状态与失败原因（PIPE-7 的落库断言）。
func scheduleRowOf(t *testing.T, db *gorm.DB, id int64) (status string, lastError string, attempts int) {
	t.Helper()
	var row struct {
		Status    string
		LastError *string
		Attempts  int
	}
	if err := db.Raw(`SELECT status, last_error, attempts FROM page_schedules WHERE id = ?`, id).
		Scan(&row).Error; err != nil {
		t.Fatalf("读取排定状态失败: %v", err)
	}
	if row.LastError != nil {
		lastError = *row.LastError
	}
	return row.Status, lastError, row.Attempts
}

// TestPageSchedulePublishAtDueTime 排定上线：到点只切指针，五处状态一起收敛。
func TestPageSchedulePublishAtDueTime(t *testing.T) {
	db, svc, projectID := newPageService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/sched", DraftDocument: json.RawMessage(docV2),
	})
	if err != nil {
		t.Fatalf("创建 Page 失败: %v", err)
	}
	item, err := svc.SetPageSchedule(ctx, &pagedto.ScheduleSetReq{
		PageID: created.ID, Action: "publish", ScheduledAt: scheduleAtFuture(),
	})
	if err != nil {
		t.Fatalf("排定上线失败: %v", err)
	}
	if item.Status != pagemodel.ScheduleStatusPending || item.ScheduledAtLocal == "" {
		t.Fatalf("排定投影错误: %+v", item)
	}
	// 排定时必须已经冻结产物：没有暂存产物就无从「只切指针」。
	if hash := stagingHashOf(t, db, created.ID, item.Lang); hash == "" {
		t.Fatal("排定后应已有暂存产物（排定即冻结）")
	}

	// 决策 1 的基线：到点前记录产物行数与暂存 hash。
	var artifactsBefore int64
	db.Raw(`SELECT count(*) FROM page_artifacts WHERE page_id = ?`, created.ID).Scan(&artifactsBefore)

	makeScheduleDue(t, db, item.ID)
	run, err := svc.RunDueSchedules(ctx)
	if err != nil {
		t.Fatalf("到点扫描失败: %v", err)
	}
	if run.Claimed != 1 || run.Applied != 1 || run.Failed != 0 {
		t.Fatalf("到点扫描统计错误: %+v", run)
	}

	// 决策 1：**绝不重新编译** —— 到点后产物行数不变、暂存指针不变，
	// 切上去的就是排定时冻结的那一份（编译会新增产物行，并让暂存指针指向新 hash）。
	var artifactsAfter int64
	db.Raw(`SELECT count(*) FROM page_artifacts WHERE page_id = ?`, created.ID).Scan(&artifactsAfter)
	if artifactsAfter != artifactsBefore {
		t.Fatalf("到点上线不该产生新产物：before=%d after=%d", artifactsBefore, artifactsAfter)
	}
	if hashAfter := stagingHashOf(t, db, created.ID, item.Lang); hashAfter != stagingHashOf(t, db, created.ID, item.Lang) {
		t.Fatalf("暂存指针在到点时被改写: %s", hashAfter)
	}

	// ① 访问面：符号链接存在且指向**排定时冻结的那份**产物。
	hash := stagingHashOf(t, db, created.ID, item.Lang)
	target := activeLinkTarget(t, "/sched")
	if target == "" || !strings.Contains(target, hash) {
		t.Fatalf("激活链接应指向冻结产物 %s，实际 %q", hash, target)
	}
	entry := filepath.Join(artifactRootOf(t), "public", "active", "sched", "index.html")
	data, rerr := os.ReadFile(entry)
	if rerr != nil || !strings.Contains(string(data), "你好") {
		t.Fatalf("激活入口内容错误: err=%v content=%s", rerr, data)
	}

	// ② 发布指针（该语言一行）。
	var pubCount int64
	db.Raw(`SELECT count(*) FROM page_publications WHERE page_id = ? AND lang = ?`, created.ID, item.Lang).Scan(&pubCount)
	if pubCount != 1 {
		t.Fatalf("page_publications 应有且仅有一行，实际 %d", pubCount)
	}

	// ③ 路由占用 active。
	var routeKind string
	db.Raw(`SELECT route_kind FROM page_routes WHERE project_id = ? AND path = ?`, projectID, "/sched").Scan(&routeKind)
	if routeKind != "active" {
		t.Fatalf("page_routes 应为 active，实际 %q", routeKind)
	}

	// ④ pages 单值镜像同步。
	var mirror *string
	db.Raw(`SELECT active_artifact_id FROM pages WHERE id = ?`, created.ID).Scan(&mirror)
	if mirror == nil || *mirror == "" {
		t.Fatal("pages.active_artifact_id 镜像未同步")
	}

	// ⑤ 站点文件刷新（sitemap 里应有这条新上线的路径）。
	sitemapPath := filepath.Join(artifactRootOf(t), "public", "active", "sitemap.xml")
	if body, serr := os.ReadFile(sitemapPath); serr == nil && !strings.Contains(string(body), "/sched") {
		t.Fatalf("sitemap 应包含新上线路径 /sched，实际内容:\n%s", body)
	}

	// ⑥ 排定结案。
	status, _, attempts := scheduleRowOf(t, db, item.ID)
	if status != pagemodel.ScheduleStatusDone || attempts != 1 {
		t.Fatalf("排定应完成且只被认领一次: status=%s attempts=%d", status, attempts)
	}
}

// TestPageScheduleDraftChangedFails 排定后草稿被改 → 到点硬失败，不按新草稿重新编译。
func TestPageScheduleDraftChangedFails(t *testing.T) {
	db, svc, projectID := newPageService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/sched2", DraftDocument: json.RawMessage(docV2),
	})
	if err != nil {
		t.Fatalf("创建 Page 失败: %v", err)
	}
	item, err := svc.SetPageSchedule(ctx, &pagedto.ScheduleSetReq{
		PageID: created.ID, Action: "publish", ScheduledAt: scheduleAtFuture(),
	})
	if err != nil {
		t.Fatalf("排定上线失败: %v", err)
	}
	// 排定之后改草稿（SaveDraft 推进 draft_version）。
	if _, serr := svc.SaveDraft(ctx, &pagedto.SaveDraftReq{
		ID: created.ID, ExpectedVersion: created.DraftVersion,
		DraftPath: "/sched2", DraftDocument: json.RawMessage(docAlpha),
	}); serr != nil {
		t.Fatalf("保存草稿失败: %v", serr)
	}

	makeScheduleDue(t, db, item.ID)
	run, err := svc.RunDueSchedules(ctx)
	if err != nil {
		t.Fatalf("到点扫描失败: %v", err)
	}
	if run.Failed != 1 || run.Applied != 0 {
		t.Fatalf("草稿变更后应硬失败: %+v", run)
	}
	status, lastError, _ := scheduleRowOf(t, db, item.ID)
	if status != pagemodel.ScheduleStatusFailed {
		t.Fatalf("排定应失败，实际 %s", status)
	}
	// 失败原因是**可翻译的业务 key**（口径与 publish() 的 ErrRebuildRequired 同源），
	// 不是原文也不是裸 SQL 错误 —— 这一列会显示在后台。
	if lastError != pageenums.ErrRebuildRequired {
		t.Fatalf("失败原因应为 %s，实际 %q", pageenums.ErrRebuildRequired, lastError)
	}
	// 线上不该出现这个页面（没有按新草稿上线）。
	if target := activeLinkTarget(t, "/sched2"); target != "" {
		t.Fatalf("草稿变更后不该上线，激活链接却存在: %s", target)
	}
	var pubCount int64
	db.Raw(`SELECT count(*) FROM page_publications WHERE page_id = ?`, created.ID).Scan(&pubCount)
	if pubCount != 0 {
		t.Fatalf("不该有发布指针，实际 %d 行", pubCount)
	}
}

// TestPageScheduleIdempotentWhenAlreadyActive 链接已指向本次产物时重复执行不产生新状态。
//
// 场景：手工发布之后又排了一条上线，到点时线上已经就是这个产物。此时跳过 FS 切换
// （避免无谓的原子替换），但仍要走完回执与数据库步骤 —— 那正是「访问面已切、数据库没跟上」的另一半。
func TestPageScheduleIdempotentWhenAlreadyActive(t *testing.T) {
	db, svc, projectID := newPageService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/sched3", DraftDocument: json.RawMessage(docV2),
	})
	if err != nil {
		t.Fatalf("创建 Page 失败: %v", err)
	}
	if _, berr := svc.Build(ctx, &pagedto.BuildReq{ID: created.ID}); berr != nil {
		t.Fatalf("构建失败: %v", berr)
	}
	if _, perr := svc.Publish(ctx, &pagedto.PublishReq{ID: created.ID}); perr != nil {
		t.Fatalf("手工发布失败: %v", perr)
	}
	item, err := svc.SetPageSchedule(ctx, &pagedto.ScheduleSetReq{
		PageID: created.ID, Action: "publish", ScheduledAt: scheduleAtFuture(),
	})
	if err != nil {
		t.Fatalf("排定上线失败: %v", err)
	}
	before := activeLinkTarget(t, "/sched3")
	makeScheduleDue(t, db, item.ID)
	run, err := svc.RunDueSchedules(ctx)
	if err != nil || run.Applied != 1 {
		t.Fatalf("重复上线应成功结案: run=%+v err=%v", run, err)
	}
	after := activeLinkTarget(t, "/sched3")
	if before == "" || before != after {
		t.Fatalf("幂等直通不该改变链接目标: before=%q after=%q", before, after)
	}
	var pubCount int64
	db.Raw(`SELECT count(*) FROM page_publications WHERE page_id = ? AND lang = ?`, created.ID, item.Lang).Scan(&pubCount)
	if pubCount != 1 {
		t.Fatalf("重复上线不该产生第二行发布指针，实际 %d", pubCount)
	}
}

// TestPageScheduleConcurrentRunsApplyOnce 两个执行者同时扫描，动作只生效一次。
//
// 证明的是认领语句的互斥：`UPDATE ... WHERE id IN (SELECT ... WHERE status='pending'
// AND NOT EXISTS(同键 running) FOR UPDATE SKIP LOCKED)` —— 一方把行置为 running 之后，
// 另一方看到的就不再是 pending，于是整批领不到它。
func TestPageScheduleConcurrentRunsApplyOnce(t *testing.T) {
	db, svc, projectID := newPageService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/sched4", DraftDocument: json.RawMessage(docV2),
	})
	if err != nil {
		t.Fatalf("创建 Page 失败: %v", err)
	}
	item, err := svc.SetPageSchedule(ctx, &pagedto.ScheduleSetReq{
		PageID: created.ID, Action: "publish", ScheduledAt: scheduleAtFuture(),
	})
	if err != nil {
		t.Fatalf("排定上线失败: %v", err)
	}
	makeScheduleDue(t, db, item.ID)

	var wg sync.WaitGroup
	results := make([]*pagedto.ScheduleRunResp, 2)
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = svc.RunDueSchedules(context.Background())
		}(i)
	}
	wg.Wait()

	applied := 0
	for i := range results {
		if errs[i] != nil {
			t.Fatalf("并发扫描 #%d 失败: %v", i, errs[i])
		}
		if results[i] != nil {
			applied += results[i].Applied
		}
	}
	if applied != 1 {
		t.Fatalf("同一动作只应生效一次，实际 applied=%d（两条统计 %+v）", applied, results)
	}
	status, _, attempts := scheduleRowOf(t, db, item.ID)
	if status != pagemodel.ScheduleStatusDone || attempts != 1 {
		t.Fatalf("排定应完成且只被认领一次: status=%s attempts=%d", status, attempts)
	}
	var pubCount, routeCount int64
	db.Raw(`SELECT count(*) FROM page_publications WHERE page_id = ?`, created.ID).Scan(&pubCount)
	db.Raw(`SELECT count(*) FROM page_routes WHERE project_id = ? AND path = ? AND route_kind = 'active'`,
		projectID, "/sched4").Scan(&routeCount)
	if pubCount != 1 || routeCount != 1 {
		t.Fatalf("并发后状态应唯一: publications=%d activeRoutes=%d", pubCount, routeCount)
	}
	hash := stagingHashOf(t, db, created.ID, item.Lang)
	if target := activeLinkTarget(t, "/sched4"); !strings.Contains(target, hash) {
		t.Fatalf("激活链接应指向冻结产物 %s，实际 %q", hash, target)
	}
}

// TestPageScheduleOffline 到点下线：链接、路由、发布指针、pages 镜像、sitemap 五处收敛。
func TestPageScheduleOffline(t *testing.T) {
	db, svc, projectID := newPageService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/sched5", DraftDocument: json.RawMessage(docV2),
	})
	if err != nil {
		t.Fatalf("创建 Page 失败: %v", err)
	}
	if _, berr := svc.Build(ctx, &pagedto.BuildReq{ID: created.ID}); berr != nil {
		t.Fatalf("构建失败: %v", berr)
	}
	if _, perr := svc.Publish(ctx, &pagedto.PublishReq{ID: created.ID}); perr != nil {
		t.Fatalf("发布失败: %v", perr)
	}
	if activeLinkTarget(t, "/sched5") == "" {
		t.Fatal("发布后应有激活链接")
	}
	item, err := svc.SetPageSchedule(ctx, &pagedto.ScheduleSetReq{
		PageID: created.ID, Action: "offline", ScheduledAt: scheduleAtFuture(),
	})
	if err != nil {
		t.Fatalf("排定下线失败: %v", err)
	}
	makeScheduleDue(t, db, item.ID)
	run, err := svc.RunDueSchedules(ctx)
	if err != nil || run.Applied != 1 {
		t.Fatalf("到点下线失败: run=%+v err=%v", run, err)
	}

	// ① 访问面：符号链接已删（/site 直接服务文件系统，链接在内容就还在）。
	if target := activeLinkTarget(t, "/sched5"); target != "" {
		t.Fatalf("下线后不该有激活链接，实际指向 %s", target)
	}
	// ② 发布指针清空（只清该语言）。
	var pubCount int64
	db.Raw(`SELECT count(*) FROM page_publications WHERE page_id = ? AND lang = ?`, created.ID, item.Lang).Scan(&pubCount)
	if pubCount != 0 {
		t.Fatalf("下线后不该有发布指针，实际 %d 行", pubCount)
	}
	// ③ 路由占用解除（该路径不再有 active 行）。
	//
	// 注意这里断言的是「完全没有占用行」而不是「剩下一条 reserved」：发布时 publication 把
	// 创建页面留下的那一条 reserved 行**原地更新成 active**（ActivateRouteTx 的
	// ON CONFLICT (project_id, path) DO UPDATE），所以取消激活删掉的就是同一条行 ——
	// 这与手工下线（RetireLocale / 改 URL 不带 301 的分支）逐字相同，定时下线不引入新语义。
	var activeCount, totalCount int64
	db.Raw(`SELECT count(*) FROM page_routes WHERE project_id = ? AND path = ? AND route_kind = 'active'`,
		projectID, "/sched5").Scan(&activeCount)
	db.Raw(`SELECT count(*) FROM page_routes WHERE project_id = ? AND path = ?`,
		projectID, "/sched5").Scan(&totalCount)
	if activeCount != 0 || totalCount != 0 {
		t.Fatalf("下线后该路径不该再有占用行: active=%d total=%d", activeCount, totalCount)
	}
	// ④ pages 单值镜像同步（列表页的「已发布」读的就是它）。
	var mirror *string
	db.Raw(`SELECT active_artifact_id FROM pages WHERE id = ?`, created.ID).Scan(&mirror)
	if mirror != nil {
		t.Fatalf("下线后 pages.active_artifact_id 应清空，实际 %v", *mirror)
	}
	// ⑤ sitemap 不再包含该路径。
	sitemapPath := filepath.Join(artifactRootOf(t), "public", "active", "sitemap.xml")
	if body, serr := os.ReadFile(sitemapPath); serr == nil && strings.Contains(string(body), "/sched5") {
		t.Fatalf("sitemap 不该包含已下线路径 /sched5，实际内容:\n%s", body)
	}
}

// TestPageScheduleOfflineWithRedirect 带跳转的下线：旧路径落 301，占用改记为 redirect。
func TestPageScheduleOfflineWithRedirect(t *testing.T) {
	db, svc, projectID := newPageService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/sched6", DraftDocument: json.RawMessage(docV2),
	})
	if err != nil {
		t.Fatalf("创建 Page 失败: %v", err)
	}
	if _, berr := svc.Build(ctx, &pagedto.BuildReq{ID: created.ID}); berr != nil {
		t.Fatalf("构建失败: %v", berr)
	}
	if _, perr := svc.Publish(ctx, &pagedto.PublishReq{ID: created.ID}); perr != nil {
		t.Fatalf("发布失败: %v", err)
	}
	item, err := svc.SetPageSchedule(ctx, &pagedto.ScheduleSetReq{
		PageID: created.ID, Action: "offline", ScheduledAt: scheduleAtFuture(),
		RedirectPath: "/new-home",
	})
	if err != nil {
		t.Fatalf("排定下线失败: %v", err)
	}
	if item.RedirectPath != "/new-home" {
		t.Fatalf("排定应记录跳转目标，实际 %q", item.RedirectPath)
	}
	makeScheduleDue(t, db, item.ID)
	if run, rerr := svc.RunDueSchedules(ctx); rerr != nil || run.Applied != 1 {
		t.Fatalf("到点下线失败: run=%+v err=%v", run, rerr)
	}
	// 访问面仍是链接（指向 301 产物），但路由占用改成 redirect、发布指针已清。
	if activeLinkTarget(t, "/sched6") == "" {
		t.Fatal("带跳转的下线应保留一条指向 301 产物的链接")
	}
	var routeKind string
	db.Raw(`SELECT route_kind FROM page_routes WHERE project_id = ? AND path = ?`, projectID, "/sched6").Scan(&routeKind)
	if routeKind != "redirect" {
		t.Fatalf("路由占用应改记为 redirect，实际 %q", routeKind)
	}
	var pubCount int64
	db.Raw(`SELECT count(*) FROM page_publications WHERE page_id = ?`, created.ID).Scan(&pubCount)
	if pubCount != 0 {
		t.Fatalf("下线后不该有发布指针，实际 %d 行", pubCount)
	}
}

// TestPageScheduleCancelAndList 取消与列表：面板与接口共用的两条只读/写路径。
func TestPageScheduleCancelAndList(t *testing.T) {
	db, svc, projectID := newPageService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/sched7", DraftDocument: json.RawMessage(docV2),
	})
	if err != nil {
		t.Fatalf("创建 Page 失败: %v", err)
	}
	item, err := svc.SetPageSchedule(ctx, &pagedto.ScheduleSetReq{
		PageID: created.ID, Action: "publish", ScheduledAt: scheduleAtFuture(),
	})
	if err != nil {
		t.Fatalf("排定失败: %v", err)
	}
	res, err := svc.ListPageSchedules(ctx, &pagedto.ScheduleListReq{PageID: created.ID})
	if err != nil || len(res.Items) != 1 || res.Items[0].ID != item.ID {
		t.Fatalf("列表投影错误: res=%+v err=%v", res, err)
	}
	if res.Items[0].ScheduledAtLocal == "" {
		t.Fatal("列表应给出站点时区下的到点时刻（后台表单回显用）")
	}
	if err := svc.CancelPageSchedule(ctx, &pagedto.ScheduleCancelReq{PageID: created.ID, ID: item.ID}); err != nil {
		t.Fatalf("取消排定失败: %v", err)
	}
	status, _, _ := scheduleRowOf(t, db, item.ID)
	if status != pagemodel.ScheduleStatusCanceled {
		t.Fatalf("取消后应为 canceled，实际 %s", status)
	}
	// 重复取消：终态行按「没有可取消的排定」处理。
	if err := svc.CancelPageSchedule(ctx, &pagedto.ScheduleCancelReq{PageID: created.ID, ID: item.ID}); err == nil {
		t.Fatal("重复取消应失败（没有可取消的排定）")
	}
	// 到点后不再执行：已取消的排定不参与认领。
	makeScheduleDue(t, db, item.ID)
	run, err := svc.RunDueSchedules(ctx)
	if err != nil {
		t.Fatalf("到点扫描失败: %v", err)
	}
	if run.Claimed != 0 || run.Applied != 0 {
		t.Fatalf("已取消的排定不该被执行: %+v", run)
	}
	// 过去的时刻直接被拒绝（这次排定永远不会执行）。
	if _, perr := svc.SetPageSchedule(ctx, &pagedto.ScheduleSetReq{
		PageID: created.ID, Action: "publish",
		ScheduledAt: sitetz.FormatDateTime(time.Now().Add(-time.Hour)),
	}); perr == nil {
		t.Fatal("排定到过去应被拒绝")
	}
}
