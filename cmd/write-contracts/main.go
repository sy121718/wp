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
	imports   map[string]string
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
		binding, ok := index.Bindings[handlerIdentity(r.Handler)]
		if !ok {
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
				if node.Recv != nil && len(node.Recv.List) == 1 {
					name = typeName(node.Recv.List[0].Type, pkg, imports) + "." + node.Name.Name
				}
				index.funcs[name] = &funcSource{name: name, body: node.Body, pkg: pkg, imports: imports}
				if binding := nativeBinding(node.Body, pkg, imports); binding.DTO != "" || binding.Reason != "" {
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
func nativeBinding(body *ast.BlockStmt, pkg string, imports map[string]string) bodyBinding {
	var bindings []bodyBinding
	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
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
			binding := bodyBinding{DTO: typeName(expr, pkg, imports), Protocol: protocol}
			if protocol == "unknown" || binding.DTO == "" {
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
