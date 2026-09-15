package unit

// page_block_content_i18n_test.go — 块内文本进内容翻译链路（多语言 P5b 缺口补齐，docs/06-D §15.11）。
//
// 装配层真实链路（真 PG：pages/blocks/project_locales 表；内容译文端口注入内存假存储）：
//  1. 页眉/页脚块（settings.structure 绑定）内的按钮/标题随语言切换（zh-CN vs en-US 产物不同）；
//  2. core.globalref 内联块的文本同样可翻译；
//  3. 缺译文时回退原文：产物与「不注入译文端口」的构建逐字节一致；
//  4. 每页每语言**一次**查库：页眉块 + 页脚块 + globalref 块 + 本页文本共用同一个取词器。
//
// 为什么注入端口而不是走默认存储：默认存储读全局数据库组件（pkg/i18n.defaultContentStore），
// 本包用独立 schema 不接全局；注入端口后同一份装配代码可在隔离环境里验证取词链路。

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	blockdto "go_wp/internal/module/block/dto"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	"go_wp/pkg/i18n"
)

// countingContentStore 内存版内容译文存储（记录 LoadTargets 调用次数）。
type countingContentStore struct {
	rows  map[string]string // "lang|hash|context" → 译文
	loads int
}

// LoadTargets 实现 i18n.ContentStore（一次调用返回全部命中）。
func (s *countingContentStore) LoadTargets(_ context.Context, lang string, hashes []string) (map[string]string, error) {
	s.loads++
	out := map[string]string{}
	for _, h := range hashes {
		for k, v := range s.rows {
			if strings.HasPrefix(k, lang+"|"+h+"|") {
				out[i18n.ContentIndexKey(h, strings.TrimPrefix(k, lang+"|"+h+"|"))] = v
			}
		}
	}
	return out, nil
}

// blockContentStore 构造假存储：每条 {lang, context, source, target}。
func blockContentStore(rows ...[4]string) *countingContentStore {
	s := &countingContentStore{rows: map[string]string{}}
	for _, r := range rows {
		s.rows[r[0]+"|"+i18n.ContentHash(r[2])+"|"+r[1]] = r[3]
	}
	return s
}

// setContentStore 经导出 setter 注入内容译文端口（生产走默认 sys_translation 存储）。
func setContentStore(t *testing.T, svc pagecontract.PageService, store i18n.ContentStore) {
	t.Helper()
	setter, ok := svc.(interface {
		SetContentTranslationStore(i18n.ContentStore)
	})
	if !ok {
		t.Fatalf("page service 未提供内容译文端口注入点")
	}
	setter.SetContentTranslationStore(store)
}

// newBlockI18nEnv 建测试环境：生产 blocks 表（迁移建）+ 页眉块 + 页脚块 + globalref 块 + 含三种文本的页面。
func newBlockI18nEnv(t *testing.T) (pagecontract.PageService, string, string) {
	t.Helper()
	db, svc, projects, projectID := newPageService(t)
	ctx := context.Background()
	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)

	header, err := blocks.Create(ctx, &blockdto.CreateReq{
		ProjectID: projectID, Name: "站点页眉", Kind: "header",
		Document: json.RawMessage(`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"hb1","type":"core.button","props":{"text":"页眉按钮","action":"internal","value":"/a"}}]}`),
	})
	if err != nil {
		t.Fatalf("创建页眉块失败: %v", err)
	}
	footer, err := blocks.Create(ctx, &blockdto.CreateReq{
		ProjectID: projectID, Name: "站点页脚", Kind: "footer",
		Document: json.RawMessage(`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"fb1","type":"core.heading","props":{"text":"页脚标题"}}]}`),
	})
	if err != nil {
		t.Fatalf("创建页脚块失败: %v", err)
	}
	promo, err := blocks.Create(ctx, &blockdto.CreateReq{
		ProjectID: projectID, Name: "促销块", Kind: "block",
		Document: json.RawMessage(`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"pb1","type":"core.text","props":{"text":"促销正文"}}]}`),
	})
	if err != nil {
		t.Fatalf("创建促销块失败: %v", err)
	}

	doc := fmt.Sprintf(`{"settings":{"layout":{"mode":"full"},"structure":{"headerBlockId":"%s","footerBlockId":"%s"}},"root":[{"id":"ph1","type":"core.heading","props":{"text":"本页标题"}},{"id":"ref1","type":"core.globalref","props":{"blockId":"%s"}}]}`,
		header.ID, footer.ID, promo.ID)
	page := createPage(t, svc, projectID, "/block-i18n", doc)
	return svc, projectID, page.ID
}

