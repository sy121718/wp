package pagehttp

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"testing"

	"strings"

	pageenums "go_wp/internal/module/page/enums"
	"go_wp/pkg/utils"
)

func TestPageRowUpdatedAtCarriesTimeFact(t *testing.T) {
	field, ok := reflect.TypeOf(PageRowResp{}).FieldByName("UpdatedAt")
	if !ok || field.Type != reflect.TypeOf(utils.JSONTime{}) {
		t.Fatalf("UpdatedAt must carry JSONTime, got %v", field.Type)
	}
}

// Scan the real source: adding an unused presentation helper is also a regression.
func TestPagePresentationHasNoBackendUIHelpers(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "pages_handle.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		switch fn.Name.Name {
		case "translationOriginText", "reuseHint":
			t.Errorf("UI helper remains in backend: %s", fn.Name.Name)
		}
	}
}

func TestTranslationProjectionCarriesFacts(t *testing.T) {
	typ := reflect.TypeOf(TranslationRowResp{})
	if _, ok := typ.FieldByName("ReuseHint"); ok {
		t.Fatal("ReuseHint is UI text")
	}
	field, ok := typ.FieldByName("ReusePaths")
	if !ok || field.Type != reflect.TypeOf([]string{}) {
		t.Fatal("ReusePaths must carry path facts")
	}
	for _, origin := range []string{pageenums.TranslationOriginHeader, pageenums.TranslationOriginFooter, pageenums.TranslationOriginBlock} {
		if strings.Contains(origin, ".") {
			t.Fatalf("Origin must be a domain fact, got UI key %q", origin)
		}
	}
}
