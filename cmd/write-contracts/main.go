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
	// Variants 是同一 METHOD+PATH 的第二个输入形状：处理器在入口先按请求字段是否存在
	// 分叉（如「有 ids 就批量、否则单条」），两条分支各自绑定不同的 DTO。
	// 不写死处理器名字：只有「分叉条件探测请求 + 提前 return 的委托分支绑定到不同 DTO」
	// 这一可静态证明的形状才产出变体；识别不出时保持单一契约，不猜。
	Variants []variantSchema `json:"variants,omitempty"`
}

// variantSchema 是一条变体契约：线上字段由该分支真实绑定的 DTO 决定。
type variantSchema struct {
	DTO      string        `json:"dto,omitempty"`
	Protocol string        `json:"protocol"`
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

// resolvedBinding 判断绑定是否已确定输入协议。
func resolvedBinding(b bodyBinding) bool {
	if b.Reason != "" {
		return false
	}
	return b.Protocol != "" && b.Protocol != "unknown"
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
	// genericOrder 是按声明顺序排列的类型参数名，用于把显式类型实参对回参数位置。
	genericOrder []string
	// typeParams 记录形参的源码表达式，供泛型骨架按实参类型替换出真实 DTO。
	typeParams map[string]ast.Expr
	// paramDecls 按源码顺序记录形参（名字 + 类型），供泛型骨架按位置把调用方实参
	// 对回骨架的形参：只有 binder 真正绑定的那个类型参数才是输入 DTO。
	paramDecls []paramDecl
}

// paramDecl 是一个形参声明：可能是多名字共用一个类型（a, b *T），展开成逐名字。
type paramDecl struct {
	name string
	typ  ast.Expr
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
		if !resolvedBinding(binding) && binding.Reason == "" {
			binding = directGenericBinder(identity, index)
		}
		if !resolvedBinding(binding) && binding.Reason == "" {
			binding = delegateBinding(identity, index)
		}
		if !resolvedBinding(binding) && binding.Reason == "" {
			// 没有任何 binder 的入口先做**正向证明**：处理器（含可解析的委托链）
			// 完全不读 body → nobody；只读原始字节 → raw。两者都是可证伪的
			// 事实（出现任何 body 读取痕迹就退回 unknown），不是「未检查」。
			binding = noBinderEvidence(identity, index)
		}
		if !resolvedBinding(binding) && binding.Reason == "" {
			binding = bodyBinding{Protocol: "unknown", Reason: "真实处理器没有唯一显式 body binder，或是包装闭包"}
		}
		// 本地有确定的 binder，不代表这是唯一输入路径：只要还有一条分支委托给
		// 无法确定形状的目标，整体仍应如实报未检查，不能被本地 binder 洗白。
		if resolvedBinding(binding) {
			if target, bad := delegatedUnknownBranch(identity, index); bad {
				binding = bodyBinding{Protocol: "unknown", Reason: "处理器还有一条委托分支无法确定输入形状：" + target}
			}
		}
		ep := endpointSchema{DTO: binding.DTO, Handler: r.Handler, Protocol: binding.Protocol, Reason: binding.Reason, Fields: []fieldSchema{}}
		if binding.Reason == "" && binding.DTO != "" {
			fields, err := dtoFields(binding.DTO, binding.Protocol, index.Types, map[string]bool{})
			if err != nil {
				ep.Protocol, ep.Reason = "unknown", err.Error()
			} else {
				ep.Fields = append(ep.Fields, fields...)
			}
		}
		// 入口先按请求字段分叉时，另一支是同一端点的第二个输入形状。
		// 只有两条分支都能解析出确定 DTO 且互不相同时才产出变体；否则保持单一契约。
		if len(ep.Fields) > 0 {
			ep.Variants = variantBindings(identity, binding, index)
		}
		out[r.Method+" "+r.Path] = ep
	}
	return out
}

