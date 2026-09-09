package unit

// page_publications_test.go — 每语言激活状态（多语言 P3，docs/06-D-site-i18n.md §15.5 第 2 条）。
//
// 回归对象：pages.active_path 单值时，Publish(en-US) 会把 /about 当作本页旧路径
// 取消激活（Deactivate 删除路由行），「一页多语言同时在线」不成立。
// 本文件验证：Publish / Rollback / UpdateURL（含旧路径 Deactivate）全部按语言作用域，
// 语言之间互不干扰；灰度开关关闭时保持单路径语义。

import (
	"context"
	"testing"

	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	pubmodel "go_wp/internal/module/publication/model"
	"go_wp/pkg/i18n"

	"gorm.io/gorm"
)

// publicationRow 页面某语言的激活记录投影。
type publicationRow struct {
	Lang         string
	ActivePath   string
	ArtifactHash string
}

// publicationsOfDB 读取 page_publications 全部行（语言升序）。
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

// publicationOf 取某语言的激活记录（不存在返回零值）。
func publicationOf(t *testing.T, db *gorm.DB, pageID, lang string) publicationRow {
	t.Helper()
	for _, r := range publicationsOfDB(t, db, pageID) {
		if r.Lang == lang {
			return r
		}
	}
	return publicationRow{}
}

// activeRouteArtifactHash 读取激活路由指向产物的内容 hash。
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

// TestPagePublicationsLanguageScopedLifecycle 一页两语言同时在线，
// 且 Publish / Rollback / UpdateURL 只作用于目标语言。
func TestPagePublicationsLanguageScopedLifecycle(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	withLangPrefix(t)

	page := createPage(t, svc, projectID, "/about", headingDocument)

	// ---- 1) 默认语言 zh-CN 上线 ----
	zhHash := buildAndPublish(t, svc, page.ID)
	if kind := activeKind(t, "/about"); kind != "page" {
		t.Fatalf("/about 应激活为 page，实际 %s", kind)
	}

	// ---- 2) en-US 上线：不得取消 zh-CN 的激活 ----
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

	// ---- 3) zh-CN 重新发布：不得影响 en-US ----
	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: page.ID}); err != nil {
		t.Fatalf("zh-CN 二次发布失败: %v", err)
	}
	if kind := routeKind(t, db, projectID, "/en/about"); kind != pubmodel.RouteActive {
		t.Fatalf("重新发布 zh-CN 后 /en/about 应仍 active，实际 %q", kind)
	}
	if kind := activeKind(t, "/en/about"); kind != "page" {
		t.Fatalf("/en/about 的 FS 激活链接应保留，实际 %s", kind)
	}

	// ---- 4) 改草稿后再发布 en-US，回滚 en-US 到上一版：只动 en-US ----
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

	// ---- 5) 改 en-US 的 URL（无重定向）：只迁移 en-US 路径 ----
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

	// ---- 6) 详情投影带每语言激活状态（多语言真源） ----
	detail, err := svc.Detail(ctx, &pagedto.DetailReq{ID: page.ID})
	if err != nil {
		t.Fatalf("查询页面失败: %v", err)
	}
	if len(detail.Publications) != 2 {
		t.Fatalf("详情应带两条每语言激活状态，实际 %+v", detail.Publications)
	}
	// pages.active_path 是「最近发布语言」的单值镜像（en-US 最后发布/改 URL）。
	if detail.ActivePath == nil || *detail.ActivePath != "/en/about-us" {
		t.Fatalf("pages.active_path 镜像应为最近发布语言路径，实际 %v", detail.ActivePath)
	}

	// ---- 7) 软删清理每语言激活状态 ----
	if err = svc.Delete(ctx, &pagedto.DeleteReq{ID: page.ID}); err != nil {
		t.Fatalf("删除页面失败: %v", err)
	}
	if rows := publicationsOfDB(t, db, page.ID); len(rows) != 0 {
		t.Fatalf("软删后应清理 page_publications，实际 %+v", rows)
	}
}

// TestPagePublicationsGateOffKeepsSinglePath off 方案（各语言共用逻辑路径）时，
// 两种语言共享逻辑路径：路由只有一行、每语言仍各留一条激活记录，
// 后发布者覆盖线上内容——「一页多语言同时在线」必须使用 default_plain / all_prefix。
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
	// Manifest.lang 仍按构建语言记录（语言是构建维度），只是路径不带前缀。
	zhLang, zhCanonical := artifactManifest(t, zhHash)
	enLang, enCanonical := artifactManifest(t, builtEn.StagedHash)
	if zhLang != "zh-CN" || enLang != "en-US" {
		t.Fatalf("关闭前缀时 Manifest.lang 仍应记录构建语言: zh=%q en=%q", zhLang, enLang)
	}
	if zhCanonical != "/about" || enCanonical != "/about" {
		t.Fatalf("关闭前缀时产物路径应保持逻辑路径: zh=%q en=%q", zhCanonical, enCanonical)
	}
}

// TestPageBuildDeterministicSameInputTwice 确定性构建：同输入两次构建字节相同
// （hash 相同），不同语言输入产生不同 hash；重复发布幂等。
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
	// 重复发布同一语言幂等：激活行与激活状态保持一致。
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
// TestPageStagingsPerLanguageIndependent 「先构建两种语言、再逐个发布」可用：
// pages.staged_artifact_id 单值时代，Build(en-US) 覆盖 Build(zh-CN) 的暂存指针，
// 第二个 Publish(zh-CN) 复构建 hash 与暂存不一致 → ErrRebuildRequired。
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

	// 跨语言暂存不互认：删掉 zh-CN 的暂存行后，pages 单值镜像里只剩 en-US 的产物，
	// Publish(zh-CN) 必须报「无暂存产物」，而不是把 en-US 的产物发布到 /。
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
