package unit

// manifest_projection_test.go — 审计 DB-02：重复 manifest 列的收敛与列表查询的列白名单。
//
// 三组断言（全部对着真实 PostgreSQL + 生产迁移建出的表，不用 AutoMigrate）：
//  1. 写入侧不再产出同字节的第二份 manifest：Record / EnsureRecord（含「同版本替换」路径）
//     落库后 page_artifacts.build_input_manifest 必须是 NULL（305 已把该列放开为可空），
//     而 manifest 仍是本次归档的那份输出清单 —— 真源没丢，只是不再有第二份副本；
//  2. ListByPage / ListGCCandidates 实际发出的 SQL 只取白名单列，
//     不得出现 source_document / manifest / build_input_manifest；
//  3. 两个投影类型的字段集合与白名单逐列相等（SQL 与 Go 类型两侧同时钉住）。
//
// 白名单在这里**独立重写一遍**（而不是从实现里读常量）：测试要能发现「有人把大字段
// 加回列表查询」，照抄实现常量就永远发现不了。

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	artifactdto "go_wp/internal/module/artifact/dto"
	artifactmodel "go_wp/internal/module/artifact/model"
	artifactservice "go_wp/internal/module/artifact/service"
	"go_wp/public/test/support"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// wantSummaryColumns / wantGCCandidateColumns 列表投影的期望列（独立于实现）。
var wantSummaryColumns = []string{
	"id", "page_id", "version", "lang", "source_hash", "build_input_hash",
	"artifact_provider", "artifact_key", "artifact_hash", "compiler_version",
	"registry_version", "payload_state", "create_time",
}

var wantGCCandidateColumns = []string{
	"id", "page_id", "version", "lang", "artifact_hash", "artifact_key", "create_time",
}

// bigColumns 任何列表查询都不该 SELECT 的大字段。
var bigColumns = []string{"source_document", "build_input_manifest", "manifest"}

// sqlCapture 收集 gorm 实发 SQL（不打印）。
type sqlCapture struct {
	mu   sync.Mutex
	sqls []string
}

func (c *sqlCapture) LogMode(gormlogger.LogLevel) gormlogger.Interface { return c }
func (c *sqlCapture) Info(context.Context, string, ...interface{})     {}
func (c *sqlCapture) Warn(context.Context, string, ...interface{})     {}
func (c *sqlCapture) Error(context.Context, string, ...interface{})    {}

func (c *sqlCapture) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	sql, _ := fc()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sqls = append(c.sqls, sql)
}

// lastSelect 返回最近一条 SELECT 的 SQL。
func (c *sqlCapture) lastSelect(t *testing.T) string {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := len(c.sqls) - 1; i >= 0; i-- {
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(c.sqls[i])), "SELECT") {
			return c.sqls[i]
		}
	}
	t.Fatalf("未捕获到任何 SELECT 语句，已捕获：%v", c.sqls)
	return ""
}

// captureDB 建隔离测试库 + 捕获 SQL 的 model 句柄。
func captureDB(t *testing.T) (*gorm.DB, *sqlCapture) {
	t.Helper()
	db := support.NewMigratedPGTestDBTranslateError(t)
	seedArtifactFixtures(t, db)
	cap := &sqlCapture{}
	return db, cap
}

// selectColumns 解析 SQL 的 SELECT 列表（逗号分隔，去掉表名前缀与引号）。
func selectColumns(t *testing.T, sql string) []string {
	t.Helper()
	upper := strings.ToUpper(sql)
	start := strings.Index(upper, "SELECT ")
	end := strings.Index(upper, " FROM ")
	if start < 0 || end < 0 || end <= start {
		t.Fatalf("无法解析 SELECT 列表：%s", sql)
	}
	raw := strings.Split(sql[start+len("SELECT "):end], ",")
	out := make([]string, 0, len(raw))
	for _, c := range raw {
		c = strings.TrimSpace(c)
		// 先去掉表名前缀，再去掉引号：gorm 自己生成的列是 "page_artifacts"."id" 形态，
		// 而调用方手写的 Select 字符串是裸列名 —— 两种形态都要能解析。
		if idx := strings.LastIndex(c, "."); idx >= 0 {
			c = c[idx+1:]
		}
		c = strings.TrimSpace(strings.Trim(c, "\""))
		out = append(out, c)
	}
	return out
}

