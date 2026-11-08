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
	"reflect"
	"sort"
	"strconv"
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

type fieldSchema struct {
	Name     string `json:"name"`
	Required bool   `json:"required,omitempty"`
}

type endpointSchema struct {
	DTO      string        `json:"dto,omitempty"`
	Handler  string        `json:"handler"`
	Protocol string        `json:"protocol"`
	Reason   string        `json:"reason,omitempty"`
	Fields   []fieldSchema `json:"fields"`
}

type route struct {
	Method  string
	Path    string
	Handler string
}

type bodyBinding struct {
	DTO      string
	Protocol string
	Reason   string
}

type sourceStruct struct {
	Node    *ast.StructType
	Package string
	Imports map[string]string
}

type sourceIndex struct {
	Bindings map[string]bodyBinding
	Types    map[string]sourceStruct

	// funcs 是本次扫描到的全部函数体，用于识别「处理器把请求委托给另一个处理器」：
	// 这种端点的输入形状由被委托方决定，不能只取本函数里的那一个 binder 冒充完整契约。
	funcs map[string]*funcSource
}

type funcSource struct {
	name string
	body *ast.BlockStmt
	pkg  string
	// ctxParams 是本函数里类型为 gin.Context 的参数名，用于识别「把请求委托给另一个处理器」。
	ctxParams map[string]bool
	// selfParams 是接收者标识与其它形参名，委托调用时按位置把它们映射成被委托方的形参。
	recvName   string
	paramNames []string
	imports    map[string]string
	// generics 是本函数声明的类型参数名（如 runProductWrite[T any] 的 T）。
	// 泛型写骨架的请求类型由调用方的实参决定，不能在骨架体里凭空推断。
	generics map[string]bool
	// typeParams 记录形参的源码表达式，供泛型骨架按实参类型替换出真实 DTO。
	typeParams map[string]ast.Expr
}

func run(check bool) error {
	routes, err := runtimeWriteRoutes()
	if err != nil {
		return err
	}
	index, err := scanSources("internal", "go_wp/internal")
	if err != nil {
		return err
	}
	contracts := buildContracts(routes, index)
	for _, r := range routes {
		key := r.Method + " " + r.Path
		if contracts[key].Reason != "" {
			fmt.Fprintf(os.Stderr, "未检查 %s：%s（%s）\n", key, contracts[key].Reason, r.Handler)
		}
	}
	data, err := marshalStable(contracts)
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
	return os.WriteFile(outPath, data, 0o644)
}

func runtimeWriteRoutes() ([]route, error) {
	cmd := exec.Command("go", "test", "./internal/routers/", "-run", "^TestDumpRoutesForAudit$", "-count=1", "-v")
	cmd.Env = append(os.Environ(), "WP_DUMP_ROUTES=1")
	raw, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("导出运行时路由表失败（需要数据库；使用根目录 config.yaml 或显式 GOWP_AUDIT_CONFIG）：%w", err)
	}
	return parseRoutes(string(raw))
}

func parseRoutes(raw string) ([]route, error) {
	var out []route
	seen := map[string]bool{}
	for _, line := range strings.Split(raw, "\n") {
		parts := strings.Fields(line)
		if len(parts) == 0 || parts[0] != "HANDLER" {
			continue
		}
		if len(parts) != 4 {
			return nil, fmt.Errorf("处理器审计行格式错误：%s", line)
		}
		switch parts[1] {
		case "POST", "PUT", "PATCH", "DELETE":
		default:
			continue
		}
		key := parts[1] + " " + parts[2]
		if seen[key] {
			return nil, fmt.Errorf("重复路由：%s", key)
		}
		seen[key] = true
		out = append(out, route{Method: parts[1], Path: parts[2], Handler: parts[3]})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("未导出写端点及真实处理器；不能宣称契约已检查")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Method+" "+out[i].Path < out[j].Method+" "+out[j].Path })
	return out, nil
}

func handlerIdentity(name string) string {
	name = strings.TrimSuffix(name, "-fm")
	name = strings.ReplaceAll(name, "(*", "")
	return strings.ReplaceAll(name, ")", "")
}

