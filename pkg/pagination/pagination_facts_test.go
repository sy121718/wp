package pagination

// pagination_facts_test.go — 事实层（Page/Facts）的单元测试（task-94 P1 / task-99）。
//
// 这一层的验收判据只有一条：**它算出来的值与旧实现渲染出来的值逐项相同。**
// 旧实现（BuildPagination）不暴露 Start/End，它把它们直接烧进了信息行句子
// （「共 %s 条，第 %s-%s 条」）—— 所以这里就**从句子里把数字读回来**做对照，
// 而不是自己重算一遍（自己重算等于拿同一个公式自证，等于没测）。
//
// 旧实现在「单页/空集」时返回 nil，这一段没有句子可读：那两档的判据改为
// 「Facts 的 Pages <= 1 且 Start/End 为 0」（模板据此不渲染分页条）。

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

// legacyInfoNumbers 从旧实现的信息行里把「第 X-Y 条」的数字读回来。
var legacyInfoNumbers = regexp.MustCompile(`第 (\d+)-(\d+) 条`)

func TestFactsMatchesLegacyInfoSentence(t *testing.T) {
	cases := []struct {
		name         string
		total        int64
		page, limit  int
		wantPages    int
		wantPage     int
		wantStartEnd [2]int
	}{
		{name: "空集", total: 0, page: 1, limit: 20, wantPages: 0, wantPage: 1},
		{name: "不足一页", total: 5, page: 1, limit: 20, wantPages: 1, wantPage: 1},
		{name: "恰好一页", total: 20, page: 1, limit: 20, wantPages: 1, wantPage: 1},
		{name: "恰好多一条", total: 21, page: 1, limit: 20, wantPages: 2, wantPage: 1, wantStartEnd: [2]int{1, 20}},
		{name: "整除末页", total: 40, page: 2, limit: 20, wantPages: 2, wantPage: 2, wantStartEnd: [2]int{21, 40}},
		{name: "末页不满", total: 120, page: 2, limit: 20, wantPages: 6, wantPage: 2, wantStartEnd: [2]int{21, 40}},
		{name: "末页只剩一条", total: 21, page: 2, limit: 20, wantPages: 2, wantPage: 2, wantStartEnd: [2]int{21, 21}},
		{name: "页码超出上界被夹到末页", total: 120, page: 99, limit: 20, wantPages: 6, wantPage: 6, wantStartEnd: [2]int{101, 120}},
		{name: "页码下界被夹到第一页", total: 120, page: -3, limit: 20, wantPages: 6, wantPage: 1, wantStartEnd: [2]int{1, 20}},
		{name: "limit 非法回落到 20", total: 120, page: 1, limit: 0, wantPages: 6, wantPage: 1, wantStartEnd: [2]int{1, 20}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Facts(tc.total, tc.page, tc.limit)
			if got.Pages != tc.wantPages {
				t.Fatalf("Pages = %d, 期望 %d", got.Pages, tc.wantPages)
			}
			if got.Page != tc.wantPage {
				t.Fatalf("Page = %d, 期望 %d（夹紧口径必须与旧实现一致）", got.Page, tc.wantPage)
			}

			legacy := BuildPagination(tc.total, tc.page, tc.limit, "/admin/x", nil)
			if legacy == nil {
				// 旧实现在单页/空集直接返回 nil（连 Start/End 都没算），没有句子可对照。
				if got.Pages > 1 {
					t.Fatalf("旧实现判为无分页，Facts 却给出 %d 页", got.Pages)
				}
				if tc.total <= 0 {
					if got.Start != 0 || got.End != 0 {
						t.Fatalf("空集的 Start/End 应为 0/0，实际 %d/%d", got.Start, got.End)
					}
					return
				}
				// 有数据但只有一页时，事实层仍如实给出本页覆盖范围（1..min(total,limit)），
				// 因为"这一页有哪些条"是事实，与"要不要渲染分页条"无关（P0b 探针「单页」档同此形态）。
				wantEnd := int(tc.total)
				if wantEnd > got.Limit {
					wantEnd = got.Limit
				}
				if got.Start != 1 || got.End != wantEnd {
					t.Fatalf("单页的 Start/End 应为 1/%d，实际 %d/%d", wantEnd, got.Start, got.End)
				}
				return
			}

			m := legacyInfoNumbers.FindStringSubmatch(legacy.Info)
			if m == nil {
				t.Fatalf("旧实现信息行形态变了，无法对照: %q", legacy.Info)
			}
			want := [2]int{atoi(t, m[1]), atoi(t, m[2])}
			if got.Start != want[0] || got.End != want[1] {
				t.Fatalf("Start/End = %d/%d，旧实现信息行说的是 %d/%d（%q）",
					got.Start, got.End, want[0], want[1], legacy.Info)
			}
			if got.Total != legacy.Total || got.Limit != legacy.Limit {
				t.Fatalf("Total/Limit = %d/%d，旧实现 = %d/%d", got.Total, got.Limit, legacy.Total, legacy.Limit)
			}
			if tc.wantStartEnd != [2]int{} && want != tc.wantStartEnd {
				t.Fatalf("用例期望 %v，旧实现给出 %v（用例表与旧实现不一致，先查用例）", tc.wantStartEnd, want)
			}
		})
	}
}

