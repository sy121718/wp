package unit

import (
	"context"
	"testing"

	pagedto "go_wp/internal/module/page/dto"

	"gorm.io/gorm"
)

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

func TestPageArtifactLangCoexistAndRouteBinding(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	withLangPrefix(t)

	created := createPage(t, svc, projectID, "/about", headingDocument)

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

	if zhRow.ArtifactHash != builtZh.StagedHash {
		t.Fatalf("zh-CN 行产物被 en-US 构建覆盖: 期望 %s，实际 %s", builtZh.StagedHash, zhRow.ArtifactHash)
	}
	if enRow.ArtifactHash != builtEn.StagedHash {
		t.Fatalf("en-US 行产物错误: 期望 %s，实际 %s", builtEn.StagedHash, enRow.ArtifactHash)
	}

	if got := activeRouteArtifactID(t, db, projectID, "/en/about"); got != enRow.ID {
		t.Fatalf("/en/about 路由应指向 en-US 产物行 %s，实际 %s", enRow.ID, got)
	}
	if lang, canonical := artifactLangAndPath(t, db, enRow.ID); lang != "en-US" || canonical != "/en/about" {
		t.Fatalf("en-US 产物行内容错误: lang=%q path=%q", lang, canonical)
	}

	if lang, canonical := artifactLangAndPath(t, db, zhRow.ID); lang != "zh-CN" || canonical != "/about" {
		t.Fatalf("zh-CN 产物行内容错误: lang=%q path=%q", lang, canonical)
	}

	var zhActive int64
	if err := db.Table("page_routes").
		Where("project_id = ? AND path = ? AND route_kind = ?", projectID, "/about", "active").
		Count(&zhActive).Error; err != nil {
		t.Fatalf("统计 zh-CN 激活路由失败: %v", err)
	}
	if zhActive != 1 {
		t.Fatalf("一页两语言应同时在线：/about 激活行=%d（期望 1）", zhActive)
	}

	if got := activeRouteArtifactID(t, db, projectID, "/about"); got != zhRow.ID {
		t.Fatalf("/about 路由应指向 zh-CN 产物行 %s，实际 %s", zhRow.ID, got)
	}
	if got := activeRouteArtifactID(t, db, projectID, "/en/about"); got != enRow.ID {
		t.Fatalf("/en/about 路由应指向 en-US 产物行 %s，实际 %s", enRow.ID, got)
	}

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
