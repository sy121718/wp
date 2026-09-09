package unit

// page_artifact_lang_test.go — 同页多语言产物行并存与路由绑定（多语言 P3 前置，
// docs/06-D-site-i18n.md §15.5 第 1 条）。
//
// 回归对象：page_artifacts 唯一键 (page_id, version) 曾让第二个语言的产物替换
// 第一个语言的行，page_routes.artifact_id 于是指向错内容。本用例走完整装配链
// （Build → Publish，开关 i18n.site_lang_prefix 开启）验证：
//  1) 同一草稿版本下两个语言各登记一行、互不覆盖；
//  2) 路由行的 artifact_id 指向「该语言」的产物行。

import (
	"context"
	"testing"

	pagedto "go_wp/internal/module/page/dto"

	"gorm.io/gorm"
)

// activeRouteArtifactID 读取某激活路由指向的产物行 ID。
func activeRouteArtifactID(t *testing.T, db *gorm.DB, projectID, path string) string {
	t.Helper()
	var id string
	if err := db.Table("page_routes").Select("artifact_id").
		Where("project_id = ? AND path = ? AND route_kind = ?", projectID, path, "active").
		Scan(&id).Error; err != nil {
		t.Fatalf("查询激活路由 %s 失败: %v", path, err)
	}
	return id
}

// artifactLangAndPath 读取产物行 manifest 中的语言与规范路径。
func artifactLangAndPath(t *testing.T, db *gorm.DB, artifactID string) (lang, canonicalPath string) {
	t.Helper()
	var row struct {
		Lang          string
		CanonicalPath string
	}
	if err := db.Table("page_artifacts").
		Select("manifest->>'lang' AS lang, manifest->>'canonicalPath' AS canonical_path").
		Where("id = ?", artifactID).Scan(&row).Error; err != nil {
		t.Fatalf("查询产物行 %s 失败: %v", artifactID, err)
	}
	return row.Lang, row.CanonicalPath
}

