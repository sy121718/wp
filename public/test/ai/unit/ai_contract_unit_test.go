package unit

import (
	json "go_wp/pkg/jsonx"
	"net/http"
	"reflect"
	"strings"
	"testing"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aimodel "go_wp/internal/module/ai/model"
)

// 状态码是协议事实源：同一档错误必须给同一个真实状态码。
//
// 这里不为每个 key 抄一份表（抄表只证明「抄得对」，不证明 StatusOf 覆盖了全部声明）。
// 断言的是分档等式：404 只能给「对象不存在」那三个，401 只给身份类，409 只给冲突类，
// 500 只给「我们这边出错」，其余一律 400。改档位必须同时改这里的等式。
func TestAIStatusBucketsMatchDeclaredSemantics(t *testing.T) {
	cases := []struct {
		name string
		want int
		keys []string
	}{
		{
			name: "对象不存在 → 404",
			want: http.StatusNotFound,
			keys: []string{aienums.ErrProviderNotFound, aienums.ErrSessionNotFound, aienums.ErrTokenNotFound},
		},
		{
			name: "没有身份 → 401",
			want: http.StatusUnauthorized,
			keys: []string{aienums.ErrTokenInvalid, aienums.ErrUserRequired},
		},
		{
			name: "有身份没权限 → 403",
			want: http.StatusForbidden,
			keys: []string{aienums.ErrToolForbidden},
		},
		{
			name: "对象在但状态/版本不允许 → 409",
			want: http.StatusConflict,
			keys: []string{aienums.ErrVersionConflict, aienums.ErrSessionConflict, aienums.ErrSessionProviderMismatch},
		},
		{
			name: "我们这边出错 → 500",
			want: http.StatusInternalServerError,
			keys: []string{aienums.ErrCipherUnavailable, aienums.ErrToolRunFailed, aienums.ErrInternal},
		},
		{
			name: "参数不对 → 400",
			want: http.StatusBadRequest,
			keys: []string{
				aienums.ErrInvalidParam,
				aienums.ErrProviderKeyRequired, aienums.ErrProviderKeyExists, aienums.ErrDisplayNameRequired,
				aienums.ErrAPIKeyRequiredOnBaseURLChange, aienums.ErrVersionRequired,
				aienums.ErrProtocolUnsupported, aienums.ErrNoBuiltinModels, aienums.ErrBaseURLRequired,
				aienums.ErrModelIDRequired, aienums.ErrModelIDDuplicated, aienums.ErrModelsFetchFailed,
				aienums.ErrURLMalformed, aienums.ErrURLSchemeUnsupported,
				aienums.ErrURLHostMissing, aienums.ErrURLUnresolvable, aienums.ErrURLDenied,
				aienums.ErrNoModelSelected,
				aienums.ErrSessionKeyMissing, aienums.ErrSessionArchived,
				aienums.ErrEventKindInvalid, aienums.ErrEventContentEmpty,
				aienums.ErrFoldRangeInvalid, aienums.ErrFoldSummaryEmpty,
				aienums.ErrSessionChatUnavailable, aienums.ErrSessionChatInputRequired,
				aienums.ErrSessionChatModelRequired, aienums.ErrSessionChatEmptyReply,
				aienums.ErrSessionToolRoundsExceeded,
				aienums.ErrTokenNameRequired, aienums.ErrTokenScopeRequired,
				aienums.ErrTokenScopeUnknown, aienums.ErrTokenExpiredInPast,
			},
		},
	}

	got := make(map[string]int)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, key := range tc.keys {
				if prev, dup := got[key]; dup {
					t.Fatalf("%q 同时登记在两个档位（先前 %d，本次 %d）", key, prev, tc.want)
				}
				got[key] = tc.want
				if status := aienums.StatusOf(key); status != tc.want {
					t.Errorf("StatusOf(%q) = %d，想要 %d", key, status, tc.want)
				}
			}
		})
	}
}

// 未声明的 key 在 StatusOf 里回 0（「没声明」与「声明成 400」是两件事）：
// 出口 aiErrStatus 见 0 才按 400 兜底，未知 key 因此不会伪装成已声明。
func TestAIUnknownErrorKeyHasNoStatus(t *testing.T) {
	if got := aienums.StatusOf("ai.err.undeclared"); got != 0 {
		t.Errorf("未声明 key 应回 0，实际 %d", got)
	}
	if got := aienums.StatusOf(""); got != 0 {
		t.Errorf("空 key 应回 0，实际 %d", got)
	}
}

