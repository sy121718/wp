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

var templateMoneyFields = []string{
	"Price", "Amount", "Total", "Cost", "Fee", "Subtotal",
	"Qty", "Quantity", "Sales", "Revenue", "Discount",
}

var moneyArithBudget = map[string]int{

	"internal/templates/admin/dashboard.html": 2,
}

var jetCommentRe = regexp.MustCompile(`(?s)[{][*].*?[*][}]`)

func templateMoneyArithRe() *regexp.Regexp {
	return regexp.MustCompile(`[{][{][^}]*[.]((?:` + strings.Join(templateMoneyFields, "|") + `)[A-Za-z]*)[ 	]*[*/][ 	]*[A-Za-z0-9_.(]`)
}

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
