package unit

import (
	"context"
	json "go_wp/pkg/jsonx"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pagedto "go_wp/internal/module/page/dto"
)

func artifactDependencies(t *testing.T, hash string) []string {
	t.Helper()
	root := os.Getenv("GO_WP_ARTIFACT_ROOT")
	data, err := os.ReadFile(filepath.Join(root, "artifacts", hash, "manifest.json"))
	if err != nil {
		t.Fatalf("读取 manifest 失败: %v", err)
	}
	var m struct {
		Dependencies []struct {
			Kind     string `json:"kind"`
			Key      string `json:"key"`
			Revision string `json:"revision"`
		} `json:"dependencies"`
	}
	if err = json.Unmarshal(data, &m); err != nil {
		t.Fatalf("解析 manifest 失败: %v", err)
	}
	out := make([]string, 0, len(m.Dependencies))
	for _, d := range m.Dependencies {
		out = append(out, d.Kind+"|"+d.Key+"|"+d.Revision)
	}
	return out
}

func hasDependencyKey(deps []string, key string) bool {
	for _, d := range deps {
		if strings.Contains(d, "|"+key+"|") {
			return true
		}
	}
	return false
}

func TestPageContentTranslationDependency(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	_ = db
	ctx := context.Background()
	withLangPrefix(t)

	created := createPage(t, svc, projectID, "/p5b", headingDocument)

	built, err := svc.Build(ctx, &pagedto.BuildReq{ID: created.ID, Lang: "en-US"})
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	deps := artifactDependencies(t, built.StagedHash)
	t.Logf("en-US manifest dependencies: %v", deps)
	if !hasDependencyKey(deps, "i18n:content") {
		t.Fatalf("非默认语言构建必须登记 i18n:content 依赖: %v", deps)
	}
	if !hasDependencyKey(deps, "i18n:site") {
		t.Fatalf("文案词条依赖（P4）不应丢失: %v", deps)
	}

	if deps[0] != "i18n|i18n:content|" && !strings.Contains(deps[0], "i18n:content") {
		t.Fatalf("依赖条目排序异常: %v", deps)
	}

	body, rerr := os.ReadFile(filepath.Join(os.Getenv("GO_WP_ARTIFACT_ROOT"), "artifacts", built.StagedHash, "index.html"))
	if rerr != nil {
		t.Fatalf("读取产物失败: %v", rerr)
	}
	if !strings.Contains(string(body), "你好") {
		t.Fatalf("无译文时应回退原文\nHTML=%s", string(body))
	}

	builtZh, err := svc.Build(ctx, &pagedto.BuildReq{ID: created.ID})
	if err != nil {
		t.Fatalf("默认语言构建失败: %v", err)
	}
	depsZh := artifactDependencies(t, builtZh.StagedHash)
	t.Logf("zh-CN manifest dependencies: %v", depsZh)
	if hasDependencyKey(depsZh, "i18n:content") {
		t.Fatalf("默认语言构建不应登记内容译文依赖: %v", depsZh)
	}

	empty := createPage(t, svc, projectID, "/p5b-empty", pageDocument)
	builtEmpty, err := svc.Build(ctx, &pagedto.BuildReq{ID: empty.ID, Lang: "en-US"})
	if err != nil {
		t.Fatalf("空页面构建失败: %v", err)
	}
	depsEmpty := artifactDependencies(t, builtEmpty.StagedHash)
	t.Logf("en-US（无候选）manifest dependencies: %v", depsEmpty)
	if hasDependencyKey(depsEmpty, "i18n:content") {
		t.Fatalf("无可翻译候选时不应登记内容译文依赖: %v", depsEmpty)
	}
}
