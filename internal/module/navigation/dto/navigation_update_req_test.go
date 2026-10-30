package navigationdto_test

// navigation_update_req_test.go — UpdateEnvelope 的判别契约（包外测试：直接调用 dto 的解析入口）。
//
// 这一批用例钉住的是**形状语义**（哪一档、为什么），不涉及 binding 接线；
// 真实 binding.JSONLimited 的接线与四种投放由 binding_contract_test.go 覆盖。
// 包外（navigationdto_test）是为了不把 pkg/binding 拉进 dto 包本身的依赖图。

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"testing"

	navigationdto "go_wp/internal/module/navigation/dto"
	"go_wp/pkg/i18n"
)

// langOK 取语言表里的第一种可用语言：用例不写死语言码（换语言表时不该红）。
func langOK() string {
	langs := i18n.AvailableLangs()
	if len(langs) == 0 {
		return "en-US"
	}
	return langs[0]
}

// jsonEnv 走 JSON 路的解析入口（binding 的 ShouldBind 在 JSON 体上调的就是它）。
func jsonEnv(t *testing.T, body string) (*navigationdto.UpdateEnvelope, error) {
	t.Helper()
	env := &navigationdto.UpdateEnvelope{}
	err := env.UnmarshalJSON([]byte(body))
	return env, err
}

// formEnv 走表单路的解析入口（binding.FormBinder 的 BindForm）。
func formEnv(t *testing.T, values url.Values) (*navigationdto.UpdateEnvelope, error) {
	t.Helper()
	env := &navigationdto.UpdateEnvelope{}
	err := env.BindForm(values)
	return env, err
}

// translationJSON 造一份译文提交体（rows 由调用方给）。
func translationJSON(rows string) string {
	return fmt.Sprintf(`{"action":"translations","project":"p-1","lang":%q,"rows":%s}`, langOK(), rows)
}

// translationForm 造同样内容的表单键值。
func translationForm(rows string) url.Values {
	return url.Values{"action": {"translations"}, "project": {"p-1"}, "lang": {langOK()}, "rows": {rows}}
}

// bigRows 造 n 行形状合法的译文行。
func bigRows(n int) string {
	rows := make([]string, 0, n)
	for range n {
		rows = append(rows, `{"context":"c","hash":"h","target":"t"}`)
	}
	return "[" + strings.Join(rows, ",") + "]"
}

// TestUpdateEnvelopeRoute 判别：三分支 + 未知标签 + 空 / 坏 action。
func TestUpdateEnvelopeRoute(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want navigationdto.RouteKind
	}{
		{"缺 action = 普通更新", `{"id":"n-1","title":"home"}`, navigationdto.RouteLegacy},
		{"action=translations", translationJSON(`[]`), navigationdto.RouteTranslations},
		{"action=move", `{"action":"move","id":"n-1","dir":"up"}`, navigationdto.RouteMove},
		{"未知 action 不猜分支", `{"action":"delete"}`, navigationdto.RouteRejected},
		{"action 为 null", `{"action":null,"id":"n-1"}`, navigationdto.RouteRejected},
		{"action 为空串", `{"action":"","id":"n-1"}`, navigationdto.RouteRejected},
		{"action 为纯空白", `{"action":"   ","id":"n-1"}`, navigationdto.RouteRejected},
		{"action 为数字", `{"action":1,"id":"n-1"}`, navigationdto.RouteRejected},
		{"action 为对象", `{"action":{},"id":"n-1"}`, navigationdto.RouteRejected},
		{"action 为数组", `{"action":[],"id":"n-1"}`, navigationdto.RouteRejected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, err := jsonEnv(t, tc.body)
			if err != nil {
				t.Fatalf("JSON 解析应成功，实际 %v（body=%s）", err, tc.body)
			}
			if got := env.Route(); got != tc.want {
				t.Fatalf("Route() = %v, want %v（body=%s）", got, tc.want, tc.body)
			}
		})
	}
}

