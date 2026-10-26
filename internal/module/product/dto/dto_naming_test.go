// dto_naming_test.go — dto 目录的形状门禁：文件成对、类型名带后缀。
//
// 口径（2026-10-18 用户拍板，原话「dto那边只有req和resp这两个结尾的，分别是接手和返回，
// 然后的话，只能有这里两个文件，你写这么多干嘛」）：
//
//  1. 目录里**只能有** `<域>_req.go` / `<域>_resp.go` 两种非测试文件 —— 一个域最多两份，
//     入参进 req、回执进 resp。原先的 product_view.go / product_bulk.go / product_row_view.go /
//     product_variant_save.go / product_variant.go 都是同一份域被拆成了别的名字，读的人要先
//     「这个文件是进还是出」才能定位，所以一并按这条规则收口。
//  2. **导出类型一律以 Req / Resp 结尾**，除了下面 allowlist 里的三类值对象 —— 它们既不是
//     入参也不是回执，而是**两个方向共用的形状**（持久化形状、表单回填值、绑定原语），
//     硬加后缀会让「这是存进库里的东西」这个事实丢失。
//
// allowlist 是**双向**的：登记了却不再存在同样判失败（防止它退化成注释），
// 新增值对象必须显式登记并在此写明理由。
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

// dtoValueObjects 允许不带 Req / Resp 后缀的导出类型（既进请求也进回执的共用形状）。
var dtoValueObjects = map[string]string{
	"PricingForm":       "表单回填值：试算后原样回填的取值，由 Req.Form() 产生",
	"AttrValueFormRow":  "表单行：属性值行，请求提交与页面回显是同一形状",
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
