package feature

// mail_split_guard_test.go — 邮件模块拆页的**静态**残留守卫（不连库、不渲染）。
//
// 拆页把「一页多表」拆成「一页一职能」，留下的往往不是编译错误，而是三类静默缺陷：
//  1. 引用落点写错：extends / import / include 都相对「当前模板所在目录」解析，模板搬目录后
//     少一层（../ 写成 ../../）既不报编译错、也不会让既有测试变红 —— 该页 500，
//     且 htmx 档下 5xx 不 swap，用户点了按钮毫无反应（见 internal/templates/CLAUDE.md 首节）；
//  2. 旧页残留：旧模板 / 旧路径还被别的模板引用，页面照常渲染，只是链到一个已被 302 的地址；
//  3. 列数错配：表头删/加一列而空态行的 colspan 没跟着改 —— 浏览器把缺的那列补在行尾，
//     整表左移一列，模板层没有异常，纯计数断言（表数量、行数量）也抓不到。
//
// 判据一律是机械的、带坏样本自检（没有自检的门禁很容易退化成空转仍然全绿，
// 与 internal/templates/admin_template_resolve_test.go 同一套做法）。
// 渲染层的事（200 / h1 出现在正文 / 302 / key 残留）不在这里，交给页面级守卫。

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	// mailGuardTemplateDir 相对本包目录的邮件模板目录（go test 的工作目录就是包目录）。
	mailGuardTemplateDir = "../../../../internal/templates/admin/mail"
	// mailGuardRetiredTemplate 拆页后不再存在的旧模板：它的职能分别落到活动页与联系人页。
	mailGuardRetiredTemplate = "mail_marketing.html"
)

var (
	// mailGuardRefRe 抓 extends / include / import 的第一个字面量路径参数；变量形式不匹配。
	mailGuardRefRe = regexp.MustCompile(`\{\{\s*(?:extends|include|import)\s+"([^"]+)"`)
	// mailGuardCommentRe 模板注释（注释里常写示例路径，照字面统计必然误报）。
	mailGuardCommentRe = regexp.MustCompile(`(?s)\{\*.*?\*\}`)
	// mailGuardH1Re 一级标题标签。
	mailGuardH1Re = regexp.MustCompile(`<h1[\s>]`)
	// mailGuardTheadRe 表头块（非贪婪，逐表匹配）。
	mailGuardTheadRe = regexp.MustCompile(`(?s)<thead>.*?</thead>`)
	// mailGuardThRe 表头单元格。
	mailGuardThRe = regexp.MustCompile(`<th[\s>]`)
	// mailGuardColspanRe 跨列值（空态行 / 分组行都用它）。
	mailGuardColspanRe = regexp.MustCompile(`colspan="(\d+)"`)
)

// mailGuardStripComments 去掉模板注释，剩下的文本才参与判据。
func mailGuardStripComments(src string) string {
	return mailGuardCommentRe.ReplaceAllString(src, "")
}

// mailGuardTemplates 列出邮件模板目录下全部 .html 文件（工程根相对路径）。
func mailGuardTemplates(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(mailGuardTemplateDir)
	if err != nil {
		t.Fatalf("读取邮件模板目录失败 %s: %v", mailGuardTemplateDir, err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".html") {
			continue
		}
		files = append(files, mailGuardTemplateDir+"/"+e.Name())
	}
	if len(files) == 0 {
		t.Fatalf("%s 下没有 .html 模板 —— 路径写错了，判据会变成空转", mailGuardTemplateDir)
	}
	return files
}

// mailGuardRefProblems 检查一份模板源码的引用落点，返回问题描述（空 = 全部可解析）。
func mailGuardRefProblems(path string, src string) []string {
	var problems []string
	dir := filepath.Dir(filepath.FromSlash(path))
	for _, m := range mailGuardRefRe.FindAllStringSubmatch(mailGuardStripComments(src), -1) {
		rel := m[1]
		if !strings.HasSuffix(rel, ".html") {
			rel += ".html" // Jet 允许省略后缀
		}
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if _, err := os.Stat(target); err != nil {
			problems = append(problems, "引用 "+m[1]+" 的落点不存在："+filepath.ToSlash(target))
		}
	}
	return problems
}

// TestMailGuardTemplateReferencesResolve 邮件模板的每个 extends / import / include 都要有落点。
func TestMailGuardTemplateReferencesResolve(t *testing.T) {
	files := mailGuardTemplates(t)
	checked := 0
	for _, f := range files {
		raw, err := os.ReadFile(filepath.FromSlash(f))
		if err != nil {
			t.Fatalf("读模板失败 %s: %v", f, err)
		}
		src := string(raw)
		checked += len(mailGuardRefRe.FindAllStringSubmatch(mailGuardStripComments(src), -1))
		for _, p := range mailGuardRefProblems(f, src) {
			t.Errorf("%s：%s", f, p)
		}
	}
	if checked == 0 {
		t.Fatal("没扫到任何 extends/import/include —— 判据成了空转（写法或路径变了？）")
	}
	t.Logf("检查了 %d 个邮件模板、%d 处引用", len(files), checked)
}

