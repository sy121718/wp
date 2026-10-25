// Command write-contracts 生成写端点的字段契约。
//
// 详见同目录 README 与 .thinkrail/context/TASK-frontend-batch-2-write-base.md。
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	json "go_wp/pkg/jsonx"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const outPath = "internal/templates/static/js/ui/write-contracts.json"

func main() {
	check := flag.Bool("check", false, "仅检查生成文件是否与源码一致")
	flag.Parse()
	if err := run(*check); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// fieldSchema 一个可提交字段。
//
// 只有两件事对前端有意义：字段名（模板 name 要对上）与是否必填（缺了必然失败）。
// 不记 Go 类型 —— 实测 formdata 的 "3" 直接绑进 int、"a&a=b" 直接绑成 []string，
// 类型转换由服务端反射完成；记类型只会让人以为前端该做转换。
type fieldSchema struct {
	Name     string `json:"name"`
	Required bool   `json:"required,omitempty"`
}

// endpointSchema 一个写端点的契约。
type endpointSchema struct {
	// DTO 绑定的 Go 类型全名（如 productdto.CreateBrandReq）。
	DTO string `json:"dto"`
	// Handler handler 方法名（排查时能直接跳过去）。
	Handler string `json:"handler"`
	// Tables 这个端点的嵌套容器该怎么组装（对应 data-write-rows/-object/-builder）。
	// 空表示纯扁平表单。
	Tables []tableSchema `json:"tables,omitempty"`
	Fields []fieldSchema `json:"fields"`
}

// tableSchema 扁平 formdata 表达不了的字段（嵌套 struct / struct 切片 / map）。
//
// 实测：`inner.days=30` 绑不出 `Inner`（得到 nil），`Raw` 也是空 map —— 这类字段
// **必须由前端组装**，因此模板里的 `data-write-rows` / `-object` / `-builder` 删不掉。
// 这里记下它们的名字，门禁据此判断「这个表单项允许用整形属性」。
type tableSchema struct {
	Name string `json:"name"`
	// Shape 说明为什么需要组装：slice-struct / ptr-struct / map。
	Shape string `json:"shape"`
}

func run(check bool) error {
	routes, err := runtimeWriteRoutes()
	if err != nil {
		return err
	}
	handlerDTO, err := scanHandlerBindings()
	if err != nil {
		return err
	}
	dtoInfo, err := scanDTOs()
	if err != nil {
		return err
	}

	out := map[string]endpointSchema{}
	var unmatched []string
	for _, r := range routes {
		mod := moduleOfRoutePath(r.Path)
		h := matchHandler(r.Path, mod, handlerDTO)
		if h == "" {
			unmatched = append(unmatched, r.Method+" "+r.Path)
			continue
		}
		dtoName := handlerDTO[h]
		info := dtoInfo[dtoName]
		handlerName := h
		if i := strings.Index(h, ":"); i >= 0 {
			handlerName = h[i+1:]
		}
		ep := endpointSchema{DTO: dtoName, Handler: handlerName, Fields: info.Fields}
		if ep.Fields == nil {
			ep.Fields = []fieldSchema{}
		}
		ep.Tables = info.Tables
		out[r.Path] = ep
	}

	data, err := marshalStable(out)
	if err != nil {
		return err
	}
	if check {
		current, err := os.ReadFile(outPath)
		if err != nil {
			return fmt.Errorf("读不到生成文件（先跑 go run ./cmd/write-contracts）：%w", err)
		}
		if !bytes.Equal(current, data) {
			return fmt.Errorf("写端点契约已过期，请执行：go run ./cmd/write-contracts")
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	if len(unmatched) > 0 {
		// 未匹配不阻断生成，但必须说出来：门禁按契约对账，漏掉的端点等于没人守。
		fmt.Fprintf(os.Stderr, "警告：%d 条写端点没匹配到 handler（这些端点没有契约）:\n", len(unmatched))
		for _, u := range unmatched {
			fmt.Fprintf(os.Stderr, "  %s\n", u)
		}
	}
	return os.WriteFile(outPath, data, 0o644)
}

type route struct {
	Method string
	Path   string
	Perm   string
}

// runtimeWriteRoutes 跑一次运行时路由导出，取全部写方法的 API 路由。
//
// 为什么用运行时而不是静态扫描：路由注册是 `SetupXxxRoutes(permission.NewRouteGroup(api), db)`
// 这种跨包传递，组前缀要跨文件追踪才拼得对，手拼一次的代价是「拼错了没人发现」。
// WP_DUMP_ROUTES 那条测试是仓库既有的门禁入口，行为稳定。
func runtimeWriteRoutes() ([]route, error) {
	cmd := exec.Command("go", "test", "./internal/routers/", "-run", "TestDumpRoutesForAudit", "-count=1", "-v")
	cmd.Env = append(os.Environ(), "WP_DUMP_ROUTES=1")
	raw, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("导出运行时路由表失败（需要数据库；先在仓库根目录配好 config.yaml）：%w", err)
	}
	var out []route
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "DECLARED ") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		method, path, perm := f[1], f[2], f[3]
		switch method {
		case "POST", "PUT", "PATCH", "DELETE":
		default:
			continue
		}
		if !strings.HasPrefix(path, "/api/") {
			continue
		}
		out = append(out, route{Method: method, Path: path, Perm: perm})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("运行时路由表里没有写端点 —— 装配失败或 WP_DUMP_ROUTES 输出格式变了")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// matchHandler 把路径对上 handler 方法名。
//
// 路径段先剔除模块名（/api/<模块>/...），剩下的资源段与动词拼成候选 handler 名：
//   - 末段是动词（create/update/delete/...）时，试「动词+全部资源」与「动词+末段资源」；
//   - 否则退化为「全部资源词都出现在 handler 名里且唯一」。
//
// 多条命中就放弃 —— 配错会让门禁按错的 DTO 对账，比不配更糟。
func matchHandler(path, mod string, handlerDTO map[string]string) string {
	segs := strings.Split(strings.Trim(path, "/"), "/")
	if len(segs) < 3 {
		return ""
	}
	rest := segs[2:]

	// 候选键**只认同模块**的 "mod:Handler"。
	//
	// 不回退到裸 handler 名：跨模块同名会把契约配错，而配错比漏配更糟 ——
	// 门禁会拿错的 DTO 去对账（实测：/api/i18n/save 匹到 gorm.DB、
	// /api/product/detail-template/create 匹到 productdto.CreateReq）。
	// 漏配的端点没有契约，check-write-fields.sh 会明确报「端点不在契约里」。
	idsOf := func(name string) []string {
		if mod == "" {
			return nil   // 拿不到模块名 → 不做猜测（宁可漏配，让门禁报出来）
		}
		return []string{mod + ":" + name}
	}
	verbs := map[string]string{
		"create": "Create", "update": "Update", "delete": "Delete",
		"save": "Save", "add": "Add", "remove": "Remove",
		"bulk-delete": "BulkDelete", "bulk-update": "BulkUpdate", "bulk-status": "BulkStatus",
		"toggle": "Toggle", "apply": "Apply", "restore": "Restore",
		"import": "Import", "upload": "Upload", "sort": "Sort",
		"activate": "Activate", "deactivate": "Deactivate", "approve": "Approve",
		"reject": "Reject", "receive": "Receive", "cancel": "Cancel",
		"publish": "Publish", "unpublish": "Unpublish", "review": "Review",
		"unlock": "Unlock", "lock": "Lock", "assign": "Assign",
		"set": "Set", "status": "Status", "validate": "Validate",
		"refresh": "Refresh", "sync": "Sync", "fetch": "Fetch",
		"send": "Send", "test": "Test", "reset": "Reset", "rebuild": "Rebuild",
		"preview": "Preview", "generate": "Generate", "apply-pricing": "ApplyPricing",
		"layout": "Layout", "note": "Note", "refund": "Refund",
	}
	var words []string
	verb := ""
	for _, s := range rest {
		if v, ok := verbs[s]; ok {
			verb = v
		} else {
			words = append(words, s)
		}
	}

	var camel []string
	for _, w := range words {
		camel = append(camel, camelWord(w))
	}

	if verb != "" {
		var cands []string
		if len(camel) > 0 {
			cands = append(cands, verb+strings.Join(camel, ""))
			cands = append(cands, verb+camel[len(camel)-1])
		}
		// 只有路径**没有任何资源段**时才试裸动词（`/api/x/publish` → Publish）。
		// 有资源段时不能回退到裸动词：`/api/product/detail-template/create` 会因此
		// 匹到商品模块的 Create（productdto.CreateReq），而它其实该匹
		// CreateDetailTemplate —— 实测就是这么错配的。
		if len(camel) == 0 {
			cands = append(cands, verb)
		}
		for _, c := range cands {
			for _, id := range idsOf(c) {
				if _, ok := handlerDTO[id]; ok {
					return id
				}
			}
		}
	}

	// 退化：全部资源词都出现在 handler 名里，且唯一。
	var hits []string
	for name := range handlerDTO {
		// 只在本模块的候选里退化匹配（跨模块同名会让结果随机）。
		if mod != "" && !strings.HasPrefix(name, mod+":") {
			continue
		}
		all := len(camel) > 0
		for _, c := range camel {
			if !strings.Contains(name, c) {
				all = false
				break
			}
		}
		if all {
			hits = append(hits, name)
		}
	}
	if len(hits) == 1 {
		return hits[0]
	}
	return ""
}

// moduleOfRoutePath 从 /api/<mod>/... 取出模块名（与 handler 声明的模块目录同名）。
func moduleOfRoutePath(path string) string {
	segs := strings.Split(strings.Trim(path, "/"), "/")
	if len(segs) < 2 {
		return ""
	}
	return segs[1]
}

// camelWord 单个路径段转驼峰首字母大写（复数去尾 s）。
func camelWord(w string) string {
	w = strings.ReplaceAll(w, "-", " ")
	w = strings.ReplaceAll(w, "_", " ")
	var parts []string
	for _, p := range strings.Fields(w) {
		if p == "" {
			continue
		}
		parts = append(parts, strings.ToUpper(p[:1])+p[1:])
	}
	s := strings.Join(parts, "")
	if len(s) > 2 && strings.HasSuffix(s, "s") &&
		!strings.HasSuffix(s, "ss") && !strings.HasSuffix(s, "us") && !strings.HasSuffix(s, "is") {
		s = s[:len(s)-1]
	}
	return s
}

func marshalStable(in map[string]endpointSchema) ([]byte, error) {
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf bytes.Buffer
	buf.WriteString("{\n")
	for i, k := range keys {
		inner, err := json.Marshal(in[k])
		if err != nil {
			return nil, err
		}
		keyJSON, _ := json.Marshal(k)
		fmt.Fprintf(&buf, "  %s: %s", keyJSON, inner)
		if i < len(keys)-1 {
			buf.WriteString(",")
		}
		buf.WriteString("\n")
	}
	buf.WriteString("}\n")
	return buf.Bytes(), nil
}

// scanHandlerBindings 扫出「handler 方法名 → DTO 全名」。
//
// 用 go/ast 而不是正则：要确定「这个方法绑定的是哪个 DTO」，正则分不清函数体里的
// 第一处 binding.JSON 与嵌套函数里的。
func scanHandlerBindings() (map[string]string, error) {
	out := map[string]string{}
	fset := token.NewFileSet()
	err := filepath.WalkDir("internal", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return nil
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || fn.Recv == nil {
				continue
			}
			// 同名 handler 在多个模块里都有（每个模块都有自己的 Create/Update/Delete）。
			// 键用 "模块:Handler"，避免 /api/product/create 被匹到 blockdto.CreateReq
			//（实测踩过：契约里 /api/product/create 的 dto 是 blockdto.CreateReq）。
			name := fn.Name.Name
			mod := moduleOfPath(path)
			key := name
			if mod != "" {
				key = mod + ":" + name
			}
			// 带模块前缀的键**先登记**（权威），裸名只在尚未占用时登记（兜底）。
			if _, seen := out[key]; seen {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				idx, ok := call.Fun.(*ast.IndexExpr)
				if !ok {
					return true
				}
				sel, ok := idx.X.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkgID, ok := sel.X.(*ast.Ident)
				if !ok || pkgID.Name != "binding" {
					return true
				}
				if sel.Sel.Name != "JSON" && sel.Sel.Name != "Form" {
					return true
				}
				dt, ok := idx.Index.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkgName, ok := dt.X.(*ast.Ident)
				if !ok {
					return true
				}
				// 只认 dto 包（*dto.XxxReq）。不设这条会把辅助函数的类型形参
				// （`binding.JSON[gorm.DB]` 之类）当成 DTO —— 实测过。
				if !strings.HasSuffix(pkgName.Name, "dto") {
					return true
				}
				if mod != "" {
					out[key] = pkgName.Name + "." + dt.Sel.Name
					if _, taken := out[name]; !taken {
						out[name] = out[key]
					}
				} else {
					out[name] = pkgName.Name + "." + dt.Sel.Name
				}
				return false
			})
			// 兜底：handler 可能把 binding 包在**泛型辅助函数**里
			//（`detailTemplateWriteJSON(c, func(req *productdto.DetailTemplateCreateReq, …)`），
			// 此时方法体内没有 binding.JSON，类型实参在调用处。
			found := func() bool { _, ok := out[key]; return ok }
			if !found() {
				if dto := dtoFromGenericHelperCall(fn.Body); dto != "" {
					out[key] = dto
					if _, taken := out[name]; !taken {
						out[name] = dto
					}
				}
			}
		}
		return nil
	})
	return out, err
}

