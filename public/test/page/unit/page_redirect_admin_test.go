// page_redirect_admin_test.go — 重定向管理（审计 SEO-025）的链路测试。
//
// 覆盖能力的完整面：列出当前重定向（DB 占用账 + 访问面 redirect.json 合并成一行）、
// 手动新增、删除、四种拒绝（自环 / 成环 / 目标不存在 / 源被占用）、
// 多跳链检测与一键合并。用真实 PG + 真实产物目录（support.NewMigratedPGTestDB）。
package unit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
)

// TestRedirectAdminCreateListDelete 手动新增 → 列表可见 → 删除后下线且可重新占用。
func TestRedirectAdminCreateListDelete(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	page := createPage(t, svc, projectID, "/landing", headingDocument)
	buildAndPublish(t, svc, page.ID)

	created, err := svc.CreateRedirect(ctx, &pagedto.RedirectCreateReq{
		ProjectID: projectID, SourcePath: "/promo/spring", TargetPath: "/landing",
	})
	if err != nil {
		t.Fatalf("新增重定向失败: %v", err)
	}
	if !created.Effective || created.TargetPath != "/landing" || created.StatusCode != 301 {
		t.Fatalf("新增结果错误: %+v", created)
	}
	if created.OwnerKind != "page" {
		t.Fatalf("归属者应为目标页面: %+v", created)
	}
	// 访问面必须真的可跳：只有 DB 行没有 redirect.json 时线上仍是 404。
	if got := activeRedirectTarget(t, "/promo/spring"); got != "/landing" {
		t.Fatalf("访问面 301 应指向 /landing: %q", got)
	}
	if kind := routeKind(t, db, projectID, "/promo/spring"); kind != "redirect" {
		t.Fatalf("路由行应为 redirect: %s", kind)
	}

	res, err := svc.ListRedirects(ctx, &pagedto.RedirectListReq{ProjectID: projectID})
	if err != nil {
		t.Fatalf("列出重定向失败: %v", err)
	}
	if res.Total != 1 || res.EffectiveCount != 1 || len(res.Items) != 1 {
		t.Fatalf("列表统计错误: %+v", res)
	}
	if res.Items[0].SourcePath != "/promo/spring" || res.Items[0].TargetPath != "/landing" ||
		res.Items[0].MultiHop || res.Items[0].Loop || res.Items[0].UpdatedAt == "" {
		t.Fatalf("列表条目错误: %+v", res.Items[0])
	}
	if res.ProjectID != projectID || len(res.Projects) != 1 {
		t.Fatalf("工程上下文错误: %+v", res)
	}

	if err = svc.DeleteRedirect(ctx, &pagedto.RedirectDeleteReq{ProjectID: projectID, Path: "/promo/spring"}); err != nil {
		t.Fatalf("删除重定向失败: %v", err)
	}
	if kind := routeKind(t, db, projectID, "/promo/spring"); kind != "" {
		t.Fatalf("删除后路由行应消失: %s", kind)
	}
	if kind := activeKind(t, "/promo/spring"); kind != "none" {
		t.Fatalf("删除后访问面激活应解除: %s", kind)
	}
	after, err := svc.ListRedirects(ctx, &pagedto.RedirectListReq{ProjectID: projectID})
	if err != nil || after.Total != 0 {
		t.Fatalf("删除后列表应为空: %+v err=%v", after, err)
	}
	// 删除即释放路径占用：同一条源路径必须能再次新增（否则「删不掉又加不回」）。
	if _, err = svc.CreateRedirect(ctx, &pagedto.RedirectCreateReq{
		ProjectID: projectID, SourcePath: "/promo/spring", TargetPath: "/landing",
	}); err != nil {
		t.Fatalf("删除后应可重新占用该源路径: %v", err)
	}

	// 删除不存在的重定向：明确报「不存在」，不静默成功。
	if err = svc.DeleteRedirect(ctx, &pagedto.RedirectDeleteReq{ProjectID: projectID, Path: "/nope"}); err == nil ||
		err.Error() != pageenums.ErrRedirectNotFound {
		t.Fatalf("删除不存在的重定向应报 %q: %v", pageenums.ErrRedirectNotFound, err)
	}
}

