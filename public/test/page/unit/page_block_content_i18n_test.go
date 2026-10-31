package unit

import (
	"context"
	"fmt"
	json "go_wp/pkg/jsonx"
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

type countingContentStore struct {
	rows  map[string]string
	loads int
}

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

func blockContentStore(rows ...[4]string) *countingContentStore {
	s := &countingContentStore{rows: map[string]string{}}
	for _, r := range rows {
		s.rows[r[0]+"|"+i18n.ContentHash(r[2])+"|"+r[1]] = r[3]
	}
	return s
}

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

	if store.loads != 1 {
		t.Fatalf("块内文本必须复用同一个取词器（一次批量查库），实际 %d 次", store.loads)
	}

	t.Logf("zh-CN 页眉片段: %s", around(zh, "页眉按钮"))
	t.Logf("en-US 页眉片段: %s", around(en, "Header button"))
	t.Logf("zh-CN 页脚片段: %s", around(zh, "页脚标题"))
	t.Logf("en-US 页脚片段: %s", around(en, "Footer heading"))
	t.Logf("zh-CN 内联块片段: %s", around(zh, "促销正文"))
	t.Logf("en-US 内联块片段: %s", around(en, "Promo body"))
}

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

func TestPageBlockContentFallbackOriginal(t *testing.T) {
	svc, _, pageID := newBlockI18nEnv(t)
	baseline := buildIndexHTML(t, svc, pageID, "en-US")

	empty := blockContentStore()
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

	builtZh, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID})
	if err != nil {
		t.Fatalf("默认语言构建失败: %v", err)
	}
	if hasDependencyKey(artifactDependencies(t, builtZh.StagedHash), "i18n:content") {
		t.Fatal("默认语言构建不应登记 i18n:content 依赖")
	}
}

func snippet(html string) string {
	if len(html) > 600 {
		return html[:600]
	}
	return html
}