func buildContracts(routes []route, index sourceIndex) map[string]endpointSchema {
	out := map[string]endpointSchema{}
	for _, r := range routes {
		identity := handlerIdentity(r.Handler)
		binding, ok := index.Bindings[identity]
		if !ok {
			binding = genericSkeletonBinding(identity, index)
		}
		if binding.Protocol == "" && binding.Reason == "" {
			binding = directGenericBinder(identity, index)
		}
		if binding.Protocol == "" && binding.Reason == "" {
			binding = delegateBinding(identity, index)
		}
		if binding.Protocol == "" && binding.Reason == "" {
			binding = bodyBinding{Protocol: "unknown", Reason: "真实处理器没有唯一显式 body binder，或是包装闭包"}
		}
		ep := endpointSchema{DTO: binding.DTO, Handler: r.Handler, Protocol: binding.Protocol, Reason: binding.Reason, Fields: []fieldSchema{}}
		if binding.Reason == "" {
			fields, err := dtoFields(binding.DTO, binding.Protocol, index.Types, map[string]bool{})
			if err != nil {
				ep.Protocol, ep.Reason = "unknown", err.Error()
			} else {
				ep.Fields = append(ep.Fields, fields...)
			}
		}
		out[r.Method+" "+r.Path] = ep
	}
	return out
}

func scanSources(root, prefix string) (sourceIndex, error) {
	index := sourceIndex{
		Bindings: map[string]bodyBinding{},
		Types:    map[string]sourceStruct{},
		funcs:    map[string]*funcSource{},
	}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return walkErr
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		pkg := prefix
		if relative != "." {
			pkg += "/" + filepath.ToSlash(relative)
		}
		imports := map[string]string{}
		for _, spec := range file.Imports {
			name, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			alias := filepath.Base(name)
			if spec.Name != nil {
				alias = spec.Name.Name
			}
			imports[alias] = name
		}
		for _, decl := range file.Decls {
			switch node := decl.(type) {
			case *ast.GenDecl:
				for _, spec := range node.Specs {
					if typ, ok := spec.(*ast.TypeSpec); ok {
						if st, ok := typ.Type.(*ast.StructType); ok {
							index.Types[pkg+"."+typ.Name.Name] = sourceStruct{Node: st, Package: pkg, Imports: imports}
						}
					}
				}
			case *ast.FuncDecl:
				if node.Body == nil {
					continue
				}
				name := pkg + "." + node.Name.Name
				src := &funcSource{name: name, body: node.Body, pkg: pkg, imports: imports,
					ctxParams: map[string]bool{}, generics: map[string]bool{}}
				if node.Recv != nil && len(node.Recv.List) == 1 {
					name = typeName(node.Recv.List[0].Type, pkg, imports) + "." + node.Name.Name
					if names := node.Recv.List[0].Names; len(names) == 1 {
						src.recvName = names[0].Name
					}
				}
				src.name = name
				src.typeParams = map[string]ast.Expr{}
				if node.Type.TypeParams != nil {
					for _, tp := range node.Type.TypeParams.List {
						for _, tpName := range tp.Names {
							src.generics[tpName.Name] = true
						}
					}
				}
				for _, field := range node.Type.Params.List {
					for _, paramName := range field.Names {
						src.paramNames = append(src.paramNames, paramName.Name)
						src.typeParams[paramName.Name] = field.Type
						if typeName(field.Type, pkg, imports) == "github.com/gin-gonic/gin.Context" {
							src.ctxParams[paramName.Name] = true
						}
					}
				}
				index.funcs[name] = src
				if binding := nativeBinding(node.Body, pkg, imports, src.generics); binding.Protocol != "" || binding.Reason != "" {
					// 泛型函数体的请求类型是类型参数而不是具体 DTO：这里只登记协议，
					// DTO 留空，由调用方的实参补齐；DTO 写死了类型参数的绑定不能当契约用。
					if len(src.generics) > 0 {
						if binding.Protocol == "unknown" {
							binding = bodyBinding{}
						} else {
							binding.DTO = ""
						}
					}
					index.Bindings[name] = binding
				}
			}
		}
		return nil
	})
	return index, err
}

func typeName(expr ast.Expr, pkg string, imports map[string]string) string {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	switch typ := expr.(type) {
	case *ast.Ident:
		return pkg + "." + typ.Name
	case *ast.SelectorExpr:
		if alias, ok := typ.X.(*ast.Ident); ok && imports[alias.Name] != "" {
			return imports[alias.Name] + "." + typ.Sel.Name
		}
	}
	return ""
}

