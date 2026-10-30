package navigationdto

// navigation_update_req.go — POST /api/navigation/update 的唯一入口形状（UpdateEnvelope）。
//
// 三层归属：
//   - 绑定：binding.JSONLimited[UpdateEnvelope](c, 1<<20) 是**唯一**入口。它跑 gin 的
//     ShouldBind：JSON 路调本类型的 UnmarshalJSON，表单路（urlencoded / multipart）调
//     binding.FormBinder 的 BindForm(url.Values)；两条路汇到同一份顶层键事实。本类型
//     自己不读请求体、不 ParseForm、不另造通用 binder。
//   - 判别：Route() 给出 RouteLegacy / RouteTranslations / RouteMove / RouteRejected。
//   - 解码：DecodeTranslation() / DecodeLegacy() / MoveRequest()，零参数使用同一次解析。
//
// 本文件只做形状：不判未知 hash、不判可翻译、不计数、不拼文案，不 import service/model。

import (
	"bytes"
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"

	ginbinding "github.com/gin-gonic/gin/binding"

	"go_wp/pkg/i18n"
	json "go_wp/pkg/jsonx"
)

// RouteKind UpdateEnvelope 的判别结论（handler 只按它分派）。
type RouteKind int

const (
	// RouteLegacy 缺 action：本来的更新请求（UpdateReq），行为逐字不变。
	RouteLegacy RouteKind = iota
	// RouteTranslations action="translations"：译文整表提交。
	RouteTranslations
	// RouteMove action="move"：同级上移 / 下移。
	RouteMove
	// RouteRejected 形状非法：混形、未知标签、action 类型不对、顶层不是对象。
	RouteRejected
)

// 判别与解码用的键字面量（集中一处，便于对齐既有协议）。
const (
	updateActionKey = "action"
	updateRowsKey   = "rows"

	updateActionTranslations = "translations"
	updateActionMove         = "move"

	// updateTranslationProjectKey 译文分支的工程键。与 UpdateReq 的 projectId 不同名，
	// 因此它出现即属于译文分支的形状。
	updateTranslationProjectKey = "project"
	updateTranslationLangKey    = "lang"

	// updateCSRFKey CSRF 令牌的表单字段名（与 middleware/builtin 的 csrfFormKey 同名）。
	// 它不是业务键：写基座把整份表单收成 FormData 时 token 随之进身体，不认它就无法消费
	// 带隐藏域的表单；JSON 客户端走 X-CSRF-Token 头，身体里没有这个键（JSON 白名单放宽
	// 也解决不了 CSRF —— 那由中间件负责）。
	updateCSRFKey = "csrf_token"
)

// updateReservedKeys 普通更新的混形专属键：缺 action 时出现任一即拒。
//
// 判据是「键存在」而不是「值非空」：空值同样说明调用方按另一份契约在说话。
var updateReservedKeys = []string{
	updateRowsKey, updateTranslationProjectKey, updateTranslationLangKey, "dir", "projectId",
}

// updateTranslationTopKeys 译文分支顶层允许出现的键（严格白名单）。
//
// 与迁移前的 navigationTranslationWriteKeys 同一份集合，外加公共 CSRF 键。JSON 顶层不放宽：
// 整表覆盖写里没被理解的键说明调用方按另一份契约说话，静默丢弃会让「限定了作用域」落空。
// legacy 档相反 —— 既有普通更新一直忽略未知字段，借迁移把它们变成 400 会让旧客户端
// 无感知地开始报错。
var updateTranslationTopKeys = []string{
	updateActionKey, updateTranslationProjectKey, updateTranslationLangKey, updateRowsKey, updateCSRFKey,
}

// updateMoveTopKeys move 分支顶层允许出现的键（严格白名单）。
//
// 与 handler 侧 navigationMoveKeys 同一份集合，但判据方向相反、作用点不同：
//   - 这里是**形状门**：出现任何别的键即整条拒（400），在分流之前判，三种投放一致；
//   - handler 的 navigationMoveKeys 是**二次防线的入参检查**（它拿到的是已解析的 raw），
//     两者不是重复实现 —— 少了这一处，`action=move` + 任意键都会被 200 接受。
//
// kind 刻意不在表内：kind 在页面里是**筛选上下文**，不是写请求的作用域字段。
// 页面旧表单曾把它一起发上来，白名单外的键必须整条拒掉，否则服务端开始接受
// 「调用方以为限定过的上下文」。
var updateMoveTopKeys = []string{
	updateActionKey, "id", "projectId", "dir",
}

