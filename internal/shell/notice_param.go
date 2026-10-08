package shell

// notice_param.go — 后台列表页回执的**带参数受控形状**：URL 里只走 key 与整数参数，
// 句子由词条决定。
//
// 解决的是什么：写动作成功后把**成品中文句子**塞进 ?err= / ?ok= / ?done= 回带的那些页面，
// 在英文界面下永远是中文 —— 那句话在写侧就被 fmt.Sprintf 拼死了，读侧只能按整句比对，
// 「防伪造」于是退化成「枚举两种语言的全部句子」。本文件把一条回执拆成两样受控输入：
//
//	?doneKey=admin.customers.bulk.result.disabled&doneN=3&doneM=1
//	 ^^^^^^^^ key：必须在调用方给的白名单里（白名单同时说明「这一页真的会产出它」）
//	                     ^^^^^^ ^^^^^^ 参数：必须是十进制整数，且不超过声明上限
//
// 读侧拿到 key 之后只做两件事：**取词**（shell.TranslateFor(c)，按当前语言出译文；
// 库里没有该语言的词条时用白名单里登记的兜底原文）与**填占位符**（词条里的 {n} / {max}）。
// 于是两件事同时成立：
//
//  1. 英文界面显示英文词条 —— 写侧不再产出任何语言相关的文本，句子的语言由读侧取词决定；
//  2. 伪造 URL 能造出的只有「白名单里的 key + 范围内的整数」，能改的只是计数，
//     塞不进整句话（旧形态可以塞任意中文文案、顶着系统提示的样式）。
//
// 与既有的 FacingQueryText / FacingNotice（整句形状判定）**并存**：那两条的语义与调用点
// 一个字都不动（另有模块在用），新页面用本文件的三件套，旧页面按批次迁移。
//
// 三件套（读写两侧共用同一个 spec，因此不存在「写侧写了个读侧不认的 key」这种静默失配）：
//
//	// 包级声明一次（spec 是不可变配置，不要每请求重建 map）
//	var customerBulkNotice = shell.FacingNoticeSpec{
//		Slot: "done",
//		Keys: map[string]string{
//			"admin.customers.bulk.result.disabled": "批量操作：已停用 {n} 个，{m} 个未处理。",
//		},
//		Params: []shell.FacingNoticeParam{{Name: "n", Max: shell.MaxBulkIDs}, {Name: "m", Max: shell.MaxBulkIDs}},
//	}
//
//	// 写侧：302 回列表页前把 key 与计数放进 query
//	q := url.Values{}
//	if err := shell.SetFacingNoticeQuery(q, customerBulkNotice, key, map[string]int{"n": done, "m": skipped}); err == nil {
//		target += "?" + q.Encode()
//	}
//
//	// 读侧：页面装载时从 query 取回受控回执（无回执返回空串；错误槽传归口文案作 fallback）
//	data["Done"] = shell.FacingNoticeText(c, customerBulkNotice, "")
//	data["Err"] = shell.FacingNoticeText(c, customerBulkNoticeErr, shell.PageInternalText(c))

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/logger"
)

const (
	// NoticeMaxCount 计数参数的默认上限（Param.Max <= 0 时用它）。
	//
	// 它挡的是**伪造的计数**：回执里的数字直接来自 URL，没有上限的话
	// ?doneN=999999999 会让页面显示「已停用 999999999 个」。真实业务的单次批量上限是
	// shell.MaxBulkIDs（200），所以这个默认值对任何调用方都足够宽松，调用方应当按自己的
	// 真实上限把 Param.Max 写实。
	NoticeMaxCount = 1000000

	// noticeMaxParams 一个槽位允许声明的参数个数上限。
	// 回执是「一句话里的几个计数」，超过 4 个说明调用方把别的用途混进来了。
	noticeMaxParams = 4

	// noticeSlotMaxBytes / noticeParamNameMaxBytes 槽位名与参数名的形态长度上限
	// （两者都是 ASCII 标识符片段，16 字节足够：done / err / ok / n / m / max）。
	noticeSlotMaxBytes      = 16
	noticeParamNameMaxBytes = 16

	// noticeKeyMaxBytes 词条 key 的长度上限。
	// 库里的 key 最长的在 60 字节量级（admin.<模块>.<语义>.<子语义>），128 留了余量。
	noticeKeyMaxBytes = 128

	// noticeCountDigits 计数参数允许的最大位数。
	// 9 位（≤ 999999999）已经高于 NoticeMaxCount，这里只用来挡住「一次提交几十 KB 的数字串」
	// 进 strconv.Atoi（那会返回错误，但没必要让它进解析）。
	noticeCountDigits = 9

	// noticeLogRawMaxRunes 写进日志的原始值（key / 参数）截断长度。
	noticeLogRawMaxRunes = 64
)

