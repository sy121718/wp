package templates

// admin_pagination_b3b_facts_test.go — task-117（P3-B3b）：user / content / inventory
// 三个模块**七个**整页分页站点的「旧成品 vs 新事实」逐字节差分，外加一条源码级
// 结构断言（这三个模块里不许再出现旧键与旧构造器）。
//
// 与 admin_pagination_b3_facts_test.go（task-116，四小模块）同一口径：证明的不是
// 「大概一样」而是**渲染字节完全相同** —— 迁移前 handler 交的是 BuildPagination
// 预制好的信息行句子 + 链接（PaginationInfo / PaginationLinks），迁移后只交
// pagination.Page 事实，句子 / 页码窗口 / 链接参数顺序全部由
// admin/partials/pagination.html 用 sprintf / queryURL / pageNumbers 现算。
//
// 只读复用 P2 的 p2RenderLegacy / p2RenderFacts / p2FirstDiff 与 B3 的 b3Site /
// b3Shapes（**不改**那两个文件：前者带 const baseline = 31 的「只减不增」计数判据）。
//
// 本批特别钉住的两处：
//   · user/customer-rfm 的基址 = 路径 + 区间查询串（可选 &segment=…）——「翻页不丢
//     区间与分段」在旧实现里靠基址拼接保证，迁到事实层后必须一字不差。
//   · content/article-translations 的基址**显式带 limit**（本页 limit 由筛选表单
//     钉死），旧链接会把 limit 发射两次（值相同，gin 取首个）。此处保持逐字节一致：
//     已知遗留，登记在卡面，不顺带修正以维持 0 字节差判据。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// b3bSites 七个站点的两种基址形态（带全量筛选 / 最小筛选）。
//
// 基址取值照抄 handler 侧的真实构造（FilterBaseURL 的输出：已 Encode、按 Keys 序），
// 包括 content/article-translations 那种**自带 limit** 的形态 —— 只有把真实基址
// 钉住，才算钉住「翻页不丢条件」与「不凭空多出参数」。
func b3bSites() []b3Site {
	const pid = "11111111-1111-1111-1111-111111111111"
	return []b3Site{
		{
			name:     "user-customers",
			filtered: "/admin/customers?keyword=%E9%98%BF%E9%87%8C&tierId=3&status=1",
			plain:    "/admin/customers",
		},
		{
			// 区间查询串由 customerRangeQuery 拼（from/to + 可选 segment）。
			name:     "user-customer-rfm",
			filtered: "/admin/customers/rfm?from=2026-01-01&to=2026-01-31&segment=champions",
			plain:    "/admin/customers/rfm?from=2026-01-01&to=2026-01-31",
		},
		{
			name:     "content-articles",
			filtered: "/admin/articles?keyword=%E4%BB%B7%E6%A0%BC",
			plain:    "/admin/articles",
		},
		{
			name:     "content-article-translations",
			filtered: "/admin/articles/translations?lang=zh-CN&project=" + pid + "&keyword=&limit=20",
			plain:    "/admin/articles/translations?lang=zh-CN&project=" + pid + "&limit=20",
		},
		{
			name: "inventory-movements",
			filtered: "/admin/inventory?project=" + pid + "&sku=SKU-1&variantId=42&warehouseId=9" +
				"&direction=in&reasonCode=purchase&timeFrom=2026-01-01&timeTo=2026-01-31",
			plain: "/admin/inventory?project=" + pid,
		},
		{
			name:     "inventory-purchases",
			filtered: "/admin/inventory/purchases?project=" + pid + "&status=open&sourceId=5&keyword=PO-",
			plain:    "/admin/inventory/purchases?project=" + pid,
		},
		{
			name:     "inventory-sources",
			filtered: "/admin/inventory/sources?project=" + pid + "&type=internal&relatedParty=7&status=active&keyword=%E5%B7%A5%E5%8E%82",
			plain:    "/admin/inventory/sources?project=" + pid,
		},
	}
}

// TestB3bPaginationSitesRenderIdenticallyToLegacy 七站点 × 两基址 × 六形态：逐字节相同。
func TestB3bPaginationSitesRenderIdenticallyToLegacy(t *testing.T) {
	for _, site := range b3bSites() {
		for baseName, base := range map[string]string{"带筛选": site.filtered, "无筛选": site.plain} {
			for _, sh := range b3Shapes() {
				name := site.name + "/" + baseName + "/" + sh.name
				s := p0PaginationShape{
					name:  name,
					total: sh.total,
					page:  sh.page,
					limit: sh.limit,
					base:  base,
				}
				legacy := p2RenderLegacy(t, s)
				facts := p2RenderFacts(t, s, nil)
				if legacy != facts {
					t.Errorf("[%s] 旧成品与新事实的渲染字节不同：\n%s", name, p2FirstDiff(legacy, facts))
				}
			}
		}
	}
}

// TestB3bPaginationSitesSinglePageAndEmptyRenderNothing 单页与空集两边都渲染空。
func TestB3bPaginationSitesSinglePageAndEmptyRenderNothing(t *testing.T) {
	for _, site := range b3bSites() {
		for _, sh := range []struct {
			name  string
			total int64
			page  int
		}{
			{"单页", 5, 1},
			{"空集", 0, 1},
		} {
			s := p0PaginationShape{name: site.name + "/" + sh.name, total: sh.total, page: sh.page, limit: 20, base: site.filtered}
			for _, out := range []struct {
				kind string
				html string
			}{
				{"旧成品", p2RenderLegacy(t, s)},
				{"新事实", p2RenderFacts(t, s, nil)},
			} {
				if strings.Contains(out.html, "<nav") || strings.Contains(out.html, "pagination") {
					t.Errorf("[%s/%s] %s 档不该渲染分页条，实际:\n%s", site.name, sh.name, out.kind, out.html)
				}
			}
		}
	}
}

// TestB3bModulesHaveNoLegacyPaginationResidue 源码级：三个模块的生产代码里不许再有
// 旧键 / 旧构造器 / 旧读取方式。
//
// 为什么单独一条：上面两条差分证明的是「partial 两种形态等价」，而**handler 是否真的
// 改成了新形态**只有源码能证明 —— 一个漏改的站点会继续注入旧键，差分照样全绿
// （旧分支还在）。这条把「三个模块里 0 处残留」钉死。
func TestB3bModulesHaveNoLegacyPaginationResidue(t *testing.T) {
	dirs := []string{
		filepath.Join("..", "module", "user", "inbound", "http"),
		filepath.Join("..", "module", "content", "inbound", "http"),
		filepath.Join("..", "module", "inventory", "inbound", "http"),
	}
	// 旧构造器 / 旧键 / 旧读取方式：
	banned := []string{
		"pagination.BuildPagination",
		"TemplateKeys()",
		"PaginationLinks",
		"PaginationInfo",
	}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("读目录失败 %s: %v", dir, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			body, rerr := os.ReadFile(filepath.Join(dir, name))
			if rerr != nil {
				t.Fatalf("读文件失败 %s: %v", name, rerr)
			}
			for _, b := range banned {
				if strings.Contains(string(body), b) {
					t.Errorf("%s/%s 仍有旧分页残留 %q —— 该站点未迁到 pagination.Page 事实层", dir, name, b)
				}
			}
		}
	}
}
