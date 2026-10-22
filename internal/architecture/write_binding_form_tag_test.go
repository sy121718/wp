package architecture

// write_binding_form_tag_test.go — 写端点 DTO 的 form tag 覆盖（htmx 表单的前提）。
//
// 背景：写端点现在有**两个**客户端 —— MCP/AI/fetch 发 JSON，htmx 的写表单发 formdata。
// pkg/binding.JSON 用 gin 的 ShouldBind 按 Content-Type 自适应，两边都能进同一个端点。
//
// 但 formdata 能否绑上，取决于 DTO 的 `form` tag：
//   - 有 `form:"tagIds"` → 能收到同名多值（tagIds=a&tagIds=b → [a b]）、指针、内嵌 struct；
//   - **没有 form tag** → gin 只按**字段名**匹配（大小写不敏感），`ProjectID` 能收到
//     `ProjectID`/`projectID`，但收不到 `projectId` —— 而 JSON tag 通常就是小驼峰。
//
// 后者的失效是**静默**的：页面点了保存，端点收到零值，写入成功但什么都没改；
// 或者 `binding:"required"` 报 400，而用户看到的只是「保存失败」。
//
// 这份门禁扫的是「哪些 DTO 缺 form tag」，用清单方式登记存量：
// 补一批就删一批，新增的写 DTO 必须带 form tag，否则直接红。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// pendingFormTagDTOs 还没补 form tag 的写 DTO（键 = 包路径别名走的类型名）。
//
// 收一个删一个：某个类型全部字段都带 form tag 后，这条登记要一起删，
// 否则本测试的反向判定会报「登记了却已无缺口」。
var pendingFormTagDTOs = map[string]int{
	"internal/module/ai/inbound/http:sessionAppendBody":     1,
	"internal/module/ai/inbound/http:sessionFoldBody":       1,
	"internal/module/ai/inbound/http:sessionFoldPlanBody":   1,
	"internal/module/ai/inbound/http:sessionRenameBody":     1,
	"internal/module/ai/inbound/http:tokenCreateBody":       1,
	"internal/module/ai/inbound/http:tokenRevokeBody":       1,
	"internal/module/build/inbound/http:retryReq":           1,
	"internal/module/product/inbound/http:productPricingApplyReq": 1,
}

// TestWriteDTOsHaveFormTag 扫描全部 binding.JSON[...] 的 DTO，要求字段的 json tag 与 form tag 对齐。
//
// 判据是**双向**的：
//   - 有 json tag 却没有 form tag → 失败（htmx 表单绑不上）；
//   - 登记表里列了、实际已无缺口 → 失败（提醒删登记）。
func TestWriteDTOsHaveFormTag(t *testing.T) {
	root := repoRoot(t)
	roots := []string{
		filepath.Join(root, "internal", "module"),
		filepath.Join(root, "internal", "adminui"),
	}

	type reqType struct {
		pkg  string // 相对仓库的目录
		name string // 类型名
	}
	used := map[reqType]bool{}

	// 第一步：找出所有 binding.JSON[T] 里的 T（只认同包内的类型名，跨包带别名的记原名）。
	fset := token.NewFileSet()
	for _, dir := range roots {
		files := goFilesUnder(t, dir, func(string) bool { return true })
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				continue
			}
			ast.Inspect(f, func(n ast.Node) bool {
				idx, ok := n.(*ast.IndexExpr)
				if !ok {
					return true
				}
				sel, ok := idx.X.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "JSON" {
					return true
				}
				pkgIdent, ok := sel.X.(*ast.Ident)
				if !ok || pkgIdent.Name != "binding" {
					return true
				}
				name := typeNameOf(idx.Index)
				if name == "" {
					return true
				}
				used[reqType{pkg: relDir(root, path), name: name}] = true
				return true
			})
		}
	}

	// 第二步：在这些 DTO 的定义文件里找结构体，检查 json/form tag 对齐。
	hits := map[string]int{}
	for _, dir := range roots {
		files := goFilesUnder(t, dir, func(string) bool { return true })
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			rel := relDir(root, path)
			f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if err != nil {
				continue
			}
			ast.Inspect(f, func(n ast.Node) bool {
				ts, ok := n.(*ast.TypeSpec)
				if !ok {
					return true
				}
				if !used[reqType{pkg: rel, name: ts.Name.Name}] {
					return true
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					return true
				}
				if !structLacksFormTag(st) {
					return true
				}
				hits[rel+":"+ts.Name.Name] = 1
				return true
			})
		}
	}

	assertBudget(t, hits, pendingFormTagDTOs, "写 DTO 的字段都带 form tag（htmx 表单靠它绑定）")
}

// structLacksFormTag 结构体里是否有「带 json tag 却没有 form tag」的导出字段。
//
// 三个豁免：
//   - json.RawMessage：编码层绑定不了表单字符串，由 DTO 的 BindForm 自己接管
//     （product 的 CreateTagReq 就是这样处理的）。
//   - 内嵌结构体（匿名字段）：其字段在父结构体上不可见，由内嵌类型自己负责。
//   - json:"-"：本就不参与序列化。
func structLacksFormTag(st *ast.StructType) bool {
	for _, field := range st.Fields.List {
		if len(field.Names) == 0 { // 内嵌
			continue
		}
		if !field.Names[0].IsExported() {
			continue
		}
		if field.Tag == nil {
			continue
		}
		tag := reflect.StructTag(strings.Trim(field.Tag.Value, "`"))
		jsonTag := tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			continue
		}
		if tag.Get("form") != "" {
			continue
		}
		if isRawMessageField(field.Type) {
			continue
		}
		return true
	}
	return false
}

func isRawMessageField(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "RawMessage"
}

// typeNameOf 取泛型实参里的类型名（`dto.CreateReq` → `CreateReq`，`CreateReq` → `CreateReq`）。
func typeNameOf(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return e.Sel.Name
	case *ast.StarExpr:
		return typeNameOf(e.X)
	}
	return ""
}

func relDir(root, path string) string {
	dir := filepath.Dir(path)
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return dir
	}
	return filepath.ToSlash(rel)
}
