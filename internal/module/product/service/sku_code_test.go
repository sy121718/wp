package productservice

import (
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	productdto "go_wp/internal/module/product/dto"
)

func TestBuildContainerSKU(t *testing.T) {
	cases := []struct {
		name      string
		slug      string
		custom    string
		warehouse string
		want      string
	}{
		{"从仓库选：仓码 + 仓库 SKU 原样", "tee", "TEE-001", "sz", "SZ_TEE-001"},
		{"自己创建：仓码 + 自定义编码原样", "tee", "my-tee", "SZ", "SZ_my-tee"},
		{"已带仓码前缀不重复拼接", "tee", "SZ_TEE-001", "SZ", "SZ_TEE-001"},
		{"未选仓：编码原样", "tee", "TEE-001", "", "TEE-001"},
		{"没给编码：仓码 + 商品 URL 段（确定性，非随机）", "summer-shirt", "", "SZ", "SZ_SUMMERSHIRT"},
		{"没给编码且未选仓：商品 URL 段", "summer-shirt", "", "", "SUMMERSHIRT"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := buildContainerSKU(c.slug, c.custom, c.warehouse)
			if err != nil {
				t.Fatalf("不应报错: %v", err)
			}
			if got != c.want {
				t.Fatalf("主体 SKU 应为 %q，实际 %q", c.want, got)
			}
		})
	}
}

func TestBuildContainerSKUExplicitError(t *testing.T) {
	if _, err := buildContainerSKU("中文商品", "", "SZ"); err == nil {
		t.Fatal("中文 slug 派生不出商品段，应明确报错而不是退回随机码")
	}

	if got, err := buildContainerSKU("中文商品", "MY-TEE", "SZ"); err != nil || got != "SZ_MY-TEE" {
		t.Fatalf("有自定义编码时不应报错，实际 %q / %v", got, err)
	}
}

func TestVariantSKUCodeFixedOrder(t *testing.T) {
	order := map[string]int{"color": 0, "size": 1}

	inOrder := json.RawMessage(`{"color":"red","size":"m"}`)
	reversed := json.RawMessage(`{"size":"m","color":"red"}`)

	first := variantSKUCode("SZ_TEE", inOrder, order)
	second := variantSKUCode("SZ_TEE", reversed, order)
	if first != "SZ_TEE_red_m_V" {
		t.Fatalf("变体 SKU 应为 SZ_TEE_red_m_V，实际 %q", first)
	}
	if first != second {
		t.Fatalf("顺序不同但组合相同必须得到同一个 SKU：%q vs %q", first, second)
	}

	swapped := map[string]int{"size": 0, "color": 1}
	if got := variantSKUCode("SZ_TEE", inOrder, swapped); got != "SZ_TEE_m_red_V" {
		t.Fatalf("属性段顺序应跟随 groupOrder，实际 %q", got)
	}
}

func TestVariantSKUCodeNoOptions(t *testing.T) {
	for _, raw := range []json.RawMessage{nil, json.RawMessage(`{}`), json.RawMessage(`[]`)} {
		if got := variantSKUCode("SZ_TEE", raw, nil); got != "SZ_TEE" {
			t.Fatalf("无规格时应返回容器主体，实际 %q（输入 %s）", got, string(raw))
		}
	}
}

func TestVariantSKUCodeASCIISegments(t *testing.T) {
	raw := json.RawMessage(`{"color":"红色","size":""}`)
	got := variantSKUCode("SZ_TEE", raw, map[string]int{"color": 0, "size": 1})
	for _, r := range got {
		if r > 127 {
			t.Fatalf("SKU 必须全 ASCII，实际 %q", got)
		}
	}
	if !strings.HasPrefix(got, "SZ_TEE_v") || !strings.HasSuffix(got, "_V") {
		t.Fatalf("非 ASCII 段应以 v+短码 兜底并保留 _V 后缀，实际 %q", got)
	}
	if again := variantSKUCode("SZ_TEE", raw, map[string]int{"color": 0, "size": 1}); again != got {
		t.Fatalf("同一输入的短码必须稳定：%q vs %q", got, again)
	}
	if strings.Count(got, "_v") != 2 {
		t.Fatalf("两个非 ASCII/空段都应有短码兜底，实际 %q", got)
	}
}

func TestAttributeValueCodeEmptyKey(t *testing.T) {
	if got := attributeValueCode(productdto.AttributeValueResp{ID: "id-1", Key: "red"}); got != "red" {
		t.Fatalf("有 key 时应原样使用，实际 %q", got)
	}
	empty := attributeValueCode(productdto.AttributeValueResp{ID: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"})
	if empty == "" {
		t.Fatal("key 为空时应有短码兜底")
	}
	if again := attributeValueCode(productdto.AttributeValueResp{ID: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"}); again != empty {
		t.Fatalf("短码必须稳定：%q vs %q", empty, again)
	}
}
