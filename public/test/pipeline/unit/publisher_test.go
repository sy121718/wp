package unit

import (
	"context"
	"errors"
	publish "go_wp/pkg/publish"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go_wp/internal/pipeline"
)

const (
	docV1 = `{"settings":{"layout":{"mode":"full"}},"root":[]}`

	docV2 = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"h1","type":"core.heading","props":{"text":"你好"}}]}`
)

func newPublisherEnv(t *testing.T, opts ...pipeline.Option) (*pipeline.Publisher, *publish.LocalStore, *publish.LocalPublicationStore, string) {
	t.Helper()
	root := t.TempDir()
	store := &publish.LocalStore{Root: root}
	pub := &publish.LocalPublicationStore{ActiveRoot: filepath.Join(root, "public/active")}
	// 不注入编译函数就构建会显式失败；测试统一用应用默认编译。
	p := pipeline.NewPublisher(store, pub, append([]pipeline.Option{pipeline.WithCompile(pipeline.DefaultCompile)}, opts...)...)
	return p, store, pub, root
}

func activeHTML(t *testing.T, pub *publish.LocalPublicationStore, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(pub.ActiveRoot, strings.TrimPrefix(path, "/"), "index.html"))
	if err != nil {
		t.Fatalf("读取激活产物失败: %v", err)
	}
	return string(data)
}

func TestPublisherLifecycle(t *testing.T) {
	p, _, pub, _ := newPublisherEnv(t)

	v, err := p.SaveDraft("page-1", 0, "/about", []byte(docV1))
	if err != nil || v != 1 {
		t.Fatalf("保存草稿失败: v=%d err=%v", v, err)
	}
	hash, err := p.Build(context.Background(), "page-1", 1)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	st, _ := p.Status("page-1")
	if st.Status != pipeline.StateReady || st.StagedHash != hash {
		t.Errorf("构建后应为 ready 且有暂存产物: %+v", st)
	}

	published, err := p.Publish("page-1")
	if err != nil {
		t.Fatalf("发布失败: %v", err)
	}
	if published != hash {
		t.Errorf("发布返回的哈希应为暂存产物哈希")
	}
	st, _ = p.Status("page-1")
	if st.Status != pipeline.StatePublished || st.ActiveHash != hash {
		t.Errorf("发布后应为 published: %+v", st)
	}
	if html := activeHTML(t, pub, "/about"); !strings.Contains(html, "<!DOCTYPE html>") {
		t.Errorf("激活路径入口应为完整文档:\n%s", html)
	}
	if len(st.Histories) != 1 || st.Histories[0].Status != pipeline.StatePublished {
		t.Errorf("历史应记录一个 published 版本: %+v", st.Histories)
	}
}

func TestPublisherDeterminism(t *testing.T) {
	p, _, _, _ := newPublisherEnv(t)

	if _, err := p.SaveDraft("page-1", 0, "/about", []byte(docV1)); err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}
	h1, err := p.Build(context.Background(), "page-1", 1)
	if err != nil {
		t.Fatalf("首次构建失败: %v", err)
	}
	if _, err = p.Publish("page-1"); err != nil {
		t.Fatalf("首次发布失败: %v", err)
	}

	if _, err = p.SaveDraft("page-1", 1, "/about", []byte(docV1)); err != nil {
		t.Fatalf("再次保存草稿失败: %v", err)
	}
	h2, err := p.Build(context.Background(), "page-1", 2)
	if err != nil {
		t.Fatalf("再次构建失败: %v", err)
	}
	if h1 != h2 {
		t.Errorf("确定性构建失效: %s vs %s", h1, h2)
	}
	if _, err = p.Publish("page-1"); err != nil {
		t.Fatalf("二次发布失败: %v", err)
	}
	st, _ := p.Status("page-1")
	if st.Histories[0].Status != pipeline.StateSuperseded {
		t.Errorf("旧版本应转为 Superseded: %+v", st.Histories)
	}
	if st.Histories[1].Status != pipeline.StatePublished {
		t.Errorf("新版本应为 Published: %+v", st.Histories)
	}
}

func TestPublisherVersionConflict(t *testing.T) {
	p, _, _, _ := newPublisherEnv(t)

	if _, err := p.SaveDraft("page-1", 0, "/about", []byte(docV1)); err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}
	if _, err := p.SaveDraft("page-1", 1, "/about", []byte(docV2)); err != nil {
		t.Fatalf("二次保存失败: %v", err)
	}
	if _, err := p.SaveDraft("page-1", 1, "/about", []byte(docV1)); !errors.Is(err, pipeline.ErrVersionConflict) {
		t.Errorf("旧版本写入应被拒绝: %v", err)
	}
	if _, err := p.Build(context.Background(), "page-1", 1); !errors.Is(err, pipeline.ErrVersionConflict) {
		t.Errorf("基于旧版本的构建应被拒绝: %v", err)
	}

	if _, err := p.Publish("page-1"); !errors.Is(err, pipeline.ErrNoStagedArtifact) {
		t.Errorf("无暂存产物不允许发布: %v", err)
	}
}

func TestPublisherBuildFailure(t *testing.T) {
	boom := errors.New("编译器故障")
	p, _, pub, _ := newPublisherEnv(t,
		pipeline.WithCompile(func(context.Context, pipeline.BuildInput) ([]byte, error) { return nil, boom }))

	if _, err := p.SaveDraft("page-1", 0, "/about", []byte(docV1)); err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}
	if _, err := p.Build(context.Background(), "page-1", 1); !errors.Is(err, boom) {
		t.Fatalf("构建应失败: %v", err)
	}
	st, _ := p.Status("page-1")
	if st.Status != pipeline.StateFailed || st.FailedReason == "" {
		t.Errorf("构建失败后应为 failed: %+v", st)
	}
	if _, err := p.Publish("page-1"); !errors.Is(err, pipeline.ErrNoStagedArtifact) {
		t.Errorf("构建失败后不允许发布: %v", err)
	}
	if insp, err := pub.Inspect("/about"); err != nil || insp.Kind != publish.PublicationNone {
		t.Errorf("构建失败不应激活任何 URL: %+v %v", insp, err)
	}
}

func TestPublisherRollback(t *testing.T) {
	p, _, pub, _ := newPublisherEnv(t)

	if _, err := p.SaveDraft("page-1", 0, "/about", []byte(docV1)); err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}
	h1, _ := p.Build(context.Background(), "page-1", 1)
	if _, err := p.Publish("page-1"); err != nil {
		t.Fatalf("首次发布失败: %v", err)
	}

	if _, err := p.SaveDraft("page-1", 1, "/about", []byte(docV2)); err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}
	if _, err := p.Build(context.Background(), "page-1", 2); err != nil {
		t.Fatalf("二次构建失败: %v", err)
	}
	if _, err := p.Publish("page-1"); err != nil {
		t.Fatalf("二次发布失败: %v", err)
	}
	if html := activeHTML(t, pub, "/about"); !strings.Contains(html, "你好") {
		t.Errorf("二次发布后激活的应为 V2:\n%s", html)
	}

	if err := p.Rollback("page-1", h1); err != nil {
		t.Fatalf("回滚失败: %v", err)
	}
	st, _ := p.Status("page-1")
	if st.ActiveHash != h1 || st.Status != pipeline.StatePublished {
		t.Errorf("回滚后应为 V1 活跃: %+v", st)
	}
	if html := activeHTML(t, pub, "/about"); strings.Contains(html, "你好") {
		t.Errorf("回滚后不应再输出 V2 内容:\n%s", html)
	}
	found := map[string]string{}
	for _, h := range st.Histories {
		found[h.Hash] = h.Status
	}
	if found[h1] != pipeline.StatePublished {
		t.Errorf("回滚目标版本应为 Published: %+v", st.Histories)
	}
}

func TestPublisherRollbackPathMismatch(t *testing.T) {
	p, _, _, _ := newPublisherEnv(t)

	if _, err := p.SaveDraft("page-1", 0, "/about", []byte(docV1)); err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}
	h1, _ := p.Build(context.Background(), "page-1", 1)
	if _, err := p.Publish("page-1"); err != nil {
		t.Fatalf("首次发布失败: %v", err)
	}
	if _, err := p.UpdateURL(context.Background(), "page-1", "/about-us", false); err != nil {
		t.Fatalf("URL 修改失败: %v", err)
	}
	if err := p.Rollback("page-1", h1); !errors.Is(err, pipeline.ErrRollbackPathMismatch) {
		t.Errorf("路径不一致的回滚应被拒绝: %v", err)
	}
}

func TestPublisherUpdateURLWithRedirect(t *testing.T) {
	p, _, pub, _ := newPublisherEnv(t)

	if _, err := p.SaveDraft("page-1", 0, "/about", []byte(docV1)); err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}
	if _, err := p.Build(context.Background(), "page-1", 1); err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if _, err := p.Publish("page-1"); err != nil {
		t.Fatalf("首次发布失败: %v", err)
	}

	oldPath, err := p.UpdateURL(context.Background(), "page-1", "/about-us", true)
	if err != nil {
		t.Fatalf("URL 修改失败: %v", err)
	}
	if oldPath != "/about" {
		t.Errorf("应返回旧路径: %s", oldPath)
	}
	st, _ := p.Status("page-1")
	if st.Path != "/about-us" || st.Status != pipeline.StatePublished {
		t.Errorf("URL 修改后状态错误: %+v", st)
	}

	insp, err := pub.Inspect("/about-us")
	if err != nil || insp.Kind != publish.PublicationPage {
		t.Errorf("新 URL 应为激活页面: %+v %v", insp, err)
	}
	insp, err = pub.Inspect("/about")
	if err != nil || insp.Kind != publish.PublicationRedirect ||
		insp.Redirect.TargetPath != "/about-us" || insp.Redirect.StatusCode != 301 {
		t.Errorf("旧 URL 应为 301 重定向: %+v %v", insp, err)
	}
}

func TestPublisherUpdateURLNoRedirect(t *testing.T) {
	p, _, pub, _ := newPublisherEnv(t)

	if _, err := p.SaveDraft("page-1", 0, "/about", []byte(docV1)); err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}
	if _, err := p.Build(context.Background(), "page-1", 1); err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if _, err := p.Publish("page-1"); err != nil {
		t.Fatalf("发布失败: %v", err)
	}
	if _, err := p.UpdateURL(context.Background(), "page-1", "/about-us", false); err != nil {
		t.Fatalf("URL 修改失败: %v", err)
	}
	insp, err := pub.Inspect("/about")
	if err != nil || insp.Kind != publish.PublicationNone {
		t.Errorf("旧 URL 应已取消激活: %+v %v", insp, err)
	}
	insp, err = pub.Inspect("/about-us")
	if err != nil || insp.Kind != publish.PublicationPage {
		t.Errorf("新 URL 应为激活页面: %+v %v", insp, err)
	}
}

func TestPublisherUpdateURLGuards(t *testing.T) {
	p, _, _, _ := newPublisherEnv(t)
	if _, err := p.SaveDraft("page-1", 0, "/about", []byte(docV1)); err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}
	if _, err := p.Build(context.Background(), "page-1", 1); err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if _, err := p.Publish("page-1"); err != nil {
		t.Fatalf("发布失败: %v", err)
	}

	if _, err := p.UpdateURL(context.Background(), "page-1", "/about", true); err == nil {
		t.Error("相同路径应被拒绝")
	}

	if _, err := p.UpdateURL(context.Background(), "page-1", "no-slash", true); err == nil {
		t.Error("非法路径应被拒绝")
	}

	if _, err := p.UpdateURL(context.Background(), "missing", "/x", true); !errors.Is(err, pipeline.ErrPageNotFound) {
		t.Errorf("不存在页面应报错: %v", err)
	}
}

func TestPublisherUpdateURLBuildFailure(t *testing.T) {
	compile := func(ctx context.Context, in pipeline.BuildInput) ([]byte, error) {
		if strings.Contains(string(in.DocJSON), "boom") {
			return nil, errors.New("boom")
		}
		return pipeline.DefaultCompile(ctx, in)
	}
	p, _, pub, _ := newPublisherEnv(t, pipeline.WithCompile(compile))

	if _, err := p.SaveDraft("page-1", 0, "/about", []byte(docV1)); err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}
	if _, err := p.Build(context.Background(), "page-1", 1); err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if _, err := p.Publish("page-1"); err != nil {
		t.Fatalf("发布失败: %v", err)
	}

	if _, err := p.SaveDraft("page-1", 1, "/about", []byte(docV2+"boom")); err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}
	if _, err := p.UpdateURL(context.Background(), "page-1", "/about-us", true); err == nil {
		t.Fatal("构建失败时 URL 修改应报错")
	}
	insp, _ := pub.Inspect("/about")
	if insp.Kind != publish.PublicationPage {
		t.Errorf("旧 URL 应保持激活: %+v", insp)
	}
}

func TestPublisherSaveDraftSnapshot(t *testing.T) {
	p, _, _, _ := newPublisherEnv(t)
	doc := []byte(docV1)
	if _, err := p.SaveDraft("page-1", 0, "/about", doc); err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}

	copy(doc, `{"broken`)
	if _, err := p.Build(context.Background(), "page-1", 1); err != nil {
		t.Fatalf("快照应隔离调用方修改: %v", err)
	}
}