// buildIndexHTML 构建指定语言并读取产物 index.html。
func buildIndexHTML(t *testing.T, svc pagecontract.PageService, pageID, lang string) string {
	t.Helper()
	req := &pagedto.BuildReq{ID: pageID}
	if lang != "" {
		req.Lang = lang
	}
	built, err := svc.Build(context.Background(), req)
	if err != nil {
		t.Fatalf("构建失败（lang=%q）: %v", lang, err)
	}
	body, err := os.ReadFile(filepath.Join(os.Getenv("GO_WP_ARTIFACT_ROOT"), "artifacts", built.StagedHash, "index.html"))
	if err != nil {
		t.Fatalf("读取产物失败: %v", err)
	}
	return string(body)
}

// TestPageBlockContentTranslationLangDiffers 页眉/页脚块 + globalref 内联块文本随语言切换。
func TestPageBlockContentTranslationLangDiffers(t *testing.T) {
	svc, _, pageID := newBlockI18nEnv(t)
	store := blockContentStore(
		[4]string{"en-US", "core.button.text", "页眉按钮", "Header button"},
		[4]string{"en-US", "core.heading.text", "页脚标题", "Footer heading"},
		[4]string{"en-US", "core.text.text", "促销正文", "Promo body"},
		[4]string{"en-US", "core.heading.text", "本页标题", "Page heading"},
	)
	setContentStore(t, svc, store)

	zh := buildIndexHTML(t, svc, pageID, "")
	store.loads = 0
	en := buildIndexHTML(t, svc, pageID, "en-US")

	if zh == en {
		t.Fatal("zh-CN 与 en-US 产物不应相同")
	}
	for _, want := range []string{"Header button", "Footer heading", "Promo body", "Page heading"} {
		if !strings.Contains(en, want) {
			t.Fatalf("en-US 产物缺少 %q；产物片段=%s", want, snippet(en))
		}
	}
	for _, bad := range []string{"页眉按钮", "页脚标题", "促销正文", "本页标题"} {
		if strings.Contains(en, bad) {
			t.Fatalf("en-US 产物不应出现原文 %q；产物片段=%s", bad, snippet(en))
		}
	}
	for _, want := range []string{"页眉按钮", "页脚标题", "促销正文", "本页标题"} {
		if !strings.Contains(zh, want) {
			t.Fatalf("zh-CN 产物应保持原文 %q；产物片段=%s", want, snippet(zh))
		}
	}
	// 每页每语言一次查库：页眉块 + 页脚块 + globalref 块 + 本页文本共用一个取词器。
	if store.loads != 1 {
		t.Fatalf("块内文本必须复用同一个取词器（一次批量查库），实际 %d 次", store.loads)
	}
	// 真实输出证据（-v 可见）：同一块在两种语言下的产物片段。
	t.Logf("zh-CN 页眉片段: %s", around(zh, "页眉按钮"))
	t.Logf("en-US 页眉片段: %s", around(en, "Header button"))
	t.Logf("zh-CN 页脚片段: %s", around(zh, "页脚标题"))
	t.Logf("en-US 页脚片段: %s", around(en, "Footer heading"))
	t.Logf("zh-CN 内联块片段: %s", around(zh, "促销正文"))
	t.Logf("en-US 内联块片段: %s", around(en, "Promo body"))
}

// around 截取 needle 前后各 60 字节的产物片段（找不到时返回「未找到」）。
func around(html, needle string) string {
	i := strings.Index(html, needle)
	if i < 0 {
		return "未找到 " + needle
	}
	start := i - 60
	if start < 0 {
		start = 0
	}
	end := i + len(needle) + 20
	if end > len(html) {
		end = len(html)
	}
	return strings.ReplaceAll(html[start:end], "\n", " ")
}