// TestMailGuardReferenceCheckIsNotVacuous 用坏样本钉住判据本身。
func TestMailGuardReferenceCheckIsNotVacuous(t *testing.T) {
	// 样本路径与真实扫描同源（相对本包目录），否则落点检查会一律判「不存在」，
	// 这条自检本身就成了误报源。
	sample := mailGuardTemplateDir + "/mail.html"
	// 坏样本：从 admin/mail/ 里按同目录解析 —— admin/mail/layout.html 并不存在。
	bad := `{{extends "./layout.html"}}`
	if got := mailGuardRefProblems(sample, bad); len(got) != 1 {
		t.Fatalf("坏样本应报 1 个问题，实际 %d 个：%v", len(got), got)
	}
	good := `{{extends "../layout.html"}}` + "\n" + `{{include "../partials/pagination.html"}}`
	if got := mailGuardRefProblems(sample, good); len(got) != 0 {
		t.Fatalf("正确样本不该报问题：%v", got)
	}
	commented := `{* 例：{{include "../partials/nope.html"}} *}`
	if got := mailGuardRefProblems(sample, commented); len(got) != 0 {
		t.Fatalf("注释里的示例不该被算作引用：%v", got)
	}
}

// TestMailGuardNoStaleLegacyPage 拆页后不许残留旧页：旧模板不被引用、旧路径不进模板。
//
// 旧路径 /admin/mail/marketing 只允许出现在 302 实现（Go）与测试断言里 ——
// 模板里出现它，说明那一页的链接没跟着拆页改，用户会被送到一个重定向。
func TestMailGuardNoStaleLegacyPage(t *testing.T) {
	files := mailGuardTemplates(t)
	for _, f := range files {
		raw, err := os.ReadFile(filepath.FromSlash(f))
		if err != nil {
			t.Fatalf("读模板失败 %s: %v", f, err)
		}
		src := mailGuardStripComments(string(raw))
		if strings.Contains(src, mailSplitLegacyPath) {
			t.Errorf("%s 仍在引用旧路径 %s（拆页后应指向 /admin/mail/contacts 或 /admin/mail/campaigns）",
				f, mailSplitLegacyPath)
		}
		if strings.Contains(src, mailGuardRetiredTemplate) {
			t.Errorf("%s 仍引用已拆掉的模板 %s", f, mailGuardRetiredTemplate)
		}
	}
	if _, err := os.Stat(filepath.Join(mailGuardTemplateDir, mailGuardRetiredTemplate)); err == nil {
		t.Errorf("%s 应随拆页删除：它的两张表已分别落到活动页与联系人页，留着就是第二份真相",
			mailGuardRetiredTemplate)
	}
}

// TestMailGuardEveryRowColspanMatchesItsTableHeader 空态行 / 跨列行的 colspan 必须等于该表列数。
//
// 单表模板（拆页后每页一张主表）判据最强：文件里所有 colspan 都必须等于这张表的 th 数。
// 多表模板（拆页尚未落地的过渡态、或页内确有主表 + 次要表）退化成
// 「每个 colspan 必须命中该文件任一 thead 的列数」，先不制造误报。
func TestMailGuardEveryRowColspanMatchesItsTableHeader(t *testing.T) {
	files := mailGuardTemplates(t)
	for _, f := range files {
		raw, err := os.ReadFile(filepath.FromSlash(f))
		if err != nil {
			t.Fatalf("读模板失败 %s: %v", f, err)
		}
		src := mailGuardStripComments(string(raw))

		heads := mailGuardTheadRe.FindAllString(src, -1)
		if len(heads) == 0 {
			continue // 无表格的页面（画布 / 编辑表单）不在本判据范围内
		}
		cols := map[int]bool{}
		for _, head := range heads {
			cols[len(mailGuardThRe.FindAllString(head, -1))] = true
		}
		for _, m := range mailGuardColspanRe.FindAllStringSubmatch(src, -1) {
			n := 0
			for _, ch := range m[1] {
				n = n*10 + int(ch-'0')
			}
			if !cols[n] {
				t.Errorf("%s 的 colspan=\"%d\" 与任何表头列数都对不上（本文件的表头列数：%v）——"+
					"colspan 必须精确等于列数，否则整表错位", f, n, keysOfIntSet(cols))
			}
			if len(heads) == 1 && n != len(mailGuardThRe.FindAllString(heads[0], -1)) {
				t.Errorf("%s 只有一张表（%d 列），却出现 colspan=\"%d\"", f,
					len(mailGuardThRe.FindAllString(heads[0], -1)), n)
			}
		}
	}
}

