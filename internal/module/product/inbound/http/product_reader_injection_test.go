package producthttp

// product_reader_injection_test.go — 「页面读模型由装配期注入」这条契约的守门判据。
//
// 背景（本批之前）：控制器持有的是 ProductService，读模型不在它的能力面上，于是 12 处处理器
// 各自写 reader, ok := h.products.(productcontract.ProductPageReader) 兜底 —— 漏接在编译期
// 与门禁里都看不见，只有运营点开那一页才得到 500。现在读模型是**构造实参**
//（ProductPageHandle.reader）与 SetupProductRoutes 的**第二个返回值**：漏接 = 编译错误。
//
// 本文件钉三件事：① 构造函数第 2 个形参就是读模型（删掉 / 挪位 / 换成 ProductService 都变红）；
// ② 装配链两端（接口面产出 → 页面面收下）的类型一致；③ 控制器源码里不再出现运行期断言。
//
// 判据口径：不用计数当判据 —— 第 ③ 条断的是「这段源码文本不存在」，出现即说明兜底路径回来了。

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	productcontract "go_wp/internal/module/product/contract"
)

// readerIfaceType 读模型接口的反射类型（下面三处共用同一份，避免各写一遍 T 参数）。
func readerIfaceType() reflect.Type {
	return reflect.TypeOf((*productcontract.ProductPageReader)(nil)).Elem()
}

// TestPageHandleReaderIsConstructorRequired 读模型是构造函数的第 2 个形参，类型必须是读模型接口。
func TestPageHandleReaderIsConstructorRequired(t *testing.T) {
	ctor := reflect.TypeOf(NewProductPageHandle)
	if ctor.NumIn() != 3 {
		t.Fatalf("NewProductPageHandle 应有 3 个形参（products / reader / projects），实际 %d 个", ctor.NumIn())
	}
	if got, want := ctor.In(1), readerIfaceType(); got != want {
		t.Fatalf("第 2 个形参应为 %v，实际 %v（读模型不再是构造实参 = 漏接退化成运行期）", want, got)
	}
	if ctor.NumOut() != 1 {
		t.Fatalf("NewProductPageHandle 应只返回 1 个值，实际 %d 个", ctor.NumOut())
	}
}

// TestSetupProductRoutesReturnsReader 接口面装配产出读模型，页面面装配把它收下（同一类型）。
func TestSetupProductRoutesReturnsReader(t *testing.T) {
	routes := reflect.TypeOf(SetupProductRoutes)
	if routes.NumOut() != 2 {
		t.Fatalf("SetupProductRoutes 应返回 (ProductService, ProductPageReader) 两个值，实际 %d 个", routes.NumOut())
	}
	if got, want := routes.Out(0), reflect.TypeOf((*productcontract.ProductService)(nil)).Elem(); got != want {
		t.Fatalf("第 1 个返回值应为 %v，实际 %v", want, got)
	}
	if got, want := routes.Out(1), readerIfaceType(); got != want {
		t.Fatalf("第 2 个返回值应为读模型 %v，实际 %v（装配层拿不到读模型 = 页面装配只能断言）", want, got)
	}
	pages := reflect.TypeOf(SetupProductPages)
	if pages.NumIn() < 4 {
		t.Fatalf("SetupProductPages 形参不足（pages / products / reader / projects …），实际 %d 个", pages.NumIn())
	}
	if got, want := pages.In(2), readerIfaceType(); got != want {
		t.Fatalf("SetupProductPages 第 3 个形参应为读模型 %v，实际 %v", want, got)
	}
}

// TestNoRuntimeReaderAssertionInControllers 控制器里不许再出现读模型的运行期断言。
//
// 出现即说明「装配漏接 → 运行期 500 兜底」这条路径回来了：那种漏接不编译报错、门禁也不红，
// 只会在运营点开某一页时暴露。处理器改读 h.reader 才是唯一形态。
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
