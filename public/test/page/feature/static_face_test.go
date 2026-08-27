package feature

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pagedto "go_wp/internal/module/page/dto"

	"github.com/gin-gonic/gin"
)

// artifactRootOf 读取测试装配的产物根（newPageService 中以 GO_WP_ARTIFACT_ROOT 注入）。
func artifactRootOf(t *testing.T) string {
	t.Helper()
	root := os.Getenv("GO_WP_ARTIFACT_ROOT")
	if root == "" {
		t.Fatal("测试产物根未注入")
	}
	return root
}
func TestStaticFaceServesActiveArtifact(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, svc, projectID := newPageService(t)
	ctx := context.Background()

	page, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/smoke", DraftDocument: json.RawMessage(docV2),
	})
	if err != nil {
		t.Fatalf("创建 Page 失败: %v", err)
	}
	if _, err = svc.Build(ctx, &pagedto.BuildReq{ID: page.ID}); err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: page.ID}); err != nil {
		t.Fatalf("发布失败: %v", err)
	}

	// 访问面根 = 产物根/public/active，与 routes.go setupStaticFace 一致。
	root := artifactRootOf(t)
	activeDir := filepath.Join(root, "public", "active")
	entry := filepath.Join(activeDir, "smoke", "index.html")
	data, err := os.ReadFile(entry)
	if err != nil {
		t.Fatalf("激活入口不存在: %v", err)
	}
	if !strings.Contains(string(data), "<!DOCTYPE html>") || !strings.Contains(string(data), "你好") {
		t.Fatalf("直出内容错误:\n%s", data)
	}

	router := gin.New()
	router.StaticFS("/site", http.Dir(activeDir))
	// http.FileServer 对 index.html 请求会 301 到 ./，按目录路径断言直出内容。
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/site/smoke/", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "你好") {
		t.Fatalf("静态访问面响应错误: code=%d body=%s", recorder.Code, recorder.Body.String())
	}
	_ = db
	_ = os.Stdout
}
