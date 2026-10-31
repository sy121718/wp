package artifacthttp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	artifacthttp "go_wp/internal/module/artifact/inbound/http"
	artifactmodel "go_wp/internal/module/artifact/model"
	artifactservice "go_wp/internal/module/artifact/service"
	"go_wp/public/test/support"
)

func TestArtifactDetailEndpointShapes(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		t.Skip("本地 PostgreSQL 不可用，跳过")
	}

	projectID := uuid.NewString()
	if err := db.Exec("INSERT INTO projects (id, name, settings, create_time, update_time) VALUES (?, '产物详情用例', '{}'::jsonb, now(), now())", projectID).Error; err != nil {
		t.Fatalf("准备工程失败：%v", err)
	}
	pageID := uuid.NewString()
	artifactID := uuid.NewString()
	if err := db.Exec(
		"INSERT INTO pages (id, project_id, kind, content_target_type, draft_path, draft_document, draft_version, create_time, update_time) "+
			"VALUES (?, ?, 'home', 'none', '/', '{}'::jsonb, 1, now(), now())", pageID, projectID).Error; err != nil {
		t.Fatalf("准备页面失败：%v", err)
	}
	if err := db.Exec(`
		INSERT INTO page_artifacts (id, page_id, version, source_document, page_document_schema_version,
			source_hash, build_input_manifest, build_input_hash, artifact_provider, artifact_key,
			artifact_hash, compiler_version, registry_version, manifest, created_by, create_time, lang)
		VALUES (?, ?, 1, '{}'::jsonb, 1, 'sh', '{}'::jsonb, 'bh', 'local', 'ak',
			'ah-deadbeef', 'c1', 'r1', '{}'::jsonb, ?, NOW(), 'zh-CN')`,
		artifactID, pageID, uuid.NewString()).Error; err != nil {
		t.Fatalf("准备产物行失败：%v", err)
	}
	svc := artifactservice.NewService(artifactmodel.NewArtifactModel(db))
	h := artifacthttp.ArtifactDetail(svc)

	call := func(query string) *httptest.ResponseRecorder {
		t.Helper()
		gin.SetMode(gin.TestMode)
		engine := gin.New()
		engine.GET("/api/artifact/detail", h)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/artifact/detail"+query, nil))
		return rec
	}

	rec := call("?id=" + artifactID)
	if rec.Code != http.StatusOK {
		t.Fatalf("按 id 查询应 200，实际 %d（body=%s）", rec.Code, rec.Body.String())
	}
	t.Logf("① /detail?id=<uuid> → %d %s", rec.Code, truncate(rec.Body.String()))

	rec = call("?pageId=" + pageID + "&hash=ah-deadbeef")
	if rec.Code != http.StatusOK {
		t.Fatalf("按 (pageId,hash) 查询应 200，实际 %d（body=%s）", rec.Code, rec.Body.String())
	}
	t.Logf("② /detail?pageId=…&hash=ah-deadbeef → %d %s", rec.Code, truncate(rec.Body.String()))

	rec = call("?id=" + uuid.NewString())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在的产物应 404，实际 %d（body=%s）", rec.Code, rec.Body.String())
	}
	t.Logf("③ /detail?id=<不存在的 uuid> → %d（期望 404）", rec.Code)
}

func truncate(s string) string {
	if len(s) > 160 {
		return s[:160] + "…"
	}
	return s
}

var _ = context.Background
