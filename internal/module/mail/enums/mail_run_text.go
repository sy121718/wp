package mailenums

// mail_run_text.go — 自动化实例的**运行文案**（节点日志 detail / 实例 error_message / 排障说明）。
//
// 为什么不能直接存中文：这些文本会落进
// `mail_automation_node_logs.detail` 与 `mail_automation_runs.error_message`，
// 而排障页（/admin/mail/automation/run）与实例列表会把它们直接渲染出来 ——
// 落中文等于把「写这一行时的语言」固化进数据，英文界面上永远是中文。
//
// ## 落库形态
//
//   - 无参数：就是 key 本身（`mail.run.triggerEntered`）；
//   - 带参数：`{"k":"mail.run.sendFailed","a":{"reason":"…"}}` —— **命名**参数映射。
//
// 两个决定性的理由：
//
//  1. **命名**：词条里的占位符是 `{name}` 形态，填充按名对齐、与顺序无关。位置式参数
//     （`%s` 依次填充）在译者调整中英词序时（zh `{a}…{b}` vs en `{b}…{a}`）会**静默错配** ——
//     把 A 的值填进 B 的位置，页面显示错误的数字或节点名，而没有任何报错。
//  2. **JSON**：参数值是外部输入（如 RunKeySendFailed 的参数就是 SMTP 服务器返回的原文），
//     用不可见分隔符拼串时，值里恰好含该分隔符就会多切一段、导致后续参数整体错位；
//     JSON 自带转义，这类问题从结构上不存在。
//
// ## 判定与兼容
//
// 「是不是本模块的编码」按**白名单**判（首段/`k` 必须登记在 runTextFallbacks）：只看
// 「长得像 key」会把用户填的标签误判成编码串，页面就会显示裸 key。
// 旧数据（迁移前落库的中文原文、自由文本）判定不通过 → **原样显示**，不回填（那是运行事实）。
//
// 与 `internal/shell/notice_param.go` 是**两套独立机制**：那一套服务 URL 回执
// （从 query 取十进制整数参数、有上限），本文件服务落库文本（参数是任意字符串）。
// 照抄的是它的两条渲染判据：**缺参丢弃整条**、**残留占位符丢弃整条**。

import (
	"encoding/json"
	"regexp"
	"strings"
)

// runTextPlaceholderRE 词条里的命名占位符（形态与 shell/notice_param.go 的判据一致）。
var runTextPlaceholderRE = regexp.MustCompile(`\{[A-Za-z_][A-Za-z0-9_]*\}`)

// runTextPayload 落库载荷：key + 命名参数。
type runTextPayload struct {
	Key  string            `json:"k"`
	Args map[string]string `json:"a,omitempty"`
}

