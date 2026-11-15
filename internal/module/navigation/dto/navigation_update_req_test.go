package navigationdto_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	navigationdto "go_wp/internal/module/navigation/dto"
	"go_wp/pkg/i18n"
)

func langOK() string {
	langs := i18n.AvailableLangs()
	if len(langs) == 0 {
		return "en-US"
	}
	return langs[0]
}

func jsonEnv(t *testing.T, body string) (*navigationdto.UpdateEnvelope, error) {
	t.Helper()
	env := &navigationdto.UpdateEnvelope{}
	err := env.UnmarshalJSON([]byte(body))
	return env, err
}

func translationJSON(rows string) string {
	return fmt.Sprintf(`{"action":"translations","project":"p-1","lang":%q,"rows":%s}`, langOK(), rows)
}

func bigRows(n int) string {
	rows := make([]string, 0, n)
	for range n {
		rows = append(rows, `{"context":"c","hash":"h","target":"t"}`)
	}
	return "[" + strings.Join(rows, ",") + "]"
}

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

func TestUpdateEnvelopeLegacyToleratesUnknownKeys(t *testing.T) {
	body := `{"id":"n-1","title":"home","unknownField":123,"another":{"deep":true}}`
	env, err := jsonEnv(t, body)
	if err != nil {
		t.Fatalf("未知键应被忽略，实际 %v", err)
	}
	if got := env.Route(); got != navigationdto.RouteLegacy {
		t.Fatalf("未知键应判为 legacy，实际 %v", got)
	}
}

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

func TestUpdateEnvelopeDecodeTranslation(t *testing.T) {
	rows := `[{"context":"nav.header","hash":"h1","target":"Home"},{"context":"nav.footer","hash":"h2","target":""}]`
	env, err := jsonEnv(t, translationJSON(rows))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	req, err := env.DecodeTranslation()
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if req.ProjectID != "p-1" || req.Lang != langOK() {
		t.Fatalf("顶层字段不符: %+v", req)
	}
	if len(req.Rows) != 2 || req.Rows[0].Target != "Home" || req.Rows[1].Target != "" {
		t.Fatalf("行不得解包成 JSON 文本，实际 %+v", req.Rows)
	}
}

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
		})
	}
}

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
}

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

func TestUpdateEnvelopeTranslationTopLevelWhitelist(t *testing.T) {
	rows := `[{"context":"c","hash":"h","target":"t"}]`
	for _, tc := range []struct {
		name   string
		body   string
		accept bool
	}{
		{"CSRF 字段放行", fmt.Sprintf(`{"action":"translations","project":"p-1","lang":%q,"rows":%s,"csrf_token":"tok"}`, langOK(), rows), true},
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

func TestUpdateEnvelopeMoveRequest(t *testing.T) {
	env, err := jsonEnv(t, `{"action":"move","id":"n-1","dir":"up","projectId":"p-1"}`)
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
	legacy, _ := jsonEnv(t, `{"id":"n-1"}`)
	if _, err := legacy.MoveRequest(); !errors.Is(err, navigationdto.ErrUpdateEnvelopeShape) {
		t.Fatalf("非 move 档不得产出 move 形状，实际 %v", err)
	}
}

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

func TestUpdateEnvelopeMoveKeepsLegacyContract(t *testing.T) {
	for _, body := range []string{
		`{"action":"move","id":"n-1","dir":"up"}`,
		`{"action":"move","id":"n-1","dir":"down","projectId":"p-1"}`,
	} {
		env, err := jsonEnv(t, body)
		if err != nil {
			t.Fatalf("解析失败: %v（body=%s）", err, body)
		}
		if got := env.Route(); got != navigationdto.RouteMove {
			t.Fatalf("want move, got %v (body=%s)", got, body)
		}
	}
	for _, body := range []string{
		`{"action":"move","id":"n-1","dir":"down","kind":"x"}`,
		`{"action":"move","id":"n-1","dir":"down","rows":[]}`,
		`{"action":"move","id":"n-1","dir":"down","lang":"zh-CN"}`,
	} {
		if _, err := jsonEnv(t, body); !errors.Is(err, navigationdto.ErrUpdateEnvelopeShape) {
			t.Fatalf("白名单外的键必须以 ErrUpdateEnvelopeShape 拒绝，实际 %v（body=%s）", err, body)
		}
	}
}

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

func TestUpdateEnvelopeRepeatedKeysFollowMapSemantics(t *testing.T) {
	env, err := jsonEnv(t, `{"action":"delete","action":"move","id":"n-1","dir":"up"}`)
	if err != nil {
		t.Fatalf("JSON 重复 action 应成功: %v", err)
	}
	if got := env.Route(); got != navigationdto.RouteMove {
		t.Fatalf("JSON 重复 action 必须取最后一个（move），实际 %v", got)
	}
}