// ErrUpdateEnvelopeShape 形状被拒。
var ErrUpdateEnvelopeShape = errors.New("navigationdto: update envelope shape rejected")

// ErrUpdateEnvelopeBody 请求体不可用（空体 / 非法 JSON / 顶层不是对象 / 表单解析失败）。
var ErrUpdateEnvelopeBody = errors.New("navigationdto: update envelope body unusable")

// UpdateEnvelope POST /api/navigation/update 的全部分支事实。
//
// Rows 为 nil 表示「本档不是译文分支」；零行提交是非 nil 的空切片。
type UpdateEnvelope struct {
	// Action / HasAction action 的原始解析结果；HasAction=false 即缺 action（legacy）。
	Action    string `json:"action" form:"action"`
	HasAction bool   `json:"-" form:"-"`
	// Rows 只在 RouteTranslations 时有值。form:"-" 是必须的：gin 的 ShouldBind 先做反射
	// 绑定再调 BindForm，没这个 tag 时它会拿表单里的 rows 字符串去解 []struct 而整体失败。
	Rows []TranslationWriteRow `json:"rows" form:"-"`
	// LegacyBody 同一次读体的原始字节（legacy 档还原 UpdateReq 用；JSON 投放直接透传）。
	LegacyBody []byte `json:"-" form:"-"`

	raw     map[string]json.RawMessage
	form    bool
	shapeOK bool
	// formErr 解析失败的原因：BindForm 返回它，JSON 路（零参数重新解析）也复用它。
	formErr error
}

// TranslationWriteRow 一行译文（只是形状，业务规则在 service）。
//
// Target 是普通字符串而不是指针：请求里必须带 target 键（缺键是形状错），空串是合法值 ——
// 「本次不动这一行」由 service 解释，dto 只保证空值仍是空值。
type TranslationWriteRow struct {
	Context string `json:"context" form:"context"`
	Hash    string `json:"hash" form:"hash"`
	Target  string `json:"target" form:"target"`
}

// TranslationUpdateReq 译文整表提交的业务入参（只能由 DecodeTranslation 产出）。
type TranslationUpdateReq struct {
	ProjectID string
	Lang      string
	Rows      []TranslationWriteRow
}

// MoveUpdateReq move 分支已解析的形状（只能由 MoveRequest 产出）。
//
// ID 与 Dir 是既有必填项；ProjectID 可选（空 = 不校验归属）。kind / sortOrder 不在既有
// move 白名单内，因此不在此结构里 —— 收下它们就等于放宽了既有请求形状。
type MoveUpdateReq struct {
	ID        string
	ProjectID string
	Dir       string
}

// UnmarshalJSON JSON 路的解析入口（binding.JSON / JSONLimited 在 JSON 体上调用）。
//
// 不解引用自身（*e）：jsonx 对地址相同的自引用会直接失败，且本类型是一次性解析对象。
func (e *UpdateEnvelope) UnmarshalJSON(body []byte) error {
	e.LegacyBody = body
	return e.parse(body, false)
}

// BindForm 表单路的解析入口：binding.JSON / Form / ParseForm 在表单体上补调一次，
// 收 gin 已解析好的 urlencoded 与 multipart 键值（不要自己 ParseForm —— gin 的 PostForm
// 是惰性填充的，绕过它再解析会拿到空 map）。
//
// 重置只有 Action 与本类型的私有事实：gin 先反射绑定（Rows 有 form:"-"，不会误绑）。
func (e *UpdateEnvelope) BindForm(values url.Values) error {
	e.Action = ""
	e.HasAction = false
	e.Rows = nil
	e.raw = nil
	e.form = false
	e.shapeOK = false
	e.formErr = nil
	if values == nil {
		e.formErr = ErrUpdateEnvelopeBody
		return e.formErr
	}
	// 表单投放没有 JSON 字节：按 urlencoded 形态重建一份，供 legacy 档还原标量。
	if encoded := values.Encode(); encoded != "" {
		e.LegacyBody = []byte(encoded)
	}
	raw, err := updateFormRaw(values)
	if err != nil {
		e.formErr = err
		return err
	}
	e.applyRaw(raw, true)
	return e.formErr
}