// variantBindings 找「入口按请求字段分叉」的第二条输入形状。
//
// 要识别的形状（写成代码就是）：
//
//	if probe(c) != nil {          // probe 探测某个字段是否存在
//	    h.bulkBranch(c)           // 提前 return 的分支，自己绑定另一个 DTO
//	    return
//	}
//	... 主流��继续绑定主 DTO ...
//
// 只认这一种**可静态证明**的形状：分叉条件是「探针函数返回非 nil」，
// 分支体是整包委托且提前 return。识别不出、或两条分支解析出同一个 DTO，
// 就不产出变体——宁可不报，也不猜。探针名字不写死，任何返回可比对值的
// 本地函数（本例是 orderBulkFormIDs）都行。
func variantBindings(identity string, mainB bodyBinding, index sourceIndex) []variantSchema {
	src, ok := index.funcs[identity]
	if !ok || src.body == nil {
		return nil
	}
	var out []variantSchema
	ast.Inspect(src.body, func(n ast.Node) bool {
		if len(out) > 0 {
			return false
		}
		ifStmt, okIf := n.(*ast.IfStmt)
		if !okIf {
			return true
		}
		// 条件必须是 `probe(c) != nil`：左值是对 gin.Context 形参的调用。
		binary, okBin := ifStmt.Cond.(*ast.BinaryExpr)
		if !okBin || binary.Op != token.NEQ {
			return true
		}
		call, okCall := binary.X.(*ast.CallExpr)
		if !okCall || !takesContext(call, src) {
			return true
		}
		// 分支体必须以 return 结束（提前退出），否则它只是条件处理、不是输入形状分叉。
		if !endsWithReturn(ifStmt.Body) {
			return true
		}
		// 分支体里必须有一处整包委托（把 gin.Context 原样转交给另一个函数）。
		var branch string
		for _, target := range delegatedTargetsOfBlock(ifStmt.Body, src, index) {
			branch = target
			break
		}
		if branch == "" {
			return true
		}
		inner := bindingForIdentity(branch, index)
		if inner.Protocol == "" || inner.Protocol == "unknown" || inner.DTO == "" {
			return true
		}
		if inner.DTO == mainB.DTO {
			return true
		}
		fields, err := dtoFields(inner.DTO, inner.Protocol, index.Types, map[string]bool{})
		if err != nil {
			return true
		}
		out = append(out, variantSchema{DTO: inner.DTO, Protocol: inner.Protocol, Fields: fields})
		return false
	})
	return out
}

// takesContext 判断一次调用是否把某个 gin.Context 形参原样传了出去。
func takesContext(call *ast.CallExpr, src *funcSource) bool {
	for _, arg := range call.Args {
		if ident, ok := arg.(*ast.Ident); ok && src.ctxParams[ident.Name] {
			return true
		}
	}
	return false
}

// endsWithReturn 判断块体最后一句是 return。
func endsWithReturn(body *ast.BlockStmt) bool {
	if body == nil || len(body.List) == 0 {
		return false
	}
	_, ok := body.List[len(body.List)-1].(*ast.ReturnStmt)
	return ok
}

// delegatedTargetsOfBlock 返回某个块里「把 gin.Context 原样转交出去」的调用目标。
func delegatedTargetsOfBlock(body *ast.BlockStmt, src *funcSource, index sourceIndex) []string {
	seen := map[string]bool{}
	var out []string
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !takesContext(call, src) {
			return true
		}
		target := calleeOf(src, call.Fun, index)
		if target == nil || seen[target.name] {
			return true
		}
		seen[target.name] = true
		out = append(out, target.name)
		return true
	})
	sort.Strings(out)
	return out
}

