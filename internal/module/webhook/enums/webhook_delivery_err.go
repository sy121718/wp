package webhookenums

import (
	json "go_wp/pkg/jsonx"
	"regexp"
	"strings"
)

var deliveryErrPlaceholderRE = regexp.MustCompile(`\{[A-Za-z_][A-Za-z0-9_]*\}`)

type deliveryErrPayload struct {
	Key  string            `json:"k"`
	Args map[string]string `json:"a,omitempty"`
}

const DeliveryErrArgHTTP = "http"

const (
	DeliveryErrEndpointMissing = "webhook.delivery.err.endpointMissing"

	DeliveryErrRemoteStatus = "webhook.delivery.err.remoteStatus"

	DeliveryErrCipherUnavailable = "webhook.delivery.err.cipherUnavailable"
)

var deliveryErrFallbacks = map[string]string{
	DeliveryErrEndpointMissing:   "端点不存在或已删除",
	DeliveryErrRemoteStatus:      "远端返回非 2xx 状态（HTTP {" + DeliveryErrArgHTTP + "}）",
	DeliveryErrCipherUnavailable: "未配置加密密钥，无法解密 webhook 签名密钥",
}

func DeliveryErrFallback(key string) string {
	if tpl, ok := deliveryErrFallbacks[key]; ok {
		return tpl
	}
	return key
}

func EncodeDeliveryErr(key string, args map[string]string) string {
	if len(args) == 0 {
		return key
	}
	if b, err := json.Marshal(deliveryErrPayload{Key: key, Args: args}); err == nil {
		return string(b)
	}
	return key
}

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