// TestRedirectAdminCreateValidation 四类非法新增必须被拒（每条都对应真实会出事的场景）。
func TestRedirectAdminCreateValidation(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	for _, p := range []string{"/alpha", "/beta"} {
		page := createPage(t, svc, projectID, p, headingDocument)
		buildAndPublish(t, svc, page.ID)
	}

	// 1) 自环：源与目标相同。
	if _, err := svc.CreateRedirect(ctx, &pagedto.RedirectCreateReq{
		ProjectID: projectID, SourcePath: "/loop", TargetPath: "/loop",
	}); err == nil || err.Error() != pageenums.ErrRedirectLoop {
		t.Fatalf("自环应报 %q: %v", pageenums.ErrRedirectLoop, err)
	}

	// 2) 目标不存在（或未发布）：重定向只能指向已激活的站内路径。
	if _, err := svc.CreateRedirect(ctx, &pagedto.RedirectCreateReq{
		ProjectID: projectID, SourcePath: "/ghost", TargetPath: "/nowhere",
	}); err == nil || err.Error() != pageenums.ErrRedirectTargetMiss {
		t.Fatalf("目标缺失应报 %q: %v", pageenums.ErrRedirectTargetMiss, err)
	}

	// 3) 源路径已被 active 占用（页面自己住在这儿）。
	if _, err := svc.CreateRedirect(ctx, &pagedto.RedirectCreateReq{
		ProjectID: projectID, SourcePath: "/alpha", TargetPath: "/beta",
	}); err == nil || err.Error() != pageenums.ErrRedirectOccupied {
		t.Fatalf("源被占用应报 %q: %v", pageenums.ErrRedirectOccupied, err)
	}

	// 4) 参数不合法：空工程 / 相对路径。
	if _, err := svc.CreateRedirect(ctx, &pagedto.RedirectCreateReq{SourcePath: "/a", TargetPath: "/beta"}); err == nil ||
		err.Error() != pageenums.ErrInvalidParam {
		t.Fatalf("空工程应报 %q: %v", pageenums.ErrInvalidParam, err)
	}

	// 前三条都没落地：失败不得留下半成品（路由行与访问面链接都不该出现）。
	for _, p := range []string{"/loop", "/ghost"} {
		if kind := routeKind(t, db, projectID, p); kind != "" {
			t.Fatalf("失败的新增不应留下路由行 %s: %s", p, kind)
		}
		if kind := activeKind(t, p); kind != "none" {
			t.Fatalf("失败的新增不应留下激活链接 %s: %s", p, kind)
		}
	}
}

// TestRedirectAdminDetectsLoop 脏数据（链指回自己）必须被识别为成环，
// 而不是让页面挂死或静默当成正常条目：列出时标 Loop，合并时拒绝。
func TestRedirectAdminDetectsLoop(t *testing.T) {
	_, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	page := createPage(t, svc, projectID, "/alpha", headingDocument)
	buildAndPublish(t, svc, page.ID)

	if _, err := svc.CreateRedirect(ctx, &pagedto.RedirectCreateReq{
		ProjectID: projectID, SourcePath: "/ring", TargetPath: "/alpha",
	}); err != nil {
		t.Fatalf("新增重定向失败: %v", err)
	}
	// 把产物改成指向自己（模拟外部误改 / 历史脏数据）。
	forceRedirectTarget(t, "/ring", "/ring")

	res, err := svc.ListRedirects(ctx, &pagedto.RedirectListReq{ProjectID: projectID})
	if err != nil {
		t.Fatalf("列出重定向失败: %v", err)
	}
	if len(res.Items) != 1 || !res.Items[0].Loop || res.LoopCount != 1 {
		t.Fatalf("自环条目应标记成环: %+v", res)
	}
	if res.Items[0].MultiHop {
		t.Fatalf("成环不该同时标成多跳: %+v", res.Items[0])
	}
	if _, err = svc.MergeRedirectChain(ctx, &pagedto.RedirectMergeReq{ProjectID: projectID, Path: "/ring"}); err == nil ||
		err.Error() != pageenums.ErrRedirectLoop {
		t.Fatalf("合并成环链应报 %q: %v", pageenums.ErrRedirectLoop, err)
	}
}