// parse JSON 路的唯一解析逻辑。
func (e *UpdateEnvelope) parse(body []byte, form bool) error {
	e.Action = ""
	e.HasAction = false
	e.Rows = nil
	e.raw = nil
	e.form = false
	e.shapeOK = false
	e.formErr = nil
	raw, err := updateJSONRaw(body)
	if err != nil {
		e.formErr = err
		return err
	}
	e.applyRaw(raw, form)
	return e.formErr
}

// updateRowsCell 取 rows 的 JSON 值，必要时**剥掉一层字符串包装**。
//
// 两种投放的 rows 深度不同：JSON 里它是数组本身；表单里它是写基座 JSON.stringify 之后的值，
// 也就是「一个字符串，内容是 JSON 数组」。step 只差这一层，所以在这里统一，
// 而不是让下游（或 service）去猜自己拿到的是哪一层。
//
// JSON 投放**不剥**：JSON 里 `"rows":"[]"` 是形状错（值类型不对），不能因为宽容而放过。
func updateRowsCell(value json.RawMessage, form bool) json.RawMessage {
	if !form || value == nil {
		return value
	}
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return value
	}
	return json.RawMessage(text)
}

// applyRaw 把顶层键事实收进信封：判别键 + 译文分支的白名单与行形状。
func (e *UpdateEnvelope) applyRaw(raw map[string]json.RawMessage, form bool) {
	e.raw = raw
	e.form = form
	e.Action, e.HasAction, e.shapeOK = updateActionOf(raw[updateActionKey])
	// 顶层白名单按档各一份：漏一条就是「静默接受另一档的键」。
	// 曾经漏掉 move 这条，于是 `action=move` + kind / rows / project / lang 全部 200 ——
	// 服务端按 move 分支执行，而调用方以为限定过的上下文被无声丢弃。
	switch e.Action {
	case updateActionTranslations:
		if !updateTranslationTopAllowed(raw) {
			e.formErr = ErrUpdateEnvelopeShape
			return
		}
	case updateActionMove:
		if !updateMoveTopAllowed(raw) {
			e.formErr = ErrUpdateEnvelopeShape
			return
		}
	}
	if e.Route() != RouteTranslations {
		return
	}
	// 走到这里的只剩译文档（move 已在上面 return）。
	rows, ok := updateTranslationRows(updateRowsCell(raw[updateRowsKey], form))
	if !ok {
		e.formErr = ErrUpdateEnvelopeShape
		return
	}
	e.Rows = rows
}

// Route 给出本次请求属于哪一档（顺序即判据）。
//
//  1. action 出现但形状非法（null / 空串 / 纯空白 / 非字符串） → 拒绝
//  2. action = translations                                  → 译文分支
//  3. action = move                                          → 移动分支
//  4. action 为其他值                                         → 拒绝（未知标签不猜分支）
//  5. 缺 action 且带 rows / project / lang / dir / projectId  → 拒绝（混形）
//  6. 缺 action 且无上述键                                    → legacy 更新
//
// 空串 action 与缺 action 不是同一条语义：空串（含纯空白）是写坏了的判别键，一律拒；
// 只有真的没有该键才是 legacy（与迁移前 navigationWriteAction 一致）。
func (e *UpdateEnvelope) Route() RouteKind {
	if e.HasAction {
		if !e.shapeOK {
			return RouteRejected
		}
		switch e.Action {
		case updateActionTranslations:
			return RouteTranslations
		case updateActionMove:
			return RouteMove
		default:
			return RouteRejected
		}
	}
	for _, key := range updateReservedKeys {
		if _, ok := e.raw[key]; ok {
			return RouteRejected
		}
	}
	return RouteLegacy
}

// DecodeTranslation 解出译文分支的业务字段（只在 RouteTranslations 时成功）。
func (e *UpdateEnvelope) DecodeTranslation() (*TranslationUpdateReq, error) {
	if e.Route() != RouteTranslations {
		return nil, ErrUpdateEnvelopeShape
	}
	projectID, ok := updateRequiredString(e.raw[updateTranslationProjectKey])
	if !ok {
		return nil, ErrUpdateEnvelopeShape
	}
	lang, ok := updateRequiredString(e.raw[updateTranslationLangKey])
	if !ok || !updateLangSupported(lang) {
		return nil, ErrUpdateEnvelopeShape
	}
	return &TranslationUpdateReq{ProjectID: projectID, Lang: lang, Rows: e.Rows}, nil
}