// bindingForIdentity 用与 buildContracts 相同的优先级解析一个 handler 的输入契约。
func bindingForIdentity(identity string, index sourceIndex) bodyBinding {
	binding, ok := index.Bindings[identity]
	if !ok {
		binding = genericSkeletonBinding(identity, index)
	}
	if !resolvedBinding(binding) && binding.Reason == "" {
		binding = directGenericBinder(identity, index)
	}
	if !resolvedBinding(binding) && binding.Reason == "" {
		binding = delegateBinding(identity, index)
	}
	return binding
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
							src.genericOrder = append(src.genericOrder, tpName.Name)
						}
					}
				}
				for _, field := range node.Type.Params.List {
					for _, paramName := range field.Names {
						src.paramNames = append(src.paramNames, paramName.Name)
						src.typeParams[paramName.Name] = field.Type
						src.paramDecls = append(src.paramDecls, paramDecl{name: paramName.Name, typ: field.Type})
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
			case "ShouldBindQuery":
				// 显式查询串绑定：只读 URL query，不读 body。字段名在 DTO 的 form tag 上
				// （Gin 的 query binder 与 form binder 共用 form tag）。
				protocol = "query"
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

// noBinderEvidence 对「没有任何 binder」的入口做正向证明。
//
// 只有两种可证伪的结论：
//   - raw：处理器（含可解析的委托链）直接读原始请求体（io.ReadAll(c.Request.Body)、
//     GetRawData、FormFile、ParseMultipartForm）。这类端点的 body 形状由外部协议
//     （支付方回调、采集 beacon）决定，不由本仓字段表声明；
//   - nobody：整条可解析调用链完全不读 body，也不读 form/query 字段。
//
// 出现任何手工读 form/query 的痕迹（PostForm、Query、DefaultQuery）或把 ctx 交给
// 无法解析的目标时，返回空绑定 —— 由调用方落回 unknown，提醒仍需迁移成显式 DTO。
// 这是「未证明」而不是「已检查」，不会被当成已覆盖。
func noBinderEvidence(name string, index sourceIndex) bodyBinding {
	readsRaw, manual, unresolved := bodyReadEvidence(name, index, map[string]bool{})
	if unresolved {
		return bodyBinding{}
	}
	// 读原始字节时，body 协议就是「原始」——query 里另读的字段不影响 body 形状
	// （支付回调的 projectId 就在 query 上，正文仍交验签）。
	if readsRaw {
		return bodyBinding{Protocol: "raw"}
	}
	// 不读原始字节却手工读 form/query：说明输入仍未绑定成 DTO，保持 unknown
	// 等迁移，不把「未证明」写成「已检查」。
	if manual {
		return bodyBinding{}
	}
	return bodyBinding{Protocol: "nobody"}
}

// bodyReadEvidence 沿委托链收集读取痕迹。
// readsRaw 表示读了原始字节；manual 表示手工读了 form/query 字段；unresolved 表示
// 把 gin.Context 交给了无法解析的目标（证明不了它不读 body）。
//
// visited 防止同一条辅助链被重复遍历，也避免递归辅助函数无限下探。
func bodyReadEvidence(name string, index sourceIndex, visited map[string]bool) (readsRaw, manual, unresolved bool) {
	src, ok := index.funcs[name]
	if !ok || src.body == nil {
		return false, false, true
	}
	if visited[name] {
		return false, false, false
	}
	visited[name] = true
	// 没有 gin.Context 形参的函数证明不了「不读 HTTP body」：它不是请求处理器。
	if len(src.ctxParams) == 0 {
		return false, false, true
	}
	ast.Inspect(src.body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncLit:
			// 闭包体不展开：闭包里可能有 binder，跳过会得到错误的「不读 body」。
			unresolved = true
			return false
		case *ast.CallExpr:
			// 任何 ShouldBind*/Bind* 都是绑定尝试；nativeBinding 没认出来的形状
			// 说明协议不确定，不能因为「没读到 body 字段」就判成 nobody。
			if sel, okSel := node.Fun.(*ast.SelectorExpr); okSel {
				if strings.HasPrefix(sel.Sel.Name, "ShouldBind") || sel.Sel.Name == "Bind" {
					unresolved = true
					return false
				}
			}
			if !takesContext(node, src) {
				return true
			}
			target := calleeOf(src, node.Fun, index)
			if target == nil {
				// 解析不到的目标是服务/响应辅助函数（接口方法、pkg 包函数）：
				// 它们不读 HTTP 请求体，不参与 body 读取判定。
				// 同一 internal 树里的本地函数/方法都能被 calleeOf 解析。
				return true
			}
			r, m, u := bodyReadEvidence(target.name, index, visited)
			readsRaw = readsRaw || r
			manual = manual || m
			unresolved = unresolved || u
			return true
		case *ast.SelectorExpr:
			// c.Request.Body / c.Request.FormFile / c.Request.ParseMultipartForm
			if node.Sel.Name == "Body" {
				if inner, okInner := node.X.(*ast.SelectorExpr); okInner && inner.Sel.Name == "Request" && isCtxExpr(inner.X, src) {
					readsRaw = true
					return false
				}
			}
			if node.Sel.Name == "FormFile" || node.Sel.Name == "ParseMultipartForm" || node.Sel.Name == "MultipartForm" {
				readsRaw = true
				return false
			}
			if node.Sel.Name == "GetRawData" && isCtxExpr(node.X, src) {
				readsRaw = true
				return false
			}
			if (node.Sel.Name == "PostForm" || node.Sel.Name == "GetPostForm" ||
				node.Sel.Name == "PostFormArray" || node.Sel.Name == "GetPostFormArray" ||
				node.Sel.Name == "Query" || node.Sel.Name == "DefaultQuery" ||
				node.Sel.Name == "FormValue") && isCtxExpr(node.X, src) {
				manual = true
				return false
			}
		}
		return true
	})
	return readsRaw, manual, unresolved
}

