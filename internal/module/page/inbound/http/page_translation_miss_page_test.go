package pagehttp_test

import (
	pageview "go_wp/pkg/view"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	artifactcontract "go_wp/internal/module/artifact/contract"
	pagehttp "go_wp/internal/module/page/inbound/http"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	"go_wp/internal/templates"
	"go_wp/public/test/support"
)

func seedMissFixture(t *testing.T) (svc *pageservice.Service, projectID, projectEmpty string) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		t.Skip("本地 PostgreSQL 不可用，跳过")
	}
	projectID = "50000000-0000-0000-0000-000000000001"
	projectEmpty = "50000000-0000-0000-0000-000000000002"
	support.SeedProjectRow(t, db, projectID, "缺译报告用例")
	support.SeedProjectRow(t, db, projectEmpty, "缺译报告空态用例")

	pageID := "50000000-0000-0000-0000-0000000000a1"
	if err := db.Exec(
		"INSERT INTO pages (id, project_id, kind, content_target_type, draft_path, draft_document, draft_version, create_time, update_time) "+
			"VALUES (?, ?, 'home', 'none', '/about', '{}'::jsonb, 1, now(), now())",
		pageID, projectID).Error; err != nil {
		t.Fatalf("准备页面行失败：%v", err)
	}

	insertMissArtifact(t, db, pageID, "zh-CN", 1, `{"misses": 3, "candidates": 3, "policy": "fallback"}`)
	insertMissArtifact(t, db, pageID, "zh-CN", 2, `{"misses": 48, "candidates": 45, "policy": "fallback"}`)

	insertMissArtifact(t, db, pageID, "ja", 1, `{"misses": 0, "candidates": 12, "policy": "fallback"}`)

	svc = pageservice.NewService(pagemodel.NewPageModel(db), nil, nil, nil, nil, nil, nil, nil, nil)
	svc.SetPageArtifacts(stubPageArtifacts{db: db})
	return svc, projectID, projectEmpty
}

type stubPageArtifacts struct{ db *gorm.DB }

func (stubPageArtifacts) ListPageArtifactHashes(context.Context) ([]string, error) { return nil, nil }
func (stubPageArtifacts) PageArtifactHashByID(context.Context, string) (string, error) {
	return "", nil
}
func (stubPageArtifacts) PageArtifactPageID(context.Context, string) (string, error) { return "", nil }

func (s stubPageArtifacts) TranslationMisses(ctx context.Context, pageIDs []string) (rows []artifactcontract.PageArtifactMissRow, err error) {
	if len(pageIDs) == 0 {
		return nil, nil
	}
	var raw []struct {
		PageID     string `gorm:"column:page_id"`
		Lang       string `gorm:"column:lang"`
		Version    int64  `gorm:"column:version"`
		Misses     int64  `gorm:"column:misses"`
		Candidates int64  `gorm:"column:candidates"`
	}
	err = s.db.WithContext(ctx).Raw(
		`SELECT DISTINCT ON (page_id, lang) page_id, lang, version,
		        COALESCE((manifest->'translationMisses'->>'misses')::bigint, 0) AS misses,
		        COALESCE((manifest->'translationMisses'->>'candidates')::bigint, 0) AS candidates
		   FROM page_artifacts
		  WHERE page_id = ANY(string_to_array(?, ',')::uuid[])
		  ORDER BY page_id, lang, version DESC`,
		strings.Join(pageIDs, ",")).Scan(&raw).Error
	if err != nil {
		return nil, err
	}
	rows = make([]artifactcontract.PageArtifactMissRow, 0, len(raw))
	for i := range raw {
		if raw[i].Misses > 0 {
			rows = append(rows, artifactcontract.PageArtifactMissRow{
				PageID: raw[i].PageID, Lang: raw[i].Lang, Version: raw[i].Version,
				Misses: raw[i].Misses, Candidates: raw[i].Candidates,
			})
		}
	}
	return rows, nil
}

