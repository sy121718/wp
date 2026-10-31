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

	if _, err = svc.CreateRedirect(ctx, &pagedto.RedirectCreateReq{
		ProjectID: projectID, SourcePath: "/promo/spring", TargetPath: "/landing",
	}); err != nil {
		t.Fatalf("删除后应可重新占用该源路径: %v", err)
	}

	if err = svc.DeleteRedirect(ctx, &pagedto.RedirectDeleteReq{ProjectID: projectID, Path: "/nope"}); err == nil ||
		err.Error() != pageenums.ErrRedirectNotFound {
		t.Fatalf("删除不存在的重定向应报 %q: %v", pageenums.ErrRedirectNotFound, err)
	}
}

func TestRedirectAdminCreateValidation(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	for _, p := range []string{"/alpha", "/beta"} {
		page := createPage(t, svc, projectID, p, headingDocument)
		buildAndPublish(t, svc, page.ID)
	}

	if _, err := svc.CreateRedirect(ctx, &pagedto.RedirectCreateReq{
		ProjectID: projectID, SourcePath: "/loop", TargetPath: "/loop",
	}); err == nil || err.Error() != pageenums.ErrRedirectLoop {
		t.Fatalf("自环应报 %q: %v", pageenums.ErrRedirectLoop, err)
	}

	if _, err := svc.CreateRedirect(ctx, &pagedto.RedirectCreateReq{
		ProjectID: projectID, SourcePath: "/ghost", TargetPath: "/nowhere",
	}); err == nil || err.Error() != pageenums.ErrRedirectTargetMiss {
		t.Fatalf("目标缺失应报 %q: %v", pageenums.ErrRedirectTargetMiss, err)
	}

	if _, err := svc.CreateRedirect(ctx, &pagedto.RedirectCreateReq{
		ProjectID: projectID, SourcePath: "/alpha", TargetPath: "/beta",
	}); err == nil || err.Error() != pageenums.ErrRedirectOccupied {
		t.Fatalf("源被占用应报 %q: %v", pageenums.ErrRedirectOccupied, err)
	}

	if _, err := svc.CreateRedirect(ctx, &pagedto.RedirectCreateReq{SourcePath: "/a", TargetPath: "/beta"}); err == nil ||
		err.Error() != pageenums.ErrInvalidParam {
		t.Fatalf("空工程应报 %q: %v", pageenums.ErrInvalidParam, err)
	}

	for _, p := range []string{"/loop", "/ghost"} {
		if kind := routeKind(t, db, projectID, p); kind != "" {
			t.Fatalf("失败的新增不应留下路由行 %s: %s", p, kind)
		}
		if kind := activeKind(t, p); kind != "none" {
			t.Fatalf("失败的新增不应留下激活链接 %s: %s", p, kind)
		}
	}
}

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

func TestRedirectAdminMultiHopChainMerged(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	page := createPage(t, svc, projectID, "/chain-a", headingDocument)
	buildAndPublish(t, svc, page.ID)

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
	var head *pagedto.RedirectItemResp
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

	if kind := routeKind(t, db, projectID, "/chain-b"); kind != "redirect" {
		t.Fatalf("中间路径仍是重定向行: %s", kind)
	}
	after, err := svc.ListRedirects(ctx, &pagedto.RedirectListReq{ProjectID: projectID})
	if err != nil || after.MultiHopCount != 0 {
		t.Fatalf("合并后不应再有多跳: %+v err=%v", after, err)
	}

	if _, err = svc.MergeRedirectChain(ctx, &pagedto.RedirectMergeReq{ProjectID: projectID, Path: "/chain-a"}); err != nil {
		t.Fatalf("重复合并应幂等: %v", err)
	}
}

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