// declaredType 使用解析器的词法对象，不用同名变量表覆盖不同作用域的请求。
func declaredType(expr ast.Expr, seen map[*ast.Object]bool) ast.Expr {
	switch node := expr.(type) {
	case *ast.UnaryExpr:
		if node.Op == token.AND {
			return declaredType(node.X, seen)
		}
	case *ast.CompositeLit:
		return node.Type
	case *ast.Ident:
		obj := node.Obj
		if obj == nil || seen[obj] {
			return nil
		}
		seen[obj] = true
		switch decl := obj.Decl.(type) {
		case *ast.Field:
			return decl.Type
		case *ast.ValueSpec:
			if decl.Type != nil {
				return decl.Type
			}
			for i, name := range decl.Names {
				if name.Obj == obj && i < len(decl.Values) {
					return declaredType(decl.Values[i], seen)
				}
			}
		case *ast.AssignStmt:
			for i, left := range decl.Lhs {
				if name, ok := left.(*ast.Ident); ok && name.Obj == obj && i < len(decl.Rhs) {
					return declaredType(decl.Rhs[i], seen)
				}
			}
		}
	}
	return nil
}

// nativeBinding 只识别 Gin Context 的确定 body binder；自动绑定与多种形状不猜协议。
func nativeBinding(body *ast.BlockStmt, pkg string, imports map[string]string,
	generics map[string]bool) bodyBinding {
	var bindings []bodyBinding
	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			// pkg/binding 的泛型入口 binding.Form[T](c) / JSON[T](c) / Query[T](c) /
			// ParseForm[T](c) / ParseJSON[T](c)：协议由函数名给，DTO 是类型实参。
			// 它们不是 gin.Context 的方法调用，必须先于下面的方法锁判定识别。
			if bind, okBind := bindingGenericDTO(node, pkg, imports); okBind {
				bindings = append(bindings, bind)
				break
			}
			method, ok := node.Fun.(*ast.SelectorExpr)
			if !ok || len(node.Args) == 0 {
				break
			}
			contextType := declaredType(method.X, map[*ast.Object]bool{})
			if typeName(contextType, pkg, imports) != "github.com/gin-gonic/gin.Context" {
				break
			}
			protocol := ""
			switch method.Sel.Name {
			case "ParseJSON":
				// pkg/binding 的显式 JSON 口：只接收 JSON，不按 Content-Type 自动选择。
				// 它是 `binding.ParseJSON[T](c)` 的泛型调用：请求类型在接收者上，不在实参上。
				// 这里只登记协议，DTO 留给调用方的类型实参补齐。
				if sel, okSel := method.X.(*ast.SelectorExpr); okSel {
					if blind, okBlind := sel.X.(*ast.Ident); okBlind && indexImport(imports, blind.Name) == "go_wp/pkg/binding" {
						bindings = append(bindings, bodyBinding{Protocol: "json"})
						break
					}
				}
				protocol = "unknown"
			case "ShouldBindJSON":
				protocol = "json"
			case "ShouldBindWith":
				if len(node.Args) == 2 {
					if binder, ok := node.Args[1].(*ast.SelectorExpr); ok {
						if alias, ok := binder.X.(*ast.Ident); ok && alias.Obj == nil && imports[alias.Name] == "github.com/gin-gonic/gin/binding" {
							switch binder.Sel.Name {
							case "FormPost":
								protocol = "form"
							case "FormMultipart":
								protocol = "multipart"
							case "JSON":
								protocol = "json"
							}
						}
					}
				}
				if protocol == "" {
					protocol = "unknown"
				}
			case "ShouldBind", "Bind":
				protocol = "unknown"
			default:
				break
			}
			if protocol == "" {
				break
			}
			expr := declaredType(node.Args[0], map[*ast.Object]bool{})
			dto := typeName(expr, pkg, imports)
			// 泛型骨架里请求是 *T：协议能确定，DTO 由调用方实参补齐，不算「无法确定契约」。
			genericReq := false
			switch node := expr.(type) {
			case *ast.StarExpr:
				if ident, okIdent := node.X.(*ast.Ident); okIdent && generics[ident.Name] {
					genericReq = true
				}
			case *ast.Ident:
				// `var in T; c.ShouldBindJSON(&in)` 解析出来是裸类型参数 T。
				if generics[node.Name] {
					genericReq = true
				}
			}
			binding := bodyBinding{DTO: dto, Protocol: protocol}
			if protocol == "unknown" {
				binding.Protocol, binding.Reason = "unknown", "自动、未知 binder 或非具名请求类型，无法确定输入契约"
			} else if binding.DTO == "" && !genericReq {
				binding.Protocol, binding.Reason = "unknown", "自动、未知 binder 或非具名请求类型，无法确定输入契约"
			}
			bindings = append(bindings, binding)
		}
		return true
	})
	if len(bindings) == 0 {
		return bodyBinding{}
	}
	for _, binding := range bindings[1:] {
		if binding != bindings[0] {
			return bodyBinding{Protocol: "unknown", Reason: "处理器包含多种请求绑定，不能取第一个冒充完整契约"}
		}
	}
	return bindings[0]
}

