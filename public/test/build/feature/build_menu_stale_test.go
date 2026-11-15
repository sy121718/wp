package feature

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
	"go_wp/public/test/support"
)

type menuFixture struct {
	db        *gorm.DB
	svc       *buildservice.Service
	projectID string
}

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

func (f *menuFixture) inScope(t *testing.T, fn func(tx *gorm.DB) error) error {
	t.Helper()
	return rls.InProjectScope(context.Background(), f.db, f.projectID, fn)
}

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

func (f *menuFixture) seedSourceRow(t *testing.T, sourceType string) string {
	t.Helper()
	id := uuid.NewString()
	var sql string
	switch sourceType {
	case buildmodel.SourceTypePage:

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

	for i, want := range []int64{v1, v2, v3} {
		if jobs[i].InputVersion == nil || *jobs[i].InputVersion != want {
			t.Fatalf("第 %d 条任务的版本号应为 %d，实际 %v", i+1, want, jobs[i].InputVersion)
		}
		if jobs[i].Status != buildmodel.StatusPending {
			t.Fatalf("第 %d 条任务应为 pending，实际 %q", i+1, jobs[i].Status)
		}

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

func TestMenuStaleKeepsPendingAndRunningAtDifferentVersions(t *testing.T) {
	f := newMenuFixture(t)
	if f == nil {
		return
	}
	v1 := f.enqueue(t, "header_mobile")
	v2 := f.enqueue(t, "header_mobile")

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

func TestMenuStaleABARetainsThreeTasks(t *testing.T) {
	f := newMenuFixture(t)
	if f == nil {
		return
	}
	vA1 := f.enqueue(t, "footer_mobile")
	_ = f.enqueue(t, "footer_mobile")
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

	if err := f.db.Exec(`INSERT INTO build_jobs (source_type, source_id, project_id, lang, intent,
		draft_version, build_input_hash, status, attempt, create_time, input_version, context_key)
		VALUES ('menu_stale', ?, ?, '', 'dependency', 0, 'shape-check-ok', 'pending', 0, now(), 1, ?)`,
		goodSourceID, f.projectID, goodSourceID).Error; err != nil {
		t.Fatalf("形状自洽的 menu_stale 行应能插入，实际 %v", err)
	}

	if err := f.db.Exec("DELETE FROM build_jobs WHERE build_input_hash = 'shape-check-ok'").Error; err != nil {
		t.Fatalf("清理正例行失败: %v", err)
	}
}

func TestMenuStaleLegacyIndexesKeepPredicates(t *testing.T) {
	f := newMenuFixture(t)
	if f == nil {
		return
	}
	type indexFact struct {
		KeyColumns string `gorm:"column:key_columns"`
		Predicate  string `gorm:"column:predicate"`

		SourceIDType string `gorm:"column:source_id_type"`
	}
	read := func(name string) (indexFact, bool) {
		var rows []indexFact

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

func ptrStr(s string) *string { return &s }

func ptrInt64(v int64) *int64 { return &v }

func TestLegacyProjectBackfillJoinNeedsExplicitCast(t *testing.T) {
	f := newMenuFixture(t)
	if f == nil {
		return
	}
	pageID := f.seedSourceRow(t, buildmodel.SourceTypePage)
	instanceID := f.seedSourceRow(t, buildmodel.SourceTypePresentation)

	if err := f.svc.EnqueuePageBuild(context.Background(), pageID, f.projectID, "zh-CN",
		buildmodel.IntentManual, 3, "hash-page"); err != nil {
		t.Fatalf("page 入队失败: %v", err)
	}
	if err := f.svc.EnqueuePresentationBuild(context.Background(), instanceID, f.projectID); err != nil {
		t.Fatalf("presentation 入队失败: %v", err)
	}
	f.enqueue(t, "header")

	if err := f.db.Exec("UPDATE build_jobs SET project_id = NULL WHERE source_type <> 'menu_stale'").Error; err != nil {
		t.Fatalf("清空 project_id 失败: %v", err)
	}

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

	var sourceIDType string
	if e := f.db.Raw(`SELECT data_type FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'build_jobs'
		  AND column_name = 'source_id'`).Scan(&sourceIDType).Error; e != nil {
		t.Fatalf("读取 source_id 列型失败: %v", e)
	}
	if sourceIDType != "text" {
		t.Fatalf("迁移模板库应当是 698 之后的形态（text），实际 %q", sourceIDType)
	}

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

			if r.ProjectID == nil || *r.ProjectID != f.projectID {
				t.Fatalf("menu_stale 行不该被 295 的回填改动，实际 project_id=%v", r.ProjectID)
			}
		}
	}
}

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

	probeUUID := "probe_uuid(id)"
	probeText := "probe_text(id)"
	assertOperatorWorks := func(stage, query string) {
		t.Helper()
		if err := f.db.Exec(query).Error; err != nil {
			t.Fatalf("%s：该比较应成立，实际错误 %v", stage, err)
		}
	}

	assertOperatorWorks("uuid 阶段两端 ::text",
		"WITH "+probeUUID+" AS (SELECT NULLIF('', '')::uuid AS id) SELECT 1 FROM probe_uuid u WHERE u.id::text = u.id::text")
	assertOperatorWorks("text 阶段两端 ::text",
		"WITH "+probeText+" AS (SELECT ''::text AS id) SELECT 1 FROM probe_text u WHERE u.id::text = u.id::text")

	assertOperatorWorks("uuid 阶段的裸比较（改型前的原语句）",
		"WITH "+probeUUID+" AS (SELECT NULLIF('', '')::uuid AS id) SELECT 1 FROM probe_uuid a JOIN probe_uuid b ON a.id = b.id")
	assertOperatorMissing("uuid 阶段只给右侧 cast",
		"WITH "+probeUUID+" AS (SELECT NULLIF('', '')::uuid AS id), "+probeText+" AS (SELECT ''::text AS id) SELECT 1 FROM probe_uuid a JOIN probe_text b ON a.id = b.id")
	assertOperatorMissing("text 阶段不给 cast",
		"WITH "+probeText+" AS (SELECT ''::text AS id), "+probeUUID+" AS (SELECT NULLIF('', '')::uuid AS id) SELECT 1 FROM probe_text a JOIN probe_uuid b ON a.id = b.id")
}

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

func mustMenuSource(t *testing.T, projectID, kind string) string {
	t.Helper()
	sid, ok := builddto.MenuStaleSourceID(projectID, kind)
	if !ok {
		t.Fatalf("组装 source_id 失败: (%s,%s)", projectID, kind)
	}
	return sid
}
