// product_tag_hits_card_test.go — 标签页「命中的商品」卡改为内容驱动（审计 02-L P1-7）。
//
// 模板层单测（internal/templates/admin_product_tag_hits_card_test.go）用**手工数据**渲染，
// 挡不住「handler 实际注入的键与模板期望不一致」这一类中断：Jet 缺键时 HTTP 仍是 200，
// 而那一行之后的 HTML 整块消失（页面看起来只是「后半截没了」）。这里用真实 handler +
// 真实 service + 真实 Jet 渲染再钉一遍，把两件事落在同一条链路上：
//
//	① 首屏只有标签列表一张带卡体的卡，命中卡整张不渲染，只留一个**空的**锚点容器
//	  （且这个容器仍是 role="region" / aria-live="polite" 的播报区域）；
//	② 点开命中数之后，换进锚点的片段自带卡壳 + 商品表，且锚点 id 只出现在页面上。
package feature

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// tagHitsPanelRe 抓页面上锚点容器的整段开标签（属性判据都从这里读）。
var tagHitsPanelRe = regexp.MustCompile(`<div id="tag-hits-panel"[^>]*>`)

var tagHitsProductRe = regexp.MustCompile(`<strong>(命中商品 [0-9]{4})</strong>`)

// TestTagPageHitsCardIsContentDriven 首屏与片段在同一条真实链路上的形态。
func TestTagPageHitsCardIsContentDriven(t *testing.T) {
	f := newTagQueryFixture(t)
	if f == nil {
		return
	}
	projectID := f.newProject(t, "命中卡内容驱动工程")
	tagID := seedTags(t, f.db, projectID, 1)[0]
	// 60 个商品 = 2 页（每页 50）：让片段里的「下一页」真的渲染出来，
	// 从而能断言翻页按钮换回的仍是页面上那个锚点。
	seedTagProducts(t, f.db, projectID, tagID, "命中商品", 60)

	// —— ① 首屏：没有空壳卡 ——
	rec := httptestGet(f.engine, "/admin/product-tags?project="+projectID)
	if rec.Code != http.StatusOK {
		t.Fatalf("标签页应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// 尾部标记先证明没中断：缺键的渲染错误会静默吃掉后半截 HTML。
	if !strings.Contains(body, "</html>") {
		t.Fatalf("标签页渲染中断（尾部 </html> 缺失）：\n%s", body)
	}
	if got := strings.Count(body, `class="card card-body"`); got != 1 {
		t.Fatalf("首屏应有且只有标签列表一张 .card.card-body，实际 %d 张", got)
	}
	if strings.Contains(body, `<h2 class="card-title mb-lg">命中的商品`) {
		t.Fatal("首屏不应渲染「命中的商品」卡壳：卡壳在片段里，内容到了才出现")
	}
	anchor := tagHitsPanelRe.FindString(body)
	if anchor == "" {
		t.Fatal("首屏缺少 #tag-hits-panel 锚点容器（hx-target 的落点）")
	}
	for _, want := range []string{`role="region"`, `aria-live="polite"`, `aria-label="`} {
		if !strings.Contains(anchor, want) {
			t.Fatalf("锚点容器丢了 %s —— 换入的商品行对读屏不再播报：%s", want, anchor)
		}
	}
	if !strings.Contains(body, anchor+"</div>") {
		t.Fatalf("锚点容器初始应为空：%s", anchor)
	}

	// —— ② 点开命中数：换进来的片段自带卡壳与表格 ——
	rec = httptestGet(f.engine, "/admin/product-tags/hits?project="+projectID+"&id="+tagID+"&page=1")
	if rec.Code != http.StatusOK {
		t.Fatalf("命中商品片段应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	frag := rec.Body.String()
	for _, want := range []string{
		`class="card card-body"`, "命中的商品", "命中 60 个商品",
		`<table class="data-table`,
		`hx-target="#tag-hits-panel"`, // 翻页换回的仍是页面上的锚点
	} {
		if !strings.Contains(frag, want) {
			t.Fatalf("片段缺少 %q：\n%s", want, frag)
		}
	}
	// 锚点只属于页面：片段自带一份就会出现重复 id（HTMX 取第一个，翻页落点会漂移）。
	if strings.Contains(frag, `id="tag-hits-panel"`) {
		t.Fatalf("片段不应自带 #tag-hits-panel：\n%s", frag)
	}
	if strings.Contains(frag, "aria-live") {
		t.Fatalf("aria-live 必须留在页面容器上，不能随片段一起被替换：\n%s", frag)
	}

	// 同批插入商品按随机 UUID 兜底排序，编号 0000 不保证在第一页。
	seen := make(map[string]bool, 60)
	for page := 1; page <= 2; page++ {
		body := frag
		if page == 2 {
			rec = httptestGet(f.engine, "/admin/product-tags/hits?project="+projectID+"&id="+tagID+"&page=2")
			if rec.Code != http.StatusOK {
				t.Fatalf("第 2 页命中商品片段应 200，实际 %d：%s", rec.Code, rec.Body.String())
			}
			body = rec.Body.String()
		}
		names := tagHitsProductRe.FindAllStringSubmatch(body, -1)
		wantCount := 50
		if page == 2 {
			wantCount = 10
		}
		if len(names) != wantCount {
			t.Fatalf("第 %d 页应有 %d 个命中商品，实际 %d 个：\n%s", page, wantCount, len(names), body)
		}
		for _, name := range names {
			if seen[name[1]] {
				t.Fatalf("商品 %q 跨页重复出现", name[1])
			}
			seen[name[1]] = true
		}
	}
	for i := 0; i < 60; i++ {
		name := fmt.Sprintf("命中商品 %04d", i)
		if !seen[name] {
			t.Fatalf("两页片段缺少 %q", name)
		}
	}
}
