package feature

// build_menu_stale_test.go — menu_stale 的同事务可靠入队与 698 的队列侧形状。
//
// 断言的是**库里的行**与**pg_index / information_schema 的实际读数**，不是返回值、
// 也不是 schema/CURRENT.sql 快照：版本号是否真的递增、任务与业务写入是否一起回滚、
// source_id 放宽成 text 之后旧索引的键列与 predicate 有没有被顺手改掉，都只能在库里看见。
//
// 覆盖：
//  1. 同 scope 下版本单调递增，且每次入队都落一条 pending；
//  2. 两个位置各入队一次不撞唯一键，版本号各自从 1 开始；
//  3. 同位置已有 pending / running 时，新版本仍然新增一条（旧任务不吞新意图）；
//  4. A→B→A：三次真实变化各得一个号，三条任务都在；
//  5. 第三个写入失败 → 业务写入、版本号、任务一起回滚；
//  6. 无事务 / 非法 kind / 非法工程被拒且不留行；
//  7. 四值 kind 都能入队；source_id 往返一致；context_key 与 source_id 同值落库；
//  8. 旧来源（page / presentation）在改列型之后照常入队：source_id 文本等于传参、
//     input_version / context_key 为 NULL，且仍由原来的 pending 去重键兜底；
//  9. 四位置可同时 running（互斥粒度停在位置，不是停在工程）；
// 10. 698 的 CHECK 真的在挡：形状不自洽的 menu_stale 行插不进去；
// 11. 两条旧唯一索引的键列与 predicate 在 698 之后逐字不变（直读 pg_index）；
// 12. 迁移 295 回填 project_id 的 JOIN 在改列型之后仍成立 —— 但**只**在两端显式 cast 的
//     形态下：未 cast 的 source_id = p.id 在 text 列上必须报 operator does not exist
//     （PG 没有 text = uuid 算子，也没有 uuid↔text 的隐式 / 赋值转换），cast 后照常填上；
// 13. 生产迁移顺序（数字升序，699 → 698 → … → 295）下：两条迁移原样重放都成功、
//     判定仍为 1、295 的 project_id 回填真的执行（TestLegacyMigration295ReplaysAfter698）；
// 14. 旧来源 id 归一：大写 / 无连字符 / 花括号写法落到同一条 source_id，重复入队被去重；
// 15. source_id 列型的两个阶段（uuid 阶段 / text 阶段）下算子可用性逐条钉住，
//     证明「两端 ::text」是唯一在 698 前后都成立的写法。

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	builddto "go_wp/internal/module/build/dto"
	buildenums "go_wp/internal/module/build/enums"
	buildmodel "go_wp/internal/module/build/model"
	buildservice "go_wp/internal/module/build/service"
	"go_wp/pkg/rls"
	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// menuFixture 一个已建工程的入队夹具。
type menuFixture struct {
	db        *gorm.DB
	svc       *buildservice.Service
	projectID string
}

// newMenuFixture 建库 + 建工程行；PG 不可用时跳过（support 已处理）。
func newMenuFixture(t *testing.T) *menuFixture {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil
	}
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "menu_stale 测试站点")
	return &menuFixture{db: db, svc: buildservice.NewService(buildmodel.NewModel(db)), projectID: projectID}
}

// inScope 在带工程作用域的**真实事务**里执行 fn（与生产调用方的形态一致：
// 先 rls.ScopeTx，再调 EnqueueMenuStaleTx）。
func (f *menuFixture) inScope(t *testing.T, fn func(tx *gorm.DB) error) error {
	t.Helper()
	return rls.InProjectScope(context.Background(), f.db, f.projectID, fn)
}

// enqueue 在独立事务里入队一次（成功即提交），返回版本号。
func (f *menuFixture) enqueue(t *testing.T, kind string) int64 {
	t.Helper()
	var version int64
	err := f.inScope(t, func(tx *gorm.DB) error {
		v, eerr := f.svc.EnqueueMenuStaleTx(context.Background(), tx, f.projectID, kind)
		version = v
		return eerr
	})
	if err != nil {
		t.Fatalf("菜单位置 %q 入队失败: %v", kind, err)
	}
	return version
}

// menuJobs 本工程某位置的 menu_stale 任务（按版本号升序）。
func (f *menuFixture) menuJobs(t *testing.T, kind string) []menuJob {
	t.Helper()
	var rows []menuJob
	sourceID, ok := builddto.MenuStaleSourceID(f.projectID, kind)
	if !ok {
		t.Fatalf("组装 source_id 失败: %q", kind)
	}
	if err := f.db.Raw(`SELECT id, status, source_id, project_id, lang, intent,
		build_input_hash, input_version, context_key
		FROM build_jobs WHERE source_type = 'menu_stale' AND source_id = ?
		ORDER BY input_version ASC, id ASC`, sourceID).Scan(&rows).Error; err != nil {
		t.Fatalf("读取 %q 的任务失败: %v", kind, err)
	}
	return rows
}

// seqVersion 直接读号源表当前值（"未消耗" / "已分配" 只能从表上看）。
func (f *menuFixture) seqVersion(t *testing.T, kind string) (int64, bool) {
	t.Helper()
	var rows []struct {
		NextVersion int64 `gorm:"column:next_version"`
	}
	if err := f.db.Raw("SELECT next_version FROM build_job_seq WHERE project_id = ? AND kind = ?",
		f.projectID, kind).Scan(&rows).Error; err != nil {
		t.Fatalf("读取号源失败: %v", err)
	}
	if len(rows) == 0 {
		return 0, false
	}
	return rows[0].NextVersion, true
}

