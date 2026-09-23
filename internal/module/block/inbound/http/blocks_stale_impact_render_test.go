package blockhttp

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"go_wp/internal/templates"
	"go_wp/pkg/logger"
)

// blocks_stale_impact_render_test.go — 全局块管理页的渲染级契约（02-L P1-4 / P1-10）。
//
// 钉两件事，都是「改错了不会报错、只会静默变形」的那一类：
//
//	① **一张表**（P1-4）：合并前三段各渲染一张 <table>，[data-check-all] 的唯一性靠
//	   「走在前面的段都为空」这组跨段条件保证 —— 加一类块、调一次顺序都要重推一遍，
//	   推错就多出一个全选框，而 admin.js 的 refresh 只认 scope 里第一个。现在判据是
//	   结构事实：整页只有一张表、一个全选框。
//	② **影响面降级**（P1-10）：从常驻只读卡降为页头徽章 + 折叠清单，但**三档语义一个
//	   都不能少** —— 尤其是 Available=false（契约未装配 / 读工程失败）这一档：它必须继续
//	   输出 Hint 原文，绝不能与「没有待重建」合并成同一句话，否则故障被静默成「一切正常」。
//
// 这些断言都用**中文兜底**：测试进程不连库，pkg/i18n 缓存为空，t() 一律回落到模板内原文。

func blocksLayoutData(base gin.H) gin.H {
	lang := "zh-CN"
	base["csrf_token"] = "test-token"
	base["lang"] = lang
	base["t"] = templates.TranslateFunc(lang)
	base["langs"] = templates.LanguageOptions(lang)
	base["lang_redirect"] = "/admin/blocks"
	base["PermSet"] = map[string]any{"block:create": true, "block:delete": true}
	return base
}

// blocksRow 一行列表数据（形状取自 handler 的 blockRow 投影）。
func blocksRow(id, name, kindLabel, reuseLabel string) gin.H {
	return gin.H{
		"ID": id, "Name": name, "KindLabel": kindLabel, "ReuseModeLabel": reuseLabel,
		"UpdatedAt": "2026-01-02 15:04", "RefCountText": "2 个页面引用",
	}
}

// renderBlocksPage 用真实模板渲染 /admin/blocks，返回完整响应体。
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
	// 渲染中途中断的形态是「HTTP 200 + 后半页整块消失」，只断言状态码抓不到它。
	if !strings.Contains(body, "</html>") {
		t.Fatalf("admin/blocks 未渲染到布局尾部（模板在某一行中断，列表可能整块消失）")
	}
	return body
}

// blocksBaseData 一套最小可用数据（一个工程、三类块各一行）。
func blocksBaseData(rows bool) gin.H {
	data := gin.H{
		"title": "区块管理", "menu": "blocks",
		"Projects": []gin.H{{"ID": "prj", "Name": "站点"}}, "SelectedProject": "prj",
		"Headers": []gin.H{}, "Footers": []gin.H{}, "Blocks": []gin.H{},
		"Err": "", "Done": "",
	}
	if rows {
		data["Headers"] = []gin.H{blocksRow("b1", "站点页眉", "页眉", "引用")}
		data["Footers"] = []gin.H{blocksRow("b2", "站点页脚", "页脚", "引用")}
		data["Blocks"] = []gin.H{
			blocksRow("b3", "信任徽章", "信任徽章", "引用"),
			blocksRow("b4", "商品卡", "区块", "复制"),
		}
	}
	return data
}

// TestBlocksListIsSingleTable 一页一张表、一个全选框（合并三段表后的核心结构断言）。
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
			// 全选框只在有行时出现：判据是「有行 ⇒ 恰好一个」，不是「恒为一个」。
			// 空表勾选没有意义；而**多于一个**才是原设计要防的那个缺陷（admin.js 只认第一个）。
			wantCheckAll := 0
			if tc.rows {
				wantCheckAll = 1
			}
			if n := strings.Count(body, "data-check-all"); n != wantCheckAll {
				t.Errorf("data-check-all 数量 = %d，期望 %d（唯一性必须由结构保证，不再依赖跨段 if）", n, wantCheckAll)
			}
			// 表头始终在：空态也要能看见有哪些列（scripts/check-empty-state-table-head.sh 的判据）。
			if !strings.Contains(body, "<thead") || !strings.Contains(body, "类型") {
				t.Error("空态/有数据两种形态下表头都必须在，且含「类型」列")
			}
			// 空态行落在 <tbody> 的 <td colspan> 里（表头因此不会被空态吃掉）。
			if !tc.rows && !strings.Contains(body, `colspan="7"`) {
				t.Error("空态行的 colspan 应为 7（勾选/名称/类型/复用方式/更新时间/影响面/操作）")
			}
			if !tc.rows && !strings.Contains(body, "还没有全局块") {
				t.Error("三类全空时缺合并后的唯一空态")
			}
		})
	}
}

