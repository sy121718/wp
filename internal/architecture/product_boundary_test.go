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

var productPresentationFields = map[string]bool{
	"Tone": true, "Class": true, "StatusClass": true,
	"StatusLabel": true, "TypeLabel": true, "KindLabel": true,
	"LabelKey": true, "LabelFallback": true, "TextKey": true,
	"LabelText": true, "DisplayText": true, "DisplayName": true,
	"HiddenFields": true, "SerpTitle": true, "SerpDesc": true, "SerpURL": true,
	"HasSerpTitle": true, "HasSerpDesc": true, "HasSerpURL": true,
}

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

var translateSelectors = map[string]bool{
	"Translate": true, "TranslateFunc": true, "FillTranslate": true, "TranslateArgs": true,
}

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

var productControllerI18nBudget = map[string]string{}

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

func TestProductBusinessLayersDoNotImportScoreView(t *testing.T) {
	root := repoRoot(t)
	for _, dir := range []string{"dto", "service", "inbound/http"} {
		for _, pkg := range parseDir(t, filepath.Join(root, "internal/module/product", dir)) {
			for name, file := range pkg {
				for _, spec := range file.Imports {
					if spec.Path.Value == `"go_wp/internal/seo/scoreview"` {
						t.Errorf("%s: 商品只消费评分事实，展示归前端", name)
					}
				}
			}
		}
	}
}
