package unit

import (
	"context"
	"testing"

	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	pubmodel "go_wp/internal/module/publication/model"
	"go_wp/pkg/i18n"

	"gorm.io/gorm"
)

type publicationRow struct {
	Lang         string
	ActivePath   string
	ArtifactHash string
}

func publicationsOfDB(t *testing.T, db *gorm.DB, pageID string) []publicationRow {
	t.Helper()
	var rows []publicationRow
	if err := db.Table("page_publications").
		Select("lang, active_path, artifact_hash").
		Where("page_id = ?", pageID).Order("lang").Scan(&rows).Error; err != nil {
		t.Fatalf("查询 page_publications 失败: %v", err)
	}
	return rows
}

func publicationOf(t *testing.T, db *gorm.DB, pageID, lang string) publicationRow {
	t.Helper()
	for _, r := range publicationsOfDB(t, db, pageID) {
		if r.Lang == lang {
			return r
		}
	}
	return publicationRow{}
}

func activeRouteArtifactHash(t *testing.T, db *gorm.DB, projectID, path string) string {
	t.Helper()
	var hash string
	if err := db.Raw(`SELECT a.artifact_hash FROM page_routes r
		JOIN page_artifacts a ON a.id = r.artifact_id
		WHERE r.project_id = ? AND r.path = ? AND r.route_kind = 'active'`, projectID, path).
		Scan(&hash).Error; err != nil {
		t.Fatalf("查询激活路由产物 hash 失败: %v", err)
	}
	return hash
}