func dtoFields(name, protocol string, types map[string]sourceStruct, visiting map[string]bool) ([]fieldSchema, error) {
	source, ok := types[name]
	if !ok || visiting[name] {
		return nil, fmt.Errorf("请求类型缺失或递归嵌入：%s", name)
	}
	visiting[name] = true
	defer delete(visiting, name)
	tagName := "json"
	if protocol == "form" || protocol == "multipart" {
		tagName = "form"
	}
	var fields []fieldSchema
	for _, fld := range source.Node.Fields.List {
		tag := reflect.StructTag("")
		if fld.Tag != nil {
			raw, err := strconv.Unquote(fld.Tag.Value)
			if err != nil {
				return nil, err
			}
			tag = reflect.StructTag(raw)
		}
		fieldName := strings.Split(tag.Get(tagName), ",")[0]
		if fieldName == "-" {
			continue
		}
		if len(fld.Names) == 0 && (fieldName == "" || tagName == "form") {
			embedded := typeName(fld.Type, source.Package, source.Imports)
			childFields, err := dtoFields(embedded, protocol, types, visiting)
			if err != nil {
				return nil, err
			}
			_, pointer := fld.Type.(*ast.StarExpr)
			if (pointer && !requiredField(tag.Get("binding"))) || strings.Contains(","+tag.Get("binding")+",", ",omitempty,") {
				for i := range childFields {
					childFields[i].Required = false
				}
			}
			fields = append(fields, childFields...)
			continue
		}
		names := fld.Names
		if len(names) == 0 {
			names = []*ast.Ident{ast.NewIdent(fieldName)}
		}
		for _, name := range names {
			if !name.IsExported() && len(fld.Names) > 0 {
				continue
			}
			wireName := fieldName
			if wireName == "" {
				wireName = name.Name
			}
			fields = append(fields, fieldSchema{Name: wireName, Required: requiredField(tag.Get("binding"))})
		}
	}
	seen := map[string]bool{}
	for _, field := range fields {
		if seen[field.Name] {
			return nil, fmt.Errorf("请求字段名冲突，未模拟嵌入覆盖规则：%s.%s", name, field.Name)
		}
		seen[field.Name] = true
	}
	return fields, nil
}

func requiredField(binding string) bool {
	for _, rule := range strings.Split(binding, ",") {
		switch strings.TrimSpace(rule) {
		case "required":
			return true
		case "omitempty", "omitnil", "omitzero", "dive":
			return false
		}
	}
	return false
}

func marshalStable(in map[string]endpointSchema) ([]byte, error) {
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var buf bytes.Buffer
	buf.WriteString("{\n")
	for i, key := range keys {
		inner, err := json.Marshal(in[key])
		if err != nil {
			return nil, err
		}
		keyJSON, _ := json.Marshal(key)
		fmt.Fprintf(&buf, "  %s: %s", keyJSON, inner)
		if i < len(keys)-1 {
			buf.WriteString(",")
		}
		buf.WriteString("\n")
	}
	buf.WriteString("}\n")
	return buf.Bytes(), nil
}

// delegateBinding 识别「处理器把请求整包转交给另一个函数」的形态。
//
// 这种端点的输入形状由被委托方决定；只取本函数里的 binder（通常没有）
// 会把受检端点误报成未检查。沿调用链只跟一层：调用方把 gin.Context 实参
// 传给被委托方的 gin.Context 形参，被委托方自己的 binder 才是该端点的契约。
// 出现多条委托或委托目标仍无 binder 时如实返回 unknown 与原因，不猜。
func delegateBinding(name string, index sourceIndex) bodyBinding {
	return delegateBindingDepth(name, index, 3)
}

// delegateBindingDepth 沿委托链找 binder，深度有界（处理器 → bindX → bindXInto 是常见三层）。
//
// 一个处理器可能既委托「绑定」又委托「读取」：只有带明确 binder 的被委托方才说明
// 输入契约，读取方（按 id 取实体之类）没有 binder、不该把它算成第二个输入形状。
// 因此先过滤出带 binder 的目标；恰好一个才算唯一契约，多个不同 binder 仍报歧义。
func delegateBindingDepth(name string, index sourceIndex, depth int) bodyBinding {
	src, ok := index.funcs[name]
	if !ok || depth <= 0 {
		return bodyBinding{}
	}
	// 处理器常把上下文交给多个辅助函数（错误提示、回跳、取值），但只有**带 body
	// binder** 的那条才定义了输入契约；因此沿每条委托链继续下探，只收真实 binder。
	var bound []bodyBinding
	for _, target := range delegatedTargets(src, index) {
		callee, okTarget := index.funcs[target]
		if !okTarget {
			continue
		}
		binding, okBinding := index.Bindings[callee.name]
		if !okBinding || binding.Protocol == "" || binding.Protocol == "unknown" {
			if inner := delegateBindingDepth(target, index, depth-1); inner.Protocol != "" && inner.Protocol != "unknown" {
				binding, okBinding = inner, true
			}
		}
		if !okBinding || binding.Protocol == "" || binding.Protocol == "unknown" {
			continue
		}
		duplicate := false
		for _, seen := range bound {
			if seen.Protocol == binding.Protocol && seen.DTO == binding.DTO {
				duplicate = true
				break
			}
		}
		if !duplicate {
			bound = append(bound, binding)
		}
	}
	switch len(bound) {
	case 0:
		return bodyBinding{}
	case 1:
		return bound[0]
	default:
		return bodyBinding{Protocol: "unknown", Reason: "处理器把请求委托给多个处理器，无法确定唯一输入契约"}
	}
}