// dtoFromGenericHelperCall 在 handler 体里找「调用泛型写辅助，其函数字面量参数
// 的第一个形参是 *pkg.Type」的形态，返回 pkg.Type。
//
// 为什么需要：product 的 detail-template 系列把 binding 包在
// `detailTemplateWriteJSON[T any](c, run func(req *T, …))` 里，方法体只传一个
// 函数字面量 —— 只看方法体扫不到 binding.JSON。
func dtoFromGenericHelperCall(body *ast.BlockStmt) string {
	var found string
	ast.Inspect(body, func(n ast.Node) bool {
		if found != "" {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		for _, arg := range call.Args {
			lit, ok := arg.(*ast.FuncLit)
			if !ok || lit.Type.Params == nil || len(lit.Type.Params.List) == 0 {
				continue
			}
			expr := lit.Type.Params.List[0].Type
			if star, ok := expr.(*ast.StarExpr); ok {
				expr = star.X
			}
			if sel, ok := expr.(*ast.SelectorExpr); ok {
				if pkg, ok := sel.X.(*ast.Ident); ok && strings.HasSuffix(pkg.Name, "dto") {
					found = pkg.Name + "." + sel.Sel.Name
					return false
				}
			}
		}
		return true
	})
	return found
}

// moduleOfPath 从 internal/module/<mod>/... 取出 <mod>；不在模块目录下返回空串。
func moduleOfPath(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i, p := range parts {
		if p == "module" && i+2 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// dtoInfo 一个 DTO 的字段表与需组装的表字段。
type dtoInfo struct {
	Fields []fieldSchema
	Tables []tableSchema
}

// 扁平标量的 Go 类型名（含指针）。其余类型都算「formdata 表达不了」。
var scalarTypes = map[string]bool{
	"string": true, "int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"bool": true, "float32": true, "float64": true, "byte": true, "rune": true,
	"RawMessage": true, "JSONTime": true,
}

// scanDTOs 扫出「DTO 全名（pkg.Type）→ 字段表 + 表字段」。
func scanDTOs() (map[string]dtoInfo, error) {
	out := map[string]dtoInfo{}
	fset := token.NewFileSet()
	err := filepath.WalkDir("internal", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil || f.Name == nil {
			return nil
		}
		pkgName := f.Name.Name
		ast.Inspect(f, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok || st.Fields == nil {
				return true
			}
			var fields []fieldSchema
			var tables []tableSchema
			for _, fld := range st.Fields.List {
				if fld.Tag == nil || len(fld.Names) == 0 {
					continue
				}
				tag := parseStructTag(fld.Tag.Value)
				form := tag["form"]
				jsonTag := strings.Split(tag["json"], ",")[0]
				name := strings.Split(form, ",")[0]
				if name == "" || name == "-" {
					// 没有 form tag 时退到 json tag：DTO 可能还没补 form（本批会补），
					// 但契约要能提前算出前端该写什么 name。
					name = jsonTag
				}
				if name == "" || name == "-" {
					continue
				}
				shape := collectionShape(fld.Type)
				if shape != "" {
					tables = append(tables, tableSchema{Name: name, Shape: shape})
				}
				fields = append(fields, fieldSchema{
					Name:     name,
					Required: strings.Contains(tag["binding"], "required"),
				})
			}
			if len(fields) > 0 {
				out[pkgName+"."+ts.Name.Name] = dtoInfo{Fields: fields, Tables: tables}
			}
			return true
		})
		return nil
	})
	return out, err
}

// collectionShape 判断一个字段形状是否 formdata 表达不了。
//
// 实测依据：`inner.days=30` 绑不出 `*Inner`（得 nil）、`map` 字段完全绑不出。
// 返回 "" 表示扁平标量（formdata 直接能绑）。
func collectionShape(expr ast.Expr) string {
	// *T 剥一层，记下是不是指针。
	ptr := false
	switch t := expr.(type) {
	case *ast.StarExpr:
		ptr = true
		expr = t.X
	}
	switch t := expr.(type) {
	case *ast.ArrayType:
		// []scalar 可以（a&a=b），[]struct 不行。
		if isScalarExpr(t.Elt) {
			return ""
		}
		return "slice-struct"
	case *ast.MapType:
		return "map"
	case *ast.Ident:
		if scalarTypes[t.Name] {
			if ptr {
				return "" // *int 等：空值语义靠「不发这个键」，仍是扁平
			}
			return ""
		}
		// 自定义类型（同包内的 struct 或别名）：保守当作结构体。
		if ptr {
			return "ptr-struct"
		}
		return "struct"
	}
	return ""
}

func isScalarExpr(e ast.Expr) bool {
	if star, ok := e.(*ast.StarExpr); ok {
		e = star.X
	}
	if id, ok := e.(*ast.Ident); ok {
		return scalarTypes[id.Name]
	}
	return false
}

// parseStructTag 解析 struct tag 字面量（含反引号或双引号包裹）。
func parseStructTag(lit string) map[string]string {
	out := map[string]string{}
	lit = strings.Trim(lit, "`\"")
	for lit != "" {
		lit = strings.TrimLeft(lit, " ")
		i := 0
		for i < len(lit) && lit[i] > ' ' && lit[i] != ':' {
			i++
		}
		if i == 0 || i+1 >= len(lit) || lit[i] != ':' || lit[i+1] != '"' {
			break
		}
		key := lit[:i]
		lit = lit[i+2:]
		j := 0
		for j < len(lit) && lit[j] != '"' {
			if lit[j] == '\\' {
				j++
			}
			j++
		}
		if j >= len(lit) {
			break
		}
		out[key] = lit[:j]
		lit = lit[j+1:]
	}
	return out
}
