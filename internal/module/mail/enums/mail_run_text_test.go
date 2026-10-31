package mailenums

import "testing"

func stubTr(key, fallback string) string {
	if key == RunKeyWaiting {
		return "Waiting; resumes at {when} (next node {node})"
	}
	return fallback
}

func TestFormatRunTextDecodesEncodedText(t *testing.T) {
	raw := EncodeRunText(RunKeyWaiting, map[string]string{
		RunArgWhen: "2026-01-02 15:04",
		RunArgNode: EncodeRunText(RunKeyEntryNode, nil),
	})
	got := FormatRunText(stubTr, raw)
	want := "Waiting; resumes at 2026-01-02 15:04 (next node 流程入口)"
	if got != want {
		t.Fatalf("编码串应还原成 %q，实际 %q", want, got)
	}
}

func TestFormatRunTextKeepsLegacyText(t *testing.T) {
	legacy := "等待 1440 分钟后继续"
	got := FormatRunText(func(key, fallback string) string {
		t.Fatalf("旧数据不该走到取词：%s", key)
		return fallback
	}, legacy)
	if got != legacy {
		t.Fatalf("旧数据应原样返回 %q，实际 %q", legacy, got)
	}
}

func TestFormatRunTextDoesNotMangleFreeText(t *testing.T) {
	free := `{"k":"mail.unknown.node","a":{"x":"1"}}`
	if got := FormatRunText(stubTr, free); got != free {
		t.Fatalf("自由文本应原样返回 %q，实际 %q", free, got)
	}
}

func TestRunTextFallbackCoversEveryKey(t *testing.T) {
	keys := []string{
		RunKeyTriggerEntered, RunKeyDelayContinue, RunKeyEmailSent, RunKeyEmailSuppress,
		RunKeyEmailSentSubj, RunKeyEmailSuppSubj, RunKeyBranchNo, RunKeyBranchYes,
		RunKeyTagsApplied, RunKeyEnded, RunKeyUnknownNode, RunKeySendFailed,
		RunKeyDefInvalid, RunKeyCondMissing, RunKeyCondTagMissing, RunKeyCondUnknown,
		RunKeyAutomationGone, RunKeyContactGone, RunKeyNodeGone, RunKeyStepsExceeded,
		RunKeyEntryNode, RunKeyWhenUnset, RunKeyReasonUnset, RunKeyWaiting,
		RunKeyRunning, RunKeyCompleted, RunKeyStopped, RunKeyStoppedWhy,
		RunKeyFailed, RunKeyStatusUnkwn,

		RunKeyBranchOut, RunKeyABVariant, RunKeyOutletMissing,
		RunKeyWebhookOK, RunKeyWebhookFailed, RunKeyWebhookNoURL, RunKeyWebhookDenied,
		RunKeyCondInvalid, RunKeyStoppedByReply,
	}
	if len(runTextFallbacks) != len(keys) {
		t.Fatalf("兜底表应有 %d 条，实际 %d", len(keys), len(runTextFallbacks))
	}
	for _, k := range keys {
		tpl, ok := runTextFallbacks[k]
		if !ok || tpl == "" {
			t.Fatalf("%s 缺少中文兜底", k)
		}
		if RunTextFallback(k) != tpl {
			t.Fatalf("%s 的兜底取值不一致", k)
		}
	}
}