// delegatedTargets 返回本函数里「把 gin.Context 实参转交给另一个函数」的目标标识。
func delegatedTargets(src *funcSource, index sourceIndex) []string {
	seen := map[string]bool{}
	out := make([]string, 0, 1)
	ast.Inspect(src.body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		// 委托目标既可能是方法（h.bindX(c)），也可能是同包辅助函数（bindX(c)）。
		target := calleeOf(src, call.Fun, index)
		if target == nil {
			return true
		}
		// 找一找实参里有没有本函数的 gin.Context 形参。
		for i, arg := range call.Args {
			ident, ok := arg.(*ast.Ident)
			if !ok || !src.ctxParams[ident.Name] {
				continue
			}
			// 被委托方的第 i 个形参也必须是 gin.Context，才算是整包委托。
			if i >= len(target.paramNames) || !target.ctxParams[target.paramNames[i]] {
				continue
			}
			if !seen[target.name] {
				seen[target.name] = true
				out = append(out, target.name)
			}
			break
		}
		return true
	})
	sort.Strings(out)
	return out
}

// calleeOf 把一次调用的被调方解析成源码索引里的函数标识；解析不出返回 nil。
//
// 两种形态：同包辅助函数（Ident，如 bindArticleForm）与方法调用（SelectorExpr，
// 如 h.articleByID）。前者在委托链里更常见 —— 绑定与读取都收在包级辅助函数里。
func calleeOf(src *funcSource, fun ast.Expr, index sourceIndex) *funcSource {
	switch node := fun.(type) {
	case *ast.Ident:
		if callee, ok := index.funcs[src.pkg+"."+node.Name]; ok {
			return callee
		}
		return nil
	case *ast.SelectorExpr:
		return resolveCallee(src, node, index)
	default:
		return nil
	}
}

// resolveCallee 把一次方法调用解析成源码索引里的函数标识；解析不出返回 nil。
func resolveCallee(src *funcSource, sel *ast.SelectorExpr, index sourceIndex) *funcSource {
	switch recv := sel.X.(type) {
	case *ast.Ident:
		// 同包私有方法或包级函数：src 的接收者类型 + 方法名。
		if src.recvName != "" && recv.Name == src.recvName {
			if callee, ok := index.funcs[funcWithMethod(src, sel.Sel.Name)]; ok {
				return callee
			}
		}
		// 形参上的委托（接口字段等）无法静态解析成具体实现，不猜。
		return nil
	case *ast.SelectorExpr:
		// a.h.foo(...) 形态：按字段类型解析。
		if typ := typeName(recv, src.pkg, src.imports); typ != "" {
			if callee, ok := index.funcs[typ+"."+sel.Sel.Name]; ok {
				return callee
			}
		}
		return nil
	}
	return nil
}

// funcWithMethod 返回接收者方法在索引里的标识。
func funcWithMethod(src *funcSource, method string) string {
	dot := strings.LastIndex(src.name, ".")
	if dot < 0 {
		return src.name
	}
	return src.name[:dot] + "." + method
}