func TestPagePublicationsLanguageScopedLifecycle(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	withLangPrefix(t)

	page := createPage(t, svc, projectID, "/about", headingDocument)

	zhHash := buildAndPublish(t, svc, page.ID)
	if kind := activeKind(t, "/about"); kind != "page" {
		t.Fatalf("/about 应激活为 page，实际 %s", kind)
	}

	builtEn, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID, Lang: "en-US"})
	if err != nil {
		t.Fatalf("en-US 构建失败: %v", err)
	}
	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: page.ID, Lang: "en-US"}); err != nil {
		t.Fatalf("en-US 发布失败: %v", err)
	}
	if kind := routeKind(t, db, projectID, "/about"); kind != pubmodel.RouteActive {
		t.Fatalf("Publish(en-US) 后 /about 仍应为 active，实际 %q", kind)
	}
	if kind := routeKind(t, db, projectID, "/en/about"); kind != pubmodel.RouteActive {
		t.Fatalf("/en/about 应为 active，实际 %q", kind)
	}
	if kind := activeKind(t, "/about"); kind != "page" {
		t.Fatalf("/about 的 FS 激活链接应保留，实际 %s", kind)
	}
	if got := activeRouteArtifactHash(t, db, projectID, "/about"); got != zhHash {
		t.Fatalf("zh-CN 路由产物应保持 %s，实际 %s", zhHash, got)
	}
	if got := activeRouteArtifactHash(t, db, projectID, "/en/about"); got != builtEn.StagedHash {
		t.Fatalf("en-US 路由产物应为 %s，实际 %s", builtEn.StagedHash, got)
	}

	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: page.ID}); err != nil {
		t.Fatalf("zh-CN 二次发布失败: %v", err)
	}
	if kind := routeKind(t, db, projectID, "/en/about"); kind != pubmodel.RouteActive {
		t.Fatalf("重新发布 zh-CN 后 /en/about 应仍 active，实际 %q", kind)
	}
	if kind := activeKind(t, "/en/about"); kind != "page" {
		t.Fatalf("/en/about 的 FS 激活链接应保留，实际 %s", kind)
	}

	saved, err := svc.SaveDraft(ctx, &pagedto.SaveDraftReq{
		ID: page.ID, ExpectedVersion: page.DraftVersion,
		DraftPath: "/about", DraftDocument: []byte(pageDocument),
	})
	if err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}
	_ = saved
	if _, err = svc.Build(ctx, &pagedto.BuildReq{ID: page.ID, Lang: "en-US"}); err != nil {
		t.Fatalf("en-US 二次构建失败: %v", err)
	}
	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: page.ID, Lang: "en-US"}); err != nil {
		t.Fatalf("en-US 二次发布失败: %v", err)
	}
	zhBefore := publicationOf(t, db, page.ID, "zh-CN")
	if _, err = svc.Rollback(ctx, &pagedto.RollbackReq{
		ID: page.ID, TargetHash: builtEn.StagedHash, Lang: "en-US",
	}); err != nil {
		t.Fatalf("en-US 回滚失败: %v", err)
	}
	if got := activeRouteArtifactHash(t, db, projectID, "/en/about"); got != builtEn.StagedHash {
		t.Fatalf("回滚后 en-US 路由产物应为 %s，实际 %s", builtEn.StagedHash, got)
	}
	if kind := routeKind(t, db, projectID, "/about"); kind != pubmodel.RouteActive {
		t.Fatalf("回滚 en-US 后 /about 应仍 active，实际 %q", kind)
	}
	if after := publicationOf(t, db, page.ID, "zh-CN"); after != zhBefore {
		t.Fatalf("回滚 en-US 不应改动 zh-CN 激活记录: 前 %+v 后 %+v", zhBefore, after)
	}

	if _, err = svc.UpdateURL(ctx, &pagedto.UpdateURLReq{
		ID: page.ID, NewPath: "/about-us", WithRedirect: false, Lang: "en-US",
	}); err != nil {
		t.Fatalf("en-US 改 URL 失败: %v", err)
	}
	if kind := routeKind(t, db, projectID, "/en/about-us"); kind != pubmodel.RouteActive {
		t.Fatalf("/en/about-us 应为 active，实际 %q", kind)
	}
	if kind := routeKind(t, db, projectID, "/en/about"); kind != "" {
		t.Fatalf("en-US 旧路径激活行应被取消，实际 %q", kind)
	}
	if kind := routeKind(t, db, projectID, "/about"); kind != pubmodel.RouteActive {
		t.Fatalf("改 en-US URL 不应影响 /about，实际 %q", kind)
	}
	if kind := activeKind(t, "/about"); kind != "page" {
		t.Fatalf("改 en-US URL 不应影响 zh-CN 的 FS 激活，实际 %s", kind)
	}
	en := publicationOf(t, db, page.ID, "en-US")
	if en.ActivePath != "/en/about-us" {
		t.Fatalf("en-US 激活路径应迁移到 /en/about-us，实际 %q", en.ActivePath)
	}
	zh := publicationOf(t, db, page.ID, "zh-CN")
	if zh.ActivePath != "/about" {
		t.Fatalf("zh-CN 激活路径不应变化，实际 %q", zh.ActivePath)
	}

	detail, err := svc.Detail(ctx, &pagedto.DetailReq{ProjectID: projectID, ID: page.ID})
	if err != nil {
		t.Fatalf("查询页面失败: %v", err)
	}
	if len(detail.Publications) != 2 {
		t.Fatalf("详情应带两条每语言激活状态，实际 %+v", detail.Publications)
	}

	if detail.ActivePath == nil || *detail.ActivePath != "/en/about-us" {
		t.Fatalf("pages.active_path 镜像应为最近发布语言路径，实际 %v", detail.ActivePath)
	}

	if err = svc.Delete(ctx, &pagedto.DeleteReq{ID: page.ID}); err != nil {
		t.Fatalf("删除页面失败: %v", err)
	}
	if rows := publicationsOfDB(t, db, page.ID); len(rows) != 0 {
		t.Fatalf("软删后应清理 page_publications，实际 %+v", rows)
	}
}

func TestPagePublicationsGateOffKeepsSinglePath(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	withLangURLMode(t, i18n.SiteLangURLModeOff)

	page := createPage(t, svc, projectID, "/about", headingDocument)
	zhHash := buildAndPublish(t, svc, page.ID)
	builtEn, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID, Lang: "en-US"})
	if err != nil {
		t.Fatalf("en-US 构建失败: %v", err)
	}
	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: page.ID, Lang: "en-US"}); err != nil {
		t.Fatalf("en-US 发布失败: %v", err)
	}

	var routeRows int64
	if err = db.Table("page_routes").
		Where("project_id = ? AND path = ?", projectID, "/about").Count(&routeRows).Error; err != nil {
		t.Fatalf("统计路由失败: %v", err)
	}
	if routeRows != 1 {
		t.Fatalf("关闭前缀时同路径只应有一行占用，实际 %d", routeRows)
	}
	if kind := routeKind(t, db, projectID, "/about"); kind != pubmodel.RouteActive {
		t.Fatalf("/about 应为 active，实际 %q", kind)
	}
	if got := activeRouteArtifactHash(t, db, projectID, "/about"); got != builtEn.StagedHash {
		t.Fatalf("关闭前缀时后发布语言覆盖线上产物: 期望 %s，实际 %s", builtEn.StagedHash, got)
	}
	pubs := publicationsOfDB(t, db, page.ID)
	if len(pubs) != 2 || pubs[0].ActivePath != "/about" || pubs[1].ActivePath != "/about" {
		t.Fatalf("每语言激活记录应各留一行且路径同为 /about，实际 %+v", pubs)
	}
	if zhHash == builtEn.StagedHash {
		t.Fatal("两种语言产物 hash 不应相同")
	}

	zhLang, zhCanonical := artifactManifest(t, zhHash)
	enLang, enCanonical := artifactManifest(t, builtEn.StagedHash)
	if zhLang != "zh-CN" || enLang != "en-US" {
		t.Fatalf("关闭前缀时 Manifest.lang 仍应记录构建语言: zh=%q en=%q", zhLang, enLang)
	}
	if zhCanonical != "/about" || enCanonical != "/about" {
		t.Fatalf("关闭前缀时产物路径应保持逻辑路径: zh=%q en=%q", zhCanonical, enCanonical)
	}
}

