package unit

import (
	"context"
	"database/sql"
	json "go_wp/pkg/jsonx"
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

var wantSummaryColumns = []string{
	"id", "page_id", "version", "lang", "source_hash", "build_input_hash",
	"artifact_provider", "artifact_key", "artifact_hash", "compiler_version",
	"registry_version", "payload_state", "create_time",
}

var wantGCCandidateColumns = []string{
	"id", "page_id", "version", "lang", "artifact_hash", "artifact_key", "create_time",
}

var bigColumns = []string{"source_document", "build_input_manifest", "manifest"}

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

func captureDB(t *testing.T) (*gorm.DB, *sqlCapture) {
	t.Helper()
	db := support.NewMigratedPGTestDBTranslateError(t)
	seedArtifactFixtures(t, db)
	cap := &sqlCapture{}
	return db, cap
}

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

		if idx := strings.LastIndex(c, "."); idx >= 0 {
			c = c[idx+1:]
		}
		c = strings.TrimSpace(strings.Trim(c, "\""))
		out = append(out, c)
	}
	return out
}

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

func TestEnsureRecordRebuildDoesNotWriteDuplicateManifestColumn(t *testing.T) {
	db, _ := captureDB(t)
	svc := artifactservice.NewService(artifactmodel.NewArtifactModel(db))
	ctx := context.Background()

	mustRecord(t, svc, validReq())
	reqB := validReq()
	reqB.ArtifactID = testArtifactID2
	reqB.ArtifactHash = artifactHashV2
	if _, err := svc.EnsureRecord(ctx, reqB); err != nil {
		t.Fatalf("EnsureRecord 重建路径应成功: %v", err)
	}

	var dup sql.NullString
	if err := db.Raw("SELECT build_input_manifest::text FROM page_artifacts WHERE id = ?", testArtifactID2).
		Scan(&dup).Error; err != nil {
		t.Fatalf("读取 build_input_manifest 失败: %v", err)
	}
	if dup.Valid {
		t.Fatalf("重建路径也不得写重复 manifest 列，实际写入 %d 字节", len(dup.String))
	}
}

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

	if list[0].ArtifactHash != artifactHashV1 || list[0].Lang == "" || list[0].PageID != testPageID {
		t.Fatalf("列表投影字段值不正确: %+v", list[0])
	}
}

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

func TestPageArtifactModelColumnsSubsetOfDDL(t *testing.T) {
	db, _ := captureDB(t)
	support.AssertModelColumnsSubset(t, db, "page_artifacts", &artifactmodel.PageArtifactEntity{})
	cols := support.ModelColumns(t, &artifactmodel.PageArtifactEntity{})
	if _, ok := cols["build_input_manifest"]; ok {
		t.Fatalf("已收敛的重复列 build_input_manifest 不该再映射进 model")
	}
}

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