// Validate 两条投放共用的**唯一**形状校验入口（binding 出口调用）。
//
// 为什么必须两条投放共用：JSON 与表单是同一个 HTTP 端点（POST /api/navigation/update）
// 的两种 Content-Type，对调用方是同一件事。此前只有表单在出口校验，JSON 缺 id 得到 200，
// 前端拿到「成功但什么都没发生」；若换成只在表单里校验，两侧又会各判一套。
//
// 只判 legacy 档：译文档与 move 档的必填由各自的 Decode* 负责，在这里再跑一次
// gin 的 JSON binding 没有意义（那些分支的 UpdateReq 字段本就允许缺）。
//
// 为什么可以在 BindForm 之后调用它：BindForm 的返回是**解析事实**（能否读出键值集合），
// 本方法回答的是**可用性**（这堆键值转成 UpdateReq 后过不过 required）。两者分开，
// 构造过程（测试逐字段拼 url.Values）就不必每步都满足最终形状。
func (e *UpdateEnvelope) Validate() error {
	if e == nil {
		return ErrUpdateEnvelopeBody
	}
	if e.formErr != nil {
		return e.formErr
	}
	if e.Route() != RouteLegacy {
		return nil
	}
	if _, err := e.DecodeLegacy(); err != nil {
		return err
	}
	return nil
}

// ValidateMove 解出 move 档的请求形状：成功才说明「这一档的键与值都用得上」。
//
// 为什么必须单独一步：applyRaw 的白名单只判**键集合**（多一个键就拒），不判值
// —— `projectId: null` / `id: 7` / `dir: ""` 都落在那四个键之内，键集合是合法的。
// 若 handler 只依赖 MoveRequest 里各自的错误分支，形状错就只能靠调用方在下一跳撞见，
// 症状是「应 400 实际 200 + moved:true」。所以 handler 在分流前调这一步，
// 失败即 ParamError（与 legacy 档 Validate 的失败出口一致）。
func (e *UpdateEnvelope) ValidateMove() error {
	_, err := e.MoveRequest()
	return err
}

// DecodeLegacy 解出普通更新已绑定的 UpdateReq（只在 RouteLegacy 时成功）。
//
// 用 gin 的 JSON binding 而不是裸 jsonx.Unmarshal：binding:"required" 的校验与指针字段的
// nil / 空语义必须与迁移前一致（裸解码会静默跳过 required）。字节来自同一次解析，handler
// 不重读 Body；表单投放同样在此还原：字段集由单次解析的键值事实给出，URL 编码字节不交给
// JSON 绑定。
func (e *UpdateEnvelope) DecodeLegacy() (*UpdateReq, error) {
	if e.Route() != RouteLegacy {
		return nil, ErrUpdateEnvelopeShape
	}
	payload, err := updateLegacyPayload(e.LegacyBody, e.form)
	if err != nil {
		return nil, err
	}
	req := &UpdateReq{}
	if err := ginbinding.JSON.BindBody(payload, req); err != nil {
		return nil, ErrUpdateEnvelopeShape
	}
	return req, nil
}

// MoveRequest 解出 move 分支已解析的形状（只在 RouteMove 时成功）。
func (e *UpdateEnvelope) MoveRequest() (*MoveUpdateReq, error) {
	if e.Route() != RouteMove {
		return nil, ErrUpdateEnvelopeShape
	}
	id, ok := updateRequiredString(e.raw["id"])
	if !ok {
		return nil, ErrUpdateEnvelopeShape
	}
	dir, ok := updateRequiredString(e.raw["dir"])
	if !ok {
		return nil, ErrUpdateEnvelopeShape
	}
	req := &MoveUpdateReq{ID: id, Dir: dir}
	// projectId 是**可选键**，不是可写 null：缺省 = 不校验工程归属；显式 null 是形状错。
	// 「这一格没填」与「填了个不是类型值的东西」必须分开 —— 合并成同一种「空」会让
	// 调用方以为限定过工程、服务端却在全库范围内按 id 找。
	if value, present := e.raw["projectId"]; present {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, ErrUpdateEnvelopeShape
		}
		text, ok := updateOptionalString(value)
		if !ok {
			return nil, ErrUpdateEnvelopeShape
		}
		req.ProjectID = text
	}
	return req, nil
}