// TestUpdateEnvelopeLegacyToleratesUnknownKeys 普通更新对未知键宽容（旧契约不变）。
func TestUpdateEnvelopeLegacyToleratesUnknownKeys(t *testing.T) {
	body := `{"id":"n-1","title":"home","unknownField":123,"another":{"deep":true}}`
	env, err := jsonEnv(t, body)
	if err != nil {
		t.Fatalf("未知键应被忽略，实际 %v", err)
	}
	if got := env.Route(); got != navigationdto.RouteLegacy {
		t.Fatalf("未知键应判为 legacy，实际 %v", got)
	}
	form, err := formEnv(t, url.Values{"id": {"n-1"}, "unknownField": {"123"}})
	if err != nil {
		t.Fatalf("表单未知键应被忽略，实际 %v", err)
	}
	if got := form.Route(); got != navigationdto.RouteLegacy {
		t.Fatalf("表单未知键应判为 legacy，实际 %v", got)
	}
}

// TestUpdateEnvelopeMixedShapeRejected 缺 action 时专属键出现即拒（判据是键存在，不是值非空）。
func TestUpdateEnvelopeMixedShapeRejected(t *testing.T) {
	for _, key := range []string{"rows", "project", "lang", "dir", "projectId"} {
		for _, value := range []string{`"x"`, `""`, `null`, `[]`, `{}`} {
			t.Run(key+"="+value, func(t *testing.T) {
				env, err := jsonEnv(t, fmt.Sprintf(`{"id":"n-1",%q:%s}`, key, value))
				if err != nil {
					t.Fatalf("判别不因专属键的值而失败，实际 %v", err)
				}
				if got := env.Route(); got != navigationdto.RouteRejected {
					t.Fatalf("缺 action 带专属键 %s 应拒绝，实际 %v", key, got)
				}
			})
		}
	}
}

// TestUpdateEnvelopeFormKeysAreFacts 表单里「键在、值空」的键不能从事实里消失。
func TestUpdateEnvelopeFormKeysAreFacts(t *testing.T) {
	values, err := url.ParseQuery("id=n-1&rows")
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	env, err := formEnv(t, values)
	if err != nil {
		t.Fatalf("BindForm 应成功（形状事实由 Route 判），实际 %v", err)
	}
	if got := env.Route(); got != navigationdto.RouteRejected {
		t.Fatalf("缺少 action 且出现 rows 键（值为空）必须判混形，实际 %v", got)
	}
}

// TestUpdateEnvelopeEmptyFormRejected 空表单体（空串 / 纯空白 / 只有空值键）判为不可用体。
//
// 末尾两种是**执行器实测形态**：gin 的 PostForm 读到 "title=" 得到 url.Values{"title": {""}}，
// 读到 "a=&b=" 得到两个空值键。它们与 "" 对调用方是同一件事（没提交任何字段），
// 因此判据必须一致 —— 只看 len(list) 会让这两种判成 200（「成功但什么都没发生」）。
func TestUpdateEnvelopeEmptyFormRejected(t *testing.T) {
	for _, raw := range []string{"", "   ", "title=", "title=&sortOrder=", "title=%20"} {
		values, err := url.ParseQuery(raw)
		if err != nil {
			t.Fatalf("ParseQuery: %v", err)
		}
		env, err := formEnv(t, values)
		if !errors.Is(err, navigationdto.ErrUpdateEnvelopeBody) {
			t.Fatalf("空表单体 %q 应以 ErrUpdateEnvelopeBody 拒绝，实际 %v（env=%v）", raw, err, env.Route())
		}
	}
}

// TestUpdateEnvelopeBodyRejected 空体 / 非对象 / 非法 JSON 判为不可用体。
func TestUpdateEnvelopeBodyRejected(t *testing.T) {
	for _, body := range []string{"", "   ", "{broken", "[1,2]", `"x"`, "null"} {
		t.Run(body, func(t *testing.T) {
			_, err := jsonEnv(t, body)
			if !errors.Is(err, navigationdto.ErrUpdateEnvelopeBody) {
				t.Fatalf("应以 ErrUpdateEnvelopeBody 拒绝，实际 %v", err)
			}
		})
	}
}

