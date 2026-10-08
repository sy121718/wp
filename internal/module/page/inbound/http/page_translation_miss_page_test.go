package pagehttp_test

// page_translation_miss_page_test.go — 缺译报告页的真实渲染（U2）。
//
// 判据是**渲染结果**：报告页是自足外壳的整页（含 </html>），模板里少一个分支、
// 键名与 handler 的 gin.H 对不上、或者 colspan 数错，都不会编译失败 ——
// 前两者让整页在运行期炸掉（用户看到空白），后者让空态的 colspan 与列数不符
// （门禁 check-empty-state-table-head.sh 的判据在渲染层面的对应）。

import (
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

// seedMissFixture 一个工程 + 一个页面 + 一条带 translationMisses 的产物行。
//
// project 端口一律传 nil（下面 NewService / NewPagesAdminHandle 两处）：本用例只走报告页
// 渲染，该页读的是 page 自己的 model 与产物清单（产物那半经 artifact 契约，见下方
// NewService 的第二个实参），不经 project 服务。跨模块构造真实
// project/service 会被 architecture 门禁判违规（跨模块只允许 contract）；传 nil 一旦真被
// 读到就是 panic，不会静默通过。
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
	// 同一 (page, lang) 两条产物：报告只应展示**最新版本**那条（version 越大越新）。
	insertMissArtifact(t, db, pageID, "zh-CN", 1, `{"misses": 3, "candidates": 3, "policy": "fallback"}`)
	insertMissArtifact(t, db, pageID, "zh-CN", 2, `{"misses": 48, "candidates": 45, "policy": "fallback"}`)
	// 没有缺失的语言（misses=0）不应出现。
	insertMissArtifact(t, db, pageID, "ja", 1, `{"misses": 0, "candidates": 12, "policy": "fallback"}`)

	svc = pageservice.NewService(pagemodel.NewPageModel(db), nil, nil, nil, nil, nil, nil, nil, nil)
	svc.SetPageArtifacts(stubPageArtifacts{db: db})
	return svc, projectID, projectEmpty
}

// stubPageArtifacts 只实现 page 模块要的那个读：按页面取「每个 (page_id, lang) 最新产物」
// 的缺译计数。
//
// 刻意手写而不引 artifact 模块的 model/service：架构门禁 TestNoCrossModuleServiceModelImport
// 禁止 page 模块依赖别的模块的 model/service 包（只允许 contract）。而本用例要验的是
// **报告页的渲染**（少一个模板分支、键名与 gin.H 对不上、colspan 数错，这三样都只在
// 渲染期才炸）；artifact 侧真正的查询与排序由 artifact 模块自己的测试覆盖。
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

// insertMissArtifact 插一条带 translationMisses 的产物行（只给 NOT NULL 列 + manifest）。
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

// renderMissPage 渲染报告页一次。
func renderMissPage(t *testing.T, svc *pageservice.Service, query string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	h := pagehttp.NewPagesAdminHandle(svc, nil, nil, nil)
	router.GET("/admin/page-translation-misses", h.TranslationMissesPage)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/page-translation-misses"+query, nil))
	return rec
}

// TestTranslationMissesPageRenders 渲染：整页完整、只列最新版本、只列 misses>0、不给百分比。
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
		"/about", "zh-CN", "48", // 最新版本那条（version=2）
		`name="csrf_token"`, // 取消表单必须带 CSRF
		`action="/admin/page-translation-misses/cancel?project=`,
		"取词未命中",          // 文案有兜底
		`name="project"`, // W4：工程选择器（GET 表单 + select，照页面列表页形态）
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("报告页缺少 %q：body=%s", want, body)
		}
	}
	// 只列最新版本：老版本（misses=3）不该出现；没有缺失的语言（ja）也不该出现。
	if strings.Contains(body, ">3<") {
		t.Fatalf("报告出现了非最新版本的缺失数：%s", body)
	}
	if strings.Contains(body, "ja") {
		t.Fatalf("misses=0 的语言不该进报告：%s", body)
	}
	// 不给百分比（misses 与 candidates 量纲不同，48/45 会算出 >100% 的假比率）。
	if strings.Contains(body, "%") {
		t.Fatalf("报告不该出现百分比：%s", body)
	}
}

// TestTranslationMissesPageEmptyRenders 没有缺译时：表头常驻 + 空态整行进 tbody（colspan=4）。
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

// TestTranslationMissesPageWithoutProject 没有工程上下文时给可读文案（不直出内部错误）。
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
