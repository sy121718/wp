package aiservice

import (
	"context"
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

	turns := dialogue("user", "assistant", "user", "assistant", "user", "assistant", "user", "assistant")
	got := tailDialogueTurns(turns, 2)
	if len(got) != 4 {
		t.Fatalf("应留最后 2 轮共 4 条，实得 %d 条", len(got))
	}

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

	svc := NewSessionService(nil)
	if _, err := svc.RecentDialogue(context.Background(), "  ", 0, 0); err == nil {
		t.Error("空会话键应当报错")
	}
}
