package webhookenums

// webhook_delivery_err.go — 投递失败原因（`webhook_deliveries.last_error`）的文案形态。
//
// 为什么不能直接存中文：这一列会被 DeliveryList 原样返回给调用方（`DeliveryItem.LastError`），
// 落中文等于把「写这一行时的语言」固化进投递日志 —— 英文界面上看排障日志永远是中文。
//
// 落库形态与 mail 模块的运行文案一致（两处都是「Go 侧生成、要按语言还原」的落库文本）：
//
//   - 无参数：就是 key 本身（`webhook.delivery.err.endpointMissing`）；
//   - 带参数：`{"k":"webhook.delivery.err.remoteStatus","a":{"http":"502"}}` —— **命名**参数映射。
//
// 命名而非位置的理由：词条用 `{name}` 占位符，填充按名对齐、与词序无关（译者调整中英词序
// 不会静默错配）；JSON 而非分隔符：参数值可能来自外部（出站客户端文本、状态码），
// 自带转义就不会因值里含分隔符而错位。
//
// 判定按白名单（key 必须登记在 deliveryErrFallbacks）：只看「长得像 key」会把出站客户端
// 返回的自由文本误判成编码串，页面/接口就会显示裸 key。
// 旧数据（中文原文、客户端英文错误）判定不通过 → 原样显示，不回填（那是投递事实）。
//
// 与 mail 模块的 RunText 是同一形态的**两份独立实现**（各自模块不跨依赖）：
// 跨模块共用需要一个公共包，那超出本次改动范围，留待后续按需沉淀。

import (
	"encoding/json"
	"regexp"
	"strings"
)

// deliveryErrPlaceholderRE 词条里的命名占位符（形态与 shell/notice_param.go 的判据一致）。
var deliveryErrPlaceholderRE = regexp.MustCompile(`\{[A-Za-z_][A-Za-z0-9_]*\}`)

// deliveryErrPayload 落库载荷：key + 命名参数。
type deliveryErrPayload struct {
	Key  string            `json:"k"`
	Args map[string]string `json:"a,omitempty"`
}

// DeliveryErrArgHTTP 参数名：远端返回的 HTTP 状态码（词条里写作 {http}）。
const DeliveryErrArgHTTP = "http"

// 投递失败原因的 i18n key（词条见迁移 450，中英成对）。
const (
	// DeliveryErrEndpointMissing 端点不存在或已删除（重试无意义）。
	DeliveryErrEndpointMissing = "webhook.delivery.err.endpointMissing"
	// DeliveryErrRemoteStatus 远端返回非 2xx（参数为 HTTP 状态码）。
	DeliveryErrRemoteStatus = "webhook.delivery.err.remoteStatus"
	// DeliveryErrCipherUnavailable 签名密钥不可用（未配置 / 解不开）；原文只进日志。
	DeliveryErrCipherUnavailable = "webhook.delivery.err.cipherUnavailable"
)

// deliveryErrFallbacks 词条缺失时的中文兜底（与库内 zh-CN 值逐字一致），
// 同时是编码串的判定依据。
var deliveryErrFallbacks = map[string]string{
	DeliveryErrEndpointMissing:   "端点不存在或已删除",
	DeliveryErrRemoteStatus:      "远端返回非 2xx 状态（HTTP {" + DeliveryErrArgHTTP + "}）",
	DeliveryErrCipherUnavailable: "未配置加密密钥，无法解密 webhook 签名密钥",
}

// DeliveryErrFallback key → 中文兜底模板；未登记时返回 key 本身（保证非空）。
func DeliveryErrFallback(key string) string {
	if tpl, ok := deliveryErrFallbacks[key]; ok {
		return tpl
	}
	return key
}

// EncodeDeliveryErr 把 (key, 命名参数) 编码成落库形态；无参数时只存 key。
func EncodeDeliveryErr(key string, args map[string]string) string {
	if len(args) == 0 {
		return key
	}
	if b, err := json.Marshal(deliveryErrPayload{Key: key, Args: args}); err == nil {
		return string(b)
	}
	return key
}

// FormatDeliveryErr 把落库文本按当前语言还原。
//
//   - 非编码形态（历史中文行、出站客户端原文）→ 原样返回，兼容旧数据的唯一入口点；
//   - 编码可解且参数齐备 → 取词 + 按名填充后的文本；
//   - 缺参 / 残留占位符 → **返回空串**（整条丢弃）：补空串会渲染出一条看起来像结论的错话，
//     留下 `{http}` 字面量则既不是文案也不是数据（判据与 shell/notice_param.go 同源）。
func FormatDeliveryErr(tr func(key, fallback string) string, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	key, args, ok := decodeDeliveryErr(raw)
	if !ok {
		return raw
	}
	tpl := tr(key, DeliveryErrFallback(key))
	if strings.TrimSpace(tpl) == "" {
		return ""
	}
	placeholders := deliveryErrPlaceholderRE.FindAllString(tpl, -1)
	if len(placeholders) == 0 {
		return tpl
	}
	cleared := tpl
	for _, ph := range placeholders {
		name := ph[1 : len(ph)-1]
		if _, exists := args[name]; !exists {
			return ""
		}
		cleared = strings.ReplaceAll(cleared, ph, "")
	}
	if deliveryErrPlaceholderRE.MatchString(cleared) {
		return ""
	}
	out := tpl
	for _, ph := range placeholders {
		out = strings.ReplaceAll(out, ph, args[ph[1:len(ph)-1]])
	}
	return out
}

// decodeDeliveryErr 判定并解出 (key, args)；非本模块编码形态返回 ok=false。
func decodeDeliveryErr(raw string) (key string, args map[string]string, ok bool) {
	if !strings.HasPrefix(raw, "{") {
		if _, exists := deliveryErrFallbacks[raw]; exists {
			return raw, nil, true
		}
		return "", nil, false
	}
	var payload deliveryErrPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "", nil, false
	}
	if _, exists := deliveryErrFallbacks[payload.Key]; !exists {
		return "", nil, false
	}
	return payload.Key, payload.Args, true
}