// TestUpdateEnvelopeDecodeTranslationSameAcrossForms 两条投放解出同一份业务入参。
func TestUpdateEnvelopeDecodeTranslationSameAcrossForms(t *testing.T) {
	rows := `[{"context":"nav.header","hash":"h1","target":"Home"},{"context":"nav.footer","hash":"h2","target":""}]`
	j, err := jsonEnv(t, translationJSON(rows))
	if err != nil {
		t.Fatalf("JSON 解析失败: %v", err)
	}
	jreq, err := j.DecodeTranslation()
	if err != nil {
		t.Fatalf("JSON 解码失败: %v", err)
	}
	f, err := formEnv(t, translationForm(rows))
	if err != nil {
		t.Fatalf("表单解析失败: %v", err)
	}
	freq, err := f.DecodeTranslation()
	if err != nil {
		t.Fatalf("表单解码失败: %v", err)
	}
	if jreq.ProjectID != freq.ProjectID || jreq.Lang != freq.Lang {
		t.Fatalf("两种投放必须解出同一份入参：JSON=%+v Form=%+v", jreq, freq)
	}
	if !slices.Equal(jreq.Rows, freq.Rows) {
		t.Fatalf("两种投放的行必须逐行相等：JSON=%+v Form=%+v", jreq.Rows, freq.Rows)
	}
}

// TestUpdateEnvelopeRowsPreserveEmptyTargetAndPosition 空 target 与空行保留原位。
func TestUpdateEnvelopeRowsPreserveEmptyTargetAndPosition(t *testing.T) {
	rows := `[{"context":"a","hash":"h1","target":"A"},{"context":"b","hash":"h2","target":""},{"context":"","hash":"","target":""},{"context":"c","hash":"h3","target":"C"}]`
	env, err := jsonEnv(t, translationJSON(rows))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	req, err := env.DecodeTranslation()
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if len(req.Rows) != 4 {
		t.Fatalf("空行不能被挤掉，实际 %d 行", len(req.Rows))
	}
	if req.Rows[1].Target != "" || req.Rows[1].Hash != "h2" {
		t.Fatalf("空 target 必须保持空串，实际 %+v", req.Rows[1])
	}
	if req.Rows[2] != (navigationdto.TranslationWriteRow{}) {
		t.Fatalf("全空行必须原样保留，实际 %+v", req.Rows[2])
	}
	if req.Rows[3].Hash != "h3" {
		t.Fatalf("空行之后的顺序不能错位，实际 %+v", req.Rows)
	}
}

// TestUpdateEnvelopeTranslationShapeRejected 译文行的严格形状：缺键 / 多余键 / 非字符串 / null 全拒。
func TestUpdateEnvelopeTranslationShapeRejected(t *testing.T) {
	for _, tc := range []struct {
		name string
		rows string
	}{
		{"missing target", `[{"context":"c","hash":"h"}]`},
		{"missing context", `[{"hash":"h","target":"t"}]`},
		{"missing hash", `[{"context":"c","target":"t"}]`},
		{"extra key", `[{"context":"c","hash":"h","target":"t","extra":1}]`},
		{"non-string value", `[{"context":"c","hash":"h","target":1}]`},
		{"null value", `[{"context":"c","hash":"h","target":null}]`},
		{"rows null", `null`},
		{"rows object", `{}`},
		{"element not object", `["x"]`},
		{"rows string", `""`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := jsonEnv(t, translationJSON(tc.rows)); !errors.Is(err, navigationdto.ErrUpdateEnvelopeShape) {
				t.Fatalf("必须以 ErrUpdateEnvelopeShape 拒绝，实际 %v", err)
			}
			if _, err := formEnv(t, translationForm(tc.rows)); !errors.Is(err, navigationdto.ErrUpdateEnvelopeShape) {
				t.Fatalf("表单投放同样必须拒绝，实际 %v", err)
			}
		})
	}
}

// TestUpdateEnvelopeTranslationRowLimit 行数上限：恰好 1000 收下，1001 拒（不截断）。
func TestUpdateEnvelopeTranslationRowLimit(t *testing.T) {
	env, err := jsonEnv(t, translationJSON(bigRows(1000)))
	if err != nil {
		t.Fatalf("1000 行必须通过，实际 %v", err)
	}
	req, err := env.DecodeTranslation()
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if len(req.Rows) != 1000 {
		t.Fatalf("want 1000 rows, got %d", len(req.Rows))
	}
	if _, err := jsonEnv(t, translationJSON(bigRows(1001))); !errors.Is(err, navigationdto.ErrUpdateEnvelopeShape) {
		t.Fatalf("1001 行必须拒绝（不截断），实际 %v", err)
	}
	if _, err := formEnv(t, translationForm(bigRows(1001))); !errors.Is(err, navigationdto.ErrUpdateEnvelopeShape) {
		t.Fatalf("表单 1001 行必须拒绝，实际 %v", err)
	}
}

