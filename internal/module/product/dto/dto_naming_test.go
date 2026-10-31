package productdto

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var dtoValueObjects = map[string]string{
	"FieldKind":         "绑定原语的类型事实（product_form_req.go）",
	"FieldValueError":   "绑定原语的错误类型（product_form_req.go）",
	"BulkRejectedError": "错误包装：整批拒绝时把可定位条目带出 service（既不是入参也不是回执）",
}

var dtoFileName = regexp.MustCompile(`^[a-z0-9_]+_(req|resp)\.go$`)

func TestDTOFileNamesAreReqOrResp(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("读取 dto 目录失败: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		if !dtoFileName.MatchString(name) {
			t.Errorf("dto 目录只允许 <域>_req.go / <域>_resp.go，发现 %s", name)
		}
	}
}

func TestDTOTypeNamesAreReqOrResp(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("解析 dto 包失败: %v", err)
	}
	seen := map[string]bool{}
	for _, pkg := range pkgs {
		for path, file := range pkg.Files {
			_ = path
			for _, decl := range file.Decls {
				gd, ok := decl.(*ast.GenDecl)
				if !ok || gd.Tok != token.TYPE {
					continue
				}
				for _, spec := range gd.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok || !ts.Name.IsExported() {
						continue
					}
					name := ts.Name.Name
					seen[name] = true
					if strings.HasSuffix(name, "Req") || strings.HasSuffix(name, "Resp") {
						continue
					}
					if _, allowed := dtoValueObjects[name]; allowed {
						continue
					}
					t.Errorf("%s:%d dto 导出类型 %s 必须以 Req 或 Resp 结尾（共用形状请登记到 dtoValueObjects 并写明理由）",
						filepath.Base(fset.Position(ts.Pos()).Filename), fset.Position(ts.Pos()).Line, name)
				}
			}
		}
	}
	for name, why := range dtoValueObjects {
		if !seen[name] {
			t.Errorf("dtoValueObjects 登记了 %s（%s），但 dto 包里已无此类型 —— 请删掉这条登记", name, why)
		}
	}
}