// TestFactsDefaultsKeepCurrentLinkBytes PageParam/KeepLimit 的默认值不是随手取的：
// 迁移前所有链接都是 `?page=N&limit=M`，默认值必须与它一致，P3 才可能做到链接一字不变。
func TestFactsDefaultsKeepCurrentLinkBytes(t *testing.T) {
	got := Facts(120, 2, 20)
	if got.PageParam != "page" {
		t.Fatalf("PageParam 默认应为 page，实际 %q", got.PageParam)
	}
	if !got.KeepLimit {
		t.Fatalf("KeepLimit 默认应为 true（现行链接带 limit），实际 %v", got.KeepLimit)
	}
	if got.Base != "" {
		t.Fatalf("Base 应由调用点给（路径+筛选），Facts 不该猜，实际 %q", got.Base)
	}
}

// TestPageZeroValueIsInert 零值必须「什么也不做」：这是值语义的全部意义
// —— 误注入零值只会让模板不渲染分页条，既不会 panic，也不会渲染出半截。
func TestPageZeroValueIsInert(t *testing.T) {
	var zero Page
	if zero.HasPagination() || zero.HasPrev() || zero.HasNext() {
		t.Fatalf("零值不该有任何可渲染行为: %+v", zero)
	}
}

func TestPageAccessors(t *testing.T) {
	// 单页与空集都不显示分页条：`Pages > 1` 这个下界是判据本身，
	// 放宽成 `Pages > 0` 会让"只有一页"的列表长出一条没用的分页条。
	if s := Facts(5, 1, 20); s.HasPagination() {
		t.Fatalf("单页不该显示分页条: %+v", s)
	}
	if e := Facts(0, 1, 20); e.HasPagination() {
		t.Fatalf("空集不该显示分页条: %+v", e)
	}

	mid := Facts(120, 2, 20)
	if !mid.HasPagination() || !mid.HasPrev() || !mid.HasNext() {
		t.Fatalf("中间页应三者皆真: %+v", mid)
	}
	if mid.PrevPage() != 1 || mid.NextPage() != 3 {
		t.Fatalf("上一页/下一页 = %d/%d，期望 1/3", mid.PrevPage(), mid.NextPage())
	}

	last := Facts(120, 6, 20)
	if last.HasNext() {
		t.Fatalf("末页不该有下一页: %+v", last)
	}

	first := Facts(120, 1, 20)
	if first.HasPrev() {
		t.Fatalf("首页不该有上一页: %+v", first)
	}
}

// TestFactsLayerStaysPresentationFree 是**常驻边界守卫**：事实层里一旦出现格式化、文案或 URL 拼装，
// 「展示搬回 Go」就会以最隐蔽的方式回来（没人会为一行 fmt.Sprintf 提反对意见，但它是这类迁移的全部意义）。
//
// 判据只看事实层这一段（上方旧 API 的那一层正在迁移中，还带着展示产物，不属本判据）。
func TestFactsLayerStaysPresentationFree(t *testing.T) {
	src, err := os.ReadFile("pagination.go")
	if err != nil {
		t.Fatalf("读取源码失败: %v", err)
	}
	marker := "// ============================ 事实层 ============================"
	idx := strings.Index(string(src), marker)
	if idx < 0 {
		t.Fatalf("找不到事实层标记 %q —— 标记被改掉时本守卫会失效，请同步更新", marker)
	}
	section := string(src)[idx:]

	// 去掉注释后再判（注释里当然会出现中文与解释性的词）。
	var code strings.Builder
	for _, line := range strings.Split(section, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		code.WriteString(line)
		code.WriteString("\n")
	}
	body := code.String()

	forbidden := map[string]string{
		"fmt.":         "格式化必须留在展示层（模板的 sprintf）",
		"strconv.":     "数字转字符串必须留在展示层",
		"Sprintf":      "拼句子就是预制展示产物",
		"%s":           "占位符属于文案层",
		"i18n":         "取词是请求级的，事实层不该碰",
		"http":         "URL 拼装属于展示层（模板的 queryURL）",
		"url.":         "URL 编码属于展示层",
		"Label":        "按钮文字是展示产物",
		"URL":          "链接是展示产物",
		"Info":         "信息行句子是展示产物",
		"Links":        "链接列表是展示产物",
		"TemplateKeys": "按模板键名打包是旧展示形态的做法",
	}
	for needle, why := range forbidden {
		if strings.Contains(body, needle) {
			t.Fatalf("事实层出现 %q —— %s", needle, why)
		}
	}
	for _, r := range body {
		if r > 0x2E80 { // CJK 起步：正文里出现中文＝有人在事实层里写文案
			t.Fatalf("事实层代码里出现非 ASCII 字符 %q —— 文案属于展示层", r)
		}
	}
}

func atoi(t *testing.T, s string) int {
	t.Helper()
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		t.Fatalf("解析 %q 失败: %v", s, err)
	}
	return n
}
