package blockhttp

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go_wp/internal/templates"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

func blocksLayoutData(base gin.H) gin.H {
	lang := "zh-CN"
	base["csrf_token"] = "test-token"
	base["lang"] = lang
	base["t"] = templates.TranslateFunc(lang)
	base["langs"] = templates.LanguageOptions(lang)
	base["lang_redirect"] = "/admin/blocks"
	base["Buttons"] = map[string]any{"block.create": true, "block.delete": true}
	return base
}

func blocksRow(id, name, kind, reuseMode string) gin.H {
	return gin.H{
		"ID": id, "Name": name, "Kind": kind, "ReuseMode": reuseMode,
		"UpdatedAt": utils.NewJSONTime(time.Date(2026, 1, 2, 15, 4, 0, 0, time.Local)), "RefCount": int64(2),
	}
}

func renderBlocksPage(t *testing.T, data gin.H) string {
	t.Helper()
	v := viper.New()
	v.Set("log.base_dir", t.TempDir())
	if err := logger.Init(v); err != nil {
		t.Fatalf("logger init: %v", err)
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/page", func(c *gin.Context) { c.HTML(http.StatusOK, "admin/block/blocks.html", blocksLayoutData(data)) })
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/page", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("admin/blocks 渲染失败，状态 %d", rec.Code)
	}
	body := rec.Body.String()

	if !strings.Contains(body, "</html>") {
		t.Fatalf("admin/blocks 未渲染到布局尾部（模板在某一行中断，列表可能整块消失）")
	}
	return body
}

func blocksBaseData(rows bool) gin.H {
	data := gin.H{
		"title": "区块管理", "menu": "blocks",
		"Projects": []gin.H{{"ID": "prj", "Name": "站点"}}, "SelectedProject": "prj",
		"Headers": []gin.H{}, "Footers": []gin.H{}, "Blocks": []gin.H{},
	}
	if rows {
		data["Headers"] = []gin.H{blocksRow("b1", "站点页眉", "header", "global")}
		data["Footers"] = []gin.H{blocksRow("b2", "站点页脚", "footer", "global")}
		data["Blocks"] = []gin.H{
			blocksRow("b3", "信任徽章", "trust", "global"),
			blocksRow("b4", "商品卡", "block", "template"),
		}
	}
	return data
}

func TestBlocksListIsSingleTable(t *testing.T) {
	cases := []struct {
		name string
		rows bool
	}{
		{"三类都有行", true},
		{"三类全空", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			body := renderBlocksPage(t, blocksBaseData(tc.rows))

			if n := strings.Count(body, `class="data-table"`); n != 1 {
				t.Errorf("data-table 数量 = %d，期望 1（三段合并成一张表）", n)
			}
			if n := strings.Count(body, "table-scroll"); n != 1 {
				t.Errorf("table-scroll 数量 = %d，期望 1", n)
			}

			wantCheckAll := 0
			if tc.rows {
				wantCheckAll = 1
			}
			if n := strings.Count(body, "data-check-all"); n != wantCheckAll {
				t.Errorf("data-check-all 数量 = %d，期望 %d（唯一性必须由结构保证，不再依赖跨段 if）", n, wantCheckAll)
			}

			if !strings.Contains(body, "<thead") || !strings.Contains(body, "类型") {
				t.Error("空态/有数据两种形态下表头都必须在，且含「类型」列")
			}

			if !tc.rows && !strings.Contains(body, `colspan="7"`) {
				t.Error("空态行的 colspan 应为 7（勾选/名称/类型/复用方式/更新时间/影响面/操作）")
			}
			if !tc.rows && !strings.Contains(body, "还没有全局块") {
				t.Error("三类全空时缺合并后的唯一空态")
			}
		})
	}
}

func TestBlocksListKeepsSelectionAndDeleteWiring(t *testing.T) {
	body := renderBlocksPage(t, blocksBaseData(true))

	for _, want := range []string{

		`hx-post="/api/block/bulk-delete"`, `data-write-status`,
		`name="ids"`, "data-check-item", "data-bulk-bar", "data-bulk-count",
		"data-filter-input", "data-filter-text", "data-filter-empty",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("合并表后缺少 %q：批量选择 / 筛选链路断了", want)
		}
	}

	for _, id := range []string{"b1", "b2", "b3", "b4"} {
		if !strings.Contains(body, `id="block-del-`+id+`"`) {
			t.Errorf("缺少 id=block-del-%s 的行内删除表单（表格外的 form 关联不到按钮）", id)
		}
		if !strings.Contains(body, `form="block-del-`+id+`"`) {
			t.Errorf("缺少 form=block-del-%s 的删除按钮关联", id)
		}
	}
	if n := strings.Count(body, `id="block-del-`); n != 4 {
		t.Errorf("行内删除表单数量 = %d，期望 4（每个块一个，id 不得重复）", n)
	}
}