func TestAIProtocolOptionsDefaultFirst(t *testing.T) {
	if len(aienums.ProtocolOptions) == 0 {
		t.Fatal("协议下拉不能为空")
	}
	first := aienums.ProtocolOptions[0]
	if first.Value != aienums.ProtocolOpenAIChatCompletions || first.Label != "OpenAI Chat Completions" {
		t.Fatalf("首项应是 OpenAI Chat Completions，实际 %+v", first)
	}
	seen := make(map[string]bool, len(aienums.ProtocolOptions))
	for _, o := range aienums.ProtocolOptions {
		if o.Value == "" || o.Label == "" {
			t.Errorf("选项值/展示名不能为空：%+v", o)
		}
		if seen[o.Value] {
			t.Errorf("协议值重复：%q", o.Value)
		}
		seen[o.Value] = true
		if !aienums.IsSupportedProtocol(o.Value) {
			t.Errorf("下拉里的值必须通过白名单：%q", o.Value)
		}
		if aienums.ProtocolLabel(o.Value) != o.Label {
			t.Errorf("%q 的展示名不一致", o.Value)
		}
	}
	if aienums.IsSupportedProtocol("openai_compatible_自创") {
		t.Error("白名单必须拒绝未登记协议")
	}
	if aienums.IsSupportedProtocol("") {
		t.Error("空协议不是合法白名单值（缺省由 service 补）")
	}
	if got := aienums.ProtocolLabel("未知协议"); got != "未知协议" {
		t.Errorf("未知协议展示名应原样返回，实际 %q", got)
	}
}

func TestAIModelEntryJSONShape(t *testing.T) {
	entry := aidto.ModelEntry{
		ID: "deepseek-v4.1-flash", DisplayName: "DeepSeek V4.1 Flash",
		ContextWindow: 1000000, MaxOutputTokens: 256000,
		InputTypes: []string{aienums.InputTypeText, aienums.InputTypeImage},
	}
	blob, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("序列化失败：%v", err)
	}
	for _, key := range []string{`"id"`, `"displayName"`, `"contextWindow"`, `"maxOutputTokens"`, `"inputTypes"`} {
		if !strings.Contains(string(blob), key) {
			t.Errorf("JSON 里缺少键 %s：%s", key, blob)
		}
	}
	var back aidto.ModelEntry
	if err := json.Unmarshal(blob, &back); err != nil {
		t.Fatalf("反序列化失败：%v", err)
	}
	if !reflect.DeepEqual(entry, back) {
		t.Errorf("往返不一致：%+v vs %+v", entry, back)
	}
}

func TestAIJSONMapValueScan(t *testing.T) {

	var nilMap aimodel.JSONMap
	v, err := nilMap.Value()
	if err != nil || v != nil {
		t.Fatalf("nil map 应写为 NULL：v=%v err=%v", v, err)
	}

	original := aimodel.JSONMap{"models": []any{map[string]any{"id": "m1"}}}
	raw, err := original.Value()
	if err != nil {
		t.Fatalf("序列化失败：%v", err)
	}
	bytes, ok := raw.([]byte)
	if !ok {
		t.Fatalf("Value 应回 []byte，实际 %T", raw)
	}

	var back aimodel.JSONMap
	if err := back.Scan(bytes); err != nil {
		t.Fatalf("反序列化失败：%v", err)
	}
	if _, ok := back["models"]; !ok {
		t.Errorf("往返丢了 models 键：%+v", back)
	}

	var fromNull aimodel.JSONMap = aimodel.JSONMap{"x": 1}
	if err := fromNull.Scan(nil); err != nil || fromNull != nil {
		t.Errorf("Scan(nil) 应回 nil：%+v err=%v", fromNull, err)
	}

	var fromEmpty aimodel.JSONMap
	if err := fromEmpty.Scan([]byte{}); err != nil || len(fromEmpty) != 0 {
		t.Errorf("Scan(空字节) 应回空 map：%+v err=%v", fromEmpty, err)
	}

	var wrongType aimodel.JSONMap
	if err := wrongType.Scan("不是字节切片"); err == nil {
		t.Error("Scan 非 []byte 应报错")
	}
}

func TestAIProviderResponseHasNoSecretField(t *testing.T) {

	forbidden := map[string]bool{
		"apikey": true, "apikeycipher": true, "cipher": true, "secret": true,
		"password": true, "token": true, "accesstoken": true, "refreshtoken": true,
	}

	assertNoForbidden := func(name string, typ reflect.Type, allowAPIKey bool) {
		t.Helper()
		for i := 0; i < typ.NumField(); i++ {
			field := strings.ToLower(typ.Field(i).Name)
			if !forbidden[field] {
				continue
			}
			if allowAPIKey && field == "apikey" {
				continue
			}
			t.Errorf("%s 不允许出现 %q 字段（密钥只存密文、只回布尔）", name, typ.Field(i).Name)
		}
	}

	assertNoForbidden("响应 Provider", reflect.TypeOf(aidto.Provider{}), false)
	assertNoForbidden("响应 ModelEntry", reflect.TypeOf(aidto.ModelEntry{}), false)
	assertNoForbidden("响应 FetchModelsResult", reflect.TypeOf(aidto.FetchModelsResult{}), false)

	assertNoForbidden("请求 SaveProviderReq", reflect.TypeOf(aidto.SaveProviderReq{}), true)

	if _, ok := reflect.TypeOf(aidto.Provider{}).FieldByName("HasAPIKey"); !ok {
		t.Error("响应 Provider 必须回 HasAPIKey（是否已配置）")
	}
}
