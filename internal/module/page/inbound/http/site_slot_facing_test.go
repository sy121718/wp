package pagehttp

// site_slot_facing_test.go — 站点槽位页白名单的**双形态**判定回归。
//
// 为什么必须有：这里曾经是真缺陷 —— 写侧把 siteSlotFacingError 转好的**中文文案**放进
// ?err=，读侧却只按 map 的**键**（错误常量名）查，于是所有业务错误都被判为未命中、
// 一律回落「系统内部错误，请稍后重试」。文案明明备好了却永远不出现，
// 而运营看到的是「系统出错了，重试吧」，实际原因却是「请先在下拉里选一个页面」。
//
// 这类缺陷静默、无报错、无日志（回落到归口文案反而**记了一条日志**，看起来还挺正常），
// 只有断言能钉住：两个形态各测一遍，外加一条「不许用包含匹配绕过白名单」的防伪测试。
//
// 断言里的中文一律**从 map 里取**，不手抄：手抄的文案在有人改白名单时会让测试
// 在「谁都没写错」的情况下变红，而那种红最后总是被改成「把测试的字符串也改掉」——
// 于是测试就失去意义了。

import (
	"testing"

	pageenums "go_wp/internal/module/page/enums"
)

func TestSiteSlotFacingTextAcceptsBothForms(t *testing.T) {
	zhMsg, ok := siteSlotFacingMessages[pageenums.ErrSlotPageMiss]
	if !ok || zhMsg == "" {
		t.Fatalf("白名单里应登记 %s（否则这条测试没有可验证的素材）", pageenums.ErrSlotPageMiss)
	}

	t.Run("常量名形态（写侧直接回填 enums 常量）", func(t *testing.T) {
		if got := siteSlotFacingText(pageenums.ErrSlotPageMiss); got != zhMsg {
			t.Fatalf("按常量名查应得到中文文案，实际 %q", got)
		}
	})

	t.Run("译文形态（写侧回填已经转好的中文）", func(t *testing.T) {
		// 这正是原先失败的那条：中文文案是 map 的值，不是键。
		if got := siteSlotFacingText(zhMsg); got != zhMsg {
			t.Fatalf("已经是白名单里的成品文案应原样放行，实际 %q（空串 = 会回落到归口文案）", got)
		}
	})

	t.Run("本页自造文案同样两种形态都认", func(t *testing.T) {
		// 自造文案的**常量值就是 i18n key**（见 site_slot_handle.go 的常量块），
		// 中文兜底在白名单里。写侧放 key（常量名形态）与放取词后的成品文案，两种都要能过 ——
		// 少了后者，?ok= 带回来的成功回执会被自己的白名单吞掉（页面上什么都不显示）。
		for _, key := range []string{siteSlotBoundText, siteSlotUnboundText, siteSlotNoProjectText, siteSlotNoPageText} {
			fallback, ok := siteSlotFacingMessages[key]
			if !ok || fallback == "" {
				t.Fatalf("白名单里应登记 %s（否则这条测试没有可验证的素材）", key)
			}
			if got := siteSlotFacingText(key); got != fallback {
				t.Fatalf("按 key 查 %q 应得到兜底文案，实际 %q", key, got)
			}
			if got := siteSlotFacingText(fallback); got != fallback {
				t.Fatalf("成品文案 %q 应原样放行，实际 %q", fallback, got)
			}
		}
	})

	t.Run("空串与未知串一律不命中", func(t *testing.T) {
		for _, raw := range []string{"", "   ", "ErrSomethingElse", "SELECT * FROM pages WHERE id = 1"} {
			if got := siteSlotFacingText(raw); got != "" {
				t.Fatalf("未命中应返回空串，实际 %q（raw=%q）", got, raw)
			}
		}
	})

	t.Run("不许用包含匹配绕过白名单", func(t *testing.T) {
		// 若实现改成 strings.Contains，下面两条就会命中 —— 那就等于把手拼 URL 变成
		// 「只要夹带一段已知文案，就能往页面上塞任意前缀 / 后缀」的口子。
		for _, raw := range []string{"<script>alert(1)</script>" + zhMsg, zhMsg + "<script>alert(1)</script>"} {
			if got := siteSlotFacingText(raw); got != "" {
				t.Fatalf("夹带内容不该命中（必须整体相等），实际 %q", got)
			}
		}
	})
}