func TestBlocksStaleImpactTierStalePage(t *testing.T) {
	data := blocksBaseData(true)
	data["StaleImpact"] = gin.H{
		"Available": true,
		"Pages":     []gin.H{{"ID": "pg1", "Path": "/blog/hello-world", "ProjectName": "站点"}},
		"Total":     1, "Truncated": false, "Limit": 8, "Hint": "",
	}
	body := renderBlocksPage(t, data)

	for _, want := range []string{
		"1", "个页面有更新未发布",
		`class="badge badge-warning badge-btn"`,
		`class="page-sub"`,
		`data-drawer-open hx-target="#admin-drawer-body" hx-swap="innerHTML" hx-get="/admin/blocks/stale/drawer"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("有 stale 时缺少 %q", want)
		}
	}

	for _, notWant := range []string{
		"/blog/hello-world",
		`<details class="section-fold card">`,
		`<section class="card">`,
	} {
		if strings.Contains(body, notWant) {
			t.Errorf("列表页不该再渲染 %q —— 清单已移入抽屉", notWant)
		}
	}
}

func TestBlocksStaleImpactTierNone(t *testing.T) {
	data := blocksBaseData(true)
	data["StaleImpact"] = gin.H{
		"Available": true, "Pages": []gin.H{}, "Total": 0, "Truncated": false, "Limit": 30, "Hint": "",
	}
	body := renderBlocksPage(t, data)

	if !strings.Contains(body, "当前没有待重建的页面") {
		t.Error("Total=0 时缺「当前没有待重建的页面」这一档")
	}
	if strings.Contains(body, "个页面有更新未发布") {
		t.Error("没有 stale 时页头不应显示待重建徽章")
	}
	if strings.Contains(body, `class="badge badge-warning badge-btn"`) {
		t.Error("没有 stale 时不该给抽屉入口")
	}
}

func TestBlocksStaleImpactTierUnavailableKeepsHint(t *testing.T) {
	const hint = "读取站点工程失败，本次无法统计待重建影响面。"
	data := blocksBaseData(true)
	data["StaleImpact"] = gin.H{
		"Available": false, "Pages": []gin.H{}, "Total": 0, "Truncated": false,
		"Limit": 8, "Hint": hint,
	}
	body := renderBlocksPage(t, data)

	if !strings.Contains(body, hint) {
		t.Error("Available=false 时必须原样输出 Hint（读失败不能与「无待重建」合并成一档）")
	}
	if strings.Contains(body, "当前没有待重建的页面") {
		t.Error("读失败被显示成了「没有待重建」：故障被静默")
	}
	if strings.Contains(body, "个页面有更新未发布") {
		t.Error("Available=false 时不应显示待重建徽章")
	}
}

func TestBlocksStaleImpactMissingKeyStillRenders(t *testing.T) {
	body := renderBlocksPage(t, blocksBaseData(true))
	if strings.Contains(body, "待重建影响面") || strings.Contains(body, "个页面有更新未发布") {
		t.Error("缺 StaleImpact 键时不该渲染任何影响面元素")
	}
	if !strings.Contains(body, "data-check-all") {
		t.Error("缺可选键影响了列表渲染（整页在某一行中断）")
	}
}

func TestBlocksWriteFormsAreHTMXWired(t *testing.T) {
	body := renderBlocksPage(t, blocksBaseData(true))

	for _, want := range []string{
		`hx-post="/api/block/create"`,
		`data-write-nav="[data-block-created-link]"`,

		`hx-post="/api/block/delete"`,
		`hx-post="/api/block/bulk-delete"`,

		`data-write-status`, `data-write-host`, `data-write-result`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("缺少 %q：写入口没接上 HTMX + Jet 片段", want)
		}
	}

	for _, stale := range []string{
		`action="/admin/blocks/create`, `action="/admin/blocks/delete`,
		`action="/admin/blocks/bulk-delete`, `data-write-url`, `data-confirm=`,
	} {
		if strings.Contains(body, stale) {
			t.Errorf("仍残留旧协议属性 %q", stale)
		}
	}

	if n := strings.Count(body, `data-write-status`); n < 3 {
		t.Errorf("结果区数量 = %d，期望至少 3（新建 / 批量 / 行内删除各一）", n)
	}
}