func insertMissArtifact(t *testing.T, db *gorm.DB, pageID, lang string, version int, missJSON string) {
	t.Helper()
	artifactID := uuid.NewString()
	if err := db.Exec(`
		INSERT INTO page_artifacts (id, page_id, version, source_document, page_document_schema_version,
			source_hash, build_input_manifest, build_input_hash, artifact_provider, artifact_key,
			artifact_hash, compiler_version, registry_version, manifest, created_by, create_time, lang)
		VALUES (?, ?, ?, '{}'::jsonb, 1, ?, '{}'::jsonb, ?, 'local', ?, ?, 'c1', 'r1', ?::jsonb, ?, NOW(), ?)`,
		artifactID, pageID, version, "h-src-"+artifactID[:8], "h-build-"+artifactID[:8],
		"key-"+artifactID, "h-art-"+artifactID[:8],
		`{"translationMisses": `+missJSON+`}`, uuid.NewString(), lang).Error; err != nil {
		t.Fatalf("准备产物行失败：%v", err)
	}
}

func renderMissPage(t *testing.T, svc *pageservice.Service, query string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	h := pagehttp.NewPagesAdminHandle(svc, nil, nil, nil)
	router.GET("/admin/page-translation-misses", pageview.Slot(pagehttp.PageTranslationMissesTemplate, pageview.RenderPage), h.TranslationMissesPage)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/page-translation-misses"+query, nil))
	return rec
}

func TestTranslationMissesPageRenders(t *testing.T) {
	svc, projectID, _ := seedMissFixture(t)
	rec := renderMissPage(t, svc, "?project="+projectID)
	if rec.Code != http.StatusOK {
		t.Fatalf("报告页应渲染成功，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.HasSuffix(strings.TrimSpace(body), "</html>") {
		t.Fatalf("整页没有渲染完（缺 </html>）：模板在某一行中断了")
	}
	for _, want := range []string{
		"/about", "zh-CN", "48",
		`name="csrf_token"`,
		// 工程上下文只经 action 的 query 串回带（行内表单不再重复一个同名字段）；
		// 过滤器里的 select 只在有工程列表时渲染，这组夹具没有。
		`action="/admin/page-translation-misses/cancel?project=` + projectID,
		"取词未命中",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("报告页缺少 %q：body=%s", want, body)
		}
	}

	if strings.Contains(body, ">3<") {
		t.Fatalf("报告出现了非最新版本的缺失数：%s", body)
	}
	if strings.Contains(body, "ja") {
		t.Fatalf("misses=0 的语言不该进报告：%s", body)
	}

	if strings.Contains(body, "%") {
		t.Fatalf("报告不该出现百分比：%s", body)
	}
}

func TestTranslationMissesPageEmptyRenders(t *testing.T) {
	svc, _, projectEmpty := seedMissFixture(t)
	rec := renderMissPage(t, svc, "?project="+projectEmpty)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("空态也应渲染成功，实际 %d", rec.Code)
	}
	if !strings.Contains(body, "<thead>") {
		t.Fatalf("空态下 table 表头必须常驻：%s", body)
	}
	if !strings.Contains(body, `colspan="4"`) {
		t.Fatalf("空态行应整行进 tbody 且 colspan 等于列数（4）：%s", body)
	}
	if !strings.HasSuffix(strings.TrimSpace(body), "</html>") {
		t.Fatalf("空态整页也要渲染完：%s", body)
	}
}

func TestTranslationMissesPageWithoutProject(t *testing.T) {
	svc, _, _ := seedMissFixture(t)
	rec := renderMissPage(t, svc, "")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("缺工程时应降级渲染，实际 %d", rec.Code)
	}
	if strings.Contains(body, "SQLSTATE") || strings.Contains(body, "ErrProjectRequired") {
		t.Fatalf("文案不该直出内部细节或裸 key：%s", body)
	}
	if !strings.Contains(body, "请先选择站点工程") {
		t.Fatalf("缺工程时应给可读中文文案：%s", body)
	}
	if !strings.HasSuffix(strings.TrimSpace(body), "</html>") {
		t.Fatalf("降级路径也要渲染完整页：%s", body)
	}
}