// TestUpdateEnvelopeTranslationTopLevelStrict project / lang / rows 的缺键、null、非字符串、
// 空串、未知语言全拒；未知 hash 属业务（不在这里）。
func TestUpdateEnvelopeTranslationTopLevelStrict(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"missing project", fmt.Sprintf(`{"action":"translations","lang":%q,"rows":[]}`, langOK())},
		{"project null", fmt.Sprintf(`{"action":"translations","project":null,"lang":%q,"rows":[]}`, langOK())},
		{"project empty", fmt.Sprintf(`{"action":"translations","project":"","lang":%q,"rows":[]}`, langOK())},
		{"project not string", fmt.Sprintf(`{"action":"translations","project":1,"lang":%q,"rows":[]}`, langOK())},
		{"missing lang", `{"action":"translations","project":"p-1","rows":[]}`},
		{"lang null", `{"action":"translations","project":"p-1","lang":null,"rows":[]}`},
		{"unknown lang", `{"action":"translations","project":"p-1","lang":"xx-YY","rows":[]}`},
		{"missing rows", fmt.Sprintf(`{"action":"translations","project":"p-1","lang":%q}`, langOK())},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, err := jsonEnv(t, tc.body)
			if err != nil {
				if errors.Is(err, navigationdto.ErrUpdateEnvelopeShape) {
					return
				}
				t.Fatalf("want shape error, got %v", err)
			}
			if _, err := env.DecodeTranslation(); !errors.Is(err, navigationdto.ErrUpdateEnvelopeShape) {
				t.Fatalf("want ErrUpdateEnvelopeShape, got %v", err)
			}
		})
	}
}

// TestUpdateEnvelopeTranslationTopLevelWhitelist 译文分支顶层是严格白名单：未知键拒，
// 公共 CSRF 表单字段放行（它不是业务键）。JSON 顶层同样不放宽其它键。
func TestUpdateEnvelopeTranslationTopLevelWhitelist(t *testing.T) {
	rows := `[{"context":"c","hash":"h","target":"t"}]`
	for _, tc := range []struct {
		name   string
		body   string
		accept bool
	}{
		{"CSRF 表单字段放行", fmt.Sprintf(`{"action":"translations","project":"p-1","lang":%q,"rows":%s,"csrf_token":"tok"}`, langOK(), rows), true},
		{"未知顶层键拒绝", fmt.Sprintf(`{"action":"translations","project":"p-1","lang":%q,"rows":%s,"id":"n-1"}`, langOK(), rows), false},
		{"专属键 dir 拒绝", fmt.Sprintf(`{"action":"translations","project":"p-1","lang":%q,"rows":%s,"dir":"up"}`, langOK(), rows), false},
		{"专属键 projectId 拒绝", fmt.Sprintf(`{"action":"translations","project":"p-1","lang":%q,"rows":%s,"projectId":"p-2"}`, langOK(), rows), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := jsonEnv(t, tc.body)
			if tc.accept {
				if err != nil {
					t.Fatalf("应接受，实际 %v（body=%s）", err, tc.body)
				}
				return
			}
			if !errors.Is(err, navigationdto.ErrUpdateEnvelopeShape) {
				t.Fatalf("未知顶层键须以 ErrUpdateEnvelopeShape 拒绝，实际 %v（body=%s）", err, tc.body)
			}
		})
	}
}

// TestUpdateEnvelopeTranslationTopLevelWhitelistOnForm 同一白名单在表单上同样生效。
func TestUpdateEnvelopeTranslationTopLevelWhitelistOnForm(t *testing.T) {
	rows := `[{"context":"c","hash":"h","target":"t"}]`
	ok := translationForm(rows)
	ok.Set("csrf_token", "tok")
	if _, err := formEnv(t, ok); err != nil {
		t.Fatalf("带 csrf_token 的表单译文提交应被接受，实际 %v", err)
	}
	bad := translationForm(rows)
	bad.Set("unexpected", "x")
	if _, err := formEnv(t, bad); !errors.Is(err, navigationdto.ErrUpdateEnvelopeShape) {
		t.Fatalf("表单未知键必须拒绝，实际 %v", err)
	}
}