// TestPageBlockOnlyContentTranslation 页面自身没有候选、只有页眉块文本时也能翻译
// （候选集合必须覆盖块，否则取词器为空索引 → 永远回退原文）。
func TestPageBlockOnlyContentTranslation(t *testing.T) {
	db, svc, projects, projectID := newPageService(t)
	ctx := context.Background()
	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	header, err := blocks.Create(ctx, &blockdto.CreateReq{
		ProjectID: projectID, Name: "站点页眉", Kind: "header",
		Document: json.RawMessage(`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"hb1","type":"core.button","props":{"text":"页眉按钮","action":"internal","value":"/a"}}]}`),
	})
	if err != nil {
		t.Fatalf("创建页眉块失败: %v", err)
	}
	// 页面 root 为空：本页 AST 没有任何候选。
	doc := fmt.Sprintf(`{"settings":{"layout":{"mode":"full"},"structure":{"headerBlockId":"%s"}},"root":[]}`, header.ID)
	page := createPage(t, svc, projectID, "/header-only", doc)

	store := blockContentStore([4]string{"en-US", "core.button.text", "页眉按钮", "Header button"})
	setContentStore(t, svc, store)
	en := buildIndexHTML(t, svc, page.ID, "en-US")

	if !strings.Contains(en, "Header button") {
		t.Fatalf("只有页眉块文本的页面也应翻译（候选必须覆盖块）；产物片段=%s", snippet(en))
	}
	if strings.Contains(en, "页眉按钮") {
		t.Fatalf("en-US 产物不应出现页眉块原文；产物片段=%s", snippet(en))
	}
	if store.loads != 1 {
		t.Fatalf("应一次批量查库，实际 %d 次", store.loads)
	}
}

// TestPageBlockContentFallbackOriginal 缺译文时回退原文：产物与「不注入译文端口」逐字节一致。
func TestPageBlockContentFallbackOriginal(t *testing.T) {
	svc, _, pageID := newBlockI18nEnv(t)
	baseline := buildIndexHTML(t, svc, pageID, "en-US") // 未注入端口 = 接入前形态（默认存储不可用）

	empty := blockContentStore() // 空存储：命中为零
	setContentStore(t, svc, empty)
	withStore := buildIndexHTML(t, svc, pageID, "en-US")

	if baseline != withStore {
		t.Fatalf("无译文时块内文本必须回退原文（产物应与接入前一致）；baseline=%s；withStore=%s", snippet(baseline), snippet(withStore))
	}
	for _, want := range []string{"页眉按钮", "页脚标题", "促销正文", "本页标题"} {
		if !strings.Contains(withStore, want) {
			t.Fatalf("回退产物应保留原文 %q；产物片段=%s", want, snippet(withStore))
		}
	}
	if empty.loads != 1 {
		t.Fatalf("空存储也应只查一次，实际 %d 次", empty.loads)
	}
}

// TestPageBlockOnlyContentTranslationDependency 只有块内文本的页面同样登记 i18n:content 依赖。
//
// §9 关键约束：缺译文回退原文后，补齐译文要能触发重建；判据必须覆盖「本页 AST 无候选
// 但引用了块」的页面，否则这类页面会长期停留在回退内容。
func TestPageBlockOnlyContentTranslationDependency(t *testing.T) {
	db, svc, projects, projectID := newPageService(t)
	ctx := context.Background()
	withLangPrefix(t)
	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	header, err := blocks.Create(ctx, &blockdto.CreateReq{
		ProjectID: projectID, Name: "站点页眉", Kind: "header",
		Document: json.RawMessage(`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"hb1","type":"core.button","props":{"text":"页眉按钮","action":"internal","value":"/a"}}]}`),
	})
	if err != nil {
		t.Fatalf("创建页眉块失败: %v", err)
	}
	doc := fmt.Sprintf(`{"settings":{"layout":{"mode":"full"},"structure":{"headerBlockId":"%s"}},"root":[]}`, header.ID)
	page := createPage(t, svc, projectID, "/header-dep", doc)

	built, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID, Lang: "en-US"})
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	deps := artifactDependencies(t, built.StagedHash)
	if !hasDependencyKey(deps, "i18n:content") {
		t.Fatalf("只有块内文本的页面也必须登记 i18n:content 依赖: %v", deps)
	}

	// 默认语言不登记（产物即原文）。
	builtZh, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID})
	if err != nil {
		t.Fatalf("默认语言构建失败: %v", err)
	}
	if hasDependencyKey(artifactDependencies(t, builtZh.StagedHash), "i18n:content") {
		t.Fatal("默认语言构建不应登记 i18n:content 依赖")
	}
}

// snippet 截取产物片段用于失败信息。
func snippet(html string) string {
	if len(html) > 600 {
		return html[:600]
	}
	return html
}
