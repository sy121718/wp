package architecture

// write_binding_form_tag_test.go — 写端点 DTO 的 form tag 覆盖（htmx 表单的前提）。
//
// 背景：写端点现在有**两个**客户端 —— MCP/AI/fetch 发 JSON，htmx 的写表单发 formdata。
// pkg/binding.JSON 用 gin 的 ShouldBind 按 Content-Type 自适应，两边都能进同一个端点。
//
// 但 formdata 能否绑上，取决于 DTO 的 `form` tag：
//   - 有 `form:"tagIds"` → 能收到同名多值（tagIds=a&tagIds=b → [a b]）、指针、内嵌 struct；
//   - **没有 form tag** → gin 的 form 映射只按 **Go 字段名精确匹配**（大小写敏感），
//     `ProjectID` 只认 `ProjectID`，收不到 `projectId`。
//
// 这一条此前写反了（写成「大小写不敏感，projectID 也认」），而它正是判断「能不能不补 tag」
// 的依据 —— 实测（独立探针，gin 同版本）：
//
//	无 form tag + camelCase   → err=失败，字段全零
//	无 form tag + Go 字段名   → err=nil，字段都对
//	有 form tag + camelCase   → err=nil，字段都对
//
// 而 JSON tag 通常就是小驼峰 —— 所以「有 json tag 无 form tag」的 DTO 在 htmx 表单下
// 要么 400（有 required），要么静默收零值（无 required，更危险）。
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
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// pendingFormTagDTOs 还没补 form tag 的写 DTO（键 = 包路径别名走的类型名）。
//
// **2026-11 已清零**：全部 43 个 dto 文件、581 个字段都补上了 form tag
// （`go run ./cmd/formtags`，幂等工具）。此前这里有 99 条按类型登记的存量，
// 现在一条不剩 —— 保留空 map 是因为判据是**双向**的：新增的写 DTO 缺 form tag
// 会被扫出来并要求登记，而登记了却已补完也会报错。
//
// 为什么必须补完而不是继续登记：写端点有两个客户端（MCP/AI 发 JSON、htmx 表单发 formdata），
// 而没有 form tag 时 gin 的 form 映射只按 **Go 字段名精确匹配**（大小写敏感）——
// `ProjectID` 收不到 `projectId`。失效是静默的：无 required 的字段静默收零值
// （保存成功但什么都没改），有 required 的报 400 而用户只看到「保存失败」。
var pendingFormTagDTOs = map[string]int{}

// intentionallyJSONOnlyDTOs **只服务 JSON 客户端**的写请求体 —— 不需要 form tag。
//
// 判据不是「懒得补」，而是：**模板里有没有 hx-post 指向它的端点**。这三类都没有：
//
//	· ai 的 sessionAppend / sessionFold / sessionRename / tokenCreate / tokenRevoke
//	  —— 由工作台的前端 JS 与 MCP 工具用 JSON 调用，没有后台表单；
//	· build 的 retryReq —— 构建重试由运维脚本与 MCP 触发；
//	· product 的 productPricingApplyReq —— 调价预览的试算是**原生表单提交**（要整页重渲
//	  出试算表），不是 htmx 写表单；它的 apply 按钮走的是另一个请求体。
//
// 这些类型若将来被模板的 hx-post 用到，必须先补 form tag 并**从这里删掉**，
// 否则那个表单会静默绑不上（gin 只按字段名匹配，projectId 收不到）。
//
// 与 pendingFormTagDTOs 的区别：那个是「该补但还没补」的欠账，这个是「明确不需要补」
// 的清单。两者都要求双向判定 —— 登记了却真有缺口，测试同样会红。
var intentionallyJSONOnlyDTOs = map[string]bool{
	"internal/module/ai/inbound/http:sessionAppendBody":           true,
	"internal/module/ai/inbound/http:sessionFoldBody":             true,
	"internal/module/ai/inbound/http:sessionFoldPlanBody":         true,
	"internal/module/ai/inbound/http:sessionRenameBody":           true,
	"internal/module/ai/inbound/http:tokenCreateBody":             true,
	"internal/module/ai/inbound/http:tokenRevokeBody":             true,
	"internal/module/build/inbound/http:retryReq":                 true,
	"internal/module/product/inbound/http:productPricingApplyReq": true,
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

	// 收集**类型名**即可，不用包路径。
	//
	// 原先 (pkg, name) 双键是这道门禁失效的根因：使用方 `binding.JSON[productdto.CreateBrandReq]`
	// 出现在 internal/module/product/inbound/http，声明方在 internal/module/product/dto ——
	// 两个 relDir 永远不相等，于是所有 DTO 都被判为「没被使用」而跳过检查，
	// 门禁 exit 0 与真实失败并存（审查指出的 P0）。
	//
	// 同名的跨模块类型（例如两个模块都有 Req）会互相覆盖，那是**偏保守**的误判方向：
	// 检查得更多，不是更少。真要精确可加 import 解析，但收益不抵复杂度。
	used := map[string]bool{}

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
				used[name] = true
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
				if !used[ts.Name.Name] {
					return true
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					return true
				}
				if !structLacksFormTag(st, implementsBindForm(fset, used, ts.Name.Name, roots, root)) {
					return true
				}
				key := rel + ":" + ts.Name.Name
				if intentionallyJSONOnlyDTOs[key] {
					return true // 明确只服务 JSON 客户端，见该表的注释
				}
				hits[key] = 1
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
func structLacksFormTag(st *ast.StructType, hasBindForm bool) bool {
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
		// json.RawMessage 豁免的**前提是这个 DTO 自己实现了 BindForm**：
		// 编码层绑不了表单字符串，只能由 BindForm 解析。
		//
		// 原先是无条件豁免 —— 而 UpdateReq 并没有 BindForm，于是 Description /
		// BundleItems / Metadata 三个字段一直静默丢失（审查 P1：富文本改完保存后没变）。
		// 现在改成：有 BindForm 才豁免，没有就照常报缺口。
		if isRawMessageField(field.Type) && hasBindForm {
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

// implementsBindForm 该类型是否实现了 `func (x *X) BindForm(url.Values) error`。
//
// 它决定 json.RawMessage 字段能不能豁免 form tag：编码层绑不了表单字符串，
// 但这种字段由 BindForm 自己解析 —— 没有 BindForm 的 DTO 就得报缺口。
//
// 扫全部 root 下的非测试文件（类型与方法是分开声明的，可能不在同一个文件）。
func implementsBindForm(_ *token.FileSet, _ map[string]bool, typeName string, roots []string, root string) bool {
	dirs := make([]string, 0, len(roots))
	dirs = append(dirs, roots...)
	for _, dir := range dirs {
		files := make([]string, 0, 64)
		_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			files = append(files, path)
			return nil
		})
		for _, path := range files {
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				continue
			}
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv == nil || fn.Name.Name != "BindForm" {
					continue
				}
				if len(fn.Recv.List) == 0 {
					continue
				}
				if recvTypeName(fn.Recv.List[0].Type) == typeName {
					return true
				}
			}
		}
	}
	return false
}

// recvTypeName 取接收者类型名（*UpdateReq → UpdateReq，UpdateReq → UpdateReq）。
func recvTypeName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.StarExpr:
		return recvTypeName(e.X)
	case *ast.Ident:
		return e.Name
	case *ast.IndexExpr: // 泛型接收者
		return recvTypeName(e.X)
	}
	return ""
}