func TestPageBuildDeterministicSameInputTwice(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	withLangPrefix(t)

	page := createPage(t, svc, projectID, "/det", headingDocument)

	zh1, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID})
	if err != nil {
		t.Fatalf("zh 构建失败: %v", err)
	}
	zh2, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID})
	if err != nil {
		t.Fatalf("zh 二次构建失败: %v", err)
	}
	en1, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID, Lang: "en-US"})
	if err != nil {
		t.Fatalf("en 构建失败: %v", err)
	}
	en2, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID, Lang: "en-US"})
	if err != nil {
		t.Fatalf("en 二次构建失败: %v", err)
	}
	if zh1.StagedHash != zh2.StagedHash {
		t.Fatalf("同语言同输入两次构建 hash 应相同: %s vs %s", zh1.StagedHash, zh2.StagedHash)
	}
	if en1.StagedHash != en2.StagedHash {
		t.Fatalf("同语言同输入两次构建 hash 应相同: %s vs %s", en1.StagedHash, en2.StagedHash)
	}
	if zh1.StagedHash == en1.StagedHash {
		t.Fatal("不同语言产物 hash 不应相同")
	}

	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: page.ID, Lang: "en-US"}); err != nil {
		t.Fatalf("发布失败: %v", err)
	}
	first := publicationOf(t, db, page.ID, "en-US")
	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: page.ID, Lang: "en-US"}); err != nil {
		t.Fatalf("重复发布失败: %v", err)
	}
	second := publicationOf(t, db, page.ID, "en-US")
	if first != second {
		t.Fatalf("重复发布应幂等: 前 %+v 后 %+v", first, second)
	}
	t.Logf("确定性：zh=%s en=%s", zh1.StagedHash, en1.StagedHash)
}

func TestPageStagingsPerLanguageIndependent(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	withLangPrefix(t)

	page := createPage(t, svc, projectID, "/staged", headingDocument)
	zh, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID})
	if err != nil {
		t.Fatalf("zh 构建失败: %v", err)
	}
	en, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID, Lang: "en-US"})
	if err != nil {
		t.Fatalf("en 构建失败: %v", err)
	}
	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: page.ID}); err != nil {
		t.Fatalf("zh 发布失败（暂存指针被 en 覆盖？）: %v", err)
	}
	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: page.ID, Lang: "en-US"}); err != nil {
		t.Fatalf("en 发布失败: %v", err)
	}
	if got := activeRouteArtifactHash(t, db, projectID, "/staged"); got != zh.StagedHash {
		t.Fatalf("zh-CN 路由产物应为 %s，实际 %s", zh.StagedHash, got)
	}
	if got := activeRouteArtifactHash(t, db, projectID, "/en/staged"); got != en.StagedHash {
		t.Fatalf("en-US 路由产物应为 %s，实际 %s", en.StagedHash, got)
	}

	if err = db.Exec("DELETE FROM page_stagings WHERE page_id = ? AND lang = ?", page.ID, "zh-CN").Error; err != nil {
		t.Fatalf("清理 zh-CN 暂存失败: %v", err)
	}
	_, err = svc.Publish(ctx, &pagedto.PublishReq{ID: page.ID})
	if err == nil || err.Error() != pageenums.ErrNoStagedArtifact {
		t.Fatalf("跨语言暂存应报 %q，实际 %v", pageenums.ErrNoStagedArtifact, err)
	}
	if got := activeRouteArtifactHash(t, db, projectID, "/staged"); got != zh.StagedHash {
		t.Fatalf("被拒绝的发布不应改变 zh-CN 线上产物，实际 %s", got)
	}
}
