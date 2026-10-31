package mailenums

import (
	json "go_wp/pkg/jsonx"
	"regexp"
	"strings"
)

var runTextPlaceholderRE = regexp.MustCompile(`\{[A-Za-z_][A-Za-z0-9_]*\}`)

type runTextPayload struct {
	Key  string            `json:"k"`
	Args map[string]string `json:"a,omitempty"`
}

const (
	RunKeyTriggerEntered = "mail.run.triggerEntered"
	RunKeyDelayContinue  = "mail.run.delayContinue"
	RunKeyEmailSent      = "mail.run.emailSent"
	RunKeyEmailSuppress  = "mail.run.emailSuppressed"
	RunKeyEmailSentSubj  = "mail.run.emailSentSubject"
	RunKeyEmailSuppSubj  = "mail.run.emailSuppressedSubject"
	RunKeyBranchNo       = "mail.run.branchNo"
	RunKeyBranchYes      = "mail.run.branchYes"
	RunKeyTagsApplied    = "mail.run.tagsApplied"
	RunKeyEnded          = "mail.run.ended"
	RunKeyUnknownNode    = "mail.run.unknownNodeType"
	RunKeySendFailed     = "mail.run.sendFailed"
	RunKeyDefInvalid     = "mail.run.definitionInvalid"
	RunKeyCondMissing    = "mail.run.condMissing"
	RunKeyCondTagMissing = "mail.run.condTagMissing"
	RunKeyCondUnknown    = "mail.run.condUnknown"
	RunKeyCondInvalid    = "mail.run.condInvalid"

	RunKeyBranchOut     = "mail.run.branchOut"
	RunKeyABVariant     = "mail.run.abVariant"
	RunKeyOutletMissing = "mail.run.outletMissing"
	RunKeyWebhookOK     = "mail.run.webhookOk"
	RunKeyWebhookFailed = "mail.run.webhookFailed"
	RunKeyWebhookNoURL  = "mail.run.webhookNoUrl"
	RunKeyWebhookDenied = "mail.run.webhookDenied"

	RunKeyStoppedByReply = "mail.run.stoppedByReply"

	RunKeyAutomationGone = "mail.run.automationDeleted"
	RunKeyContactGone    = "mail.run.contactMissing"
	RunKeyNodeGone       = "mail.run.nodeMissing"
	RunKeyStepsExceeded  = "mail.run.stepsExceeded"

	RunKeyEntryNode   = "mail.run.explain.entryNode"
	RunKeyWhenUnset   = "mail.run.explain.whenUnset"
	RunKeyReasonUnset = "mail.run.explain.reasonUnknown"
	RunKeyWaiting     = "mail.run.explain.waiting"
	RunKeyRunning     = "mail.run.explain.running"
	RunKeyCompleted   = "mail.run.explain.completed"
	RunKeyStopped     = "mail.run.explain.stopped"
	RunKeyStoppedWhy  = "mail.run.explain.stoppedReason"
	RunKeyFailed      = "mail.run.explain.failed"
	RunKeyStatusUnkwn = "mail.run.explain.unknownStatus"
)

const (
	RunArgMinutes   = "minutes"
	RunArgSubject   = "subject"
	RunArgAdded     = "added"
	RunArgRemoved   = "removed"
	RunArgType      = "type"
	RunArgReason    = "reason"
	RunArgCondition = "condition"
	RunArgOutlet    = "outlet"
	RunArgVariant   = "variant"
	RunArgNode      = "node"
	RunArgWhen      = "when"
	RunArgCount     = "count"
	RunArgStatus    = "status"
)