// TestRedirectAdminMultiHopChainMerged 改两次 URL 会留下 A→B→C 链：
// 列表要检测出来，一键合并要把它变成 A→C（只改访问面产物，不动 DB 行）。
func TestRedirectAdminMultiHopChainMerged(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	page := createPage(t, svc, projectID, "/chain-a", headingDocument)
	buildAndPublish(t, svc, page.ID)

	// 连续两次改 URL 且保留旧链接（真实运营的操作序列）。
	if _, err := svc.UpdateURL(ctx, &pagedto.UpdateURLReq{ID: page.ID, NewPath: "/chain-b", WithRedirect: true}); err != nil {
		t.Fatalf("第一次改 URL 失败: %v", err)
	}
	if _, err := svc.UpdateURL(ctx, &pagedto.UpdateURLReq{ID: page.ID, NewPath: "/chain-c", WithRedirect: true}); err != nil {
		t.Fatalf("第二次改 URL 失败: %v", err)
	}
	if got := activeRedirectTarget(t, "/chain-a"); got != "/chain-b" {
		t.Fatalf("第一条应指向中间路径: %q", got)
	}

	res, err := svc.ListRedirects(ctx, &pagedto.RedirectListReq{ProjectID: projectID})
	if err != nil {
		t.Fatalf("列出重定向失败: %v", err)
	}
	if res.Total != 2 || res.MultiHopCount != 1 {
		t.Fatalf("应检出 1 条多跳链: %+v", res)
	}
	var head *pagedto.RedirectItem
	for i := range res.Items {
		if res.Items[i].SourcePath == "/chain-a" {
			head = &res.Items[i]
		}
	}
	if head == nil || !head.MultiHop || head.FinalPath != "/chain-c" || head.TargetPath != "/chain-b" {
		t.Fatalf("链首条目错误: %+v", head)
	}

	merged, err := svc.MergeRedirectChain(ctx, &pagedto.RedirectMergeReq{ProjectID: projectID, Path: "/chain-a"})
	if err != nil {
		t.Fatalf("合并链失败: %v", err)
	}
	if merged.TargetPath != "/chain-c" || merged.MultiHop {
		t.Fatalf("合并结果错误: %+v", merged)
	}
	if got := activeRedirectTarget(t, "/chain-a"); got != "/chain-c" {
		t.Fatalf("合并后应直达链尾: %q", got)
	}
	// 中间那条不动：它仍是一条独立的重定向（自己的老链接照样能用）。
	if kind := routeKind(t, db, projectID, "/chain-b"); kind != "redirect" {
		t.Fatalf("中间路径仍是重定向行: %s", kind)
	}
	after, err := svc.ListRedirects(ctx, &pagedto.RedirectListReq{ProjectID: projectID})
	if err != nil || after.MultiHopCount != 0 {
		t.Fatalf("合并后不应再有多跳: %+v err=%v", after, err)
	}
	// 幂等：已经直达时再点一次不报错。
	if _, err = svc.MergeRedirectChain(ctx, &pagedto.RedirectMergeReq{ProjectID: projectID, Path: "/chain-a"}); err != nil {
		t.Fatalf("重复合并应幂等: %v", err)
	}
}

// forceRedirectTarget 直接改写访问面上的 redirect.json（模拟外部误改的脏数据）。
// 产物目录是内容寻址的，这里不重算 hash —— 中间件与 Inspect 都只读 redirect.json，
// 正是这种「文件说了算」的判定方式让环形链在真实环境也可能出现。
func forceRedirectTarget(t *testing.T, path, target string) {
	t.Helper()
	link := filepath.Join(activeDir(t), strings.TrimPrefix(path, "/"))
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatalf("激活链接不可解析 %s: %v", path, err)
	}
	entry := `{"targetPath":"` + target + `","statusCode":301}`
	if err = os.WriteFile(filepath.Join(resolved, "redirect.json"), []byte(entry), 0o644); err != nil {
		t.Fatalf("改写 redirect.json 失败: %v", err)
	}
}