// genericSkeletonBinding 识别「处理器把工作交给本包泛型写骨架」的形态。
//
// 为什么需要：`runProductWrite[T](c, call, success)` 这类骨架把绑定与调用收在一处，
// 请求类型由**调用方实参**决定（`runProductWrite(c, func(_ context.Context, r *XReq) ...)`）。
// 骨架体里看到的是类型参数 T，不是具体 DTO；直接扫骨架会得到空 DTO 而被误报 unknown。
// 这里只做一件事：按调用方的实参推断 T，再读骨架自己的 binder 协议。
// 推断不出来就如实返回 unknown —— 不按函数名猜「JSON 就是 JSON」。
func genericSkeletonBinding(name string, index sourceIndex) bodyBinding {
	src, ok := index.funcs[name]
	if !ok {
		return bodyBinding{}
	}
	var skeleton string
	var dto string
	var resolved bodyBinding
	ast.Inspect(src.body, func(n ast.Node) bool {
		call, okCall := n.(*ast.CallExpr)
		if !okCall {
			return true
		}
		// 泛型调用可以显式写类型实参（f[T](...)），也可以靠 Go 从回调推断（f(...)）。
		// 两种都要认：只认 IndexExpr 会漏掉生产里更常见的推断写法。
		var calleeName string
		switch fun := call.Fun.(type) {
		case *ast.IndexExpr:
			ident, okIdent := fun.X.(*ast.Ident)
			if !okIdent {
				return true
			}
			calleeName = ident.Name
		case *ast.IndexListExpr:
			ident, okIdent := fun.X.(*ast.Ident)
			if !okIdent {
				return true
			}
			calleeName = ident.Name
			_ = fun
		case *ast.Ident:
			calleeName = fun.Name
		default:
			return true
		}
		if calleeName == "" {
			return true
		}
		target, okTarget := index.funcs[src.pkg+"."+calleeName]
		if !okTarget || len(target.generics) == 0 {
			return true
		}
		// 显式类型实参（f[XReq](...)）：直接就是 DTO。
		if binding := explicitTypeArgDTO(call, calleeName, target, src, index); binding.Protocol != "" {
			resolved = binding
			return false
		}
		// 实参里的回调是 func(ctx, req *XReq) —— 兜底类型就是骨架上 T 的实例。
		for _, arg := range call.Args {
			lit, okLit := arg.(*ast.FuncLit)
			if !okLit || lit.Type == nil || lit.Type.Params == nil {
				continue
			}
			for _, field := range lit.Type.Params.List {
				for _, pname := range field.Names {
					if pname.Name == "_" {
						continue
					}
					typ, okTyp := field.Type.(*ast.StarExpr)
					if !okTyp {
						continue
					}
					identified := typeName(typ.X, src.pkg, src.imports)
					// 回调的第一个形参常是 *gin.Context —— 那是请求上下文，不是请求类型。
					if identified == "" || identified == "github.com/gin-gonic/gin.Context" {
						continue
					}
					skeleton, dto = target.name, identified
					return false
				}
			}
		}
		return true
	})
	if resolved.Protocol != "" {
		return resolved
	}
	if skeleton == "" || dto == "" {
		return bodyBinding{}
	}
	// 骨架自己的登记只有「协议」（泛型体里的 DTO 被清空），DTO 必须来自调用方；
	// 沿委托链只用来兜底补协议。
	binding, okBinding := index.Bindings[skeleton]
	if !okBinding || binding.Protocol == "" || binding.Protocol == "unknown" {
		binding = chainedBinderProtocol(index.funcs[skeleton], index, 4)
	}
	if binding.Protocol == "" || binding.Protocol == "unknown" {
		if protocol := skeletonJSONProtocol(index.funcs[skeleton]); protocol != "" {
			binding = bodyBinding{Protocol: protocol}
		}
	}
	binding.DTO = ""
	// 多个字段由骨架内的同一个 binder 绑定：helper 只说明协议，DTO 由调用方给。
	if binding.Protocol == "" || binding.Protocol == "unknown" {
		return bodyBinding{Protocol: "unknown", Reason: "泛型写骨架没有可确定的 binder：" + skeleton}
	}
	binding.DTO = dto
	return binding
}

// explicitTypeArgDTO 处理显式泛型调用 `f[XReq](...)`：类型实参就是请求类型。
//
// 与推断写法的区别：这里不需要读回调，直接取实参里的具名类型。
// 返回 Protocol 为空表示「不是显式泛型调用，交给推断分支」。
func explicitTypeArgDTO(call *ast.CallExpr, calleeName string, target *funcSource,
	src *funcSource, index sourceIndex) bodyBinding {
	var argList []ast.Expr
	switch fun := call.Fun.(type) {
	case *ast.IndexExpr:
		argList = []ast.Expr{fun.Index}
	case *ast.IndexListExpr:
		argList = fun.Indices
	default:
		return bodyBinding{}
	}
	if len(argList) != 1 {
		return bodyBinding{}
	}
	// 类型实参是**类型参数本身**（f[T](...)）时不是具体 DTO，交给实参推断。
	if ident, okIdent := argList[0].(*ast.Ident); okIdent && target.generics[ident.Name] {
		return bodyBinding{}
	}
	dto := typeName(argList[0], src.pkg, src.imports)
	if dto == "" {
		return bodyBinding{}
	}
	binding, ok := index.Bindings[target.name]
	if !ok || binding.Protocol == "" || binding.Protocol == "unknown" {
		// 骨架本身可能只是一层转发（bindX → bindXInto），沿链找真正的 binder。
		binding = chainedBinderProtocol(target, index, 4)
	}
	if binding.Protocol == "" || binding.Protocol == "unknown" {
		return bodyBinding{Protocol: "unknown", Reason: "泛型写骨架没有可确定的 binder：" + target.name}
	}
	binding.DTO = dto
	return binding
}

