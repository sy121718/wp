package mediaservice

// zz_verifier_naming_test.go — 独立验证者（verifier）构造的真实输入表驱动测试。
// 不属于任何实现者的改动；只做「反向归属命名约定」的双向验证。
//
// 复核对象：mediaFileAttachmentID 及其依赖的三个正则（mediaVariantRe /
// mediaOriginalRe / mediaBareIDRe）。判据来自 AGENTS.md：计数与命名不是判据，
// 写进结论前必须用真实输入跑一遍。

import (
	"testing"
)

// TestVerifierVariantNamingOwnership 反向归属：旧名与新名必须同时归属到同一 id。
func TestVerifierVariantNamingOwnership(t *testing.T) {
	cases := []struct {
		name   string
		rel    string
		wantID uint64
		wantOK bool
	}{
		// —— 旧名（无指纹段）：存量磁盘文件与历史产物引用的名字 ——
		{"旧名 thumb", "123_thumb.jpg", 123, true},
		{"旧名 medium", "123_medium.jpg", 123, true},
		{"旧名 full", "123_full.jpg", 123, true},
		{"旧名 带子目录", "2026/01/123_thumb.jpg", 123, true},

		// —— 新名（带指纹段）：produceVariant 产出的真实名 ——
		{"新名 thumb", "123_thumb-1-ab12cd34.jpg", 123, true},
		{"新名 medium", "123_medium-7-00000000.jpg", 123, true},
		{"新名 full", "123_full-12-deadbeef.jpg", 123, true},
		{"新名 generation=0", "123_thumb-0-ab12cd34.jpg", 123, true},
		{"新名 多位 generation", "123_medium-4294967295-ffffffff.jpg", 123, true},
		{"新名 带子目录", "2026/01/123_thumb-1-ab12cd34.jpg", 123, true},
		{"新名 深层子目录", "a/b/c/d/123_full-1-ab12cd34.jpg", 123, true},

		// —— 原图 / 裸 id（其余两个约定）——
		{"原图 png", "123.png", 123, true},
		{"原图 jpg", "123.jpg", 123, true},
		{"原图 webp", "123.webp", 123, true},
		{"裸 id", "123", 123, true},

		// —— 历史槽位名 webp ——
		// 迁移 496 只改 DB 侧的 variant_type，磁盘上那批 *_webp.jpg **不会被改名**，
		// 反查必须继续认它们：不认 = 存量文件从「可归属」退化成「无法归属」，对账能力倒退。
		// 生成侧已不再产出 webp（VariantTypes 只给 thumb/medium/full），认它纯为读懂历史。
		{"旧槽位 webp 无指纹", "123_webp.jpg", 123, true},
		{"旧槽位 webp 带指纹", "123_webp-1-ab12cd34.jpg", 123, true},

		// —— 必须**不**归属：指纹段的任何一部分不完整都不认 ——
		{"大写 hash", "123_thumb-1-AB12CD34.jpg", 0, false},
		{"7 位 hash", "123_thumb-1-ab12cd3.jpg", 0, false},
		{"9 位 hash", "123_thumb-1-ab12cd345.jpg", 0, false},
		{"无 generation", "123_thumb-ab12cd34.jpg", 0, false},
		{"generation 非数字", "123_thumb-x-ab12cd34.jpg", 0, false},
		{"hash 含非 hex 字母", "123_thumb-1-ab12cdg4.jpg", 0, false},
		{"非法类型 large", "123_large.jpg", 0, false},
		{"扩展名 jpeg", "123_thumb-1-ab12cd34.jpeg", 0, false},
		{"扩展名 png 的变体名", "123_thumb-1-ab12cd34.png", 0, false},
		{"语义化原名", "photo.png", 0, false},
		{"语义化原名带指纹形状", "x_thumb-1-abcd1234.jpg", 0, false},
		{"存量随机名原图", "1700000000000000000_ab12cd.png", 0, false},
		{"id 为 0", "0_thumb.jpg", 0, false},
		{"空", "", 0, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotID, gotOK := mediaFileAttachmentID(tc.rel)
			if gotOK != tc.wantOK || gotID != tc.wantID {
				t.Errorf("mediaFileAttachmentID(%q) = (%d, %v)，期望 (%d, %v)",
					tc.rel, gotID, gotOK, tc.wantID, tc.wantOK)
			}
		})
	}
}

// TestVerifierOldAndNewNamesShareID 双向兼容的核心断言：
// 同一附件的旧名与新名必须归属到**同一个 id** —— 只认新名会把存量文件报成
// 「有文件没记录」，只认旧名会把新产物报成孤儿，两个方向都是整份对账失真。
func TestVerifierOldAndNewNamesShareID(t *testing.T) {
	const id = uint64(4242)
	names := []string{
		"4242_thumb.jpg",
		"4242_medium.jpg",
		"4242_full.jpg",
		"4242_thumb-1-ab12cd34.jpg",
		"4242_medium-3-ffeeddcc.jpg",
		"4242_full-9-01234567.jpg",
	}
	for _, n := range names {
		gotID, ok := mediaFileAttachmentID(n)
		if !ok || gotID != id {
			t.Errorf("%q 应归属 id=%d，实际 (%d, %v)", n, id, gotID, ok)
		}
	}
}

// TestVerifierReconcileRegexesAreDistinct 记录一个**既有**边界（非本次改动引入）：
// 存量随机名 stem 的变体（<unixNano>_<6hex>_<type>-<gen>-<hash8>.jpg）在任何约定下
// 都无法反向归属 —— 旧正则与本次改动后的正则都不匹配。把它显式钉住，
// 避免将来有人误以为「对账能认出全部变体文件」。
func TestVerifierReconcileRegexesAreDistinct(t *testing.T) {
	const legacy = "1700000000000000000_ab12cd_thumb-1-ab12cd34.jpg"
	if _, ok := mediaFileAttachmentID(legacy); ok {
		t.Logf("注意：%q 现在可以归属了（既有边界已改变，需重新评估对账口径）", legacy)
	} else {
		t.Logf("确认：%q 不可归属（既有行为，调用方只计数不报孤儿）", legacy)
	}
}
