package i18nvalues

// adapter_test.go — i18n 组 JSON → pkg/i18n 运行期默认值的形状翻译（纯函数）。
//
// 这里钉的是「配置写错时进程看到什么」：类型不符 / 键名写错 / 空串都必须落到
// 「按未配置处理」，并由 problems 留痕。判错不会报错，只会让默认语言静默变一个值。

import (
	"strings"
	"testing"

	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
)

func TestRuntimeValuesOfHappyPath(t *testing.T) {
	vals, problems := RuntimeValuesOf(map[string]any{
		sysconfigcontract.KeyDefaultLang:     "zh-CN",
		sysconfigcontract.KeySiteLangURLMode: "all_prefix",
		sysconfigcontract.KeyLangURLCodes:    map[string]any{"en-AU": "en", "zh-CN": "zh"},
	})
	if len(problems) != 0 {
		t.Fatalf("合法配置不该产生问题，实际 %v", problems)
	}
	if vals.DefaultLang != "zh-CN" || vals.SiteLangURLMode != "all_prefix" {
		t.Fatalf("取值不符：%+v", vals)
	}
	if vals.LangURLCodes["en-AU"] != "en" || vals.LangURLCodes["zh-CN"] != "zh" {
		t.Fatalf("语言码覆盖不符：%v", vals.LangURLCodes)
	}
}

func TestRuntimeValuesOfMissingKeysAreUnset(t *testing.T) {
	vals, problems := RuntimeValuesOf(map[string]any{})
	if vals.DefaultLang != "" || vals.SiteLangURLMode != "" || vals.LangURLCodes != nil {
		t.Fatalf("缺键必须按未配置处理，实际 %+v", vals)
	}
	if len(problems) != 0 {
		t.Fatalf("缺键不是「配置写错」，不该报问题：%v", problems)
	}
}

func TestRuntimeValuesOfTrimsAndDropsTypeMismatch(t *testing.T) {
	vals, problems := RuntimeValuesOf(map[string]any{
		sysconfigcontract.KeyDefaultLang:     "  en-US  ",
		sysconfigcontract.KeySiteLangURLMode: 123, // 类型写错：按未配置处理并留痕
		sysconfigcontract.KeyLangURLCodes:    "en-AU: en",
	})
	if vals.DefaultLang != "en-US" {
		t.Fatalf("应去掉首尾空白，实际 %q", vals.DefaultLang)
	}
	if vals.SiteLangURLMode != "" {
		t.Fatalf("类型不符的键应按未配置处理，实际 %q", vals.SiteLangURLMode)
	}
	if vals.LangURLCodes != nil {
		t.Fatalf("类型不符的映射表应为 nil，实际 %v", vals.LangURLCodes)
	}
	if len(problems) != 2 {
		t.Fatalf("两个键有问题，应报 2 条 problem，实际 %v", problems)
	}
	for _, p := range problems {
		if !strings.Contains(p, "类型") {
			t.Errorf("problem 文案应说明类型问题：%q", p)
		}
	}
}

func TestRuntimeValuesOfDropsOnlyBadURLCodes(t *testing.T) {
	vals, problems := RuntimeValuesOf(map[string]any{
		sysconfigcontract.KeyLangURLCodes: map[string]any{
			"en-AU": "en",
			"fr-CA": 42,   // 非法值：只丢这一项
			"de-DE": "  ", // 空串：只丢这一项
		},
	})
	if len(problems) != 2 {
		t.Fatalf("应报 2 条 problem（两个非法项），实际 %v", problems)
	}
	if len(vals.LangURLCodes) != 1 || vals.LangURLCodes["en-AU"] != "en" {
		t.Fatalf("合法项必须保留（整表丢弃会让一次手滑废掉全部覆盖），实际 %v", vals.LangURLCodes)
	}
}

func TestRuntimeValuesOfNilLoaderReader(t *testing.T) {
	if _, err := New(nil).Load(t.Context()); err == nil {
		t.Fatal("reader 为 nil 时应返回明确错误，而不是静默给出一组常量")
	}
}
