package architecture

// write_binding_keep_empty_test.go — 「空串有语义」的字段必须在模板里声明 keep-empty。
//
// 背景：htmx 的写表单会提交**全部**具名控件，空输入框发的是空串。而有两类字段对空串的
// 解读正好相反：
//
//	① 普通可选字段：空串 = 没填。交给后端会被 *int 绑成 0（「没填」变成「改成 0」），
//	   所以要删掉 —— ui/htmx_form_body.js 的 dropEmpty 默认就是这么做的。
//	② **空串本身有语义**的字段：后端 DTO 的注释写着「nil 表示本次不改，指向空串表示
//	   解绑 / 提升为顶级」。这类字段的空串**不能删**：删了就把「解绑」变成「不改」，
//	   用户点了没反应，还以为保存成功。
//
// ②类的模板必须声明 data-write-keep-empty="字段名"，否则上面的静默失效就会发生。
// 这份门禁扫 DTO 里标注了「空串表示 / 指向空串」的字段，要求相模板里能查到声明。
//
// 局限（写清楚，免得被当成全覆盖）：模板里的 name 与 DTO 字段名靠**小驼峰**对上，
// 改名后本测试会漏判。它挡的是「新增了这类字段却忘了声明」，不是「改名同步」。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// emptyMeansSemantic 在 DTO 注释里标记「空串有语义」的措辞。
//
// 这三种写法在仓库里都出现过，收敛成一份判据：
//
//	nil 表示本次不改，指向空串表示解绑
//	nil 表示本次不改；指向空串表示**提升为顶级**
//	空串表示清除面板
var emptyMeansSemantic = regexp.MustCompile(`(指向空串|空串表示)`)

// pendingKeepEmptyFields 还没在模板里声明 keep-empty 的字段（登记存量，收一个删一个）。
// navigation 的 panelBlockId **确实会被 dropEmpty 碰到**（那个表单用 hx-post，
// ui/htmx_form_body.js 全局加载、WRITE_VERBS 含 post）—— 但它不需要 keep-empty：
//
//	navigation_page_handle.go 用 `c.PostForm("panelBlockId")` 取值，而 PostForm 在
//	**键不存在时返回空串**，与「键存在但值为空」完全同义。服务端拿到空串后构造
//	`&blockID`（恒非 nil 指针）→ 空串即「清除面板」。
//
// 所以 dropEmpty 删不删这个键，对结果没有任何影响 —— 与商品的 parentId /
// primaryCategoryId 不同：那两个走 JSON 端点的 binding 绑定，nil 与空串语义相反
//（nil = 不改、空串 = 解绑），删键就会把「解绑」变成「不改」。
//
// 判据因此是「空串被删掉之后，消费方还能不能区分『没填』与『显式置空』」：
//   · PostForm 取值：不能区分也不需要区分（键不存在 = 空串）；
//   · binding 绑定到 *string：能区分，而语义相反 —— 必须 keep-empty。
// 键是 **form tag 的值**（模板 name 与它逐字对齐），不是字段名推出来的小驼峰 ——
// 两者在 `PanelBlockID` 上正好不同：form tag 是 `panelBlockId`（小写 d），
// 而按字段名推会得到 `panelBlockID`。2026-11 给全部 dto 补 form tag 之后，
// 本表的键随之从 `panelBlockID` 变成 `panelBlockId`（此前那个键其实从未被扫到过，
// 属于登记表与实测不一致的存量 bug，补 tag 顺手修掉了）。
var pendingKeepEmptyFields = map[string]int{
	"panelBlockId": 1,
}

// TestKeepEmptyFieldsAreDeclared 扫「空串有语义」的 DTO 字段，要求模板里有对应声明。
func TestKeepEmptyFieldsAreDeclared(t *testing.T) {
	root := repoRoot(t)

	// 第一步：从 DTO 注释里收集这类字段名（小驼峰形式，与模板 name 对齐）。
	fields := map[string][]string{} // 字段名 -> 出处
	fset := token.NewFileSet()
	for _, path := range goFilesUnder(t, filepath.Join(root, "internal", "module"), func(string) bool { return true }) {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			st, ok := n.(*ast.StructType)
			if !ok {
				return true
			}
			for _, field := range st.Fields.List {
				if field.Doc == nil || len(field.Names) == 0 {
					continue
				}
				doc := field.Doc.Text()
				if !emptyMeansSemantic.MatchString(doc) {
					continue
				}
				// 只收指针字段：值类型字段没有「nil = 不改」这一档，空串就是零值。
				if !isPointerType(field.Type) {
					continue
				}
				// 用 **form tag** 作为模板里的 name —— 那才是端点读的键。
				// 从字段名推小驼峰会错：PrimaryCategoryID 推成 primaryCategoryID，
				// 而 DTO 上的 form tag 与模板的 name 都是 primaryCategoryId。
				name := formTagOf(field)
				if name == "" {
					name = lowerFirst(field.Names[0].Name)
				}
				fields[name] = append(fields[name], relDir(root, path)+":"+field.Names[0].Name)
			}
			return true
		})
	}

	// 第二步：在模板里找 data-write-keep-empty 声明。
	tplText := readAllTemplates(t, filepath.Join(root, "internal", "templates"))
	declared := map[string]bool{}
	for _, m := range regexp.MustCompile(`data-write-keep-empty="([^"]*)"`).FindAllStringSubmatch(tplText, -1) {
		for _, name := range strings.Split(m[1], ",") {
			if n := strings.TrimSpace(name); n != "" {
				declared[n] = true
			}
		}
	}

	hits := map[string]int{}
	for name, sites := range fields {
		if declared[name] {
			continue
		}
		hits[name] = 1
		t.Logf("字段 %q 的注释标注了「空串有语义」，但模板里没有 data-write-keep-empty 声明（出处 %v）",
			name, sites)
	}

	assertBudget(t, hits, pendingKeepEmptyFields, "「空串有语义」的字段必须在模板里声明 keep-empty")
}

// formTagOf 读结构体字段的 form tag（模板 name 与它逐字对齐）。
func formTagOf(field *ast.Field) string {
	if field.Tag == nil {
		return ""
	}
	tag := reflect.StructTag(strings.Trim(field.Tag.Value, "`"))
	return strings.Split(tag.Get("form"), ",")[0]
}

func isPointerType(expr ast.Expr) bool {
	_, ok := expr.(*ast.StarExpr)
	return ok
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// readAllTemplates 把 internal/templates 下所有 .html 拼成一串（只需字符串匹配）。
func readAllTemplates(t *testing.T, dir string) string {
	t.Helper()
	var sb strings.Builder
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".html") {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		sb.Write(b)
		sb.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatalf("读取模板失败: %v", err)
	}
	return sb.String()
}
