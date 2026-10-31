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
	DTO string `json:"dto"`

	Handler string `json:"handler"`

	Tables []tableSchema `json:"tables,omitempty"`
	Fields []fieldSchema `json:"fields"`
}

type tableSchema struct {
	Name string `json:"name"`

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

func runtimeWriteRoutes() ([]route, error) {
	cmd := exec.Command("go", "test", "./internal/routers/", "-run", "TestDumpRoutesForAudit", "-count=1", "-v")
	cmd.Env = append(os.Environ(), "WP_DUMP_ROUTES=1")
	raw, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("导出运行时路由表失败（需要数据库；使用根目录 config.yaml 或显式 GOWP_AUDIT_CONFIG）：%w", err)
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

func matchHandler(path, mod string, handlerDTO map[string]string) string {
	segs := strings.Split(strings.Trim(path, "/"), "/")
	if len(segs) < 3 {
		return ""
	}

	if alias, ok := exactHandlerAliases[path]; ok {
		if id, hit := resolveAliasHandler(alias, mod, segs[1], handlerDTO); hit {
			return id
		}
	}
	rest := segs[2:]
	if segs[1] != mod {

		rest = segs[1:]
	}

	idsOf := func(name string) []string {
		if mod == "" {
			return nil
		}
		return []string{mod + ":" + name}
	}
	verbs := map[string]string{
		"create": "Create", "update": "Update", "delete": "Delete",
		"save": "Save", "add": "Add", "remove": "Remove",
		"bulk-delete": "BulkDelete", "bulk-update": "BulkUpdate", "bulk-status": "BulkStatus", "bulk-unlock": "BulkUnlock",
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

	if verb == "" {
		for _, id := range idsOf(strings.Join(camel, "")) {
			if _, ok := handlerDTO[id]; ok {
				return id
			}
		}
	}

	if verb != "" {
		var cands []string
		if len(camel) > 0 {
			cands = append(cands, verb+strings.Join(camel, ""))
			cands = append(cands, verb+camel[len(camel)-1])
			if strings.HasPrefix(verb, "Bulk") {
				cands = append(cands, "Bulk"+strings.Join(camel, "")+strings.TrimPrefix(verb, "Bulk"))
			}
			if segs[1] != mod {
				cands = append(cands, strings.Join(camel, "")+verb)
			}
		}

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

	var hits []string
	for name := range handlerDTO {

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

var exactHandlerAliases = map[string]string{
	"/api/order/shipment/create": "ShipOrder",
}

func resolveAliasHandler(name, mod, routeModule string, handlerDTO map[string]string) (string, bool) {
	if mod == "" || name == "" {
		return "", false
	}

	if routeModule != mod {
		return "", false
	}
	id := mod + ":" + name
	dto, ok := handlerDTO[id]
	if !ok {
		return "", false
	}
	if !strings.HasPrefix(dto, mod+"dto.") {
		return "", false
	}
	return id, true
}

func moduleOfRoutePath(path string) string {
	segs := strings.Split(strings.Trim(path, "/"), "/")
	if len(segs) < 2 {
		return ""
	}
	if segs[1] == "role" {
		return "admin"
	}
	if segs[1] == "customer" {
		return "user"
	}
	return segs[1]
}

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

			name := fn.Name.Name
			mod := moduleOfPath(path)
			key := name
			if mod != "" {
				key = mod + ":" + name
			}

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

		if indexed, ok := call.Fun.(*ast.IndexExpr); ok {

			_, localHelper := indexed.X.(*ast.Ident)
			if sel, ok := indexed.Index.(*ast.SelectorExpr); ok && localHelper {
				if pkg, ok := sel.X.(*ast.Ident); ok && strings.HasSuffix(pkg.Name, "dto") {
					found = pkg.Name + "." + sel.Sel.Name
					return false
				}
			}
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

func moduleOfPath(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i, p := range parts {
		if p == "module" && i+2 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

type dtoInfo struct {
	Fields []fieldSchema
	Tables []tableSchema
}

var scalarTypes = map[string]bool{
	"string": true, "int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"bool": true, "float32": true, "float64": true, "byte": true, "rune": true,
	"RawMessage": true, "JSONTime": true,
}

func scanDTOs() (map[string]dtoInfo, error) {
	return scanDTOsFrom("internal")
}

func scanDTOsFrom(root string) (map[string]dtoInfo, error) {
	types := map[string]*ast.StructType{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gen.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if st, ok := ts.Type.(*ast.StructType); ok {
					types[f.Name.Name+"."+ts.Name.Name] = st
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := map[string]dtoInfo{}
	for name := range types {
		info := dtoFields(name, types, map[string]bool{})
		if len(info.Fields) > 0 {
			out[name] = info
		}
	}
	return out, nil
}

func dtoFields(name string, types map[string]*ast.StructType, visiting map[string]bool) dtoInfo {
	st := types[name]
	if st == nil || st.Fields == nil || visiting[name] {
		return dtoInfo{}
	}
	visiting[name] = true
	defer delete(visiting, name)
	pkg := strings.SplitN(name, ".", 2)[0]
	var info dtoInfo
	for _, fld := range st.Fields.List {
		tag := map[string]string{}
		if fld.Tag != nil {
			tag = parseStructTag(fld.Tag.Value)
		}
		formName := strings.Split(tag["form"], ",")[0]
		jsonName := strings.Split(tag["json"], ",")[0]
		if len(fld.Names) == 0 && formName == "" && jsonName == "" {
			expr := fld.Type
			optional := false
			if star, ok := expr.(*ast.StarExpr); ok {
				expr = star.X
				optional = !requiredField(tag["binding"])
			}
			embedded := ""
			switch typ := expr.(type) {
			case *ast.Ident:
				embedded = pkg + "." + typ.Name
			case *ast.SelectorExpr:
				if p, ok := typ.X.(*ast.Ident); ok {
					embedded = p.Name + "." + typ.Sel.Name
				}
			}
			child := dtoFields(embedded, types, visiting)
			if optional || strings.Contains(","+tag["binding"]+",", ",omitempty,") {
				for i := range child.Fields {
					child.Fields[i].Required = false
				}
			}
			info.Fields = append(info.Fields, child.Fields...)
			info.Tables = append(info.Tables, child.Tables...)
			continue
		}
		fieldName := formName
		if fieldName == "" || fieldName == "-" {
			fieldName = jsonName
		}
		if fieldName == "" || fieldName == "-" {
			continue
		}
		if shape := collectionShape(fld.Type); shape != "" {
			info.Tables = append(info.Tables, tableSchema{Name: fieldName, Shape: shape})
		}
		info.Fields = append(info.Fields, fieldSchema{Name: fieldName, Required: requiredField(tag["binding"])})
	}
	return info
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

func collectionShape(expr ast.Expr) string {

	ptr := false
	switch t := expr.(type) {
	case *ast.StarExpr:
		ptr = true
		expr = t.X
	}
	switch t := expr.(type) {
	case *ast.ArrayType:

		if isScalarExpr(t.Elt) {
			return ""
		}
		return "slice-struct"
	case *ast.MapType:
		return "map"
	case *ast.Ident:
		if scalarTypes[t.Name] {
			if ptr {
				return ""
			}
			return ""
		}

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

func parseStructTag(lit string) map[string]string {
	out := map[string]string{}
	raw, err := strconv.Unquote(lit)
	if err != nil {
		return out
	}
	tag := reflect.StructTag(raw)
	for _, key := range []string{"form", "json", "binding"} {
		if value, ok := tag.Lookup(key); ok {
			out[key] = value
		}
	}
	return out
}