// assertSelectWhitelist 断言 SELECT 列集合与白名单相等，且不含大字段。
func assertSelectWhitelist(t *testing.T, sql string, want []string) {
	t.Helper()
	got := selectColumns(t, sql)
	wantSet := map[string]bool{}
	for _, c := range want {
		wantSet[c] = true
	}
	gotSet := map[string]bool{}
	for _, c := range got {
		gotSet[c] = true
	}
	// SELECT * 单独报一条更指向性的错：它是「读完整实体」的形态，必然带上全部大字段
	// （旧实现就是这样），比逐列罗列更好读。
	if gotSet["*"] {
		t.Fatalf("列表查询用了 SELECT *（读完整实体，必然带上 %s）—— 列表必须走列白名单投影（SQL: %s）",
			strings.Join(bigColumns, " / "), sql)
	}
	for _, c := range got {
		if !wantSet[c] {
			t.Errorf("列表查询多选了列 %q（SQL: %s）", c, sql)
		}
	}
	for _, c := range want {
		if !gotSet[c] {
			t.Errorf("列表查询漏选列 %q（SQL: %s）", c, sql)
		}
	}
	for _, big := range bigColumns {
		if gotSet[big] {
			t.Errorf("列表查询带上了大字段 %q —— 列表投影不得取源码与清单（SQL: %s）", big, sql)
		}
	}
}

// assertProjectionFields 断言投影类型的字段集合与白名单逐列相等。
func assertProjectionFields(t *testing.T, dest any, want []string) {
	t.Helper()
	got := support.ModelColumns(t, dest)
	wantSet := map[string]bool{}
	for _, c := range want {
		wantSet[c] = true
		if _, ok := got[c]; !ok {
			t.Errorf("投影类型缺少列 %q（实际：%v）", c, got)
		}
	}
	for col, field := range got {
		if !wantSet[col] {
			t.Errorf("投影类型多出列 %q（字段 %s）—— 列表投影不得携带非白名单列", col, field)
		}
	}
}

// TestRecordDoesNotWriteDuplicateManifestColumn Record 之后重复列必须为 NULL，
// manifest 仍写下本次归档的字节（真源完整）。
//
// 失败能力：把 artifact_record.go 里的 BuildInputManifest: req.Manifest 写回去
// （并恢复 model 字段），本用例立刻变红 —— 它挡的正是这条回头路。
func TestRecordDoesNotWriteDuplicateManifestColumn(t *testing.T) {
	db, _ := captureDB(t)
	svc := artifactservice.NewService(artifactmodel.NewArtifactModel(db))
	mustRecord(t, svc, validReq())

	var dup sql.NullString
	if err := db.Raw("SELECT build_input_manifest::text FROM page_artifacts WHERE id = ?", testArtifactID).
		Scan(&dup).Error; err != nil {
		t.Fatalf("读取 build_input_manifest 失败: %v", err)
	}
	if dup.Valid {
		t.Fatalf("重复 manifest 列必须为 NULL（已在 305 收敛），实际写入 %d 字节", len(dup.String))
	}

	var manifest string
	if err := db.Raw("SELECT manifest::text FROM page_artifacts WHERE id = ?", testArtifactID).
		Scan(&manifest).Error; err != nil {
		t.Fatalf("读取 manifest 失败: %v", err)
	}
	if !jsonEqual(t, manifest, manifestJSON) {
		t.Fatalf("manifest 真源内容变了：%s", manifest)
	}
}

// TestEnsureRecordReplaceDoesNotWriteDuplicateManifestColumn 同版本同语言替换
// （编译器升级导致的 hash 变化）路径同样不得写第二份 manifest。
func TestEnsureRecordReplaceDoesNotWriteDuplicateManifestColumn(t *testing.T) {
	db, _ := captureDB(t)
	svc := artifactservice.NewService(artifactmodel.NewArtifactModel(db))
	ctx := context.Background()

	mustRecord(t, svc, validReq()) // version=1, hash=v1
	reqB := validReq()
	reqB.ArtifactID = testArtifactID2
	reqB.ArtifactHash = artifactHashV2
	if _, err := svc.EnsureRecord(ctx, reqB); err != nil {
		t.Fatalf("EnsureRecord 替换路径应成功: %v", err)
	}

	var dup sql.NullString
	if err := db.Raw("SELECT build_input_manifest::text FROM page_artifacts WHERE id = ?", testArtifactID).
		Scan(&dup).Error; err != nil {
		t.Fatalf("读取 build_input_manifest 失败: %v", err)
	}
	if dup.Valid {
		t.Fatalf("替换路径也不得写重复 manifest 列，实际写入 %d 字节", len(dup.String))
	}
}

