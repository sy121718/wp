package productservice_test

// product_source_brand_error_test.go — 品牌查询失败必须传播，不能吞掉。
//
// 审查 P1：localizeRelated 里分类 / 标签 / 属性组都是 `if err != nil { return nil, err }`，
// 只有品牌写成 `if berr == nil { ... }` —— 把所有错误都吞掉，包括查库失败与 RLS 作用域
// 未设导致的静默 0 行。后果是「品牌名突然全空了」没有任何信号：
// 构建成功、产物生成、页面只是少了一块。
//
// 这里的判据是「坏掉的连接必须让调用失败」，而不是「品牌不存在也要失败」——
// 后者是正常的引用失效，构建应当照常进行。

import (
	"os"
	"strings"
	"testing"
)

// TestLocalizeRelatedDoesNotSwallowBrandErrors 品牌查询失败必须传播。
//
// 审查 P1：localizeRelated 里分类 / 标签 / 属性组都是 `if err != nil { return nil, err }`，
// 只有品牌写成 `if berr == nil { ... }` —— 把所有错误都吞掉，包括查库失败与 RLS 作用域
// 未设导致的静默 0 行。后果是「品牌名突然全空了」没有任何信号：构建成功、产物生成、
// 页面只是少了一块。
//
// 为什么用源码判据而不是跑真库：这条分支只在「lang 非空 + contentStore 已注入」时才走到，
// 而造这两个前置条件需要一整套取词桩，测到的东西与判据不成比例。这里的判据是
// 「那个分支存在且有一个显式的错误出口」—— 与 internal/architecture 的门禁同一手法。
//
// 同时钉住「只容忍 NotFound」：品牌引用被删是正常的失效（构建照常），
// 而查库失败不是 —— 两者必须分开。
func TestLocalizeRelatedDoesNotSwallowBrandErrors(t *testing.T) {
	src := readServiceSource(t, "product_source.go")

	// 旧写法（吞掉所有错误）必须消失。
	if strings.Contains(src, "if row, berr := s.m.GetBrand(ctx, *e.BrandID, e.ProjectID); berr == nil {") {
		t.Fatal("localizeRelated 仍在吞掉品牌查询的全部错误 —— " +
			"查库失败/RLS 未设会表现为「品牌名全空」，构建却成功")
	}
	// 新写法：NotFound 跳过、其余传播。
	for _, want := range []string{
		"row, berr := s.m.GetBrand(ctx, *e.BrandID, e.ProjectID)",
		"errors.Is(berr, gorm.ErrRecordNotFound)",
		"return nil, berr",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("localizeRelated 缺少 %q（品牌错误要有显式出口）", want)
		}
	}
}

func readServiceSource(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("读取 %s 失败：%v", name, err)
	}
	return string(b)
}