// isCtxExpr 判断表达式是否就是本函数声明的 gin.Context 形参。
func isCtxExpr(expr ast.Expr, src *funcSource) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && src.ctxParams[ident.Name]
}

func dtoFields(name, protocol string, types map[string]sourceStruct, visiting map[string]bool) ([]fieldSchema, error) {
	source, ok := types[name]
	if !ok || visiting[name] {
		return nil, fmt.Errorf("请求类型缺失或递归嵌入：%s", name)
	}
	visiting[name] = true
	defer delete(visiting, name)
	tagName := "json"
	if protocol == "form" || protocol == "multipart" || protocol == "query" {
		// Gin 的 query binder 与 form binder 共用 form tag：显式查询串端点
		// 的线上键名同样由 form tag 声明。
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
	//
	// 三种被委托方要分开：
	//   - 根本没有 binder（读取辅助函数）：不构成输入契约，跳过；
	//   - 明确登记了未知协议且解析不出确定形状：说明这条委托链自己也没定，
	//     不能把它悄悄滤掉再报「唯一的 form」；
	//   - 协议确定：参与唯一性判断。
	var bound []bodyBinding
	var unresolved []string
	for _, target := range delegatedTargets(src, index) {
		callee, okTarget := index.funcs[target]
		if !okTarget {
			continue
		}
		binding, okBinding := index.Bindings[callee.name]
		if !okBinding || binding.Protocol == "" || binding.Protocol == "unknown" {
			// 被委托方自己可能只是一个泛型骨架的调用者（bindX[T](c) 显式写实参）：
			// 先按调用方实参解析出真实 DTO，再退回沿链继续下探。
			if inner := genericSkeletonBinding(target, index); resolvedBinding(inner) {
				binding, okBinding = inner, true
			} else if inner := directGenericBinder(target, index); resolvedBinding(inner) {
				binding, okBinding = inner, true
			} else if inner := delegateBindingDepth(target, index, depth-1); resolvedBinding(inner) {
				binding, okBinding = inner, true
			}
		}
		switch {
		case resolvedBinding(binding):
			// 协议确定（含按路由方法确定的 Form/JSON），参与唯一性判断。
		case okBinding || binding.Protocol == "unknown":
			// 被委托方明确绑定了一个无法确定的输入形状：整条委托链不定。
			unresolved = append(unresolved, callee.name)
			continue
		default:
			// 无 binder 的读取辅助函数。
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
	if len(unresolved) > 0 {
		return bodyBinding{Protocol: "unknown", Reason: "委托链包含无法确定输入形状的分支：" +
			strings.Join(unresolved, ", ")}
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
	case *ast.IndexExpr:
		// 显式类型实参的泛型骨架调用（runWrite[SomeReq](c, ...)）。
		return calleeOf(src, node.X, index)
	case *ast.IndexListExpr:
		return calleeOf(src, node.X, index)
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
		// 显式转换函数作为实参（(*FormReq).CreateReq）：**表单类型**才是线上字段的属主，
		// 回调形参里的请求类型只是 service 侧形状，不能拿它当字段契约。
		if formDTO := conversionFuncDTO(call, src, index); formDTO != "" {
			// 骨架自身的登记说明协议（泛型体里的 DTO 已被清空），沿链只用来兜底。
			protocol, okProtocol := index.Bindings[target.name]
			if !okProtocol || protocol.Protocol == "" || protocol.Protocol == "unknown" {
				protocol = chainedBinderProtocol(target, index, 4)
			}
			if protocol.Protocol == "" || protocol.Protocol == "unknown" {
				if p := skeletonJSONProtocol(target); p != "" {
					protocol = bodyBinding{Protocol: p}
				}
			}
			if protocol.Protocol != "" && protocol.Protocol != "unknown" {
				resolved = bodyBinding{Protocol: protocol.Protocol, DTO: formDTO}
				return false
			}
		}
		// 回调实参：只有**骨架 binder 真正绑定的那个类型参数**的实参类型才是输入 DTO。
		// 泛型骨架常同时有输入 F 与输出 R：回调里第一个指针形参往往是被传给 service
		// 的 R。按「binder 绑定哪个类型参数」去挑，不能按「第一个指针形参」猜。
		if inputDTO := genericInputDTO(call, target, src, index); inputDTO != "" {
			skeleton, dto = target.name, inputDTO
			return false
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

// genericInputDTO 按「骨架上 binder 实际绑定的类型参数」从回调实参里取输入 DTO。
//
// 泛型写骨架常有两个类型参数：输入 F 与输出 R（runTransferWrite[F, R]、
// runMembershipWrite[F, R]）。回调里第一个指针形参往往是被交给 service 的 R，
// 拿它当字段契约会把输出类型的字段名当成线上表单字段。
//
// 这里先找骨架体里 binder 绑定的是哪个类型参数（`bindX[F](c)` 的实参、
// 或 `var req F; c.ShouldBindWith(&req, ...)` 的声明类型），再按骨架形参位置
// 找到对应回调、按回调形参位置取出该类型参数的实参类型。宁可不报也不猜。
func genericInputDTO(call *ast.CallExpr, target *funcSource, src *funcSource, index sourceIndex) string {
	inputParam := genericBoundTypeParam(index.funcs[target.name], index)
	if inputParam == "" {
		return ""
	}
	// 显式类型实参 f[F, R](...)：按类型参数声明顺序取被绑定那一个的实参。
	if dto := explicitTypeArgForParam(call, target, inputParam, src); dto != "" {
		return dto
	}
	// 骨架形参里第 i 个是回调时，回调的类型形参列表中哪个位置是 *inputParam。
	paramIndex := genericParamPosition(target, inputParam)
	if paramIndex < 0 {
		return ""
	}
	arg := callArgAt(call, paramIndex)
	lit, ok := arg.(*ast.FuncLit)
	if !ok || lit.Type == nil || lit.Type.Params == nil {
		return ""
	}
	litParamIndex := funcTypeParamPosition(target, paramIndex, inputParam)
	if litParamIndex < 0 {
		return ""
	}
	field := paramFieldAt(lit.Type.Params, litParamIndex)
	if field == nil {
		return ""
	}
	star, okStar := field.Type.(*ast.StarExpr)
	if !okStar {
		return ""
	}
	return typeName(star.X, src.pkg, src.imports)
}

// explicitTypeArgForParam 从显式泛型调用的类型实参里取指定类型参数对应的类型；
// 不是显式调用、或位置对不上时返回空，交给回调形参推断。
func explicitTypeArgForParam(call *ast.CallExpr, target *funcSource, generic string, src *funcSource) string {
	var args []ast.Expr
	switch fun := call.Fun.(type) {
	case *ast.IndexExpr:
		args = []ast.Expr{fun.Index}
	case *ast.IndexListExpr:
		args = fun.Indices
	default:
		return ""
	}
	for i, name := range target.genericOrder {
		if name != generic || i >= len(args) {
			continue
		}
		return typeName(args[i], src.pkg, src.imports)
	}
	return ""
}

// genericBoundTypeParam 找骨架体里 binder 绑定的类型参数名；找不到返回空。
func genericBoundTypeParam(src *funcSource, index sourceIndex) string {
	if src == nil || len(src.generics) == 0 {
		return ""
	}
	found := ""
	ast.Inspect(src.body, func(n ast.Node) bool {
		if found != "" {
			return false
		}
		call, okCall := n.(*ast.CallExpr)
		if !okCall {
			return true
		}
		// 形式一：bindX[F](c) —— 只有**被调函数本身是 binder** 时，类型实参才是
		// 被绑定的类型参数。骨架体里可能先出现无关泛型调用（warm[R]()），
		// 把它当成 binder 会把输出类型认成输入 DTO。
		if isBinderGenericCall(call, src, index) {
			switch fun := call.Fun.(type) {
			case *ast.IndexExpr:
				if ident, okIdent := fun.Index.(*ast.Ident); okIdent && src.generics[ident.Name] {
					found = ident.Name
					return false
				}
			case *ast.IndexListExpr:
				for _, arg := range fun.Indices {
					if ident, okIdent := arg.(*ast.Ident); okIdent && src.generics[ident.Name] {
						found = ident.Name
						return false
					}
				}
			}
		}
		// 形式二：var req F; c.ShouldBindWith(&req, ...)
		method, okSel := call.Fun.(*ast.SelectorExpr)
		if !okSel || len(call.Args) == 0 {
			return true
		}
		switch method.Sel.Name {
		case "ShouldBindWith", "ShouldBindJSON", "ShouldBind", "Bind":
		default:
			return true
		}
		if expr := declaredType(call.Args[0], map[*ast.Object]bool{}); expr != nil {
			if ident, okIdent := expr.(*ast.Ident); okIdent && src.generics[ident.Name] {
				found = ident.Name
			}
		}
		return true
	})
	return found
}

// genericParamPosition 返回骨架形参中「回调类型里含该类型参数」的位置；没有返回 -1。
func genericParamPosition(src *funcSource, generic string) int {
	for i, decl := range src.paramDecls {
		if typeMentionsGeneric(decl.typ, generic) {
			return i
		}
	}
	return -1
}

// funcTypeParamPosition 返回骨架第 paramIndex 个形参类型（func 类型）里，
// 该类型参数出现在第几个形参位置；拿不到返回 -1。
func funcTypeParamPosition(src *funcSource, paramIndex int, generic string) int {
	if paramIndex < 0 || paramIndex >= len(src.paramDecls) {
		return -1
	}
	funcType, ok := src.paramDecls[paramIndex].typ.(*ast.FuncType)
	if !ok || funcType.Params == nil {
		return -1
	}
	index := 0
	for _, field := range funcType.Params.List {
		count := len(field.Names)
		if count == 0 {
			count = 1
		}
		if typeMentionsGeneric(field.Type, generic) {
			return index
		}
		index += count
	}
	return -1
}

// typeMentionsGeneric 判断类型表达式里是否出现给定类型参数（含指针/切片/映射等）。
func typeMentionsGeneric(expr ast.Expr, generic string) bool {
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		if found {
			return false
		}
		if ident, ok := n.(*ast.Ident); ok && ident.Name == generic {
			found = true
			return false
		}
		return true
	})
	return found
}

// callArgAt 取按源码位置的第 index 个实参（多值实参不展开）。
func callArgAt(call *ast.CallExpr, index int) ast.Expr {
	if index < 0 || index >= len(call.Args) {
		return nil
	}
	return call.Args[index]
}

// paramFieldAt 取函数类型形参列表按位置展开后的字段。
func paramFieldAt(params *ast.FieldList, index int) *ast.Field {
	if params == nil {
		return nil
	}
	pos := 0
	for _, field := range params.List {
		count := len(field.Names)
		if count == 0 {
			count = 1
		}
		if index < pos+count {
			return field
		}
		pos += count
	}
	return nil
}

// conversionFuncDTO 取泛型骨架调用里「显式转换函数」的接收者类型。
//
//	runMembershipWrite(c, (*TierFormReq).CreateReq, call, success)
//
// 这类写法把「表单 → service 请求」的转换也交给骨架；线上字段由**表单类型**
// 决定（TierFormReq 的 form tag），绝不是转换后 CreateTierReq 的字段名。
// 找不到就返回空，交给回调形参推断。
func conversionFuncDTO(call *ast.CallExpr, src *funcSource, index sourceIndex) string {
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
			if _, okType := index.Types[dto]; okType {
				return dto
			}
		}
	}
	return ""
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
//
// 骨架体可能有多条分支各自 binder（bindJSON[T] 与 bindForm[T] 二选一）：
// 只取第一个会把「两条协议」报成确定契约。这里收集**所有**分支的协议，
// 恰有一种才确定；出现两种及以上如实报未检查。
func chainedBinderProtocol(src *funcSource, index sourceIndex, depth int) bodyBinding {
	if src == nil || depth <= 0 {
		return bodyBinding{}
	}
	protocols := map[string]bool{}
	if protocol := skeletonJSONProtocol(src); protocol != "" {
		protocols[protocol] = true
	}
	ast.Inspect(src.body, func(n ast.Node) bool {
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
			protocols[binding.Protocol] = true
			return true
		}
		if protocol := chainedBinderProtocol(target, index, depth-1); protocol.Protocol != "" &&
			protocol.Protocol != "unknown" {
			protocols[protocol.Protocol] = true
		}
		return true
	})
	switch len(protocols) {
	case 0:
		return bodyBinding{}
	case 1:
		for protocol := range protocols {
			return bodyBinding{Protocol: protocol}
		}
	}
	names := make([]string, 0, len(protocols))
	for protocol := range protocols {
		names = append(names, protocol)
	}
	sort.Strings(names)
	return bodyBinding{Protocol: "unknown", Reason: "泛型写骨架内含多种请求协议：" + strings.Join(names, ", ")}
}

// isBinderGenericCall 判断一次泛型调用（f[T](c)）的被调方本身是不是 binder。
//
// 骨架体里可能先出现无关的泛型调用（warm[R]()），把它当 binder 会把输出类型
// 认成输入 DTO。只有被调方自身能解析出确定的输入协议时才算。
func isBinderGenericCall(call *ast.CallExpr, src *funcSource, index sourceIndex) bool {
	var name string
	switch fun := call.Fun.(type) {
	case *ast.IndexExpr:
		if ident, okIdent := fun.X.(*ast.Ident); okIdent {
			name = ident.Name
		}
	case *ast.IndexListExpr:
		if ident, okIdent := fun.X.(*ast.Ident); okIdent {
			name = ident.Name
		}
	default:
		return false
	}
	if name == "" {
		return false
	}
	target, ok := index.funcs[src.pkg+"."+name]
	if !ok || len(target.generics) == 0 {
		return false
	}
	if binding, okBinding := index.Bindings[target.name]; okBinding && resolvedBinding(binding) {
		return true
	}
	return resolvedBinding(chainedBinderProtocol(target, index, 4))
}

// delegatedUnknownBranch 检查处理器是否还有一条委托分支无法确定输入形状。
//
// 处理器常既有本地显式 binder，又把上下文转交给辅助函数；只看本地 binder
// 就把端点报成确定契约，会把「另一条分支 ShouldBind」的端点洗白成已检查。
// 沿委托链有界下探，命中的第一处未确定目标即返回。
func delegatedUnknownBranch(name string, index sourceIndex) (string, bool) {
	return delegatedUnknownDepth(name, index, 4)
}

func delegatedUnknownDepth(name string, index sourceIndex, depth int) (string, bool) {
	src, ok := index.funcs[name]
	if !ok || depth <= 0 {
		return "", false
	}
	for _, target := range delegatedTargets(src, index) {
		callee, okTarget := index.funcs[target]
		if !okTarget {
			continue
		}
		binding, okBinding := index.Bindings[callee.name]
		switch {
		case okBinding && resolvedBinding(binding):
			continue
		case okBinding && binding.Protocol == "unknown":
			// 登记为未知协议：除非能按调用方实参重新解析出确定 DTO，否则整条链不定。
			if resolvedBinding(genericSkeletonBinding(target, index)) ||
				resolvedBinding(directGenericBinder(target, index)) {
				continue
			}
			return callee.name, true
		}
		// 本函数没有登记 binder：可能是读取辅助函数，也可能是再一层转发。
		if hit, bad := delegatedUnknownDepth(target, index, depth-1); bad {
			return hit, true
		}
	}
	return "", false
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
//	binding.ParseForm[T](c) —— 显式普通表单（FormPost，只读 body）
//	binding.ParseJSON[T](c) —— 显式 JSON
//	binding.Query[T](c)     —— 显式查询串
//	binding.Form[T](c)      —— 旧入口，c.ShouldBind 按 Content-Type 自适应，
//	                           还会把 query 并进表单；输入协议不能由函数名认定
//	binding.JSON[T](c)      —— 同上，无 Content-Type 时才走 JSON
//
// 自适应入口只登记「无法确定协议」，不按函数名猜 form/json：猜错会让模板侧的
// 协议对账拿一个并不存在的确定性去放行。
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
	dto := typeName(fun.Index, pkg, imports)
	if dto == "" {
		return bodyBinding{Protocol: "unknown", Reason: "pkg/binding 泛型调用的类型实参不是具名类型"}, true
	}
	switch sel.Sel.Name {
	case "ParseForm":
		return bodyBinding{DTO: dto, Protocol: "form"}, true
	case "ParseJSON":
		return bodyBinding{DTO: dto, Protocol: "json"}, true
	case "Query":
		return bodyBinding{DTO: dto, Protocol: "query"}, true
	case "Form", "JSON":
		// 兼容入口：按 Content-Type 在 JSON 与表单之间选择，本身不声明唯一协议。
		// 如实报未检查；声明协议的端点用 ParseForm / ParseJSON / Query，
		// 或直接写原生 ShouldBindWith / ShouldBindJSON。
		return bodyBinding{Protocol: "unknown", Reason: "pkg/binding." + sel.Sel.Name +
			" 是兼容入口，输入协议不由函数名确定；声明协议请用 ParseForm / ParseJSON / Query 或原生显式 binder"}, true
	default:
		return bodyBinding{}, false
	}
}