// IsForm 本次请求是否按表单体解析（FormData 客户端）。
func (e *UpdateEnvelope) IsForm() bool { return e.form }

// ---------------------------------------------------------------- 顶层键

// updateJSONRaw JSON 字节 → 顶层键集合。顶层必须是对象；重复键取最后一个（与 v1 一致）。
func updateJSONRaw(body []byte) (map[string]json.RawMessage, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, ErrUpdateEnvelopeBody
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil || raw == nil {
		return nil, ErrUpdateEnvelopeBody
	}
	return raw, nil
}

// updateFormRaw url.Values → 顶层键集合（值一律重新 marshal 成 JSON 字符串）。
//
// 重复键取最后一个：与 JSON 的「后者覆盖」同语义。rows 的字符串值在这里被 marshal 成
// JSON 字符串字面量，交给 updateTranslationRows 时先解成字符串、再解成数组 —— 正好匹配
// 前端 JSON.stringify 出来的一层字符串，这一层不能省。
func updateFormRaw(values url.Values) (map[string]json.RawMessage, error) {
	if updateIsEmptyQuery(values) {
		return nil, ErrUpdateEnvelopeBody
	}
	raw := make(map[string]json.RawMessage, len(values))
	for key, list := range values {
		if len(list) == 0 {
			// 键在、值空：混形判据只看键存在不存在，不能替它丢掉事实。
			raw[key] = json.RawMessage("null")
			continue
		}
		encoded, err := json.Marshal(list[len(list)-1])
		if err != nil {
			return nil, ErrUpdateEnvelopeBody
		}
		raw[key] = encoded
	}
	return raw, nil
}

// updateIsEmptyQuery 表单是否**一个可读键值都没有**（空串、纯空白、只有分隔符）。
//
// 判据是「值非空串」而不是「值列表非空」：gin 的 PostForm 对 "a=&&b=" 与 "a=&b=" 给出
// 的是带空串值的 map，而对 "" 给出空 map —— 若这里只看 len(list)，同一件事（没有提交
// 任何字段）会在两条投放上判出两种结果。有键、值列表为空（url.Values{"a": {}}）仍算
// 有键（updateFormRaw 会把它记成 null），那是键集合事实，与本判据不是一回事。
func updateIsEmptyQuery(values url.Values) bool {
	for _, list := range values {
		for _, v := range list {
			if strings.TrimSpace(v) != "" {
				return false
			}
		}
	}
	return true
}

// updateTranslationTopAllowed 译文分支的顶层键是否全在白名单里（判据是键集合，不是值）。
func updateTranslationTopAllowed(raw map[string]json.RawMessage) bool {
	return updateTopAllowed(raw, updateTranslationTopKeys)
}

// updateMoveTopAllowed move 分支的顶层白名单判定（规则同译文档）。
func updateMoveTopAllowed(raw map[string]json.RawMessage) bool {
	return updateTopAllowed(raw, updateMoveTopKeys)
}

// updateTopAllowed 顶层键全部落在白名单内才为真（两档共用同一实现，避免判据分叉）。
func updateTopAllowed(raw map[string]json.RawMessage, allowed []string) bool {
	for key := range raw {
		if !slices.Contains(allowed, key) {
			return false
		}
	}
	return true
}

// updateActionOf 解析 action 键：值、是否出现、形状是否合法。
func updateActionOf(value json.RawMessage) (string, bool, bool) {
	if value == nil {
		return "", false, true
	}
	if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return "", true, false
	}
	var action string
	if err := json.Unmarshal(value, &action); err != nil {
		return "", true, false
	}
	action = strings.TrimSpace(action)
	if action == "" {
		return "", true, false
	}
	return action, true, true
}

// updateRequiredString 读一个必需的字符串字段（缺键 / null / 非字符串 / 空 → 拒）。
func updateRequiredString(value json.RawMessage) (string, bool) {
	text, ok := updateOptionalString(value)
	if !ok || text == "" {
		return "", false
	}
	return text, true
}

// updateOptionalString 读一个可选字符串（缺键 / null → 给空串）。非字符串给 ok=false。
func updateOptionalString(value json.RawMessage) (string, bool) {
	if value == nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return "", true
	}
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return "", false
	}
	return strings.TrimSpace(text), true
}

