package mailenums

import "testing"

// 取词桩：只翻译 waiting 一条，其余回落兜底 —— 用来分辨「词条命中」与「回落」两条路径。
func stubTr(key, fallback string) string {
	if key == RunKeyWaiting {
		return "Waiting; resumes at {when} (next node {node})"
	}
	return fallback
}

// TestFormatRunTextDecodesEncodedText 编码串按语言还原，且参数**递归**解码。
//
// 递归不是可选项：explain 把 error_message（本身也是编码串）当作自己的参数传进来，
// 只在第一层解码的话，页面上会露出 `mail.run.contactMissing` 这样的裸 key。
//
// 参数是**命名式**（map）而不是位置式：词条占位符是 {name}，按名对齐，译者调整中英词序
// 也不会把 A 的值填进 B 的位置。
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

// TestFormatRunTextKeepsLegacyText 迁移前落库的中文原文原样返回（不改写历史数据）。
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

// TestFormatRunTextDoesNotMangleFreeText 形态像编码串、但 key 未登记的文本不被当成编码。
//
// 判据是**白名单**（runTextFallbacks）而不是「长得像 key」：用户填的标签、节点标识
// 都可能含奇怪字符，误判的后果是排障页显示一段被拆开的乱码。
func TestFormatRunTextDoesNotMangleFreeText(t *testing.T) {
	free := `{"k":"mail.unknown.node","a":{"x":"1"}}`
	if got := FormatRunText(stubTr, free); got != free {
		t.Fatalf("自由文本应原样返回 %q，实际 %q", free, got)
	}
}

// TestRunTextFallbackCoversEveryKey 每个运行文案 key 都有中文兜底（漏登记 = 页面显示裸 key）。
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