// keysOfIntSet 把集合转成有序切片，仅用于失败信息（map 打印顺序不稳定，读日志的人会以为是随机差异）。
func keysOfIntSet(set map[int]bool) []int {
	out := make([]int, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// TestMailGuardPageTemplatesDeclareH1 页面模板（含表格的列表页）必须有一个一级标题。
//
// h1 是「这一页是什么」的唯一正文级线索；拆页后新增的页面漏了它，页面不会报错，
// 只是从侧栏点进来后看不出自己在哪一页。
func TestMailGuardPageTemplatesDeclareH1(t *testing.T) {
	files := mailGuardTemplates(t)
	checked := 0
	for _, f := range files {
		raw, err := os.ReadFile(filepath.FromSlash(f))
		if err != nil {
			t.Fatalf("读模板失败 %s: %v", f, err)
		}
		src := mailGuardStripComments(string(raw))
		if !strings.Contains(src, "<table") {
			continue // 画布 / 表单页不参与
		}
		checked++
		if n := len(mailGuardH1Re.FindAllString(src, -1)); n != 1 {
			t.Errorf("%s 应恰好有 1 个 <h1>，实际 %d 个", f, n)
		}
	}
	if checked == 0 {
		t.Fatal("没有任何含表格的页面模板被检查 —— 判据成了空转")
	}
}

// mailGuardStyleAttrRe 抓内联 style 属性（含值），单双引号都算。
var mailGuardStyleAttrRe = regexp.MustCompile(`style\s*=\s*(?:"([^"]*)"|'([^']*)')`)

// mailGuardInlineStyleBaseline 拆页前工作树里唯一一处内联 style 的值。
//
// 为什么不是「一律禁止」：仓内已有一批历史内联 style（product / system 等模板），
// 一刀切会立刻变成一片红噪声，最后被绕过。所以固化成基线：邮件模块里内联 style
// 只允许这一种、且总数不超过 1 处 —— 新增一处就报缺陷。
var mailGuardInlineStyleBaseline = "word-break:break-all"

// mailGuardStyleValue 从匹配结果里取 style 的实参（两个捕获组，哪个非空用哪个）。
func mailGuardStyleValue(m []string) string {
	if m[1] != "" {
		return m[1]
	}
	return m[2]
}

// mailGuardInlineStyleProblems 返回一份模板里超出基线的内联 style 描述。
func mailGuardInlineStyleProblems(src string) []string {
	var problems []string
	for _, m := range mailGuardStyleAttrRe.FindAllStringSubmatch(mailGuardStripComments(src), -1) {
		v := mailGuardStyleValue(m)
		if v != mailGuardInlineStyleBaseline {
			problems = append(problems, `内联 style="`+v+`"（应改用 CSS 类）`)
		}
	}
	return problems
}

// TestMailGuardNoInlineStyleBeyondBaseline 邮件模板不得新增内联 style。
//
// 内联 style 绕过了主题变量与暗色模式（两套样式都在 CSS 类里），
// 且模板改版时是最容易被漏掉的一类残留 —— 它不影响渲染成功，只影响一致性。
func TestMailGuardNoInlineStyleBeyondBaseline(t *testing.T) {
	files := mailGuardTemplates(t)
	baselineHits := 0
	for _, f := range files {
		raw, err := os.ReadFile(filepath.FromSlash(f))
		if err != nil {
			t.Fatalf("读模板失败 %s: %v", f, err)
		}
		src := string(raw)
		for _, p := range mailGuardInlineStyleProblems(src) {
			t.Errorf("%s：%s", f, p)
		}
		for _, m := range mailGuardStyleAttrRe.FindAllStringSubmatch(mailGuardStripComments(src), -1) {
			if mailGuardStyleValue(m) == mailGuardInlineStyleBaseline {
				baselineHits++
			}
		}
	}
	if baselineHits > 1 {
		t.Errorf("基线内的内联 style（%s）出现 %d 次 > 1：照抄基线写法同样是新增内联样式",
			mailGuardInlineStyleBaseline, baselineHits)
	}
	t.Logf("扫描 %d 个邮件模板：基线内联 style %d 处", len(files), baselineHits)
}

// TestMailGuardInlineStyleCheckIsNotVacuous 用坏样本钉住 style 判据本身。
func TestMailGuardInlineStyleCheckIsNotVacuous(t *testing.T) {
	bad := `<td style="color:red">`
	if got := mailGuardInlineStyleProblems(bad); len(got) != 1 {
		t.Fatalf("坏样本应报 1 个问题，实际 %d 个：%v", len(got), got)
	}
	good := `<td style="word-break:break-all">` + "\n" + `<td class="cell-wrap">`
	if got := mailGuardInlineStyleProblems(good); len(got) != 0 {
		t.Fatalf("基线写法与类名写法都不该报问题：%v", got)
	}
	commented := `{* 例：<td style="color:red"> *}`
	if got := mailGuardInlineStyleProblems(commented); len(got) != 0 {
		t.Fatalf("注释里的示例不该被算作内联样式：%v", got)
	}
	single := `<td style='color:red'>`
	if got := mailGuardInlineStyleProblems(single); len(got) != 1 {
		t.Fatalf("单引号写法同样要抓到，实际 %d 个：%v", len(got), got)
	}
}