// TestListByPageProjectionExcludesBigColumns 列表查询只取白名单列。
func TestListByPageProjectionExcludesBigColumns(t *testing.T) {
	db, cap := captureDB(t)
	svc := artifactservice.NewService(artifactmodel.NewArtifactModel(db))
	mustRecord(t, svc, validReq())

	m := artifactmodel.NewArtifactModel(db.Session(&gorm.Session{Logger: cap}))
	list, err := m.ListByPage(context.Background(), testPageID)
	if err != nil {
		t.Fatalf("ListByPage 失败: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("列表应返回 1 行，实际 %d", len(list))
	}
	assertSelectWhitelist(t, cap.lastSelect(t), wantSummaryColumns)
	assertProjectionFields(t, &artifactmodel.PageArtifactSummary{}, wantSummaryColumns)

	// 投影仍要带出列表语义需要的值（不是「什么都不查」）。
	if list[0].ArtifactHash != artifactHashV1 || list[0].Lang == "" || list[0].PageID != testPageID {
		t.Fatalf("列表投影字段值不正确: %+v", list[0])
	}
}

// TestListGCCandidatesProjectionExcludesBigColumns GC 候选扫描同样只取白名单列。
func TestListGCCandidatesProjectionExcludesBigColumns(t *testing.T) {
	db, cap := captureDB(t)
	svc := artifactservice.NewService(artifactmodel.NewArtifactModel(db))
	mustRecord(t, svc, validReq())

	m := artifactmodel.NewArtifactModel(db.Session(&gorm.Session{Logger: cap}))
	before := time.Now().UTC().Add(time.Minute)
	list, err := m.ListGCCandidates(context.Background(), before, []string{testArtifactID2})
	if err != nil {
		t.Fatalf("ListGCCandidates 失败: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("候选应返回 1 行，实际 %d", len(list))
	}
	assertSelectWhitelist(t, cap.lastSelect(t), wantGCCandidateColumns)
	assertProjectionFields(t, &artifactmodel.GCCandidate{}, wantGCCandidateColumns)
	if list[0].ArtifactHash != artifactHashV1 || list[0].ArtifactKey == "" {
		t.Fatalf("候选投影字段值不正确: %+v", list[0])
	}
}

// TestDetailKeepsRebuildInputsAfterDedup 详情口径仍完整返回「缺文件重建」所需的输入。
//
// 收敛的只是重复副本，不是恢复所需的真源：产物文件丢失时按元数据重建要走
// page.RebuildArtifact → artifact.DetailByID，它读的正是 source_document（重编译输入）
// 与 manifest 解出的 canonicalPath、以及 artifact_hash（重建后校验 hash）。
func TestDetailKeepsRebuildInputsAfterDedup(t *testing.T) {
	db, _ := captureDB(t)
	svc := artifactservice.NewService(artifactmodel.NewArtifactModel(db))
	mustRecord(t, svc, validReq())

	got, err := svc.DetailByID(context.Background(), &artifactdto.DetailByIDReq{ID: testArtifactID})
	if err != nil {
		t.Fatalf("DetailByID 失败: %v", err)
	}
	if len(got.SourceDocument) == 0 {
		t.Fatal("详情必须带出 source_document —— 缺文件重建的唯一输入")
	}
	if got.CanonicalPath != "/index.html" {
		t.Fatalf("canonicalPath 必须仍能从 manifest 解出，实际 %q", got.CanonicalPath)
	}
	if got.ArtifactHash != artifactHashV1 {
		t.Fatalf("artifact_hash 必须仍在，实际 %q", got.ArtifactHash)
	}
}

// TestPageArtifactModelColumnsSubsetOfDDL model 列集合必须是生产 DDL 的子集。
//
// 305 之后 model 刻意**不映射** build_input_manifest（零读取者）；这条断言与
// presentation 侧的同名断言同形，防止有人把已收敛的列加回 model。
func TestPageArtifactModelColumnsSubsetOfDDL(t *testing.T) {
	db, _ := captureDB(t)
	support.AssertModelColumnsSubset(t, db, "page_artifacts", &artifactmodel.PageArtifactEntity{})
	cols := support.ModelColumns(t, &artifactmodel.PageArtifactEntity{})
	if _, ok := cols["build_input_manifest"]; ok {
		t.Fatalf("已收敛的重复列 build_input_manifest 不该再映射进 model")
	}
}

// jsonEqual 语义比较两段 JSON（忽略 key 顺序与空白）。
func jsonEqual(t *testing.T, a, b string) bool {
	t.Helper()
	var av, bv any
	if err := json.Unmarshal([]byte(a), &av); err != nil {
		t.Fatalf("解析 %q 失败: %v", a, err)
	}
	if err := json.Unmarshal([]byte(b), &bv); err != nil {
		t.Fatalf("解析 %q 失败: %v", b, err)
	}
	aj, _ := json.Marshal(av)
	bj, _ := json.Marshal(bv)
	return string(aj) == string(bj)
}
