package producthttp

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	productcontract "go_wp/internal/module/product/contract"
)

func readerIfaceType() reflect.Type {
	return reflect.TypeOf((*productcontract.ProductPageReader)(nil)).Elem()
}

func TestPageHandleReaderIsConstructorRequired(t *testing.T) {
	ctor := reflect.TypeOf(NewProductPageHandle)
	if ctor.NumIn() != 2 {
		t.Fatalf("NewProductPageHandle 应有 2 个形参（products / reader），实际 %d 个", ctor.NumIn())
	}
	if got, want := ctor.In(1), readerIfaceType(); got != want {
		t.Fatalf("第 2 个形参应为 %v，实际 %v（读模型不再是构造实参 = 漏接退化成运行期）", want, got)
	}
	if ctor.NumOut() != 1 {
		t.Fatalf("NewProductPageHandle 应只返回 1 个值，实际 %d 个", ctor.NumOut())
	}
}

func TestSetupProductRoutesReturnsReader(t *testing.T) {
	routes := reflect.TypeOf(SetupProductRoutes)
	if routes.NumOut() != 3 {
		t.Fatalf("SetupProductRoutes 应返回 (ProductService, ProductPageReader, *ProductHandle) 三个值，实际 %d 个", routes.NumOut())
	}
	if got, want := routes.Out(0), reflect.TypeOf((*productcontract.ProductService)(nil)).Elem(); got != want {
		t.Fatalf("第 1 个返回值应为 %v，实际 %v", want, got)
	}
	if got, want := routes.Out(1), readerIfaceType(); got != want {
		t.Fatalf("第 2 个返回值应为读模型 %v，实际 %v（装配层拿不到读模型 = 页面装配只能断言）", want, got)
	}
	if got, want := routes.Out(2), reflect.TypeOf((*ProductHandle)(nil)); got != want {
		t.Fatalf("第 3 个返回值应为实例注入目标 %v，实际 %v", want, got)
	}
	pages := reflect.TypeOf(SetupProductPages)
	// 形参顺序：reg / pages / api / writeHandle / products / reader / projects …
	if pages.NumIn() < 7 {
		t.Fatalf("SetupProductPages 形参不足（reg / pages / api / writeHandle / products / reader / projects …），实际 %d 个", pages.NumIn())
	}
	if got, want := pages.In(5), readerIfaceType(); got != want {
		t.Fatalf("SetupProductPages 第 6 个形参应为读模型 %v，实际 %v", want, got)
	}
}

func TestNoRuntimeReaderAssertionInControllers(t *testing.T) {
	const forbidden = ".(productcontract.ProductPageReader)"
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("读目录失败：%v", err)
	}
	seen := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		seen++
		src, rerr := os.ReadFile(filepath.Join(".", name))
		if rerr != nil {
			t.Fatalf("读 %s 失败：%v", name, rerr)
		}
		if strings.Contains(string(src), forbidden) {
			t.Errorf("%s 里仍有读模型运行期断言（%s）：漏接会退化成运行期 500，应改 h.reader", name, forbidden)
		}
	}
	if seen == 0 {
		t.Fatal("一个生产文件都没扫到：判据失效（目录 / 后缀判定写错了）")
	}
}