// 形态判据（全部按整体匹配，不做前缀或包含判断）。
var (
	noticeSlotRE        = regexp.MustCompile(`^[a-z][a-zA-Z0-9]{0,15}$`)
	noticeParamNameRE   = regexp.MustCompile(`^[a-z][a-zA-Z0-9]{0,15}$`)
	noticeKeyRE         = regexp.MustCompile(`^[A-Za-z][a-zA-Z0-9_.-]{0,127}$`)
	noticePlaceholderRE = regexp.MustCompile(`\{[a-zA-Z_][a-zA-Z0-9_]*\}`)
)

// FacingNoticeParam 声明一个回执参数。
//
// Query 名由「槽位 + 参数名」派生（Slot="done" + Name="n" → doneN），因此同一槽位内
// 不会与 key 参数（doneKey）撞名，不同槽位之间也天然隔离 —— 参数名不必自带前缀。
//
// Name 首字母**必须小写**：查询名靠 ToUpper(Name[0]) 派生，允许大小写两种写法会让
// "n" 与 "N" 派生出同一个 doneN（一个槽位里两种配置指向同一个参数，是配置错误）。
type FacingNoticeParam struct {
	// Name 参数名，同时是词条里的占位符名（Name="n" ↔ 词条里的 {n}）。
	Name string
	// Max 该参数允许的最大值；<= 0 表示用 NoticeMaxCount。
	Max int
}

// limit 该参数实际生效的上限。
func (p FacingNoticeParam) limit() int {
	if p.Max <= 0 {
		return NoticeMaxCount
	}
	return p.Max
}

// FacingNoticeSpec 一个回执槽位的定义：槽位名、key 白名单（key → 兜底原文）、参数表。
//
// 声明为**包级不可变变量**（不要每请求重建 map）：它是配置，读写两侧共用同一份。
// 一张页面上有多个槽位时（err / ok / done）用多个 spec，Slot 名互不相同即可。
type FacingNoticeSpec struct {
	// Slot 槽位名（如 "done" / "err"）：query 里 key 参数为 Slot+"Key"，计数参数为 Slot+驼峰(Name)。
	Slot string
	// Keys key 白名单：词条 key → 中文兜底原文（词条缺失时用）。
	// 不在这个 map 里的 key 一律拒绝（返回 fallback）—— 白名单就是「本页真的会产出它」的声明。
	Keys map[string]string
	// Params 允许出现的计数参数。
	Params []FacingNoticeParam
}

// keyQuery 槽位里存 key 的 query 参数名。
func (s FacingNoticeSpec) keyQuery() string { return s.Slot + "Key" }

// paramQuery 槽位里存某个计数参数的 query 参数名（槽位 + 首字母大写后的参数名）。
func (s FacingNoticeSpec) paramQuery(name string) string {
	if name == "" {
		return s.Slot
	}
	return s.Slot + strings.ToUpper(name[:1]) + name[1:]
}

// hasParam 该槽位是否声明了某个参数名。
func (s FacingNoticeSpec) hasParam(name string) bool {
	for _, p := range s.Params {
		if p.Name == name {
			return true
		}
	}
	return false
}

// Validate spec 的形状自检：槽位名 / 参数名 / 白名单 key 的形态，参数去重与个数上限。
//
// 读侧与写侧**都会先跑这一遍**（不信任调用方）：spec 写错时的表现若是「静默返回 fallback」，
// 排障只能靠猜；这里统一翻译成一条可读的 error，读侧顺手记一条结构化日志。
// 成本是每请求每槽位 len(Keys)+len(Params) 次正则匹配（白名单几十条的量级，几十微秒），
// 换来的是配置错误当场可见；白名单若真的长到几百条，再考虑构造期预校验 + 缓存结果。
func (s FacingNoticeSpec) Validate() error {
	if !noticeSlotRE.MatchString(s.Slot) || len(s.Slot) > noticeSlotMaxBytes {
		return fmt.Errorf("槽位名 %q 不合法（要求 [a-z][a-zA-Z0-9] 且 ≤ %d 字节）", s.Slot, noticeSlotMaxBytes)
	}
	if len(s.Keys) == 0 {
		return fmt.Errorf("槽位 %q 的白名单为空：空白名单等于「本槽位永远不显示回执」", s.Slot)
	}
	if len(s.Params) > noticeMaxParams {
		return fmt.Errorf("槽位 %q 声明了 %d 个参数，超过上限 %d", s.Slot, len(s.Params), noticeMaxParams)
	}
	for key := range s.Keys {
		if !noticeKeyRE.MatchString(key) || len(key) > noticeKeyMaxBytes {
			return fmt.Errorf("槽位 %q 的白名单里有不合法的 key %q", s.Slot, noticeLogTrim(key))
		}
	}
	seen := make(map[string]bool, len(s.Params))
	for _, p := range s.Params {
		if !noticeParamNameRE.MatchString(p.Name) || len(p.Name) > noticeParamNameMaxBytes {
			return fmt.Errorf("槽位 %q 的参数名 %q 不合法（要求首字母小写的 [a-z][a-zA-Z0-9]）", s.Slot, p.Name)
		}
		if seen[p.Name] {
			return fmt.Errorf("槽位 %q 重复声明了参数 %q", s.Slot, p.Name)
		}
		seen[p.Name] = true
	}
	return nil
}