// updateLegacyPayload 造出交给 gin 绑定的 JSON 对象。
//
// JSON 投放：**原样透传**请求字节 —— 值形状（数字、布尔、嵌套对象）都是调用方写的，
// 重新拼装只会引入差异（而且 payload 为空时会被当成空体）。
// 表单投放：键值来自同一次解析，只能拼回去；此时「整数字符串」要还原成 JSON 数字
// （表单里写的 sortOrder），否则 *int 会因字符串而整条绑定失败。
func updateLegacyPayload(body []byte, form bool) ([]byte, error) {
	if !form {
		return body, nil
	}
	raw, err := updateFormRawValues(body)
	if err != nil {
		return nil, err
	}
	out := make(map[string]json.RawMessage, len(raw))
	for key, list := range raw {
		if len(list) == 0 {
			continue
		}
		text := list[len(list)-1]
		if typed, ok := updateFormScalar(text); ok {
			out[key] = typed
			continue
		}
		encoded, err := json.Marshal(text)
		if err != nil {
			return nil, ErrUpdateEnvelopeShape
		}
		out[key] = encoded
	}
	payload, err := json.Marshal(out)
	if err != nil {
		return nil, ErrUpdateEnvelopeShape
	}
	return payload, nil
}

// updateFormRawValues 把重建的 urlencoded 字节解回键值（legacy 档的表单还原用）。
func updateFormRawValues(body []byte) (url.Values, error) {
	values, err := url.ParseQuery(string(body))
	if err != nil || updateIsEmptyQuery(values) {
		return nil, ErrUpdateEnvelopeShape
	}
	return values, nil
}

// updateFormScalar 表单字符串 → 对应 JSON 标量："null" → null，整数 → 数字，其余原样。
func updateFormScalar(text string) (json.RawMessage, bool) {
	if text == "null" {
		return json.RawMessage("null"), true
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, false
	}
	if _, err := strconv.Atoi(trimmed); err != nil {
		return nil, false
	}
	return json.RawMessage(trimmed), true
}

// updateLangSupported 目标语言必须在受支持的语言集合里（真源是 pkg/i18n）。
func updateLangSupported(lang string) bool {
	return slices.Contains(i18n.AvailableLangs(), lang)
}

// ---------------------------------------------------------------- 译文行

// updateTranslationRowKeys 译文行允许出现的键（严格白名单）。
var updateTranslationRowKeys = []string{"context", "hash", "target"}

// updateTranslationRows 严格解析 rows：数组、每项对象、只认三键、三键必须都在、
// 值必须是 JSON 字符串；>1000 行拒；空行与空 target 按原序原位保留。
//
// 缺 target 必须拒：放行会让下游把它当「显式空译文」跳过，形状错误被静默吞成 written:0。
func updateTranslationRows(value json.RawMessage) ([]TranslationWriteRow, bool) {
	if value == nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return nil, false
	}
	var list []map[string]json.RawMessage
	if err := json.Unmarshal(value, &list); err != nil {
		return nil, false
	}
	if len(list) > updateTranslationMaxRows {
		return nil, false
	}
	// 空数组要给非 nil 空切片：nil 的语义是「不是译文分支」。
	out := make([]TranslationWriteRow, 0, len(list))
	for _, item := range list {
		for _, required := range updateTranslationRowKeys {
			if _, ok := item[required]; !ok {
				return nil, false
			}
		}
		row := TranslationWriteRow{}
		for key, cell := range item {
			if !slices.Contains(updateTranslationRowKeys, key) {
				return nil, false
			}
			text, ok := updateRowCellText(cell)
			if !ok {
				return nil, false
			}
			switch key {
			case "context":
				row.Context = text
			case "hash":
				row.Hash = text
			case "target":
				row.Target = text
			}
		}
		out = append(out, row)
	}
	return out, true
}

// updateRowCellText 单元格必须是 JSON 字符串（null 与非字符串一律拒）。
func updateRowCellText(cell json.RawMessage) (string, bool) {
	if bytes.Equal(bytes.TrimSpace(cell), []byte("null")) {
		return "", false
	}
	var text string
	if err := json.Unmarshal(cell, &text); err != nil {
		return "", false
	}
	return text, true
}

// updateTranslationMaxRows 单次提交的行数上限：本屏一屏即全部菜单文字，1000 是余量上界，
// 同时把一次请求的最坏写入量压在可控范围。
const updateTranslationMaxRows = 1000