// TestUpdateEnvelopeMoveRequest move 档提供已解析形状，不重读 Body。
func TestUpdateEnvelopeMoveRequest(t *testing.T) {
	env, err := jsonEnv(t, `{"action":"move","id":"n-1","dir":"up","projectId":"p-1","kind":"x"}`)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if got := env.Route(); got != navigationdto.RouteMove {
		t.Fatalf("want move, got %v", got)
	}
	req, err := env.MoveRequest()
	if err != nil {
		t.Fatalf("MoveRequest 失败: %v", err)
	}
	if *req != (navigationdto.MoveUpdateReq{ID: "n-1", ProjectID: "p-1", Dir: "up"}) {
		t.Fatalf("move 形状不符（含 kind 不在结构内）: %+v", *req)
	}
	form, err := formEnv(t, url.Values{"action": {"move"}, "id": {"n-1"}, "dir": {"down"}})
	if err != nil {
		t.Fatalf("表单解析失败: %v", err)
	}
	formReq, err := form.MoveRequest()
	if err != nil {
		t.Fatalf("表单 MoveRequest 失败: %v", err)
	}
	if *formReq != (navigationdto.MoveUpdateReq{ID: "n-1", Dir: "down"}) {
		t.Fatalf("表单 move 形状不符: %+v", *formReq)
	}
	legacy, _ := jsonEnv(t, `{"id":"n-1"}`)
	if _, err := legacy.MoveRequest(); !errors.Is(err, navigationdto.ErrUpdateEnvelopeShape) {
		t.Fatalf("非 move 档不得产出 move 形状，实际 %v", err)
	}
}

// TestUpdateEnvelopeDecodeLegacyKeepsRequired 普通更新在原处跑既有 required 校验。
func TestUpdateEnvelopeDecodeLegacyKeepsRequired(t *testing.T) {
	env, err := jsonEnv(t, `{"id":"n-1","title":"home","sortOrder":3}`)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	req, err := env.DecodeLegacy()
	if err != nil {
		t.Fatalf("DecodeLegacy 失败: %v", err)
	}
	if req.ID != "n-1" || req.Title == nil || *req.Title != "home" {
		t.Fatalf("legacy 字段不符: %+v", req)
	}
	if req.SortOrder == nil || *req.SortOrder != 3 {
		t.Fatalf("sortOrder 必须是 *int 3，实际 %v", req.SortOrder)
	}
	if req.Path != nil || req.ParentID != nil {
		t.Fatalf("未出现的字段必须保持 nil（不改动语义），实际 %+v", req)
	}

	missing, err := jsonEnv(t, `{"title":"home"}`)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if _, err := missing.DecodeLegacy(); !errors.Is(err, navigationdto.ErrUpdateEnvelopeShape) {
		t.Fatalf("缺 id 必须被 required 拦下，实际 %v", err)
	}
}

// TestUpdateEnvelopeDecodeLegacyOnForm 表单投放同样还原成旧 UpdateReq（URL 编码字节不进 JSON 绑定）。
func TestUpdateEnvelopeDecodeLegacyOnForm(t *testing.T) {
	env, err := formEnv(t, url.Values{"id": {"n-1"}, "title": {"home"}, "sortOrder": {"2"}, "pageId": {"ignored"}})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	req, err := env.DecodeLegacy()
	if err != nil {
		t.Fatalf("DecodeLegacy 失败: %v", err)
	}
	if req.ID != "n-1" || req.Title == nil || *req.Title != "home" {
		t.Fatalf("表单 legacy 字段不符: %+v", req)
	}
	if req.SortOrder == nil || *req.SortOrder != 2 {
		t.Fatalf("表单 sortOrder 必须还原成 2，实际 %v", req.SortOrder)
	}
	bad := url.Values{"title": {"home"}}
	badEnv, err := formEnv(t, bad)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if _, err := badEnv.DecodeLegacy(); !errors.Is(err, navigationdto.ErrUpdateEnvelopeShape) {
		t.Fatalf("表单缺 id 必须被 required 拦下，实际 %v", err)
	}
}

