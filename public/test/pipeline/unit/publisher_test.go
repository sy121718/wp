package unit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go_wp/internal/pipeline"
)

const (
	// docV1 最小页面文档 v1。
	docV1 = `{"settings":{"layout":{"mode":"full"}},"root":[]}`
	// docV2 标题文档（与 v1 内容不同，产物哈希不同）。
	docV2 = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"h1","type":"core.heading","props":{"text":"你好"}}]}`
)

// newPublisherEnv 构造完整测试环境：临时产物根 + 本地存储/发布 + 发布服务。
func newPublisherEnv(t *testing.T, opts ...pipeline.Option) (*pipeline.Publisher, *pipeline.LocalStore, *pipeline.LocalPublicationStore, string) {
	t.Helper()
	root := t.TempDir()
	store := &pipeline.LocalStore{Root: root}
	pub := &pipeline.LocalPublicationStore{ActiveRoot: filepath.Join(root, "public/active")}
	p := pipeline.NewPublisher(store, pub, opts...)
	return p, store, pub, root
}

// activeHTML 读取激活路径下的入口文件。
func activeHTML(t *testing.T, pub *pipeline.LocalPublicationStore, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(pub.ActiveRoot, strings.TrimPrefix(path, "/"), "index.html"))
	if err != nil {
		t.Fatalf("读取激活产物失败: %v", err)
	}
	return string(data)
}

// TestPublisherLifecycle 0-A1 全链路：保存→构建→发布→激活文件就绪。
func TestPublisherLifecycle(t *testing.T) {
	p, _, pub, _ := newPublisherEnv(t)

	v, err := p.SaveDraft("page-1", 0, "/about", []byte(docV1))
	if err != nil || v != 1 {
		t.Fatalf("保存草稿失败: v=%d err=%v", v, err)
	}
	hash, err := p.Build("page-1", 1)
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

// TestPublisherDeterminism 确定性构建：相同文档两次构建产生相同哈希；
// 二次发布后旧版本转 Superseded。
func TestPublisherDeterminism(t *testing.T) {
	p, _, _, _ := newPublisherEnv(t)

	if _, err := p.SaveDraft("page-1", 0, "/about", []byte(docV1)); err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}
	h1, err := p.Build("page-1", 1)
	if err != nil {
		t.Fatalf("首次构建失败: %v", err)
	}
	if _, err = p.Publish("page-1"); err != nil {
		t.Fatalf("首次发布失败: %v", err)
	}

	// 同一文档再次保存并构建：产物哈希必须完全相同。
	if _, err = p.SaveDraft("page-1", 1, "/about", []byte(docV1)); err != nil {
		t.Fatalf("再次保存草稿失败: %v", err)
	}
	h2, err := p.Build("page-1", 2)
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

// TestPublisherVersionConflict 乐观锁：旧版本写入与构建均被拒绝。
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
	if _, err := p.Build("page-1", 1); !errors.Is(err, pipeline.ErrVersionConflict) {
		t.Errorf("基于旧版本的构建应被拒绝: %v", err)
	}
	// 无产物暂存，不允许发布。
	if _, err := p.Publish("page-1"); !errors.Is(err, pipeline.ErrNoStagedArtifact) {
		t.Errorf("无暂存产物不允许发布: %v", err)
	}
}

// TestPublisherBuildFailure 构建失败：状态 failed、线上版本保持不变、不允许发布。
func TestPublisherBuildFailure(t *testing.T) {
	boom := errors.New("编译器故障")
	p, _, pub, _ := newPublisherEnv(t,
		pipeline.WithCompile(func([]byte) ([]byte, error) { return nil, boom }))

	if _, err := p.SaveDraft("page-1", 0, "/about", []byte(docV1)); err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}
	if _, err := p.Build("page-1", 1); !errors.Is(err, boom) {
		t.Fatalf("构建应失败: %v", err)
	}
	st, _ := p.Status("page-1")
	if st.Status != pipeline.StateFailed || st.FailedReason == "" {
		t.Errorf("构建失败后应为 failed: %+v", st)
	}
	if _, err := p.Publish("page-1"); !errors.Is(err, pipeline.ErrNoStagedArtifact) {
		t.Errorf("构建失败后不允许发布: %v", err)
	}
	if insp, err := pub.Inspect("/about"); err != nil || insp.Kind != pipeline.PublicationNone {
		t.Errorf("构建失败不应激活任何 URL: %+v %v", insp, err)
	}
}

// TestPublisherRollback 秒级回滚：历史产物重新激活，无需重新编译。
func TestPublisherRollback(t *testing.T) {
	p, _, pub, _ := newPublisherEnv(t)

	if _, err := p.SaveDraft("page-1", 0, "/about", []byte(docV1)); err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}
	h1, _ := p.Build("page-1", 1)
	if _, err := p.Publish("page-1"); err != nil {
		t.Fatalf("首次发布失败: %v", err)
	}

	// 修改后再发布：V1 转 Superseded。
	if _, err := p.SaveDraft("page-1", 1, "/about", []byte(docV2)); err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}
	if _, err := p.Build("page-1", 2); err != nil {
		t.Fatalf("二次构建失败: %v", err)
	}
	if _, err := p.Publish("page-1"); err != nil {
		t.Fatalf("二次发布失败: %v", err)
	}
	if html := activeHTML(t, pub, "/about"); !strings.Contains(html, "你好") {
		t.Errorf("二次发布后激活的应为 V2:\n%s", html)
	}

	// 回滚到 V1：秒级指针切换。
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

// TestPublisherRollbackPathMismatch URL 已变化的版本禁止直接回滚（§6.4）。
func TestPublisherRollbackPathMismatch(t *testing.T) {
	p, _, _, _ := newPublisherEnv(t)

	if _, err := p.SaveDraft("page-1", 0, "/about", []byte(docV1)); err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}
	h1, _ := p.Build("page-1", 1)
	if _, err := p.Publish("page-1"); err != nil {
		t.Fatalf("首次发布失败: %v", err)
	}
	if _, err := p.UpdateURL("page-1", "/about-us", false); err != nil {
		t.Fatalf("URL 修改失败: %v", err)
	}
	if err := p.Rollback("page-1", h1); !errors.Is(err, pipeline.ErrRollbackPathMismatch) {
		t.Errorf("路径不一致的回滚应被拒绝: %v", err)
	}
}

// TestPublisherUpdateURLWithRedirect 修改 Slug：新 URL 激活 + 旧 URL 301 重定向（0-A2 §1.1）。
func TestPublisherUpdateURLWithRedirect(t *testing.T) {
	p, _, pub, _ := newPublisherEnv(t)

	if _, err := p.SaveDraft("page-1", 0, "/about", []byte(docV1)); err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}
	if _, err := p.Build("page-1", 1); err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if _, err := p.Publish("page-1"); err != nil {
		t.Fatalf("首次发布失败: %v", err)
	}

	oldPath, err := p.UpdateURL("page-1", "/about-us", true)
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

	// 新 URL 已激活；旧 URL 为重定向（301 指向新 URL）。
	insp, err := pub.Inspect("/about-us")
	if err != nil || insp.Kind != pipeline.PublicationPage {
		t.Errorf("新 URL 应为激活页面: %+v %v", insp, err)
	}
	insp, err = pub.Inspect("/about")
	if err != nil || insp.Kind != pipeline.PublicationRedirect ||
		insp.Redirect.TargetPath != "/about-us" || insp.Redirect.StatusCode != 301 {
		t.Errorf("旧 URL 应为 301 重定向: %+v %v", insp, err)
	}
}

// TestPublisherUpdateURLNoRedirect 不勾选 301 时旧 URL 直接取消激活。
func TestPublisherUpdateURLNoRedirect(t *testing.T) {
	p, _, pub, _ := newPublisherEnv(t)

	if _, err := p.SaveDraft("page-1", 0, "/about", []byte(docV1)); err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}
	if _, err := p.Build("page-1", 1); err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if _, err := p.Publish("page-1"); err != nil {
		t.Fatalf("发布失败: %v", err)
	}
	if _, err := p.UpdateURL("page-1", "/about-us", false); err != nil {
		t.Fatalf("URL 修改失败: %v", err)
	}
	insp, err := pub.Inspect("/about")
	if err != nil || insp.Kind != pipeline.PublicationNone {
		t.Errorf("旧 URL 应已取消激活: %+v %v", insp, err)
	}
	insp, err = pub.Inspect("/about-us")
	if err != nil || insp.Kind != pipeline.PublicationPage {
		t.Errorf("新 URL 应为激活页面: %+v %v", insp, err)
	}
}

// TestPublisherUpdateURLGuards 非法输入与构建失败保护。
func TestPublisherUpdateURLGuards(t *testing.T) {
	p, _, _, _ := newPublisherEnv(t)
	if _, err := p.SaveDraft("page-1", 0, "/about", []byte(docV1)); err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}
	if _, err := p.Build("page-1", 1); err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if _, err := p.Publish("page-1"); err != nil {
		t.Fatalf("发布失败: %v", err)
	}
	// 相同路径拒绝。
	if _, err := p.UpdateURL("page-1", "/about", true); err == nil {
		t.Error("相同路径应被拒绝")
	}
	// 非法路径拒绝。
	if _, err := p.UpdateURL("page-1", "no-slash", true); err == nil {
		t.Error("非法路径应被拒绝")
	}
	// 不存在的页面。
	if _, err := p.UpdateURL("missing", "/x", true); !errors.Is(err, pipeline.ErrPageNotFound) {
		t.Errorf("不存在页面应报错: %v", err)
	}
}

// TestPublisherUpdateURLBuildFailure 新 URL 构建失败：旧 URL 线上保持不变。
func TestPublisherUpdateURLBuildFailure(t *testing.T) {
	compile := func(doc []byte) ([]byte, error) {
		if strings.Contains(string(doc), "boom") {
			return nil, errors.New("boom")
		}
		return pipeline.DefaultCompile(doc)
	}
	p, _, pub, _ := newPublisherEnv(t, pipeline.WithCompile(compile))

	// 先正常发布旧 URL。
	if _, err := p.SaveDraft("page-1", 0, "/about", []byte(docV1)); err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}
	if _, err := p.Build("page-1", 1); err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if _, err := p.Publish("page-1"); err != nil {
		t.Fatalf("发布失败: %v", err)
	}

	// 保存含 boom 的新草稿（版本 2），再改 URL：构建失败，旧 URL 不受影响。
	if _, err := p.SaveDraft("page-1", 1, "/about", []byte(docV2+"boom")); err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}
	if _, err := p.UpdateURL("page-1", "/about-us", true); err == nil {
		t.Fatal("构建失败时 URL 修改应报错")
	}
	insp, _ := pub.Inspect("/about")
	if insp.Kind != pipeline.PublicationPage {
		t.Errorf("旧 URL 应保持激活: %+v", insp)
	}
}

// TestPublisherSaveDraftSnapshot 草稿快照冻结：保存后修改输入字节不影响下次构建。
func TestPublisherSaveDraftSnapshot(t *testing.T) {
	p, _, _, _ := newPublisherEnv(t)
	doc := []byte(docV1)
	if _, err := p.SaveDraft("page-1", 0, "/about", doc); err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}
	// 修改调用方缓冲区，快照不受影响。
	copy(doc, `{"broken`)
	if _, err := p.Build("page-1", 1); err != nil {
		t.Fatalf("快照应隔离调用方修改: %v", err)
	}
}
