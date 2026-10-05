package aiservice

// ai_session_dialogue_test.go — 历史回填的截断判据。
//
// 为什么要单独测这个纯函数：它决定「面板里出现哪几轮」。截错位置（比如截在
// 一条提问与它的回答之间）的表现是界面上挂着一个永远等不到回答的气泡 ——
// 看起来像卡住，而服务端没有任何异常，日志也是干净的。

import (
	"testing"

	aidto "go_wp/internal/module/ai/dto"
)

func dialogue(roles ...string) []aidto.DialogueTurn {
	out := make([]aidto.DialogueTurn, 0, len(roles))
	for _, r := range roles {
		out = append(out, aidto.DialogueTurn{Role: r, Text: r})
	}
	return out
}

func TestTailDialogueTurnsKeepsWholeTurns(t *testing.T) {
	// 4 轮（u/a ×4），要最后 2 轮。
	turns := dialogue("user", "assistant", "user", "assistant", "user", "assistant", "user", "assistant")
	got := tailDialogueTurns(turns, 2)
	if len(got) != 4 {
		t.Fatalf("应留最后 2 轮共 4 条，实得 %d 条", len(got))
	}
	// 截断必须落在 user 上：**不能**从 assistant 开头（那会留下一个没有提问的回答）。
	if got[0].Role != "user" {
		t.Errorf("截断点应落在提问上，实得首条 %q", got[0].Role)
	}
	if got[len(got)-1].Role != "assistant" {
		t.Errorf("末条应是回答，实得 %q", got[len(got)-1].Role)
	}
}

func TestTailDialogueTurnsNoopWhenShort(t *testing.T) {
	turns := dialogue("user", "assistant")
	got := tailDialogueTurns(turns, 12)
	if len(got) != 2 {
		t.Fatalf("轮数未超上限时应原样返回，实得 %d 条", len(got))
	}
}

func TestTailDialogueTurnsDropsDanglingUser(t *testing.T) {
	// 最后一轮只有提问、没有回答（上一轮被中断）：保留它 ——
	// 那是用户真的说过的话，抹掉会让历史看起来少一句。
	turns := dialogue("user", "assistant", "user", "assistant", "user")
	got := tailDialogueTurns(turns, 2)
	if len(got) != 3 {
		t.Fatalf("应留「一轮完整 + 一条悬空提问」共 3 条，实得 %d 条", len(got))
	}
	if got[len(got)-1].Role != "user" {
		t.Errorf("末条应是那条悬空提问，实得 %q", got[len(got)-1].Role)
	}
}

func TestRecentDialogueRejectsEmptyKey(t *testing.T) {
	// 空会话键是调用方的错误（不是「没有历史」）：要报错而不是静默回空，
	// 否则界面永远看不到历史也不会有人发现参数没传对。
	svc := NewSessionService(nil)
	if _, err := svc.RecentDialogue(nil, "  ", 0); err == nil {
		t.Error("空会话键应当报错")
	}
}