var runTextFallbacks = map[string]string{
	RunKeyTriggerEntered: "触发进入流程",
	RunKeyDelayContinue:  "等待 {" + RunArgMinutes + "} 分钟后继续",
	RunKeyEmailSent:      "已发信",
	RunKeyEmailSuppress:  "地址在抑制名单中，未发送",
	RunKeyEmailSentSubj:  "已发信（主题覆盖: {" + RunArgSubject + "}）",
	RunKeyEmailSuppSubj:  "地址在抑制名单中，未发送（主题覆盖: {" + RunArgSubject + "}）",
	RunKeyBranchNo:       "条件不满足，走 no 分支",
	RunKeyBranchYes:      "条件满足，走 yes 分支",
	RunKeyTagsApplied:    "加标签 {" + RunArgAdded + "}，去标签 {" + RunArgRemoved + "}",
	RunKeyEnded:          "流程结束",
	RunKeyUnknownNode:    "未知节点类型: {" + RunArgType + "}",
	RunKeySendFailed:     "发信失败: {" + RunArgReason + "}",
	RunKeyDefInvalid:     "流程定义不合法: {" + RunArgReason + "}",
	RunKeyCondMissing:    "条件分支没有条件",
	RunKeyCondTagMissing: "has_tag 条件缺少标签名",
	RunKeyCondUnknown:    "未知条件: {" + RunArgCondition + "}",
	RunKeyCondInvalid:    "条件不合法: {" + RunArgCondition + "}",

	RunKeyBranchOut:     "按状态走 {" + RunArgOutlet + "} 分支",
	RunKeyABVariant:     "命中变体 {" + RunArgVariant + "}",
	RunKeyOutletMissing: "节点 {" + RunArgNode + "} 缺少出口 {" + RunArgOutlet + "}",
	RunKeyWebhookOK:     "回调已发出（HTTP {" + RunArgStatus + "}）",
	RunKeyWebhookFailed: "回调失败: {" + RunArgReason + "}（流程继续）",
	RunKeyWebhookNoURL:  "回调节点没有配置地址",
	RunKeyWebhookDenied: "回调地址指向内网，已拒绝",

	RunKeyStoppedByReply: "联系人已回信，流程退出",

	RunKeyAutomationGone: "流程已被删除",
	RunKeyContactGone:    "联系人不存在",
	RunKeyNodeGone:       "节点不存在: {" + RunArgNode + "}",
	RunKeyStepsExceeded:  "流程执行步数超过上限，可能存在环",

	RunKeyEntryNode:   "流程入口",
	RunKeyWhenUnset:   "（未设定时间）",
	RunKeyReasonUnset: "原因未知",
	RunKeyWaiting:     "等待中，将在 {" + RunArgWhen + "} 继续（下一个节点 {" + RunArgNode + "}）",
	RunKeyRunning:     "进行中，正在处理节点 {" + RunArgNode + "}",
	RunKeyCompleted:   "已完成（共执行 {" + RunArgCount + "} 个节点）",
	RunKeyStopped:     "已停止",
	RunKeyStoppedWhy:  "已停止：{" + RunArgReason + "}",
	RunKeyFailed:      "失败于节点 {" + RunArgNode + "}：{" + RunArgReason + "}（修正后可重新触发）",
	RunKeyStatusUnkwn: "状态未知：{" + RunArgStatus + "}",
}

func RunTextFallback(key string) string {
	if tpl, ok := runTextFallbacks[key]; ok {
		return tpl
	}
	return key
}

func EncodeRunText(key string, args map[string]string) string {
	if len(args) == 0 {
		return key
	}

	if b, err := json.Marshal(runTextPayload{Key: key, Args: args}); err == nil {
		return string(b)
	}
	return key
}

func FormatRunText(tr func(key, fallback string) string, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	key, args, ok := decodeRunText(raw)
	if !ok {
		return raw
	}
	tpl := tr(key, RunTextFallback(key))
	if strings.TrimSpace(tpl) == "" {
		return ""
	}
	out, ok := renderRunText(tr, tpl, args)
	if !ok {
		return ""
	}
	return out
}

func renderRunText(tr func(key, fallback string) string, tpl string, args map[string]string) (string, bool) {
	placeholders := runTextPlaceholderRE.FindAllString(tpl, -1)
	if len(placeholders) == 0 {

		return tpl, true
	}
	args = argsOrEmpty(args)
	cleared := tpl
	for _, ph := range placeholders {
		name := ph[1 : len(ph)-1]
		if _, ok := args[name]; !ok {
			return "", false
		}
		cleared = strings.ReplaceAll(cleared, ph, "")
	}
	if runTextPlaceholderRE.MatchString(cleared) {

		return "", false
	}
	out := tpl
	for _, ph := range placeholders {
		name := ph[1 : len(ph)-1]
		out = strings.ReplaceAll(out, ph, FormatRunText(tr, args[name]))
	}
	return out, true
}

func argsOrEmpty(args map[string]string) map[string]string {
	if args == nil {
		return map[string]string{}
	}
	return args
}

func decodeRunText(raw string) (key string, args map[string]string, ok bool) {
	if !strings.HasPrefix(raw, "{") {
		if _, exists := runTextFallbacks[raw]; exists {
			return raw, nil, true
		}
		return "", nil, false
	}
	var payload runTextPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "", nil, false
	}
	if _, exists := runTextFallbacks[payload.Key]; !exists {
		return "", nil, false
	}
	return payload.Key, payload.Args, true
}