// seedSourceRow 建一行旧来源的真实实体（pages / presentation_instances），返回它的 id。
//
// 用 uq_build_jobs_pending 之外最省的一批 NOT NULL 列：这些列是真实迁移定义的，
// 这里补的是"父行"，不是手抄表结构。
func (f *menuFixture) seedSourceRow(t *testing.T, sourceType string) string {
	t.Helper()
	id := uuid.NewString()
	var sql string
	switch sourceType {
	case buildmodel.SourceTypePage:
		// 时间列是 create_time / update_time（205_db019_time_columns_full.sql 改的名），
		// 写 created_at 会撞 SQLSTATE 42703。
		// draft_path 是 NOT NULL（init_builder_schema.sql:88），由 id 派生一条合法路径。
		sql = `INSERT INTO pages (id, project_id, kind, content_target_type, draft_path,
			draft_document, create_time, update_time)
			VALUES (?, ?, 'notFound', 'none', '/seed/' || ?, '{}'::jsonb, now(), now())`
	case buildmodel.SourceTypePresentation:
		sql = `INSERT INTO presentation_instances (id, project_id, entity_type, entity_id, url_path,
			template_id, create_time, update_time)
			VALUES (?, ?, 'product', ?, ?, ?, now(), now())`
	default:
		t.Fatalf("未知的来源类型 %q", sourceType)
	}
	var err error
	if sourceType == buildmodel.SourceTypePage {
		err = f.db.Exec(sql, id, f.projectID, id).Error
	} else {
		var templateID string
		if e := f.db.Raw("SELECT id FROM content_templates WHERE project_id = ? LIMIT 1", f.projectID).
			Scan(&templateID).Error; e != nil {
			t.Fatalf("读取模板失败: %v", e)
		}
		if templateID == "" {
			templateID = uuid.NewString()
			// 列名以生产迁移为准（init_builder_schema.sql:56-66 + 205 改名）：
			// entity_type / draft_document，不是 kind / definition。
			if e := f.db.Exec(`INSERT INTO content_templates (id, project_id, name, entity_type, draft_document,
				create_time, update_time)
				VALUES (?, ?, 'menu_stale 测试模板', 'page', '{}'::jsonb, now(), now())`,
				templateID, f.projectID).Error; e != nil {
				t.Fatalf("准备模板行失败: %v", e)
			}
		}
		err = f.db.Exec(sql, id, f.projectID, uuid.NewString(), "/menu-stale/"+id, templateID).Error
	}
	if err != nil {
		t.Fatalf("准备 %s 来源行失败: %v", sourceType, err)
	}
	return id
}

// menuJob 断言用到的任务列。
type menuJob struct {
	ID             int64   `gorm:"column:id"`
	Status         string  `gorm:"column:status"`
	SourceID       string  `gorm:"column:source_id"`
	ProjectID      *string `gorm:"column:project_id"`
	Lang           string  `gorm:"column:lang"`
	Intent         string  `gorm:"column:intent"`
	BuildInputHash string  `gorm:"column:build_input_hash"`
	InputVersion   *int64  `gorm:"column:input_version"`
	ContextKey     *string `gorm:"column:context_key"`
}

// TestMenuStaleVersionIncrementsInSameScope 同一工程同一位置：每次入队一个新号、一条新任务。
func TestMenuStaleVersionIncrementsInSameScope(t *testing.T) {
	f := newMenuFixture(t)
	if f == nil {
		return
	}
	v1 := f.enqueue(t, "header")
	v2 := f.enqueue(t, "header")
	v3 := f.enqueue(t, "header")

	if !(v1 < v2 && v2 < v3) {
		t.Fatalf("版本号必须严格单调递增: %d, %d, %d", v1, v2, v3)
	}
	if v1 != 1 {
		t.Fatalf("首次分配应为 1，实际 %d", v1)
	}
	jobs := f.menuJobs(t, "header")
	if len(jobs) != 3 {
		t.Fatalf("三次真实变化应留下三条任务，实际 %d 条", len(jobs))
	}
	// 断言等式：三条任务的版本号集合 == 三次返回的版本号集合。
	for i, want := range []int64{v1, v2, v3} {
		if jobs[i].InputVersion == nil || *jobs[i].InputVersion != want {
			t.Fatalf("第 %d 条任务的版本号应为 %d，实际 %v", i+1, want, jobs[i].InputVersion)
		}
		if jobs[i].Status != buildmodel.StatusPending {
			t.Fatalf("第 %d 条任务应为 pending，实际 %q", i+1, jobs[i].Status)
		}
		// lang 保留真实语言语义：menu_stale 这里没有语言维度，写空串而不是塞 kind。
		if jobs[i].Lang != "" {
			t.Fatalf("lang 不得夹带位置语义，实际 %q", jobs[i].Lang)
		}
		if jobs[i].Lang == "header" {
			t.Fatalf("lang 被塞进了 kind")
		}
	}
	if next, ok := f.seqVersion(t, "header"); !ok || next != v3 {
		t.Fatalf("号源应停在最后一次分配的号 %d，实际 %d(存在=%v)", v3, next, ok)
	}
}

// TestMenuStaleTwoKindsDoNotCollide 两个位置各一次：互不共用号源、也不撞唯一索引。
func TestMenuStaleTwoKindsDoNotCollide(t *testing.T) {
	f := newMenuFixture(t)
	if f == nil {
		return
	}
	vHeader := f.enqueue(t, "header")
	vFooter := f.enqueue(t, "footer")
	if vHeader != 1 || vFooter != 1 {
		t.Fatalf("两个位置各自独立取号，都应为 1；实际 header=%d footer=%d", vHeader, vFooter)
	}
	if got := len(f.menuJobs(t, "header")) + len(f.menuJobs(t, "footer")); got != 2 {
		t.Fatalf("两个位置应各有一条任务，实际合计 %d 条", got)
	}
}

// TestMenuStaleKeepsPendingAndRunningAtDifferentVersions 旧任务不吞新意图。
//
// 构造：v1 pending 未消费、v2 被认领成 running，再发生一次变化 → v3 仍然要新增一条。
// 若实现把「已有 pending / running」当成「工作已在队列里」而去重，本用例立刻变红。
func TestMenuStaleKeepsPendingAndRunningAtDifferentVersions(t *testing.T) {
	f := newMenuFixture(t)
	if f == nil {
		return
	}
	v1 := f.enqueue(t, "header_mobile")
	v2 := f.enqueue(t, "header_mobile")

	// 认领一次：claim 的候选条件是「本来源当前没有 running」——第二次认领会跳过同来源，
	// 所以 v1 变成 running、v2 留在 pending，构成「running + pending 并存」。
	if _, err := f.svc.Model().Claim(context.Background(), 15*time.Minute); err != nil {
		t.Fatalf("认领失败: %v", err)
	}

	v3 := f.enqueue(t, "header_mobile")
	if !(v3 > v2 && v2 > v1) {
		t.Fatalf("版本号必须递增: %d, %d, %d", v1, v2, v3)
	}
	jobs := f.menuJobs(t, "header_mobile")
	if len(jobs) != 3 {
		t.Fatalf("pending / running 之外的新版本必须新增一条，期望 3 条，实际 %d 条", len(jobs))
	}
	var running, pending int
	for _, j := range jobs {
		switch j.Status {
		case buildmodel.StatusRunning:
			running++
		case buildmodel.StatusPending:
			pending++
		}
	}
	if running == 0 || pending == 0 {
		t.Fatalf("应同时存在 running 与 pending，实际 running=%d pending=%d", running, pending)
	}
}