// 运行文案的 i18n key（词条见迁移 450，中英成对）。
const (
	// —— 写进节点日志 detail 的（执行过程）——
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

	// —— 写进实例 error_message 的（终态原因）——
	RunKeyAutomationGone = "mail.run.automationDeleted"
	RunKeyContactGone    = "mail.run.contactMissing"
	RunKeyNodeGone       = "mail.run.nodeMissing"
	RunKeyStepsExceeded  = "mail.run.stepsExceeded"

	// —— 排障说明（explainRun 拼出的那句人话）——
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

// 命名参数名的集中登记：词条模板里的 `{name}` 与调用点传的 map 键必须一致，
// 名字写错的表现是「整条丢弃」（页面显示 —），所以名字在这里只写一次、调用点引用常量。
const (
	RunArgMinutes   = "minutes"
	RunArgSubject   = "subject"
	RunArgAdded     = "added"
	RunArgRemoved   = "removed"
	RunArgType      = "type"
	RunArgReason    = "reason"
	RunArgCondition = "condition"
	RunArgNode      = "node"
	RunArgWhen      = "when"
	RunArgCount     = "count"
	RunArgStatus    = "status"
)

// runTextFallbacks 词条缺失时的中文兜底（与库内 zh-CN 值逐字一致，含 `{name}` 占位符）。
//
// 这张表同时是**编码串的判定依据**：只有登记在册的 key 才被当作编码。
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

// RunTextFallback key → 中文兜底模板；未登记时返回 key 本身（保证调用方拿到非空串）。
func RunTextFallback(key string) string {
	if tpl, ok := runTextFallbacks[key]; ok {
		return tpl
	}
	return key
}

// EncodeRunText 把 (key, 命名参数) 编码成**落库形态**。
//
// 无参数时只存 key（白名单判定就能解出，不必套一层 JSON）。
// 参数值一律以字符串给出（数字由调用点 strconv 转），与词条的 `{name}` 对齐 ——
// 填充是纯文本替换，不存在格式化符号的类型约束。
func EncodeRunText(key string, args map[string]string) string {
	if len(args) == 0 {
		return key
	}
	// string→string 的 map marshal 不会失败；真失败了也退回 key 形态
	// （词条仍能取到，只是少了参数 —— 而填充侧会因为缺参把整条丢弃，不会显示半截话）。
	if b, err := json.Marshal(runTextPayload{Key: key, Args: args}); err == nil {
		return string(b)
	}
	return key
}

// FormatRunText 把落库文本按当前语言还原成可读文本。
//
// 三种结局，都有明确语义：
//   - 非编码形态（历史中文行、出站客户端原文）→ **原样返回**，这是兼容旧数据的唯一入口点；
//   - 编码可解且参数齐备 → 取词 + 按名填充后的句子；
//   - 缺参 / 词条残留占位符 → **返回空串**（整条丢弃）。页面模板对该列有 `—` 兜底
//     （`{{if l.Detail == ""}}<span class="text-mute">—</span>`），所以丢掉一条不会留下空白格，
//     更不会把 `{node}` 这样的字面量摆给运营看。
//
// 参数值可以是**嵌套的编码串**（`explain.failed` 把 `error_message` 当作自己的参数传进来）：
// 填充前对每个值递归走一次本函数，只解一层会漏出 `mail.run.contactMissing` 这样的裸 key。
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

// renderRunText 按**名**填充占位符；返回 false 表示这条不可信、调用方应丢弃整条。
//
// 两条判据（与 shell/notice_param.go 同源）：
//
//  1. **缺参即丢弃**：补空串会渲染出一条「看起来像结论」的错话
//     （如「失败于节点 ：（修正后可重新触发）」），比不显示更糟；
//  2. **残留即丢弃**：填充完模板里若还有 `{name}`，说明词条的占位符名与调用点的参数名
//     对不上 —— 那会把 `{node}` 字面量摆给运营看，只能靠人上报才会发现。
//
// 第 2 条判据跑在**占位符已被清空、参数值尚未填入**的模板上：参数值是外部文本
// （SMTP 原文、用户填的标签），里面本来就可能含花括号，拿填入后的成品去判会把
// 「值里有 {x}」误判成「模板有残留」而丢掉一条好数据。判定模板本身才是它的本意。
func renderRunText(tr func(key, fallback string) string, tpl string, args map[string]string) (string, bool) {
	placeholders := runTextPlaceholderRE.FindAllString(tpl, -1)
	if len(placeholders) == 0 {
		// 词条不含占位符（如「流程结束」）：调用点多传了参数是无害的，不必要求 args 为空。
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
		// 清空后仍有 `{name}` 形态的片段：说明模板里有不是由 args 声明覆盖的占位符形态文本。
		return "", false
	}
	out := tpl
	for _, ph := range placeholders {
		name := ph[1 : len(ph)-1]
		out = strings.ReplaceAll(out, ph, FormatRunText(tr, args[name]))
	}
	return out, true
}

// argsOrEmpty nil map 查询是安全的（返回零值 + false），这里只为让上面那行的意图显式化。
func argsOrEmpty(args map[string]string) map[string]string {
	if args == nil {
		return map[string]string{}
	}
	return args
}

// decodeRunText 判定并解出 (key, args)；非本模块编码形态返回 ok=false。
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