func TestDefaultCompileIncludesOnlyUsedClientAssets(t *testing.T) {
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"form","type":"core.form","props":{"fields":[{"type":"select","name":"city","label":"城市","options":["北京","上海"]}]}},{"id":"counter","type":"core.counter","props":{"end":12.5,"decimals":1}}]}`
	input := pipeline.BuildInput{DocJSON: []byte(doc)}
	out, err := pipeline.DefaultCompile(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	again, err := pipeline.DefaultCompile(context.Background(), input)
	if err != nil || string(again) != string(out) {
		t.Fatalf("重复编译必须字节一致：%v", err)
	}
	for _, required := range []string{"WBUI.select", ".wbs-trigger", "function initCounters"} {
		if !strings.Contains(string(out), required) {
			t.Errorf("默认编译缺少 %s", required)
		}
	}
	for _, unused := range []string{"WBUI.modal", "function initSliders"} {
		if strings.Contains(string(out), unused) {
			t.Errorf("默认编译不应携带 %s", unused)
		}
	}
}

func TestDefaultCompilePlainPageHasNoClientAssets(t *testing.T) {
	out, err := pipeline.DefaultCompile(context.Background(), pipeline.BuildInput{DocJSON: []byte(docV2)})
	if err != nil {
		t.Fatal(err)
	}
	body := string(out)
	if strings.Contains(body, "site-preview-mode") || strings.Contains(body, "sky-preview-notice") {
		t.Fatal("默认发布编译不应携带模拟预览标识")
	}

	for _, unwanted := range []string{"WBUI", ".wbs-trigger"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("纯内容产物不应包含 %s", unwanted)
		}
	}

	if got := strings.Count(body, "<script"); got != 1 {
		t.Fatalf("纯内容产物应只含采集脚本这一个 <script>，实际 %d 个", got)
	}
	if !strings.Contains(body, "gw_src") || !strings.Contains(body, "gw_sess") {
		t.Error("注入的脚本不是流量采集脚本")
	}

	for _, empty := range []string{"<script></script>", "<script>\n</script>", "<script>\n\n</script>"} {
		if strings.Contains(body, empty) {
			t.Errorf("不应输出空脚本 %q", empty)
		}
	}
}

func TestDefaultCompileRejectsIncompleteNode(t *testing.T) {
	doc := []byte(`{"root":[{"id":"unfinished","type":"core.text","props":{"mode":"plaintext"}}]}`)
	if _, err := pipeline.DefaultCompile(context.Background(), pipeline.BuildInput{DocJSON: doc}); err == nil {
		t.Fatal("默认发布编译必须拒绝未完成文本")
	}
}