// TestBlocksListKeepsSelectionAndDeleteWiring 勾选与批量/删除链路在合并后仍然接通。
func TestBlocksListKeepsSelectionAndDeleteWiring(t *testing.T) {
	body := renderBlocksPage(t, blocksBaseData(true))

	for _, want := range []string{
		`action="/admin/blocks/bulk-delete"`, // 勾选框所在的批量表单
		`name="ids"`, "data-check-item", "data-bulk-bar", "data-bulk-count",
		"data-filter-input", "data-filter-text", "data-filter-empty",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("合并表后缺少 %q：批量选择 / 筛选链路断了", want)
		}
	}
	// 行内删除表单：每行一个，id 取自块 id（三类块同属 blocks 表，id 全局唯一）。
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

// TestBlocksStaleImpactTierStalePage 有 stale：页头徽章 + 折叠清单，且不再有常驻只读卡。
func TestBlocksStaleImpactTierStalePage(t *testing.T) {
	data := blocksBaseData(true)
	data["StaleImpact"] = gin.H{
		"Available": true,
		"Pages":     []gin.H{{"ID": "pg1", "Path": "/blog/hello-world", "ProjectName": "站点"}},
		"Total":     1, "Truncated": false, "Limit": 30, "Hint": "",
	}
	body := renderBlocksPage(t, data)

	for _, want := range []string{
		"1", "个页面有更新未发布", // 页头徽章的计数与后缀
		`class="badge badge-warning"`,
		`class="page-sub"`,
		`<details class="section-fold card">`,
		"待重建影响面", "/blog/hello-world",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("有 stale 时缺少 %q", want)
		}
	}
	// 降级判据：列表卡之上的常驻只读卡（旧形态是 <section class="card">）不再存在。
	if strings.Contains(body, `<section class="card">`) {
		t.Error("影响面仍是常驻只读卡：应降级为页头徽章 + <details class=\"section-fold card\"> 折叠清单")
	}
	// 清单默认收起：details 不带 open（不占首屏）。
	if strings.Contains(body, `<details class="section-fold card" open>`) {
		t.Error("影响面清单默认展开了 —— 本次降级的目的正是不让它顶掉列表首屏")
	}
}

// TestBlocksStaleImpactTierNone 无 stale：只说「当前没有待重建的页面」，页头不给徽章。
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
	if strings.Contains(body, `<details class="section-fold card">`) {
		t.Error("没有 stale 时不应渲染空的折叠清单")
	}
}

// TestBlocksStaleImpactTierUnavailableKeepsHint 读失败 / 未装配：Hint 原文必须透出。
//
// 这是三档里最容易在改版中被「顺手合并掉」的一档：把读失败显示成「一切正常」，
// 故障就再也没人看见了 —— 本用例就是防这一条。
func TestBlocksStaleImpactTierUnavailableKeepsHint(t *testing.T) {
	const hint = "读取站点工程失败，本次无法统计待重建影响面。"
	data := blocksBaseData(true)
	data["StaleImpact"] = gin.H{
		"Available": false, "Pages": []gin.H{}, "Total": 0, "Truncated": false,
		"Limit": 30, "Hint": hint,
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

// TestBlocksStaleImpactMissingKeyStillRenders 缺可选键（直接渲染模板的单测形态）仍整页渲染。
func TestBlocksStaleImpactMissingKeyStillRenders(t *testing.T) {
	body := renderBlocksPage(t, blocksBaseData(true)) // 不带 StaleImpact 键
	if strings.Contains(body, "待重建影响面") || strings.Contains(body, "个页面有更新未发布") {
		t.Error("缺 StaleImpact 键时不该渲染任何影响面元素")
	}
	if !strings.Contains(body, "data-check-all") {
		t.Error("缺可选键影响了列表渲染（整页在某一行中断）")
	}
}