// FacingNoticeText 读侧：从 query 取回一条受控回执，返回**当前语言的译文**。
//
// 语义与 FacingQueryText 对齐（调用方不必为这条新路径再想一套回落规则）：
//
//   - query 里**没有**该槽位的 key 参数（正常访问列表页）→ 返回 ""，不显示提示；
//   - key 存在但不在白名单 / 参数缺失或非法或超上限 / 词条里仍有未替换的占位符 → 返回 fallback；
//   - 全部通过 → 返回按当前语言取词、占位符已填好的句子。
//
// fallback 由调用方按槽位语义给：**错误槽落归口文案**（shell.PageInternalText(c)），
// **成功槽落空串**（丢弃这条回执，而不是显示半截结论）。为什么不像 FacingQueryText 那样
// 自己决定：两者的 fallback 不同，这一层不知道调用方是在读 err 还是 done。
//
// 参数缺失 / 非法 / 超上限时**丢弃整条提示**（返回 fallback），而不是把缺失的参数当 0 渲染。
// 理由：这类回执说的都是「刚才那批操作的结果」，把缺失的计数补成 0 会渲染出一条
// 「已停用 0 个，1 个未处理」——它长得像**系统给出的成功结论**，而事实恰好相反
// （操作很可能生效了，只是回执不完整）。运营据此判断「一条都没改」，会再执行一次真正的写操作。
// 丢弃提示只是「这次没有结论」；错误槽的 fallback 是归口文案，提示条照样会亮，不是静默无反应。
//
// 注意「词条里没有 {x} 就不要求参数 x」：同一 key 在不同语言下可能不带占位符
// （如「批量操作：没有可处理的账号。」），那一路不该因为少一个计数就整条消失。
//
// 两条渲染侧约定：本函数返回的是**纯文本**（句子内容由词条决定，渲染侧照旧走模板转义，
// 不要给它套 raw / 未转义输出）；能进本机制的词条只允许 `{参数名}` 形态的占位符 ——
// 词条里出现别的花括号文本（如文档示例「请输入 {userId}」）会被残留判据整条拒掉。
func FacingNoticeText(c *gin.Context, spec FacingNoticeSpec, fallback string) string {
	if err := spec.Validate(); err != nil {
		logFacingNoticeReject(c, spec, "spec-invalid: "+err.Error(), "")
		return fallback
	}

	raw := noticeQueryValue(c, spec.keyQuery())
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	// 形态与白名单分别校验：白名单命中即说明这个 key 是本页写侧产出的，
	// 形态校验则挡住「白名单里混进了奇怪的 key」这一侧的配置错误（双保险，都是整体匹配）。
	// 用 raw 原样查白名单（不 TrimSpace）：`?doneKey=%20admin.x%20` 这类带空白的值不命中。
	fallbackText, allowed := spec.Keys[raw]
	if !allowed || !noticeKeyRE.MatchString(raw) {
		logFacingNoticeReject(c, spec, "key-not-allowed", raw)
		return fallback
	}

	text, ok := renderFacingNotice(c, spec, raw, fallbackText)
	if !ok {
		logFacingNoticeReject(c, spec, "params-not-usable", raw)
		return fallback
	}
	return text
}

// renderFacingNotice 取词 + 填占位符；返回 false 表示这条回执不可信、调用方应落 fallback。
func renderFacingNotice(c *gin.Context, spec FacingNoticeSpec, key, fallbackText string) (string, bool) {
	text := TranslateFor(c)(key, fallbackText)
	if strings.TrimSpace(text) == "" {
		return "", false
	}
	for _, p := range spec.Params {
		ph := "{" + p.Name + "}"
		if !strings.Contains(text, ph) {
			continue
		}
		v, ok := parseNoticeCount(noticeQueryValue(c, spec.paramQuery(p.Name)), p.limit())
		if !ok {
			return "", false
		}
		text = strings.ReplaceAll(text, ph, strconv.Itoa(v))
	}
	// 硬判据：输出里不允许残留任何 {占位符}。
	// 它挡的是两件事：① 词条用的占位符名与调用点声明的参数名对不上（{count} vs {n}）；
	// ② 调用方漏声明了词条里的某个占位符。两种都会把「{n}」这样的字面量摆给运营看 ——
	// 那既不是文案也不是数据，只能靠人上报才会被发现，所以在这里直接判掉、落 fallback。
	if noticePlaceholderRE.MatchString(text) {
		return "", false
	}
	return text, true
}

