package i18n

// errdetail.go — 业务错误的**补充说明**的可翻译形态（key + 具名参数）。
//
// 解决的是什么：service 判断被拒时习惯写成
//
//	fmt.Errorf("%s：%s", enums.ErrXxx, "该外部编码在本仓已属于商品 "+owner)
//
// 读侧（各模块的 XxxErrText）只按「key：」前缀认出前半截业务 key 去取词，
// **后半截中文原样拼在译文后面** —— 英文界面上永远是中文，而且它还是句子信息量
// 最大的那一半（「属于哪个商品」「超了多少」都在这半句里）。
//
// 本文件把后半截也拆成「词条 key + 具名参数」两样受控输入，读侧据此取词并填
// {name} 占位符（占位符约定见 placeholder.go）：
//
//	// 写侧（service）
//	i18n.ErrorDetail(enums.DetailExternalSKUOwner, "item", ownerName)
//	// 词条：admin.inventory.err.externalSkuOwner = "该外部编码在本仓已属于商品 {item}"
//	// 读侧取词 + 填占位后，整句都是当前语言。
//
// 形态用**不可见控制字符**开头并分隔，理由有两条：
//
//  1. 旧的「中文明细」与任何用户可见文本都不会以它开头 —— 解析器能确定地区分
//     「新形态」与「原样透出的旧文本」，读侧不必猜；
//  2. 它不会出现在译文里（词条由人写、经 HTML 转义渲染），因此不存在歧义切分。
//
// 未编码（旧形态）或编码不合法时，读侧一律原样透出补充说明 —— 退化成今天的行为，
// 不会因为一次漏改把整条业务提示打成内部错误。

import (
	"strings"
)

// ErrDetailSep 单段明细内的分隔符与前缀（见文件头，用不可见控制字符）。
const ErrDetailSep = "\x1f"

// ErrDetailPartSep 多条明细之间的分隔符。
//
// 一条业务错误可以有**多段互相独立的补充说明**（相关商品引用不合法时同时报「指向自己 N 个」
// 「不存在 N 个」「不属于本工程 N 个」），每段各有自己的词条 key 与参数。读侧按本分隔符
// 切开、逐段取词，再用「；」拼回一整句。
const ErrDetailPartSep = "\x1e"

// ErrorDetailPart 一段解析后的补充说明（词条 key + 具名参数）。
type ErrorDetailPart struct {
	Key  string
	Args map[string]string
}

// ErrorDetail 组装一条可翻译的补充说明：词条 key + 具名参数（kv 按 name, value 交替）。
//
// kv 必须是偶数个（奇数个时最后一段被忽略），name 为空或含分隔符的段被跳过 ——
// 调用点写错时的表现是「读侧取不到某个参数、判为坏词条并落中文兜底」，
// 而不是把分隔符或半截串摆到页面上。
//
// value 含分隔符时整条判为不可编码（返回空串）：value 来自业务数据（商品名 / SKU 编码 /
// 外部编码），含控制字符的概率极低但不是零，真出现时会多切一段 —— 宁可这一段不显示，
// 也不要产生一段拼接错的句子。调用方拿到空串时不带补充说明（与 key 为空同一条路）。
//
// key 为空时同样返回空串。
func ErrorDetail(key string, kv ...string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	for i := 0; i+1 < len(kv); i += 2 {
		if strings.ContainsAny(kv[i+1], ErrDetailSep+ErrDetailPartSep) {
			return ""
		}
	}
	var b strings.Builder
	b.WriteString(ErrDetailSep)
	b.WriteString(key)
	for i := 0; i+1 < len(kv); i += 2 {
		name := strings.TrimSpace(kv[i])
		if name == "" || strings.ContainsAny(name, ErrDetailSep+"=") {
			continue
		}
		b.WriteString(ErrDetailSep)
		b.WriteString(name)
		b.WriteString("=")
		b.WriteString(kv[i+1])
	}
	return b.String()
}

// JoinErrorDetails 把多段明细连成一条（空段被丢弃）。
func JoinErrorDetails(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, ErrDetailPartSep)
}

// ParseErrorDetails 解析（可能多段）的明细产物。
//
// 不是本文件定义的形态（旧的中文明细 / 受控提示 / 任意文本）时 ok=false，
// 调用方应原样透出 raw。**任何一段**形状不合法都判整体不合法 —— 只认自己写出来的形状，
// 不给「半截像」的输入留解释空间。
func ParseErrorDetails(raw string) (parts []ErrorDetailPart, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, ErrDetailSep) {
		return nil, false
	}
	for _, chunk := range strings.Split(raw, ErrDetailPartSep) {
		part, partOK := parseErrorDetailPart(chunk)
		if !partOK {
			return nil, false
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return nil, false
	}
	return parts, true
}

// parseErrorDetailPart 解析单段明细：分隔符 + key + （分隔符 + name=value）…。
func parseErrorDetailPart(raw string) (part ErrorDetailPart, ok bool) {
	segs := strings.Split(raw, ErrDetailSep)
	// segs[0] 恒为空串（raw 以分隔符开头）。
	if len(segs) < 2 {
		return ErrorDetailPart{}, false
	}
	key := strings.TrimSpace(segs[1])
	if key == "" {
		return ErrorDetailPart{}, false
	}
	args := make(map[string]string, len(segs)-2)
	for _, seg := range segs[2:] {
		if seg == "" {
			continue
		}
		name, value, found := strings.Cut(seg, "=")
		if !found || strings.TrimSpace(name) == "" {
			return ErrorDetailPart{}, false
		}
		args[name] = value
	}
	return ErrorDetailPart{Key: key, Args: args}, true
}

// ParseErrorDetail 解析**单段**明细（多段时只取第一段；多段场景请用 ParseErrorDetails）。
func ParseErrorDetail(raw string) (key string, args map[string]string, ok bool) {
	parts, partsOK := ParseErrorDetails(raw)
	if !partsOK {
		return "", nil, false
	}
	return parts[0].Key, parts[0].Args, true
}

// detailLogReplacer 把协议分隔符转成日志里人眼可读的标记。
var detailLogReplacer = strings.NewReplacer(ErrDetailSep, "|", ErrDetailPartSep, "||")

// DetailTailForLog 把（可能未编码的）补充说明原文转成**日志可读**形态。
//
// 为什么需要：读侧丢弃一条明细时要留痕，而落到日志里的原文可能是「控制字符 + key + 参数」
// 的编码串（二进制分隔符在日志里既不可读、又容易把行搞乱）。这里只做两件事 ——
// 分隔符换成可读标记、超长截断；**不做**成败判定（判定的真源只有 ParseErrorDetails）。
//
// 日志里可以出现内部原文（SQLSTATE / SMTP 响应码）：它正是「这条明细被丢掉」的现场证据。
func DetailTailForLog(raw string) string {
	out := detailLogReplacer.Replace(strings.TrimSpace(raw))
	const maxRunes = 200
	if r := []rune(out); len(r) > maxRunes {
		out = string(r[:maxRunes]) + "…"
	}
	return out
}