// TestPageArtifactLangCoexistAndRouteBinding 同页两个语言各登记一行，路由分别指向各自语言的产物。
func TestPageArtifactLangCoexistAndRouteBinding(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	withLangPrefix(t)

	created := createPage(t, svc, projectID, "/about", headingDocument)

	// ---- 默认语言（zh-CN）：构建 + 发布 ----
	builtZh, err := svc.Build(ctx, &pagedto.BuildReq{ID: created.ID})
	if err != nil {
		t.Fatalf("默认语言构建失败: %v", err)
	}
	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: created.ID}); err != nil {
		t.Fatalf("默认语言发布失败: %v", err)
	}
	zhArtifactID := activeRouteArtifactID(t, db, projectID, "/about")
	if zhArtifactID == "" {
		t.Fatal("/about 激活路由应指向产物行")
	}
	if lang, canonical := artifactLangAndPath(t, db, zhArtifactID); lang != "zh-CN" || canonical != "/about" {
		t.Fatalf("zh-CN 路由应指向 zh-CN 产物，实际 lang=%q path=%q", lang, canonical)
	}

	// ---- 第二个语言（en-US）：同一草稿版本构建 + 发布 ----
	builtEn, err := svc.Build(ctx, &pagedto.BuildReq{ID: created.ID, Lang: "en-US"})
	if err != nil {
		t.Fatalf("en-US 构建失败: %v", err)
	}
	if builtEn.StagedHash == builtZh.StagedHash {
		t.Fatalf("不同语言的产物 hash 不应相同: %s", builtEn.StagedHash)
	}
	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: created.ID, Lang: "en-US"}); err != nil {
		t.Fatalf("en-US 发布失败: %v", err)
	}

	// ---- 同页两语言各一行、互不覆盖 ----
	var rows []struct {
		ID           string
		Lang         string
		Version      int64
		ArtifactHash string
		ArtifactKey  string
	}
	if err := db.Table("page_artifacts").
		Select("id, lang, version, artifact_hash, artifact_key").
		Where("page_id = ?", created.ID).Scan(&rows).Error; err != nil {
		t.Fatalf("查询产物行失败: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("同页两语言应各登记一行（共 2 行），实际 %d 行: %+v", len(rows), rows)
	}
	byLang := map[string]struct {
		ID           string
		Version      int64
		ArtifactHash string
	}{}
	for _, r := range rows {
		byLang[r.Lang] = struct {
			ID           string
			Version      int64
			ArtifactHash string
		}{r.ID, r.Version, r.ArtifactHash}
	}
	zhRow, okZh := byLang["zh-CN"]
	enRow, okEn := byLang["en-US"]
	if !okZh || !okEn {
		t.Fatalf("应同时存在 zh-CN 与 en-US 行，实际 %+v", rows)
	}
	if zhRow.Version != enRow.Version {
		t.Fatalf("两个语言应属于同一草稿版本: zh=%d en=%d", zhRow.Version, enRow.Version)
	}
	if zhRow.ID == enRow.ID {
		t.Fatal("两个语言的行 ID 不应相同（第二个语言覆盖了第一个语言的行）")
	}
	// 核心回归点：zh-CN 行仍是第一次构建的产物，未被 en-US 构建替换。
	if zhRow.ArtifactHash != builtZh.StagedHash {
		t.Fatalf("zh-CN 行产物被 en-US 构建覆盖: 期望 %s，实际 %s", builtZh.StagedHash, zhRow.ArtifactHash)
	}
	if enRow.ArtifactHash != builtEn.StagedHash {
		t.Fatalf("en-US 行产物错误: 期望 %s，实际 %s", builtEn.StagedHash, enRow.ArtifactHash)
	}

	// ---- 路由 artifact_id 指向各自语言的产物 ----
	if got := activeRouteArtifactID(t, db, projectID, "/en/about"); got != enRow.ID {
		t.Fatalf("/en/about 路由应指向 en-US 产物行 %s，实际 %s", enRow.ID, got)
	}
	if lang, canonical := artifactLangAndPath(t, db, enRow.ID); lang != "en-US" || canonical != "/en/about" {
		t.Fatalf("en-US 产物行内容错误: lang=%q path=%q", lang, canonical)
	}
	// zh-CN 产物行内容独立保留（即使其激活路由已被后续发布取消，见下方说明）。
	if lang, canonical := artifactLangAndPath(t, db, zhRow.ID); lang != "zh-CN" || canonical != "/about" {
		t.Fatalf("zh-CN 产物行内容错误: lang=%q path=%q", lang, canonical)
	}

	// ---- P3 核心回归点：Publish(en-US) 不得取消 /about 的激活路由 ----
	// 修复前（pages.active_path 单值）：Publish(en-US) 把 pages.active_path（=/about）
	// 当作本页旧路径 Deactivate，zh-CN 激活行被 DELETE，一页多语言无法同时在线。
	var zhActive int64
	if err := db.Table("page_routes").
		Where("project_id = ? AND path = ? AND route_kind = ?", projectID, "/about", "active").
		Count(&zhActive).Error; err != nil {
		t.Fatalf("统计 zh-CN 激活路由失败: %v", err)
	}
	if zhActive != 1 {
		t.Fatalf("一页两语言应同时在线：/about 激活行=%d（期望 1）", zhActive)
	}
	// 两语言的激活行分别指向各自语言的产物行。
	if got := activeRouteArtifactID(t, db, projectID, "/about"); got != zhRow.ID {
		t.Fatalf("/about 路由应指向 zh-CN 产物行 %s，实际 %s", zhRow.ID, got)
	}
	if got := activeRouteArtifactID(t, db, projectID, "/en/about"); got != enRow.ID {
		t.Fatalf("/en/about 路由应指向 en-US 产物行 %s，实际 %s", enRow.ID, got)
	}
	// page_publications 每语言一行（多语言激活状态真源）。
	var pubs []struct{ Lang, ActivePath string }
	if err := db.Table("page_publications").
		Select("lang, active_path").Where("page_id = ?", created.ID).Order("lang").Scan(&pubs).Error; err != nil {
		t.Fatalf("查询 page_publications 失败: %v", err)
	}
	if len(pubs) != 2 || pubs[0].Lang != "en-US" || pubs[0].ActivePath != "/en/about" ||
		pubs[1].Lang != "zh-CN" || pubs[1].ActivePath != "/about" {
		t.Fatalf("每语言激活状态错误: %+v", pubs)
	}
	t.Logf("产物行：zh-CN=%s(%s) en-US=%s(%s)；激活状态：%+v", zhRow.ID, zhRow.ArtifactHash, enRow.ID, enRow.ArtifactHash, pubs)
}