// SetFacingNoticeQuery 写侧：把 key 与计数写进 url.Values（通常就是 302 回列表页的那个 query）。
//
// 校验与读侧同源（同一个 spec 的 Validate），并且**先全校验、后写入**：
// 任何一个 key / 参数不合法都不留下半条回执（半条回执的后果是页面显示一条残缺结论，
// 比不显示更误导）。
//
// 返回 error 表示**调用点写错了**（key 不在白名单、参数名拼错、计数超范围），不是运行时故障：
// 正常路径永远为 nil，因此调用点可以简单忽略它（写不进去时读侧也会丢弃整条提示，
// 不会出现「写侧以为写了、读侧读到了别的东西」）。但装配期 / 单测里应当断言它 ——
// 白名单漏登记一个 key 的后果是「写侧不写、读侧不显示」，静默失败，只能靠日志。
//
// counts 里**没有**声明的参数会被忽略（如「没有跳过项」时不带 m）；声明了但 counts 里没有的参数
// 不写进 query，由读侧按词条是否需要它决定（见 FacingNoticeText）。
func SetFacingNoticeQuery(q url.Values, spec FacingNoticeSpec, key string, counts map[string]int) error {
	if q == nil {
		return errors.New("shell: SetFacingNoticeQuery 收到 nil url.Values")
	}
	if err := spec.Validate(); err != nil {
		return err
	}
	if _, allowed := spec.Keys[key]; !allowed || !noticeKeyRE.MatchString(key) {
		return fmt.Errorf("shell: 回执 key %q 不在槽位 %q 的白名单里", noticeLogTrim(key), spec.Slot)
	}
	for name := range counts {
		if !spec.hasParam(name) {
			return fmt.Errorf("shell: 槽位 %q 未声明参数 %q", spec.Slot, name)
		}
	}

	// 先收集、后写入：任何一处不合法都不落半个键。
	type pair struct{ name, value string }
	writes := make([]pair, 0, len(spec.Params))
	for _, p := range spec.Params {
		v, given := counts[p.Name]
		if !given {
			continue
		}
		if v < 0 || v > p.limit() {
			return fmt.Errorf("shell: 槽位 %q 的参数 %q 计数 %d 超出允许范围 0..%d",
				spec.Slot, p.Name, v, p.limit())
		}
		writes = append(writes, pair{spec.paramQuery(p.Name), strconv.Itoa(v)})
	}

	q.Set(spec.keyQuery(), key)
	for _, w := range writes {
		q.Set(w.name, w.value)
	}
	return nil
}

// noticeQueryValue 读一个 query 参数；上下文或请求缺失时返回空串（不 panic）。
func noticeQueryValue(c *gin.Context, name string) string {
	if c == nil || c.Request == nil {
		return ""
	}
	return c.Query(name)
}

// parseNoticeCount 解析计数参数：只接受纯 ASCII 十进制数字（不 trim、不接受符号 / 空格 / 空串），
// 且必须 ≤ max。任何不合规都返回 false —— 调用方据此丢弃整条回执。
//
// 为什么不做宽松解析（Atoi 会接受 "+3" / "-3" / 前后空白）：回执里的数字在页面上是要被当**事实**
// 读的，宽松解析等于把「形状可疑的输入」也当成一个合法计数渲染出去；而拒绝的代价只是这一条提示
// 不显示（见 FacingNoticeText 的取舍说明）。
func parseNoticeCount(raw string, max int) (int, bool) {
	if raw == "" || len(raw) > noticeCountDigits {
		return 0, false
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] < '0' || raw[i] > '9' {
			return 0, false
		}
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	if v > max {
		return 0, false
	}
	return v, true
}

// logFacingNoticeReject 记录被丢弃的回执。
//
// 为什么不静默：被丢掉的都是一句本该显示的结论（成功行动的回执 / 可操作错误提示），
// 不显示就是「点了按钮什么都不说」。原文只进日志，且**不进响应**（返回的一律是 fallback）。
func logFacingNoticeReject(c *gin.Context, spec FacingNoticeSpec, reason, raw string) {
	logger.Scene("shell").
		With("path", requestURI(c)).
		With("user_id", CurrentUserID(c)).
		With("slot", spec.Slot).
		With("reason", reason).
		With("raw", noticeLogTrim(raw)).
		Warn("列表页回执被丢弃：key 或参数未通过受控校验")
}

// noticeLogTrim 截断写进日志的原始值（按 rune 截断，避免切断 UTF-8）。
func noticeLogTrim(s string) string {
	runes := []rune(s)
	if len(runes) <= noticeLogRawMaxRunes {
		return s
	}
	return string(runes[:noticeLogRawMaxRunes]) + "…"
}