// chainedBinderProtocol 沿骨架体里的委托链找 binder 协议（有界深度）。
//
// 泛型写骨架常见两层：runX(c, ...) → bindX(c) → bindXInto(c, req)。
// 只跟一层会把第二层误报成「没有可确定的 binder」；深度上限避免把无关调用链并进来。
func chainedBinderProtocol(src *funcSource, index sourceIndex, depth int) bodyBinding {
	if src == nil || depth <= 0 {
		return bodyBinding{}
	}
	if protocol := skeletonJSONProtocol(src); protocol != "" {
		return bodyBinding{Protocol: protocol}
	}
	var out bodyBinding
	ast.Inspect(src.body, func(n ast.Node) bool {
		if out.Protocol != "" {
			return false
		}
		call, okCall := n.(*ast.CallExpr)
		if !okCall {
			return true
		}
		var name string
		switch fun := call.Fun.(type) {
		case *ast.Ident:
			name = fun.Name
		case *ast.IndexExpr:
			if ident, okIdent := fun.X.(*ast.Ident); okIdent {
				name = ident.Name
			}
		case *ast.IndexListExpr:
			if ident, okIdent := fun.X.(*ast.Ident); okIdent {
				name = ident.Name
			}
		default:
			return true
		}
		if name == "" {
			return true
		}
		target, okTarget := index.funcs[src.pkg+"."+name]
		if !okTarget {
			return true
		}
		if binding, okBinding := index.Bindings[target.name]; okBinding &&
			binding.Protocol != "" && binding.Protocol != "unknown" {
			if len(target.generics) > 0 {
				binding.DTO = ""
			}
			out = binding
			return false
		}
		if protocol := chainedBinderProtocol(target, index, depth-1); protocol.Protocol != "" &&
			protocol.Protocol != "unknown" {
			out = bodyBinding{Protocol: protocol.Protocol}
			return false
		}
		return true
	})
	return out
}

// skeletonJSONProtocol 识别骨架体里的 `binding.ParseJSON[T](c)` 调用。
//
// 这条调用把「显式 JSON 绑定」收在泛型骨架里：nativeBinding 看到的是泛型接收者
// binding.ParseJSON[T]，识别不出协议；这里按导入路径确认它就是 pkg/binding 的 JSON 口。
func skeletonJSONProtocol(src *funcSource) string {
	if src == nil {
		return ""
	}
	out := ""
	ast.Inspect(src.body, func(n ast.Node) bool {
		if out != "" {
			return false
		}
		call, okCall := n.(*ast.CallExpr)
		if !okCall {
			return true
		}
		var sel *ast.SelectorExpr
		switch fun := call.Fun.(type) {
		case *ast.IndexExpr:
			sel, _ = fun.X.(*ast.SelectorExpr)
		case *ast.IndexListExpr:
			sel, _ = fun.X.(*ast.SelectorExpr)
		case *ast.SelectorExpr:
			sel = fun
		default:
			return true
		}
		if sel == nil || sel.Sel.Name != "ParseJSON" {
			return true
		}
		if ident, okIdent := sel.X.(*ast.Ident); okIdent && src.imports[ident.Name] == "go_wp/pkg/binding" {
			out = "json"
			return false
		}
		return true
	})
	return out
}

// indexImport 返回导入别名的完整路径；未导入返回空。
func indexImport(imports map[string]string, alias string) string { return imports[alias] }

