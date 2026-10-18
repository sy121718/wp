package architecture

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// 视图层的两条纪律（见 AGENTS.md「前后端分界」的「模板只做展示判断」）：
//
//  1. 模板里不得对金额 / 数量做乘除 —— 那是把「怎么算钱」搬进视图，与 Go 侧的算法各成一份，
//     两边都不会报错，只会在某个角落给出对不上的数字。乘除是这条最干净的判据：合法的模板算术
//     只有一种形态（字段 × 字面量，把百分比换算成像素高度），两边都是字段就一定是业务计算。
//  2. 模板里不得累计（`+=`）—— 合计只能由 service 给，模板只读。
//
// 判据是启发式的（字段名不在清单里会漏），所以「门禁绿」不等于「视图层干净」，评审仍要看。

// templateMoneyFields 参与金额 / 数量语义的字段名片段（按前缀匹配，见下方正则）。
var templateMoneyFields = []string{
	"Price", "Amount", "Total", "Cost", "Fee", "Subtotal",
	"Qty", "Quantity", "Sales", "Revenue", "Discount",
}

// moneyArithBudget 登记的存量（文件 → 处数）。只收「字段名像金额、实际是图表几何」这一类，
// 双向判定：登记了却不再命中同样失败。
var moneyArithBudget = map[string]int{
	// 销售趋势图柱高：SalesHeightPct / ViewsHeightPct 是百分比，乘 1 是换算成像素（不是金额计算）。
	"internal/templates/admin/dashboard.html": 2,
}

var jetCommentRe = regexp.MustCompile(`(?s)[{][*].*?[*][}]`)

func templateMoneyArithRe() *regexp.Regexp {
	return regexp.MustCompile(`[{][{][^}]*[.]((?:` + strings.Join(templateMoneyFields, "|") + `)[A-Za-z]*)[ 	]*[*/][ 	]*[A-Za-z0-9_.(]`)
}

// TestTemplatesDoNotComputeMoney：模板里不得对金额 / 数量做乘除。
func TestTemplatesDoNotComputeMoney(t *testing.T) {
	root := repoRoot(t)
	re := templateMoneyArithRe()
	got := map[string]int{}
	walkTemplates(t, filepath.Join(root, "internal/templates"), func(rel, body string) {
		if n := len(re.FindAllStringIndex(body, -1)); n > 0 {
			got[rel] += n
		}
	})
	assertBudget(t, got, moneyArithBudget, "模板里对金额 / 数量做乘除 —— 合计算在 service，模板只读结果")
}

// TestTemplatesDoNotAccumulate：模板里不得出现 `+=`（累计即自己算合计）。
func TestTemplatesDoNotAccumulate(t *testing.T) {
	root := repoRoot(t)
	re := regexp.MustCompile(`[{][{][^}]*[+]=`)
	var hits []string
	walkTemplates(t, filepath.Join(root, "internal/templates"), func(rel, body string) {
		for _, m := range re.FindAllString(body, -1) {
			hits = append(hits, rel+"："+strings.TrimSpace(m))
		}
	})
	for _, h := range hits {
		t.Errorf("%s —— 模板不做累计，合计由 service 放进视图数据", h)
	}
}

// walkTemplates 遍历模板文件（剥掉 Jet 注释与 HTML 注释，注释里的示例不参与判定）。
func walkTemplates(t *testing.T, dir string, fn func(rel, body string)) {
	t.Helper()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || (!strings.HasSuffix(path, ".html") && !strings.HasSuffix(path, ".jet")) {
			return nil
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		body := jetCommentRe.ReplaceAllString(string(raw), "")
		body = regexp.MustCompile(`(?s)<!--.*?-->`).ReplaceAllString(body, "")
		rel, _ := filepath.Rel(repoRoot(t), path)
		fn(filepath.ToSlash(rel), body)
		return nil
	})
	if err != nil {
		t.Fatalf("遍历 %s 失败：%v", dir, err)
	}
}

// assertBudget 双向判定：未登记即失败，登记了却不再命中（或多出/少了处数）同样失败。
func assertBudget(t *testing.T, got, budget map[string]int, hint string) {
	t.Helper()
	for file, want := range budget {
		if got[file] != want {
			t.Errorf("%s：登记 %d 处，实测 %d 处（%s）", file, want, got[file], hint)
		}
		delete(got, file)
	}
	files := make([]string, 0, len(got))
	for file := range got {
		files = append(files, file)
	}
	sort.Strings(files)
	for _, file := range files {
		t.Errorf("%s：%d 处（%s）", file, got[file], hint)
	}
}
