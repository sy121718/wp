package unit

// page_content_i18n_test.go — 内容译文依赖登记（多语言 P5b，docs/06-D §9 关键约束）。
//
// 覆盖装配层判据：只有「本次构建确实走内容翻译」（非默认语言 + 本页有可翻译候选）
// 才在 Manifest.dependencies 登记 i18n:content —— 否则补齐译文后 revision 未变
// 不会触发重建，站点长期停留在回退内容。
//
// 说明：本测试不注入内容译文存储（装配层用全局数据库组件），因此产物是「回退原文」
// 形态；译文替换链路由 internal/builder 的 content_i18n_test.go 用假存储覆盖。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pagedto "go_wp/internal/module/page/dto"
)

// artifactDependencies 读取落盘 manifest 的依赖条目（kind|key|revision）。
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

// hasDependencyKey 判定依赖条目里是否存在指定 key。
func hasDependencyKey(deps []string, key string) bool {
	for _, d := range deps {
		if strings.Contains(d, "|"+key+"|") {
			return true
		}
	}
	return false
}

// TestPageContentTranslationDependency 非默认语言 + 有可翻译候选 → 登记 i18n:content；
// 默认语言或无候选 → 不登记（产物不随 sys_translation 变化）。
func TestPageContentTranslationDependency(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	_ = db
	ctx := context.Background()
	withLangPrefix(t)

	// 含 core.heading.text（白名单字段）的页面。
	created := createPage(t, svc, projectID, "/p5b", headingDocument)

	// 1) en-US 构建：登记内容译文依赖。
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
	// 依赖按 (kind,key) 排序：i18n:content 在 i18n:site 之前。
	if deps[0] != "i18n|i18n:content|" && !strings.Contains(deps[0], "i18n:content") {
		t.Fatalf("依赖条目排序异常: %v", deps)
	}

	// 无译文（测试进程未接全局数据库）→ 产物回退原文，构建不失败。
	body, rerr := os.ReadFile(filepath.Join(os.Getenv("GO_WP_ARTIFACT_ROOT"), "artifacts", built.StagedHash, "index.html"))
	if rerr != nil {
		t.Fatalf("读取产物失败: %v", rerr)
	}
	if !strings.Contains(string(body), "你好") {
		t.Fatalf("无译文时应回退原文\nHTML=%s", string(body))
	}

	// 2) 默认语言构建（zh-CN）：产物即原文，不登记内容译文依赖。
	builtZh, err := svc.Build(ctx, &pagedto.BuildReq{ID: created.ID})
	if err != nil {
		t.Fatalf("默认语言构建失败: %v", err)
	}
	depsZh := artifactDependencies(t, builtZh.StagedHash)
	t.Logf("zh-CN manifest dependencies: %v", depsZh)
	if hasDependencyKey(depsZh, "i18n:content") {
		t.Fatalf("默认语言构建不应登记内容译文依赖: %v", depsZh)
	}

	// 3) 非默认语言但页面无可翻译候选（空 root）→ 不登记。
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
