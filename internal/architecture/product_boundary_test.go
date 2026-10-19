package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// 商品模块作为「范例模块」的边界判据（PLAN.md 阶段 2 的商品落地部分）。
//
// 这些判据在其他模块还是存量债，在商品模块是**零容忍**：它就是「改完之后长什么样」的样品，
// 样品一旦回退，后面十个模块照抄的就是回退后的样子。

// 展示字段名：出现即说明「给用户看的文本/形状」在 Go 里成形了。
var productPresentationFields = map[string]bool{
	"Tone": true, "Class": true, "StatusClass": true,
	"StatusLabel": true, "TypeLabel": true, "KindLabel": true,
	"LabelKey": true, "LabelFallback": true, "TextKey": true,
	"LabelText": true, "DisplayText": true, "DisplayName": true,
}

// TestProductDTOHasNoPresentationFields dto 只放数据：§展示字段与页面状态方法都不许有。
func TestProductDTOHasNoPresentationFields(t *testing.T) {
	root := repoRoot(t)
	for _, pkg := range parseDir(t, filepath.Join(root, "internal/module/product/dto")) {
		for name, file := range pkg {
			ast.Inspect(file, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.StructType:
					for _, f := range node.Fields.List {
						for _, ident := range f.Names {
							if productPresentationFields[ident.Name] {
								t.Errorf("%s: dto 字段 %s 是展示形状 —— 数据给原值，文本与配色归模板", name, ident.Name)
							}
						}
					}
				case *ast.FuncDecl:
					if node.Recv != nil && (node.Name.Name == "PageStatus" || node.Name.Name == "PageNoStore") {
						t.Errorf("%s: dto 方法 %s 把页面状态做进了数据类型 —— 页面出口只认 pkg/view", name, node.Name.Name)
					}
				}
				return true
			})
		}
	}
}

// TestProductTimeFieldsUseTimeTypes dto 的时间字段不许是字符串（时间形状只能有 time.Time / utils.JSONTime 两种）。
func TestProductTimeFieldsUseTimeTypes(t *testing.T) {
	root := repoRoot(t)
	for _, pkg := range parseDir(t, filepath.Join(root, "internal/module/product/dto")) {
		for name, file := range pkg {
			ast.Inspect(file, func(n ast.Node) bool {
				st, ok := n.(*ast.StructType)
				if !ok {
					return true
				}
				for _, f := range st.Fields.List {
					for _, ident := range f.Names {
						if !strings.HasSuffix(ident.Name, "At") {
							continue
						}
						if typ, ok := f.Type.(*ast.Ident); ok && typ.Name == "string" {
							t.Errorf("%s: 时间字段 %s 是字符串 —— 用 time.Time（页面）或 utils.JSONTime（接口，毫秒）", name, ident.Name)
						}
					}
				}
				return true
			})
		}
	}
}

// translateSelectors 取词调用（pkg/i18n 里混着内容翻译存储，所以不能按 import 判，要看具体调用）。
var translateSelectors = map[string]bool{
	"Translate": true, "TranslateFunc": true, "FillTranslate": true, "TranslateArgs": true,
}

// TestProductServiceDoesNotTranslate service 不做取词：文案在声明处（enums.Err / facing.New），
// 取词发生在出口（pkg/response）与模板。
func TestProductServiceDoesNotTranslate(t *testing.T) {
	root := repoRoot(t)
	for _, pkg := range parseDir(t, filepath.Join(root, "internal/module/product/service")) {
		for name, file := range pkg {
			ast.Inspect(file, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok || !translateSelectors[sel.Sel.Name] {
					return true
				}
				if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "i18n" {
					t.Errorf("%s: service 里调了 i18n.%s —— 业务层只给 key + 兜底，取词在出口与模板", name, sel.Sel.Name)
				}
				return true
			})
		}
	}
}

// productControllerI18nBudget 控制器里还能直接取词的文件（登记的是「为什么现在还留着」）。
// 双向判定：登记了不再命中同样失败。
var productControllerI18nBudget = map[string]string{
	"product_translations_handle.go": "多语言保存回执是 {n} 具名占位符（tf 只有 %s），要 FillTranslate 才能取；随回执改由 service 给结构化事实后删",
}

// TestProductControllersAvoidTranslating 控制器不取词：响应文案由 pkg/response 取一次，界面文案由模板取。
func TestProductControllersAvoidTranslating(t *testing.T) {
	root := repoRoot(t)
	got := map[string]bool{}
	for _, pkg := range parseDir(t, filepath.Join(root, "internal/module/product/inbound/http")) {
		for name, file := range pkg {
			ast.Inspect(file, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok || !translateSelectors[sel.Sel.Name] {
					return true
				}
				if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "i18n" {
					got[filepath.Base(name)] = true
				}
				return true
			})
		}
	}
	for file, reason := range productControllerI18nBudget {
		if !got[file] {
			t.Errorf("%s: 登记了「还在取词」但已不再命中 —— 删掉 productControllerI18nBudget 里的条目（%s）", file, reason)
		}
		delete(got, file)
	}
	for file := range got {
		t.Errorf("%s: 控制器不取词 —— 响应文案走 pkg/response，界面文案走模板", file)
	}
}

// parseDir 解析目录下的非测试 Go 文件（键是文件名，值是 AST）。
func parseDir(t *testing.T, dir string) map[string]map[string]*ast.File {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(info fs.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("解析 %s 失败: %v", dir, err)
	}
	out := map[string]map[string]*ast.File{}
	for name, pkg := range pkgs {
		files := map[string]*ast.File{}
		for file, f := range pkg.Files {
			files[file] = f
		}
		out[name] = files
	}
	return out
}
