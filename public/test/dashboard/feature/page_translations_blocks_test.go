package feature

// page_translations_blocks_test.go — 翻译工作台的块内文本（多语言 P5b 缺口补齐，docs/06-D §15.11/§15.12）。
//
// 判断：块内文本**应当**出现在页面翻译工作台里——它是本页产物的一部分，且工作台是
// 唯一的译文录入入口；不列出则页眉/页脚/内联块文本永远无法翻译、完成度还会误报 100%。
// 写入路径天然全局（sys_translation 主键 (hash, context, lang) + MarkStaleForI18n 全站
// 标记），故按「来源徽章 + 复用提示」告知编辑者这是共享文本。
//
// 覆盖：
//  1. 工作台列出页眉块 + core.globalref 内联块的文本，并标注来源（页眉块 / 全局块）；
//  2. 块内文本计入本页完成度分母；
//  3. 保存块内译文 → 落库（engine=manual）+ 全站标记待重建；
//  4. 全站索引把块内文本归属到引用该块的页面（复用提示可见）。

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	blockdto "go_wp/internal/module/block/dto"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	dashboardhttp "go_wp/internal/module/dashboard/inbound/http"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	"go_wp/pkg/i18n"
	"go_wp/public/migrations"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	blockHeaderDocForTr = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"hb1","type":"core.button","props":{"text":"页眉按钮","action":"internal","value":"/a"}}]}`
	blockPromoDocForTr  = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"pb1","type":"core.text","props":{"text":"促销正文"}}]}`
)

// newBlockTranslationEnv 装配「含块」的工作台环境：真 PG（全量迁移，含 blocks 表）+ 真 block 服务。
// 返回：路由、db、工程 ID、页面 ID、页眉块 ID。
func newBlockTranslationEnv(t *testing.T) (*gin.Engine, *gorm.DB, string, string, string) {
	t.Helper()
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	gin.SetMode(gin.TestMode)
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用，跳过测试：%v", err)
		return nil, nil, "", "", ""
	}
	if err := migrations.Run(db); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(t.Context(), &projectdto.CreateReq{Name: "块内文本工作台站点"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	if _, err := projects.SaveLocales(t.Context(), &projectdto.LocalesSaveReq{
		ProjectID: project.ID,
		Locales:   []projectdto.LocaleItem{{Lang: "zh-CN", IsDefault: true}, {Lang: "en-US"}},
	}); err != nil {
		t.Fatalf("初始化语言清单失败: %v", err)
	}
	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	header, err := blocks.Create(t.Context(), &blockdto.CreateReq{
		ProjectID: project.ID, Name: "站点页眉", Kind: "header", Document: []byte(blockHeaderDocForTr),
	})
	if err != nil {
		t.Fatalf("创建页眉块失败: %v", err)
	}
	promo, err := blocks.Create(t.Context(), &blockdto.CreateReq{
		ProjectID: project.ID, Name: "促销块", Kind: "block", Document: []byte(blockPromoDocForTr),
	})
	if err != nil {
		t.Fatalf("创建促销块失败: %v", err)
	}
	pageID := "dddddddd-0000-0000-0000-00000000000d"
	doc := fmt.Sprintf(`{"settings":{"layout":{"mode":"full"},"structure":{"headerBlockId":"%s"}},"root":[{"id":"hd1","type":"core.heading","props":{"text":"本页标题"}},{"id":"ref1","type":"core.globalref","props":{"blockId":"%s"}}]}`,
		header.ID, promo.ID)
	insertTranslationPage(t, db, pageID, project.ID, "/block-tr", doc)

	pages := pageservice.NewService(pagemodel.NewPageModel(db), nil, nil, projects, nil, nil, nil, nil, nil)
	handle := dashboardhttp.NewHandle(pages, projects, blocks, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	handle.SetContentTranslationStore(i18n.NewContentWriter(db))

	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	router.GET("/admin/page/translations", handle.PageTranslations)
	router.POST("/admin/page/translations/save", handle.SavePageTranslations)
	return router, db, project.ID, pageID, header.ID
}

// TestPageTranslationsListsBlockText 工作台列出块内文本并标注来源（页眉块 / 全局块）。
func TestPageTranslationsListsBlockText(t *testing.T) {
	router, db, projectID, pageID, headerID := newBlockTranslationEnv(t)
	// 第二个页面引用同一个页眉块：块内文本的「出现在哪些页面」应含两页。
	insertTranslationPage(t, db, "eeeeeeee-0000-0000-0000-00000000000e", projectID, "/other-block",
		fmt.Sprintf(`{"settings":{"layout":{"mode":"full"},"structure":{"headerBlockId":"%s"}},"root":[]}`, headerID))

	body := getTranslationPage(t, router, pageID, "en-US")

	for _, want := range []string{
		// 本页文本 + 块内文本（页眉块按钮 / 内联块正文）
		`name="rowSource" value="本页标题"`,
		`name="rowSource" value="页眉按钮"`,
		`name="rowSource" value="促销正文"`,
		// 来源徽章
		`>页眉块<`,
		`>全局块<`,
		// 完成度分母含块内文本：本页标题 + 页眉按钮 + 促销正文 = 3
		`本页完成度 0 / 3`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("工作台缺少 %q\n%s", want, body)
		}
	}
	// 块内文本的复用提示：两个页面都引用该页眉块 → 「还用在另外 1 个页面」。
	if !strings.Contains(body, "还用在另外 1 个页面") {
		t.Fatalf("块内文本应带跨页面复用提示\n%s", body)
	}
}

// TestSaveBlockTextTranslation 保存块内译文 → 落库 + 全站标记待重建。
func TestSaveBlockTextTranslation(t *testing.T) {
	router, db, _, pageID, _ := newBlockTranslationEnv(t)
	saved := postForm(t, router, "/admin/page/translations/save", url.Values{
		"pageId":     {pageID},
		"lang":       {"en-US"},
		"rowContext": {"core.button.text"},
		"rowSource":  {"页眉按钮"},
		"rowHash":    {i18n.ContentHash("页眉按钮")},
		"rowTarget":  {"Header button"},
	})
	if saved.Code != http.StatusSeeOther {
		t.Fatalf("保存块内译文应 303 回跳，实际 %d：%s", saved.Code, saved.Body.String())
	}
	if loc := saved.Header().Get("Location"); !strings.Contains(loc, "n=1") {
		t.Fatalf("应写入 1 条译文，实际回跳 %q", loc)
	}
	target, engine := translationTargetOf(t, db, "页眉按钮", "core.button.text", "en-US")
	if target != "Header button" || engine != i18n.ContentEngineManual {
		t.Fatalf("块内译文未按预期落库: %q / %q", target, engine)
	}
	if !pageStale(t, db, pageID) {
		t.Fatal("块内译文变更应触发全站标记待重建")
	}
	body := getTranslationPage(t, router, pageID, "en-US")
	for _, want := range []string{"Header button", "已翻译", "本页完成度 1 / 3"} {
		if !strings.Contains(body, want) {
			t.Fatalf("保存后工作台缺少 %q\n%s", want, body)
		}
	}
}