// directGenericBinder 识别「处理器直接调用泛型 binder」的写法：
//
//	req := bindX[SomeReq](c)          —— 类型实参就是 DTO
//	req := runX(c, func(..., r *SomeReq) ...) —— 由回调形参推断
//
// 协议与 DTO 都来自 binder 与被推断出的类型；识别不出就返回空，交给后续兜底。
func directGenericBinder(name string, index sourceIndex) bodyBinding {
	src, ok := index.funcs[name]
	if !ok {
		return bodyBinding{}
	}
	var out bodyBinding
	ast.Inspect(src.body, func(n ast.Node) bool {
		if out.Protocol != "" {
			return false
		}
		call, okCall := n.(*ast.CallExpr)
		if !okCall {
			return true
		}
		// 泛型 binder 的调用可能出现在赋值右侧、也可能包在别的表达式里；
		// 这里对每个调用都尝试一次，不依赖外层语句形状。
		var name string
		var typeArg ast.Expr
		switch fun := call.Fun.(type) {
		case *ast.Ident:
			name = fun.Name
		case *ast.IndexExpr:
			if ident, okIdent := fun.X.(*ast.Ident); okIdent {
				name = ident.Name
			}
			typeArg = fun.Index
		case *ast.IndexListExpr:
			if ident, okIdent := fun.X.(*ast.Ident); okIdent {
				name = ident.Name
			}
			if len(fun.Indices) == 1 {
				typeArg = fun.Indices[0]
			}
		default:
			return true
		}
		if name == "" {
			return true
		}
		target, okTarget := index.funcs[src.pkg+"."+name]
		if !okTarget || len(target.generics) == 0 {
			return true
		}
		protocol := chainedBinderProtocol(target, index, 4)
		if protocol.Protocol == "" || protocol.Protocol == "unknown" {
			return true
		}
		// 显式类型实参（bindX[SomeReq](c)）：实参就是 DTO。
		if typeArg != nil {
			if ident, okIdent := typeArg.(*ast.Ident); !okIdent || !target.generics[ident.Name] {
				if dto := typeName(typeArg, src.pkg, src.imports); dto != "" {
					out = bodyBinding{Protocol: protocol.Protocol, DTO: dto}
					return false
				}
			}
		}
		// 显式转换函数作为实参（(*TierFormReq).CreateReq）：括号里的接收者类型就是被绑定的表单类型。
		for _, arg := range call.Args {
			sel, okSel := arg.(*ast.SelectorExpr)
			if !okSel {
				continue
			}
			paren, okParen := sel.X.(*ast.ParenExpr)
			if !okParen {
				continue
			}
			star, okStar := paren.X.(*ast.StarExpr)
			if !okStar {
				continue
			}
			if dto := typeName(star.X, src.pkg, src.imports); dto != "" {
				out = bodyBinding{Protocol: protocol.Protocol, DTO: dto}
				return false
			}
		}
		// 靠回调形参推断的形状。
		for _, arg := range call.Args {
			lit, okLit := arg.(*ast.FuncLit)
			if !okLit || lit.Type == nil || lit.Type.Params == nil {
				continue
			}
			for _, field := range lit.Type.Params.List {
				star, okStar := field.Type.(*ast.StarExpr)
				if !okStar {
					continue
				}
				dto := typeName(star.X, src.pkg, src.imports)
				if dto == "" || dto == "github.com/gin-gonic/gin.Context" {
					continue
				}
				out = bodyBinding{Protocol: protocol.Protocol, DTO: dto}
				return false
			}
		}
		return true
	})
	return out
}

// bindingGenericDTO 识别 pkg/binding 的泛型绑定入口。
//
//	binding.Form[T](c)      —— 标准表单编码
//	binding.JSON[T](c)      —— 按 Content-Type 自适应（HTML 写端点仍在用）
//	binding.Query[T](c)     —— 查询串
//	binding.ParseForm[T](c) / ParseJSON[T](c) —— 显式表单 / JSON
//
// 这些都是 pkg/binding 的正式入口，协议由函数名确定、DTO 是类型实参；
// 不识别会让所有走它的端点都报 unknown，把真正的欠账埋在噪声里。
func bindingGenericDTO(call *ast.CallExpr, pkg string, imports map[string]string) (bodyBinding, bool) {
	fun, ok := call.Fun.(*ast.IndexExpr)
	if !ok {
		return bodyBinding{}, false
	}
	sel, ok := fun.X.(*ast.SelectorExpr)
	if !ok {
		return bodyBinding{}, false
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok || imports[ident.Name] != "go_wp/pkg/binding" {
		return bodyBinding{}, false
	}
	protocol := ""
	switch sel.Sel.Name {
	case "Form", "ParseForm":
		protocol = "form"
	case "JSON", "ParseJSON":
		protocol = "json"
	case "Query":
		protocol = "query"
	default:
		return bodyBinding{}, false
	}
	dto := typeName(fun.Index, pkg, imports)
	if dto == "" {
		return bodyBinding{Protocol: "unknown", Reason: "pkg/binding 泛型调用的类型实参不是具名类型"}, true
	}
	return bodyBinding{DTO: dto, Protocol: protocol}, true
}
