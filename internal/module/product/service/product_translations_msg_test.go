package productservice

import (
	"strings"
	"testing"
)

func TestTranslationsSavedMsgSaysMarkedForRebuild(t *testing.T) {
	msg := TranslationsSavedMsg(3, false)
	if !strings.Contains(msg.Fallback, "已标记待重建") {
		t.Fatalf("正常路径应说明已标记待重建，实际 %q", msg.Fallback)
	}
	if msg.Named["n"] != "3" {
		t.Fatalf("参数 n 应为 3，实际 %q", msg.Named["n"])
	}
}

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

func TestTranslationsSavedMsgNoChangeIgnoresMarkFlag(t *testing.T) {
	for _, failed := range []bool{false, true} {
		msg := TranslationsSavedMsg(0, failed)
		if !strings.Contains(msg.Fallback, "没有需要写入的变化") {
			t.Fatalf("written=0 应回「无变化」，实际 %q", msg.Fallback)
		}
	}
}
