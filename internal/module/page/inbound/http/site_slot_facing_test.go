package pagehttp

// site_slot_facing_test.go — 站点槽位页白名单的**键形态**判定回归。
//
// 历史背景：这里曾经是真缺陷 —— 写侧把 siteSlotFacingError 转好的**中文文案**放进 ?err=，
// 读侧却只按 map 的键（错误常量名）查，于是所有业务错误都被判为未命中、一律回落
// 「系统内部错误，请稍后重试」。现在结论走 shell.RenderJump（见 page_jump.go），唯一的
// 调用点是 siteSlotFacingError（入参恒为 service 错误的 Error() = 常量名），所以契约
// 收敛成「只认键」—— 这条测试钉住它，并挡住「用包含匹配绕过白名单」的写法。
//
// 断言里的中文一律**从 map 里取**，不手抄：手抄的文案在有人改白名单时会让测试
// 在「谁都没写错」的情况下变红，而那种红最后总是被改成「把测试的字符串也改掉」——
// 于是测试就失去意义了。

import (
	"testing"

	pageenums "go_wp/internal/module/page/enums"
)

func TestSiteSlotFacingTextKeyForm(t *testing.T) {
	zhMsg, ok := siteSlotFacingMessages[pageenums.ErrSlotPageMiss]
	if !ok || zhMsg == "" {
		t.Fatalf("白名单里应登记 %s（否则这条测试没有可验证的素材）", pageenums.ErrSlotPageMiss)
	}

	t.Run("常量名形态（写侧直接回填 enums 常量）", func(t *testing.T) {
		if got := siteSlotFacingText(pageenums.ErrSlotPageMiss); got != zhMsg {
			t.Fatalf("按常量名查应得到中文文案，实际 %q", got)
		}
	})

	t.Run("本页自造文案同样按键取词", func(t *testing.T) {
		// 自造文案的**常量值就是 i18n key**（见 pages_handle.go 的常量块），中文兜底在白名单里。
		for _, key := range []string{siteSlotBoundText, siteSlotUnboundText, siteSlotNoProjectText, siteSlotNoPageText} {
			fallback, ok := siteSlotFacingMessages[key]
			if !ok || fallback == "" {
				t.Fatalf("白名单里应登记 %s（否则这条测试没有可验证的素材）", key)
			}
			if got := siteSlotFacingText(key); got != fallback {
				t.Fatalf("按 key 查 %q 应得到兜底文案，实际 %q", key, got)
			}
		}
	})

	t.Run("译文形态与未知串一律不命中", func(t *testing.T) {
		// 契约已收敛成「只认键」：写侧不再把成品译文放进任何回显通道。
		for _, raw := range []string{"", "   ", zhMsg, "ErrSomethingElse", "SELECT * FROM pages WHERE id = 1"} {
			if got := siteSlotFacingText(raw); got != "" {
				t.Fatalf("未命中应返回空串，实际 %q（raw=%q）", got, raw)
			}
		}
	})

	t.Run("不许用包含匹配绕过白名单", func(t *testing.T) {
		// 若实现改成 strings.Contains，下面两条就会命中 —— 那就等于把「只要夹带一段已知
		// 文案就能命中」的口子留着。
		for _, raw := range []string{"<script>alert(1)</script>" + pageenums.ErrSlotPageMiss, pageenums.ErrSlotPageMiss + "<script>alert(1)</script>"} {
			if got := siteSlotFacingText(raw); got != "" {
				t.Fatalf("夹带内容不该命中（必须整体相等），实际 %q", got)
			}
		}
	})
}
