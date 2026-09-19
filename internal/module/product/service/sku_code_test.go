package productservice

// sku_code_test.go — SKU 编码新规则（2026-09-19 评审第四轮）的就近单测。
//
// 覆盖规则里「拼错了不会报错、只会静默生成错编码」的那几处：
//   · 容器主体 SKU 的两种来源（从仓库选 / 自己创建）与仓码前缀；
//   · 变体 SKU 的属性段顺序固定 + _V 后缀（顺序不同但组合相同必须得到同一个 SKU）；
//   · 属性值 key 含非 ASCII 或为空时的 ASCII 短码兜底；
//   · 捆绑主体 SKU：_B 后缀补齐 + 留空必填（不再静默派生）；
//   · 派生不出商品段时**明确报错**（旧实现会退回随机码）。
//
// 这几条都是纯函数，不碰数据库，所以放在模块内就近跑（毫秒级）。

import (
	"encoding/json"
	"strings"
	"testing"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
)

// TestBuildContainerSKU 规则 A 的容器主体 SKU：
// ① 从仓库选 = 仓短码大写_仓库里那条 SKU（原样）；② 自己创建 = 自己填的编码 + 仓码前缀。
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

// TestBuildContainerSKUExplicitError 商品 URL 段不含 ASCII 时**明确报错**。
//
// 这是旧实现与 new 的分界：旧实现退回 legacySKUCode（{slug}-{随机段}），
// 同一个请求两次调用得到不同编码，且与仓库侧永远对不上。新规则必须失败。
func TestBuildContainerSKUExplicitError(t *testing.T) {
	if _, err := buildContainerSKU("中文商品", "", "SZ"); err == nil {
		t.Fatal("中文 slug 派生不出商品段，应明确报错而不是退回随机码")
	}
	// 有自定义编码时不受影响（运营自己给了主体）。
	if got, err := buildContainerSKU("中文商品", "MY-TEE", "SZ"); err != nil || got != "SZ_MY-TEE" {
		t.Fatalf("有自定义编码时不应报错，实际 %q / %v", got, err)
	}
}

// TestVariantSKUCodeFixedOrder 规则 A：变体 SKU = 主体 + 属性值段… + _V，
// 且属性段顺序**按属性组的既定顺序**（groupOrder），与 option_values 的写入顺序无关。
func TestVariantSKUCodeFixedOrder(t *testing.T) {
	order := map[string]int{"color": 0, "size": 1}

	// 同样的组合，两种写入顺序（模拟运营点选先后不同）。
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

	// 维度顺序反了也不行：groupOrder 说了算，不是 map 遍历顺序。
	swapped := map[string]int{"size": 0, "color": 1}
	if got := variantSKUCode("SZ_TEE", inOrder, swapped); got != "SZ_TEE_m_red_V" {
		t.Fatalf("属性段顺序应跟随 groupOrder，实际 %q", got)
	}
}

// TestVariantSKUCodeNoOptions 无规格变体就是容器主体本身（不加 _V，也不拼空段）。
func TestVariantSKUCodeNoOptions(t *testing.T) {
	for _, raw := range []json.RawMessage{nil, json.RawMessage(`{}`), json.RawMessage(`[]`)} {
		if got := variantSKUCode("SZ_TEE", raw, nil); got != "SZ_TEE" {
			t.Fatalf("无规格时应返回容器主体，实际 %q（输入 %s）", got, string(raw))
		}
	}
}

// TestVariantSKUCodeASCIISegments 属性值段的 ASCII 保证：
// key 是 ASCII 标识就原样用；含中文或为空时用确定性短码兜底（同一 key 恒得同一短码）。
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

// TestAttributeValueCodeEmptyKey option_values 里的值：有标识 key 就原样用（筛选按它匹配），
// key 为空时用 id 短码兜底 —— 否则空 key 的多条值会互相覆盖。
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

// TestNormalizeBundleSKU 规则 B：捆绑主体恒以 _B 结尾 ——
// 填了但缺后缀 → 补齐（大小写不敏感）；**没填（空 / 纯空白）→ ErrBundleSKURequired**。
//
// 2026-09-19 用户拍板：不再按商品 URL 段静默派生 <商品段>_B。SKU 是商品的对外身份，
// 旧实现会让运营「看不见那个编码」就建出商品；现在改由新建抽屉预填建议值并允许修改，
// 服务端只接受一个**被看见并确认过**的编码。
func TestNormalizeBundleSKU(t *testing.T) {
	cases := []struct {
		custom, want string
	}{
		{"GIFT-BOX", "GIFT-BOX_B"},
		{"GIFT_B", "GIFT_B"},
		{"gift_b", "gift_b"},
		{"  SUMMER-SET  ", "SUMMER-SET_B"},
		{"b", "b_B"},
	}
	for _, c := range cases {
		got, err := normalizeBundleSKU(c.custom)
		if err != nil {
			t.Fatalf("输入 %q 不应报错: %v", c.custom, err)
		}
		if got != c.want {
			t.Fatalf("输入 %q 应为 %q，实际 %q", c.custom, c.want, got)
		}
	}
	// 留空不再静默派生（商品段是 giftbox 也不行）：必须拿到可行动的必填错误。
	for _, blank := range []string{"", "   ", "\t\n"} {
		got, err := normalizeBundleSKU(blank)
		if err == nil {
			t.Fatalf("留空 %q 应返回 ErrBundleSKURequired，实际得到 %q（静默派生的老行为）", blank, got)
		}
		if err.Error() != productenums.ErrBundleSKURequired {
			t.Fatalf("留空应返回 %s，实际 %v", productenums.ErrBundleSKURequired, err)
		}
	}
}
