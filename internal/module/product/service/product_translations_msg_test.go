package productservice

// product_translations_msg_test.go — 译文保存回执的两种形态。
//
// 审查 P1：原先标记「页面待重建」失败只写日志，然后照常回「已保存 N 条译文；
// 译文变更已标记待重建」—— 那是一句**假话**：前台仍是旧译文，而运营没有任何线索，
// 也没有重试入口（下次保存同样的值会走「无变化」短路，永远补不上这次标记）。

import (
	"strings"
	"testing"
)

// TestTranslationsSavedMsgSaysMarkedForRebuild 正常路径：可以声称已标记。
func TestTranslationsSavedMsgSaysMarkedForRebuild(t *testing.T) {
	msg := TranslationsSavedMsg(3, false)
	if !strings.Contains(msg.Fallback, "已标记待重建") {
		t.Fatalf("正常路径应说明已标记待重建，实际 %q", msg.Fallback)
	}
	if msg.Named["n"] != "3" {
		t.Fatalf("参数 n 应为 3，实际 %q", msg.Named["n"])
	}
}

// TestTranslationsSavedMsgDoesNotClaimMarkedOnFailure 标记失败时不能说「已标记」。
func TestTranslationsSavedMsgDoesNotClaimMarkedOnFailure(t *testing.T) {
	msg := TranslationsSavedMsg(3, true)
	if strings.Contains(msg.Fallback, "已标记待重建") {
		t.Fatalf("标记失败时不能声称已标记（假话）：%q", msg.Fallback)
	}
	if !strings.Contains(msg.Fallback, "手动重新发布") {
		t.Fatalf("应告诉运营需要手动重新发布，实际 %q", msg.Fallback)
	}
	if msg.Key == "" {
		t.Fatal("必须有词条 key（不能只给中文兜底）")
	}
}

// TestTranslationsSavedMsgNoChangeIgnoresMarkFlag 无变化时不受标记失败影响。
func TestTranslationsSavedMsgNoChangeIgnoresMarkFlag(t *testing.T) {
	for _, failed := range []bool{false, true} {
		msg := TranslationsSavedMsg(0, failed)
		if !strings.Contains(msg.Fallback, "没有需要写入的变化") {
			t.Fatalf("written=0 应回「无变化」，实际 %q", msg.Fallback)
		}
	}
}