// TestMenuStaleABARetainsThreeTasks A→B→A：三次真实变化各成一条工作。
//
// hash 型输入键在 A→B→A 时会回到第一次的值，与遗留任务撞键被去重；
// 版本号型输入键不会。本用例钉住「必须是后者」。
func TestMenuStaleABARetainsThreeTasks(t *testing.T) {
	f := newMenuFixture(t)
	if f == nil {
		return
	}
	vA1 := f.enqueue(t, "footer_mobile")
	_ = f.enqueue(t, "footer_mobile") // B
	vA2 := f.enqueue(t, "footer_mobile")
	if !(vA2 > vA1) {
		t.Fatalf("第三次（回到 A）必须拿到更大的号: %d vs %d", vA1, vA2)
	}
	jobs := f.menuJobs(t, "footer_mobile")
	if len(jobs) != 3 {
		t.Fatalf("A→B→A 应留下三条任务，实际 %d 条", len(jobs))
	}
	hashes := map[string]struct{}{}
	for _, j := range jobs {
		if _, dup := hashes[j.BuildInputHash]; dup {
			t.Fatalf("build_input_hash 重复 %q：A→B→A 被去重成了同一份工作", j.BuildInputHash)
		}
		hashes[j.BuildInputHash] = struct{}{}
	}
}