// TestUpdateEnvelopeMoveKeepsLegacyContract move 判别不受 projectId / 额外键影响。
func TestUpdateEnvelopeMoveKeepsLegacyContract(t *testing.T) {
	for _, body := range []string{
		`{"action":"move","id":"n-1","dir":"up"}`,
		`{"action":"move","id":"n-1","dir":"down","projectId":"p-1"}`,
		`{"action":"move","id":"n-1","dir":"down","kind":"x"}`,
	} {
		env, err := jsonEnv(t, body)
		if err != nil {
			t.Fatalf("解析失败: %v（body=%s）", err, body)
		}
		if got := env.Route(); got != navigationdto.RouteMove {
			t.Fatalf("want move, got %v (body=%s)", got, body)
		}
	}
}

// TestUpdateEnvelopeRowsEmptyVsAbsent 区分「零行提交」与「不是译文分支」。
func TestUpdateEnvelopeRowsEmptyVsAbsent(t *testing.T) {
	legacy, err := jsonEnv(t, `{"id":"n-1","title":"home"}`)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if legacy.Rows != nil {
		t.Fatalf("legacy 档 Rows 必须是 nil，实际 %#v", legacy.Rows)
	}
	env, err := jsonEnv(t, translationJSON(`[]`))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if env.Rows == nil || len(env.Rows) != 0 {
		t.Fatalf("零行提交必须是非 nil 空切片，实际 %#v", env.Rows)
	}
}

// TestUpdateEnvelopeIsForm 形态事实只来自解析入口。
func TestUpdateEnvelopeIsForm(t *testing.T) {
	j, _ := jsonEnv(t, translationJSON(`[]`))
	if j.IsForm() {
		t.Fatal("JSON 路 IsForm 必须是 false")
	}
	f, _ := formEnv(t, translationForm(`[]`))
	if !f.IsForm() {
		t.Fatal("表单路 IsForm 必须是 true")
	}
}

// TestUpdateEnvelopeRepeatedKeysFollowMapSemantics 重复键取最后一个（JSON 与表单一致）。
func TestUpdateEnvelopeRepeatedKeysFollowMapSemantics(t *testing.T) {
	env, err := jsonEnv(t, `{"action":"delete","action":"move","id":"n-1","dir":"up"}`)
	if err != nil {
		t.Fatalf("JSON 重复 action 应成功: %v", err)
	}
	if got := env.Route(); got != navigationdto.RouteMove {
		t.Fatalf("JSON 重复 action 必须取最后一个（move），实际 %v", got)
	}
	values := url.Values{"action": {"delete", "move"}, "id": {"n-1"}, "dir": {"up"}}
	form, err := formEnv(t, values)
	if err != nil {
		t.Fatalf("表单解析失败: %v", err)
	}
	if got := form.Route(); got != navigationdto.RouteMove {
		t.Fatalf("表单重复 action 必须取最后一个（move），实际 %v", got)
	}
}

// TestUpdateEnvelopeJSONBytesNotFormEncoded 表单路的键值来自 url.Values，不是把 URL 编码
// 字节再交给 JSON 解析 —— 一个含 & / = / % 的工程 id 必须原样到达业务字段。
func TestUpdateEnvelopeJSONBytesNotFormEncoded(t *testing.T) {
	tricky := "p&1=2%20x"
	values := translationForm(`[]`)
	values.Set("project", tricky)
	env, err := formEnv(t, values)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	req, err := env.DecodeTranslation()
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if req.ProjectID != tricky {
		t.Fatalf("工程 id 必须原样保留，实际 %q", req.ProjectID)
	}
}

// TestUpdateEnvelopeFormScalars 表单字符串按 JSON 标量还原后交给旧绑定（只在 legacy 档）。
func TestUpdateEnvelopeFormScalars(t *testing.T) {
	env, err := formEnv(t, url.Values{"id": {"n-1"}, "parentId": {"null"}})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	req, err := env.DecodeLegacy()
	if err != nil {
		t.Fatalf("DecodeLegacy 失败: %v", err)
	}
	if req.ParentID != nil {
		t.Fatalf("表单 null 必须还原成 nil（清除面板的语义不变），实际 %v", req.ParentID)
	}
}