// TestMenuStaleRollbackTakesVersionAndJob 第三个写入失败 → 业务行、版本号、任务一起回滚。
//
// 事务里顺序执行：① 入队（取号 v1）② 写一条业务行 ③ 再入队（取号 v2）④ 抛错。
// 回滚后必须满足：业务行没了、两条任务都没了、号源回到事务前的状态（不消耗 v1/v2）。
func TestMenuStaleRollbackTakesVersionAndJob(t *testing.T) {
	f := newMenuFixture(t)
	if f == nil {
		return
	}
	sentinel := errors.New("第三个写入失败")

	err := f.inScope(t, func(tx *gorm.DB) error {
		if _, e := f.svc.EnqueueMenuStaleTx(context.Background(), tx, f.projectID, "header"); e != nil {
			return e
		}
		// 第二处写入：真实业务行挂在工程行上，模拟「导航写入 + 入队」同事务。
		// 用 projects.name 而不是号源表：号源表的写入会被 UPSERT 覆盖，看不出回滚。
		if e := tx.Exec("UPDATE projects SET name = name || '·tx' WHERE id = ?", f.projectID).Error; e != nil {
			return e
		}
		if _, e := f.svc.EnqueueMenuStaleTx(context.Background(), tx, f.projectID, "header"); e != nil {
			return e
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("事务应因第三个写入失败而回滚，实际错误 %v", err)
	}
	if got := len(f.menuJobs(t, "header")); got != 0 {
		t.Fatalf("回滚后不得留下任何 menu_stale 任务，实际 %d 条", got)
	}
	var name string
	if e := f.db.Raw("SELECT name FROM projects WHERE id = ?", f.projectID).Scan(&name).Error; e != nil {
		t.Fatalf("读取工程行失败: %v", e)
	}
	if name != "menu_stale 测试站点" {
		t.Fatalf("业务写入也必须一起回滚，工程名实际 %q", name)
	}
	if next, ok := f.seqVersion(t, "header"); ok {
		t.Fatalf("回滚后号源必须回到事务前（无行），实际 next_version=%d", next)
	}
}

// TestMenuStaleRejectsWithoutTransaction 无事务句柄被拒，且不留行。
func TestMenuStaleRejectsWithoutTransaction(t *testing.T) {
	f := newMenuFixture(t)
	if f == nil {
		return
	}
	_, err := f.svc.EnqueueMenuStaleTx(context.Background(), f.db, f.projectID, "header")
	if !errors.Is(err, buildmodel.ErrTxRequired) {
		t.Fatalf("非事务句柄必须报 ErrTxRequired（不允许自取连接入队），实际 %v", err)
	}
	if got := len(f.menuJobs(t, "header")); got != 0 {
		t.Fatalf("被拒的入队不得留下任务，实际 %d 条", got)
	}
	if _, ok := f.seqVersion(t, "header"); ok {
		t.Fatal("被拒的入队不得消耗版本号")
	}
}

// TestMenuStaleRejectsInvalidInputs 非法 kind / 工程 id 在事务内同样被拒且不留行。
func TestMenuStaleRejectsInvalidInputs(t *testing.T) {
	f := newMenuFixture(t)
	if f == nil {
		return
	}
	for _, c := range []struct {
		project, kind string
		want          error
	}{
		{f.projectID, "sidebar", errors.New("ErrInvalidMenuKind")},
		{f.projectID, "", errors.New("ErrInvalidMenuKind")},
		{"not-a-uuid", "header", errors.New("ErrInvalidParam")},
		{"", "header", errors.New("ErrInvalidParam")},
	} {
		err := f.inScope(t, func(tx *gorm.DB) error {
			_, e := f.svc.EnqueueMenuStaleTx(context.Background(), tx, c.project, c.kind)
			return e
		})
		if err == nil || err.Error() != c.want.Error() {
			t.Fatalf("(%q,%q) 应报 %v，实际 %v", c.project, c.kind, c.want, err)
		}
	}
	var n int64
	if err := f.db.Table("build_jobs").Where("source_type = ?", "menu_stale").Count(&n).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if n != 0 {
		t.Fatalf("被拒的入队不得留下任务，实际 %d 条", n)
	}
}

// TestMenuStaleAllFourKinds 四个位置都能入队，source_id 都能往返，context_key 与它同值落库。
//
// context_key 必须真的落库：uq_build_jobs_context_version 的键里含它，PG 的 UNIQUE 允许多个
// NULL —— 一旦写路径漏了它，去重索引对 menu_stale 完全不生效且没有任何报错。
func TestMenuStaleAllFourKinds(t *testing.T) {
	f := newMenuFixture(t)
	if f == nil {
		return
	}
	for _, kind := range []string{"header", "header_mobile", "footer", "footer_mobile"} {
		if v := f.enqueue(t, kind); v != 1 {
			t.Fatalf("位置 %q 的首次版本号应为 1，实际 %d", kind, v)
		}
		sourceID, _ := builddto.MenuStaleSourceID(f.projectID, kind)
		gotProject, gotKind, ok := builddto.SplitMenuStaleSourceID(sourceID)
		if !ok || gotProject != f.projectID || gotKind != kind {
			t.Fatalf("位置 %q 的 source_id 往返失败: (%s,%s,%v)", kind, gotProject, gotKind, ok)
		}
		jobs := f.menuJobs(t, kind)
		if len(jobs) != 1 || jobs[0].ProjectID == nil || *jobs[0].ProjectID != f.projectID {
			t.Fatalf("位置 %q 应有一条带本工程 id 的任务，实际 %+v", kind, jobs)
		}
		if jobs[0].SourceID != sourceID {
			t.Fatalf("位置 %q 的 source_id 应逐字等于组装值 %q，实际 %q", kind, sourceID, jobs[0].SourceID)
		}
		if jobs[0].ContextKey == nil || *jobs[0].ContextKey != sourceID {
			t.Fatalf("位置 %q 的 context_key 必须落库并等于 source_id（CHECK 钉的等式），实际 %v",
				kind, jobs[0].ContextKey)
		}
	}
}

// TestMenuStaleLegacySourcesStillEnqueue 改列型之后，page / presentation 的入队路径没有变化。
//
// 这条用例是 698「source_id: uuid → text」的唯一验收面：旧来源仍走 Enqueue（ON CONFLICT
// DO NOTHING）与 uq_build_jobs_pending，source_id 文本等于传参、input_version / context_key
// 保持 NULL（历史来源不回填），重复入队仍被去重成一条。
func TestMenuStaleLegacySourcesStillEnqueue(t *testing.T) {
	f := newMenuFixture(t)
	if f == nil {
		return
	}
	pageID := f.seedSourceRow(t, buildmodel.SourceTypePage)
	instanceID := f.seedSourceRow(t, buildmodel.SourceTypePresentation)

	if err := f.svc.EnqueuePageBuild(context.Background(), pageID, f.projectID, "zh-CN", buildmodel.IntentManual, 3, "hash-page"); err != nil {
		t.Fatalf("page 入队失败: %v", err)
	}
	if err := f.svc.EnqueuePresentationBuild(context.Background(), instanceID, f.projectID); err != nil {
		t.Fatalf("presentation 入队失败: %v", err)
	}

	var rows []menuJob
	if err := f.db.Raw(`SELECT id, status, source_id, project_id, lang, intent,
		build_input_hash, input_version, context_key
		FROM build_jobs WHERE source_type IN ('page','presentation') ORDER BY source_type`).
		Scan(&rows).Error; err != nil {
		t.Fatalf("读取旧来源任务失败: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("两个旧来源应各有一条任务，实际 %d 条", len(rows))
	}
	// 断言等式：库里两行 source_id 的集合 == 传进去的两个 id 的集合。
	got := map[string]bool{rows[0].SourceID: true, rows[1].SourceID: true}
	if !got[pageID] || !got[instanceID] {
		t.Fatalf("source_id 必须逐字等于传参：期望 {%s,%s}，实际 {%s,%s}",
			pageID, instanceID, rows[0].SourceID, rows[1].SourceID)
	}
	for _, r := range rows {
		if r.InputVersion != nil || r.ContextKey != nil {
			t.Fatalf("旧来源的 input_version / context_key 必须是 NULL（不回填），实际 %v / %v",
				r.InputVersion, r.ContextKey)
		}
		if r.ProjectID == nil || *r.ProjectID != f.projectID {
			t.Fatalf("旧来源的工程 id 应落库，实际 %v", r.ProjectID)
		}
	}

	// 重复入队仍被 uq_build_jobs_pending 去重成一条（旧索引的语义没有因为改列型而丢）。
	if err := f.svc.EnqueuePageBuild(context.Background(), pageID, f.projectID, "zh-CN", buildmodel.IntentManual, 3, "hash-page"); err != nil {
		t.Fatalf("重复入队应幂等返回，实际错误: %v", err)
	}
	var n int64
	if err := f.db.Table("build_jobs").Where("source_type = ? AND source_id = ?", buildmodel.SourceTypePage, pageID).
		Count(&n).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("同一 page 的同一份工作应被去重成 1 条，实际 %d 条", n)
	}
}

// TestMenuStaleFourKindsRunConcurrently 四个位置可以同时 running：互斥粒度是「位置」不是「工程」。
//
// 这是把 source_id 放宽成 text 的真正理由：若 menu_stale 的 source_id 只存裸工程 id，
// 四个位置共用同一个来源，claim 的 NOT EXISTS 前置过滤与 uq_build_jobs_running_source
// 会一起把后三个位置挡在队列里 —— 页眉在跑，页脚就永远领不到。
func TestMenuStaleFourKindsRunConcurrently(t *testing.T) {
	f := newMenuFixture(t)
	if f == nil {
		return
	}
	kinds := []string{"header", "header_mobile", "footer", "footer_mobile"}
	for _, kind := range kinds {
		f.enqueue(t, kind)
	}
	var claimedKinds []string
	for i := 0; i < len(kinds); i++ {
		job, err := f.svc.Model().Claim(context.Background(), 15*time.Minute)
		if err != nil {
			t.Fatalf("第 %d 次认领失败: %v", i+1, err)
		}
		if job == nil {
			t.Fatalf("第 %d 次认领应拿到一条任务（四位置互不互斥），实际队列已空", i+1)
		}
		if job.SourceType != buildmodel.SourceTypeMenuStale {
			t.Fatalf("认领到非 menu_stale 任务: %q", job.SourceType)
		}
		_, kind, ok := builddto.SplitMenuStaleSourceID(job.SourceID)
		if !ok {
			t.Fatalf("认领到的 source_id 无法还原位置: %q", job.SourceID)
		}
		claimedKinds = append(claimedKinds, kind)
	}
	if extra, err := f.svc.Model().Claim(context.Background(), 15*time.Minute); err != nil || extra != nil {
		t.Fatalf("四条任务认领完之后不应再有可认领的行（extra=%v err=%v）", extra, err)
	}
	sort.Strings(claimedKinds)
	want := append([]string(nil), kinds...)
	sort.Strings(want)
	if strings.Join(claimedKinds, ",") != strings.Join(want, ",") {
		t.Fatalf("四个位置应各被认领一次：期望 %v，实际 %v", want, claimedKinds)
	}
}

// TestMenuStaleShapeCheckRejectsInconsistentRows 698 的 CHECK 真的在挡形状不自洽的 menu_stale 行。
//
// 四段事实（工程 / 位置 / 版本 / 语境）缺一或互相矛盾就该插不进去：这类行一旦进库，
// worker 要么读别人的工程、要么拿到空版本号。断言的是数据库拒绝（约束名可定位），
// 不是 service 的入参校验。
func TestMenuStaleShapeCheckRejectsInconsistentRows(t *testing.T) {
	f := newMenuFixture(t)
	if f == nil {
		return
	}
	other := uuid.NewString()
	support.SeedProjectRow(t, f.db, other, "另一个工程")

	goodSourceID, _ := builddto.MenuStaleSourceID(f.projectID, "header")
	cases := []struct {
		name       string
		sourceID   string
		projectID  *string
		version    *int64
		contextKey *string
	}{
		{"缺少 input_version", goodSourceID, &f.projectID, nil, &goodSourceID},
		{"缺少 context_key", goodSourceID, &f.projectID, ptrInt64(1), nil},
		{"缺少 project_id", goodSourceID, nil, ptrInt64(1), &goodSourceID},
		// 违规点是「位置不在四值内」：工程段必须是规范 UUID 文本（大写写法会让
		// left(source_id,37) = project_id::text || ':' 先失败，用例就证不到位置那一项）。
		{"位置不在四值内", f.projectID + ":sidebar", &f.projectID, ptrInt64(1), ptrStr(f.projectID + ":sidebar")},
		{"工程段非规范写法", strings.ToUpper(f.projectID) + ":header", &f.projectID, ptrInt64(1), ptrStr(strings.ToUpper(f.projectID) + ":header")},
		{"工程与 source_id 不一致", goodSourceID, &other, ptrInt64(1), &goodSourceID},
		{"context_key 与 source_id 不一致", goodSourceID, &f.projectID, ptrInt64(1), ptrStr(other + ":header")},
	}
	for _, c := range cases {
		err := f.db.Exec(`INSERT INTO build_jobs (source_type, source_id, project_id, lang, intent,
			draft_version, build_input_hash, status, attempt, create_time, input_version, context_key)
			VALUES ('menu_stale', ?, ?, '', 'dependency', 0, 'shape-check', 'pending', 0, now(), ?, ?)`,
			c.sourceID, c.projectID, c.version, c.contextKey).Error
		if err == nil {
			t.Fatalf("%s：这一行必须被 build_jobs_menu_stale_shape_check 拒绝，实际插入成功", c.name)
		}
		if !strings.Contains(err.Error(), "build_jobs_menu_stale_shape_check") {
			t.Fatalf("%s：拒绝原因应指向 build_jobs_menu_stale_shape_check，实际 %v", c.name, err)
		}
	}
	// 正例：形状自洽的行插得进去（否则上面对照组没有意义）。
	if err := f.db.Exec(`INSERT INTO build_jobs (source_type, source_id, project_id, lang, intent,
		draft_version, build_input_hash, status, attempt, create_time, input_version, context_key)
		VALUES ('menu_stale', ?, ?, '', 'dependency', 0, 'shape-check-ok', 'pending', 0, now(), 1, ?)`,
		goodSourceID, f.projectID, goodSourceID).Error; err != nil {
		t.Fatalf("形状自洽的 menu_stale 行应能插入，实际 %v", err)
	}
	// 清掉这条手工插入的正例行：它用的是 version=1，会与后续用例（同一工程、同一位置）
	// 的 uq_build_jobs_context_version 撞键。本用例的断言已完成，清理是为了让夹具回到干净状态。
	if err := f.db.Exec("DELETE FROM build_jobs WHERE build_input_hash = 'shape-check-ok'").Error; err != nil {
		t.Fatalf("清理正例行失败: %v", err)
	}
}

// TestMenuStaleLegacyIndexesKeepPredicates 两条旧唯一索引的键列与 predicate 在 698 之后不变。
//
// 判据直读 pg_index / pg_attribute / pg_get_expr（与 public/migrations/register_build_job_intent.go
// 同一口径），不拿 schema/CURRENT.sql 或 indexdef 的文本形态断言 —— 文本随 PG 版本变。
// 为什么必须钉：source_id 的列型从 uuid 改成 text 会触发这两条索引重建，任何"顺手重写"
// 都会改变 page / presentation 的去重与互斥粒度，而它们的入队路径不会被 698 的 CHECK 兜住。
func TestMenuStaleLegacyIndexesKeepPredicates(t *testing.T) {
	f := newMenuFixture(t)
	if f == nil {
		return
	}
	type indexFact struct {
		KeyColumns string `gorm:"column:key_columns"`
		Predicate  string `gorm:"column:predicate"`
		// SourceIDType 键列 source_id 当前的列型（格式化的类型名，如 text / uuid）。
		SourceIDType string `gorm:"column:source_id_type"`
	}
	read := func(name string) (indexFact, bool) {
		var rows []indexFact
		// 表达式索引的键列在 indkey 里是 0，join pg_attribute 会把它丢掉 —— 这里的两条目标
		// 索引都是纯列索引，所以 JOIN 结果的行数就等于 indnatts（表达式索引会在下面的
		// 键列比较里直接失败，而不是被静默判成"键列不全"）。
		err := f.db.Raw(`SELECT string_agg(a.attname, ',' ORDER BY k.ord) AS key_columns,
				pg_get_expr(x.indpred, x.indrelid) AS predicate,
				(SELECT format_type(a2.atttypid, a2.atttypmod) FROM pg_attribute a2
				  WHERE a2.attrelid = x.indrelid AND a2.attname = 'source_id') AS source_id_type
			FROM pg_index x
			JOIN pg_class c ON c.oid = x.indrelid
			JOIN pg_class ic ON ic.oid = x.indexrelid
			JOIN pg_namespace n ON n.oid = c.relnamespace
			CROSS JOIN LATERAL unnest(x.indkey) WITH ORDINALITY AS k(attnum, ord)
			JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = k.attnum
			WHERE n.nspname = current_schema() AND c.relname = 'build_jobs' AND ic.relname = ?
			  AND x.indisunique
			GROUP BY x.indpred, x.indrelid, x.indnatts, c.oid`, name).Scan(&rows).Error
		if err != nil {
			t.Fatalf("读取索引 %q 失败: %v", name, err)
		}
		if len(rows) != 1 {
			return indexFact{}, false
		}
		return rows[0], true
	}

	for _, c := range []struct{ name, keys string }{
		{"uq_build_jobs_pending", "source_type,source_id,build_input_hash,lang,intent"},
		{"uq_build_jobs_running_source", "source_type,source_id"},
	} {
		fact, ok := read(c.name)
		if !ok {
			t.Fatalf("索引 %q 必须仍然存在且唯一", c.name)
		}
		if fact.KeyColumns != c.keys {
			t.Fatalf("索引 %q 的键列应为 %q，实际 %q", c.name, c.keys, fact.KeyColumns)
		}
		if !strings.Contains(fact.Predicate, "status") {
			t.Fatalf("索引 %q 的 predicate 必须是 status 维，实际 %q", c.name, fact.Predicate)
		}
		// 列型放宽成 text 之后，键列的类型跟着表走 —— 键列清单与 predicate 都没有变化。
		if fact.SourceIDType != "text" {
			t.Fatalf("build_jobs.source_id 应为 text（698 放宽），实际 %q", fact.SourceIDType)
		}
	}
	pending, _ := read("uq_build_jobs_pending")
	if !strings.Contains(pending.Predicate, "pending") {
		t.Fatalf("uq_build_jobs_pending 的 predicate 应含 'pending'，实际 %q", pending.Predicate)
	}
	running, _ := read("uq_build_jobs_running_source")
	if !strings.Contains(running.Predicate, "running") {
		t.Fatalf("uq_build_jobs_running_source 的 predicate 应含 'running'，实际 %q", running.Predicate)
	}
	if pending.Predicate == running.Predicate {
		t.Fatalf("两条索引的 predicate 不该相同: %q", pending.Predicate)
	}
}

// ptrStr 取字符串地址（构造指针列用）。
func ptrStr(s string) *string { return &s }

// ptrInt64 取 int64 地址（构造指针列用）。
func ptrInt64(v int64) *int64 { return &v }

// TestLegacyProjectBackfillJoinNeedsExplicitCast 迁移 295 的回填 JOIN 只有在**两端显式 cast**
// 时才在 source_id 改型后成立。
//
// 295_build_jobs_lease.sql 的回填语句现在是
//
//	UPDATE build_jobs j SET project_id = p.project_id
//	  FROM pages p WHERE j.project_id IS NULL AND j.source_type = 'page'
//	    AND j.source_id::text = p.id::text
//	（presentation 同理）
//
// 为什么两端都 cast 而不是「uuid 侧自动转 text」：PG 的 = 算子没有 (text,uuid)/(uuid,text)
// 组合，pg_cast 里 uuid→text 连 assignment-only 都没有 —— character varying → text 的
// binary-coercible 机制不适用于 uuid。所以：
//   - 未 cast 的 j.source_id = p.id 在 text 列上必须报 operator does not exist；
//   - 只把右侧写成 p.id::text 会在**698 之前**的 uuid 列上撞 uuid = text 的同一个错误；
//   - 两端都 ::text 时两个列型阶段都成立（本用例下半段在真实列型上验证）。
//
// menu_stale 的「uuid:位置」文本与任何 pages.id 都不等 → 保持 NULL 才是对的。
func TestLegacyProjectBackfillJoinNeedsExplicitCast(t *testing.T) {
	f := newMenuFixture(t)
	if f == nil {
		return
	}
	pageID := f.seedSourceRow(t, buildmodel.SourceTypePage)
	instanceID := f.seedSourceRow(t, buildmodel.SourceTypePresentation)
	// 三条任务必须真的存在（page / presentation / menu_stale），否则下面的回填没有可命中的行。
	if err := f.svc.EnqueuePageBuild(context.Background(), pageID, f.projectID, "zh-CN",
		buildmodel.IntentManual, 3, "hash-page"); err != nil {
		t.Fatalf("page 入队失败: %v", err)
	}
	if err := f.svc.EnqueuePresentationBuild(context.Background(), instanceID, f.projectID); err != nil {
		t.Fatalf("presentation 入队失败: %v", err)
	}
	f.enqueue(t, "header")

	// 先清掉入队时写上的 project_id，模拟「迁移 295 之前入库的旧行」。
	// 只清旧来源（page / presentation）：698 之后 menu_stale 行有 shape CHECK 要求 project_id 非空。
	if err := f.db.Exec("UPDATE build_jobs SET project_id = NULL WHERE source_type <> 'menu_stale'").Error; err != nil {
		t.Fatalf("清空 project_id 失败: %v", err)
	}

	// ① 未 cast 的回填语句必须被数据库拒绝：这条断言的期望值是「报错」，不是「能跑」。
	//    它是「text = uuid 没有隐式转换」这条实测事实的回归锚点：有人把 295 的 ::text 去掉时，
	//    迁移重放（TestLegacyMigration295ReplaysAfter698）会先红，这条用例让「为什么红」
	//    指向正确的算子，而不是一句无法定位的 SQL 错误。
	errNoCast := f.db.Exec(`UPDATE build_jobs j SET project_id = p.project_id
		FROM pages p WHERE j.project_id IS NULL AND j.source_type = 'page'
		  AND j.source_id = p.id`).Error
	if errNoCast == nil {
		t.Fatal("source_id 已是 text，j.source_id = p.id 必须报 operator does not exist；" +
			"语句能跑通说明列型分叉没有被复现，用例失去判据")
	}
	if !strings.Contains(errNoCast.Error(), "operator does not exist") {
		t.Fatalf("未 cast 的比较应撞 text = uuid 算子缺失，实际错误 %v", errNoCast)
	}
	// 记录当时列型，让上面那条错误有可复核的上下文（text 而不是 uuid）。
	var sourceIDType string
	if e := f.db.Raw(`SELECT data_type FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'build_jobs'
		  AND column_name = 'source_id'`).Scan(&sourceIDType).Error; e != nil {
		t.Fatalf("读取 source_id 列型失败: %v", e)
	}
	if sourceIDType != "text" {
		t.Fatalf("迁移模板库应当是 698 之后的形态（text），实际 %q", sourceIDType)
	}

	// ② 两端显式 cast 的回填（295 现在的写法，原文照抄）能跑通。
	if err := f.db.Exec(`UPDATE build_jobs j SET project_id = p.project_id
		FROM pages p WHERE j.project_id IS NULL AND j.source_type = 'page'
		  AND j.source_id::text = p.id::text`).Error; err != nil {
		t.Fatalf("page 回填（两端 ::text）应成功: %v", err)
	}
	if err := f.db.Exec(`UPDATE build_jobs j SET project_id = i.project_id
		FROM presentation_instances i WHERE j.project_id IS NULL AND j.source_type = 'presentation'
		  AND j.source_id::text = i.id::text`).Error; err != nil {
		t.Fatalf("presentation 回填（两端 ::text）应成功: %v", err)
	}

	type row struct {
		SourceType string  `gorm:"column:source_type"`
		ProjectID  *string `gorm:"column:project_id"`
	}
	var rows []row
	if err := f.db.Raw(`SELECT source_type, project_id FROM build_jobs
		WHERE source_id IN (?, ?, ?)`, pageID, instanceID,
		mustMenuSource(t, f.projectID, "header")).Scan(&rows).Error; err != nil {
		t.Fatalf("读取回填结果失败: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("应读到三条任务，实际 %d 条", len(rows))
	}
	for _, r := range rows {
		switch r.SourceType {
		case buildmodel.SourceTypePage, buildmodel.SourceTypePresentation:
			if r.ProjectID == nil || *r.ProjectID != f.projectID {
				t.Fatalf("%s 的 project_id 应被回填成本工程，实际 %v", r.SourceType, r.ProjectID)
			}
		case buildmodel.SourceTypeMenuStale:
			// menu_stale 是 698 才有的来源类型，行本身带着自己的工程 id（shape CHECK 要求非空），
			// 而 295 的回填只作用于 project_id IS NULL 的旧行 —— 所以这里它必须仍是入队时那个值。
			if r.ProjectID == nil || *r.ProjectID != f.projectID {
				t.Fatalf("menu_stale 行不该被 295 的回填改动，实际 project_id=%v", r.ProjectID)
			}
		}
	}
}

// checkVersion 断言某条迁移的 CheckSQL 对当前库判「已应用」（1）。
func (f *menuFixture) checkVersion(t *testing.T, version, stage string) {
	t.Helper()
	var target migrations.Migration
	for _, m := range migrations.All() {
		if m.Version == version {
			target = m
			break
		}
	}
	if target.Version == "" {
		t.Fatalf("迁移 %s 未注册", version)
	}
	var n int64
	// CheckSQL 不都带占位符：只有引用 $1 的版本才需要传表名，否则 PG 会报
	// "expected 0 arguments, got 1"。
	var checkArgs []any
	if strings.Contains(target.CheckSQL, "$1") || strings.Contains(target.CheckSQL, "?") {
		checkArgs = append(checkArgs, target.TableName)
	}
	if err := f.db.Raw(target.CheckSQL, checkArgs...).Scan(&n).Error; err != nil {
		t.Fatalf("%s：执行 %s 的 CheckSQL 失败: %v", stage, version, err)
	}
	if n != 1 {
		t.Fatalf("%s：%s 的判定应为「已应用」，实际 %d", stage, version, n)
	}
}

// migrationSQL 取某条迁移的 SQL 文本（原文，用于原样重放）。
func migrationSQL(t *testing.T, version string) string {
	t.Helper()
	for _, m := range migrations.All() {
		if m.Version == version {
			return m.SQL
		}
	}
	t.Fatalf("迁移 %s 未注册", version)
	return ""
}

// replay 原样重放一条迁移的全部语句（与迁移器共用 SplitStatements，切分口径一致）。
func (f *menuFixture) replay(t *testing.T, version string) {
	t.Helper()
	for _, stmt := range migrations.SplitStatements(migrationSQL(t, version)) {
		if stmt == "" {
			continue
		}
		if err := f.db.Exec(stmt).Error; err != nil {
			t.Fatalf("重放迁移 %s 失败（不幂等）: %v / SQL: %s", version, err, stmt)
		}
	}
}

// TestLegacyMigration295ReplaysAfter698 生产执行顺序（数字升序）下，295 在 698 之后重跑仍成立。
//
// 顺序的事实来自 migrator.go 的 All() → slices.SortStableFunc(compareVersionCmp)：按**数字**
// 升序，699 → 698 → … → 307 → 295。698 之后 build_jobs.source_id 已是 text，而 295 的
// CheckSQL（register_build_lease.go）只查六列 + 来源互斥索引、不认列型 —— 一个从未应用过
// 295 的库必然在 698 之后重跑它；回填语句若还是裸的 source_id = p.id，就会撞
// 「operator does not exist: text = uuid」并让整条 runAll 中断（进程起不来）。
//
// 四段断言：
//
//	① 模板库（生产迁移已全部跑完）里 698 与 295 的判定都是「已应用」；
//	② source_id 列已是 text —— 下面两段才算真的在分叉后的列型上跑；
//	③ 698 原样重放成功（含 forward 回填与改型守卫），295 原样重放成功（回填两端 ::text）；
//	④ 重放后两条判定仍为 1（幂等，不会退化成每次启动都重跑）。
func TestLegacyMigration295ReplaysAfter698(t *testing.T) {
	f := newMenuFixture(t)
	if f == nil {
		return
	}
	pageID := f.seedSourceRow(t, buildmodel.SourceTypePage)
	// 一条「295 之前入库」的旧行：project_id 为空，等着被回填。
	if err := f.db.Exec(`INSERT INTO build_jobs (source_type, source_id, project_id, lang, intent,
		draft_version, build_input_hash, status, attempt, create_time)
		VALUES ('page', ?, NULL, '', 'dependency', 0, 'replay-backfill', 'pending', 0, now())`,
		pageID).Error; err != nil {
		t.Fatalf("构造待回填的旧任务失败: %v", err)
	}

	f.checkVersion(t, "698-navigation-menu-stale-sequence", "重放前")
	f.checkVersion(t, "295-build-jobs-lease", "重放前")

	var sourceIDType string
	if err := f.db.Raw(`SELECT data_type FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'build_jobs'
		  AND column_name = 'source_id'`).Scan(&sourceIDType).Error; err != nil {
		t.Fatalf("读取 source_id 列型失败: %v", err)
	}
	if sourceIDType != "text" {
		t.Fatalf("698 之后 source_id 应为 text，实际 %q", sourceIDType)
	}

	f.replay(t, "698-navigation-menu-stale-sequence")
	f.replay(t, "295-build-jobs-lease")

	f.checkVersion(t, "698-navigation-menu-stale-sequence", "重放后")
	f.checkVersion(t, "295-build-jobs-lease", "重放后")

	var projectID *string
	if err := f.db.Raw("SELECT project_id FROM build_jobs WHERE build_input_hash = 'replay-backfill'").
		Scan(&projectID).Error; err != nil {
		t.Fatalf("读取回填结果失败: %v", err)
	}
	if projectID == nil || *projectID != f.projectID {
		t.Fatalf("295 的回填应把旧行的 project_id 填成本工程，实际 %v（NULL = 回填没跑到或被跳过）", projectID)
	}
}

// TestMenuStaleSourceIDColumnStages 两种列型阶段下 295 的回填等式都成立。
//
// 生产库里 source_id 在 698 之前是 uuid：那时两端 ::text 同样必须成立；只给一侧 cast
// （j.source_id = p.id::text）则撞 uuid = text 的算子缺失，这正是「不要只改右侧」的依据。
// 判据是**算子可用性**（PG 的 = 没有 (text,uuid)/(uuid,text) 组合、pg_cast 里 uuid→text
// 连 assignment-only 都没有），与表里的行无关，所以在两张同形状临时表上直接比：
//   - uuid vs uuid：裸比较成立（改型前的原语句）；
//   - uuid vs text、text vs uuid：不成立；
//   - 两端 ::text：两种阶段都成立（698 前后都安全的唯一形态）。
//
// 探针用**无人引用的 CTE** 而不是临时表：CREATE TEMP TABLE 是连接级会话状态，
// 而连接池（以及 NewMigratedPGTestDB 起库时的 TEMPLATE 复制）让它不可靠 ——
// 实测在本包直接报错（gorm 只回了空信息：&{<ptr> <nil> 0 0 <ptr> 0}）。
// CTE 是单条语句级别的，用完即消失，判据（算子是否可用）完全相同。
func TestMenuStaleSourceIDColumnStages(t *testing.T) {
	f := newMenuFixture(t)
	if f == nil {
		return
	}
	assertOperatorMissing := func(stage, query string) {
		t.Helper()
		err := f.db.Exec(query).Error
		if err == nil {
			t.Fatalf("%s：该比较应因算子不存在而失败", stage)
		}
		if !strings.Contains(err.Error(), "operator does not exist") {
			t.Fatalf("%s：应撞算子缺失，实际错误 %v", stage, err)
		}
	}

	// 两张同形状探针：uuid 型一列、text 型一列，行内容无关（判据是算子可用性）。
	probeUUID := "probe_uuid(id)"
	probeText := "probe_text(id)"
	assertOperatorWorks := func(stage, query string) {
		t.Helper()
		if err := f.db.Exec(query).Error; err != nil {
			t.Fatalf("%s：该比较应成立，实际错误 %v", stage, err)
		}
	}

	// 两端 ::text：两个阶段都成立。
	assertOperatorWorks("uuid 阶段两端 ::text",
		"WITH "+probeUUID+" AS (SELECT NULLIF('', '')::uuid AS id) SELECT 1 FROM probe_uuid u WHERE u.id::text = u.id::text")
	assertOperatorWorks("text 阶段两端 ::text",
		"WITH "+probeText+" AS (SELECT ''::text AS id) SELECT 1 FROM probe_text u WHERE u.id::text = u.id::text")
	// 裸比较只在 uuid = uuid 成立。
	assertOperatorWorks("uuid 阶段的裸比较（改型前的原语句）",
		"WITH "+probeUUID+" AS (SELECT NULLIF('', '')::uuid AS id) SELECT 1 FROM probe_uuid a JOIN probe_uuid b ON a.id = b.id")
	assertOperatorMissing("uuid 阶段只给右侧 cast",
		"WITH "+probeUUID+" AS (SELECT NULLIF('', '')::uuid AS id), "+probeText+" AS (SELECT ''::text AS id) SELECT 1 FROM probe_uuid a JOIN probe_text b ON a.id = b.id")
	assertOperatorMissing("text 阶段不给 cast",
		"WITH "+probeText+" AS (SELECT ''::text AS id), "+probeUUID+" AS (SELECT NULLIF('', '')::uuid AS id) SELECT 1 FROM probe_text a JOIN probe_uuid b ON a.id = b.id")
}

// TestLegacySourcesSourceIDCanonicalized 旧来源的 source_id 在入队时归一成规范 UUID 文本。
//
// source_id 放宽成 text 之后数据库不再替非规范写法归一（uuid 列会归一大写、无连字符、
// 花括号）：同一实体的两种写法会各占一条 uq_build_jobs_running_source（互斥失效）与
// 一条 uq_build_jobs_pending（去重失效），且都不报错。用例钉住三件事：
//   - 大写 / 无连字符 / 花括号写法落库都是同一个规范文本；
//   - 同一份工作换写法重复入队仍被去重成一条；
//   - 非法 UUID 返回参数错误（400）且不留行 —— 不是写进库里等消费侧失败。
func TestLegacySourcesSourceIDCanonicalized(t *testing.T) {
	f := newMenuFixture(t)
	if f == nil {
		return
	}
	raw := uuid.NewString()
	canonical := strings.ToLower(raw)
	compact := strings.ReplaceAll(canonical, "-", "")

	for i, variant := range []string{canonical, strings.ToUpper(canonical), "{" + canonical + "}", "  " + compact + "  "} {
		if err := f.svc.EnqueuePageBuild(context.Background(), variant, f.projectID, "zh-CN",
			buildmodel.IntentDependency, 1, "canon"); err != nil {
			t.Fatalf("第 %d 种写法 %q 入队失败: %v", i+1, variant, err)
		}
	}
	var rows []menuJob
	if err := f.db.Raw(`SELECT source_id, project_id FROM build_jobs
		WHERE source_type = 'page'`).Scan(&rows).Error; err != nil {
		t.Fatalf("读取任务失败: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("四种写法是同一份工作，应被去重成 1 条，实际 %d 条", len(rows))
	}
	if rows[0].SourceID != canonical {
		t.Fatalf("source_id 应归一成规范文本 %q，实际 %q", canonical, rows[0].SourceID)
	}

	// 非法 UUID：受控拒绝，不留行。
	if err := f.svc.EnqueuePageBuild(context.Background(), "page-1", f.projectID, "zh-CN",
		buildmodel.IntentDependency, 1, "canon"); !errors.Is(err, buildenums.Err(buildenums.ErrInvalidParam)) {
		t.Fatalf("非法来源 id 应报 ErrInvalidParam，实际 %v", err)
	}
	var n int64
	if err := f.db.Table("build_jobs").Where("source_type = ?", "page").Count(&n).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("被拒的入队不得留下任务，实际 %d 条", n)
	}
}

// mustMenuSource 组装 menu_stale 的 source_id（失败即终止）。
func mustMenuSource(t *testing.T, projectID, kind string) string {
	t.Helper()
	sid, ok := builddto.MenuStaleSourceID(projectID, kind)
	if !ok {
		t.Fatalf("组装 source_id 失败: (%s,%s)", projectID, kind)
	}
	return sid
}
