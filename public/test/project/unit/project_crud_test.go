package unit

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	projectdto "go_wp/internal/module/project/dto"
	projectenums "go_wp/internal/module/project/enums"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"

	"go_wp/public/test/support"
)

// newProjectService 创建隔离 PG schema 并 AutoMigrate projects/themes 两表。
func newProjectService(t *testing.T) *projectservice.Service {
	t.Helper()
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用，跳过测试：%v", err)
		return nil
	}
	if err := db.AutoMigrate(&projectmodel.ProjectEntity{}, &projectmodel.ThemeEntity{}); err != nil {
		t.Fatalf("AutoMigrate projects/themes 失败: %v", err)
	}
	return projectservice.NewService(projectmodel.NewProjectModel(db))
}

func wantErrContain(t *testing.T, err error, msg string) {
	t.Helper()
	if err == nil {
		t.Fatalf("应返回错误，实际 err=nil")
	}
	if !strings.Contains(err.Error(), msg) {
		t.Fatalf("错误消息应包含 %q，实际: %v", msg, err)
	}
}

// assertJSONEqual 按 JSON 语义比较（PG jsonb 规范化存储会重排键序/空格，
// 精确字符串比较会误报）。
func assertJSONEqual(t *testing.T, got json.RawMessage, want string) {
	t.Helper()
	var gotV, wantV any
	if err := json.Unmarshal(got, &gotV); err != nil {
		t.Fatalf("got 不是合法 JSON（%s）: %v", string(got), err)
	}
	if err := json.Unmarshal([]byte(want), &wantV); err != nil {
		t.Fatalf("want 不是合法 JSON（%s）: %v", want, err)
	}
	if !reflect.DeepEqual(gotV, wantV) {
		t.Fatalf("JSON 不等：got=%s want=%s", string(got), want)
	}
}

// TestProjectCreateNilRejected 创建工程：nil 请求被拒绝，返回参数错误
// （ErrInvalidParam）而非名称校验错误（ErrInvalidName 语义只针对名称本身）。
func TestProjectCreateNilRejected(t *testing.T) {
	svc := newProjectService(t)
	_, err := svc.Create(context.Background(), nil)
	wantErrContain(t, err, projectenums.ErrInvalidParam)
}

// TestProjectCreateBlankNameRejected 创建工程：空白名被拒绝。
func TestProjectCreateBlankNameRejected(t *testing.T) {
	svc := newProjectService(t)
	for _, name := range []string{"", "   ", "\t\n"} {
		_, err := svc.Create(context.Background(), &projectdto.CreateReq{Name: name})
		wantErrContain(t, err, projectenums.ErrInvalidName)
	}
}

// TestProjectCreateNameBoundary 创建工程：200 字边界（200 放行、201 拒绝）。
func TestProjectCreateNameBoundary(t *testing.T) {
	svc := newProjectService(t)
	ctx := context.Background()

	t.Run("200字通过", func(t *testing.T) {
		name := strings.Repeat("中", 200)
		res, err := svc.Create(ctx, &projectdto.CreateReq{Name: name})
		if err != nil {
			t.Fatalf("200 字名称应放行: %v", err)
		}
		if res.Name != name {
			t.Fatalf("名称被改变: got=%q want=%q", res.Name, name)
		}
	})

	t.Run("201字拒绝", func(t *testing.T) {
		_, err := svc.Create(ctx, &projectdto.CreateReq{Name: strings.Repeat("中", 201)})
		wantErrContain(t, err, projectenums.ErrInvalidName)
	})

	t.Run("多字节边界", func(t *testing.T) {
		// 20 个 emoji（每个 4 字节）应被 RuneCount 计为 20 个字符而非 80 字节。
		name := strings.Repeat("😀", 20)
		res, err := svc.Create(ctx, &projectdto.CreateReq{Name: name})
		if err != nil {
			t.Fatalf("按 rune 计数的名称应放行: %v", err)
		}
		if res.Name != name {
			t.Fatalf("名称被改变: %q", res.Name)
		}
	})
}

// TestProjectCreateSettingsVariants 创建工程：settings 各形态。
func TestProjectCreateSettingsVariants(t *testing.T) {
	svc := newProjectService(t)
	ctx := context.Background()

	cases := []struct {
		name     string
		settings json.RawMessage
	}{
		{"Null字面量", json.RawMessage("null")},
		{"空数组", json.RawMessage("[]")},
		{"字符串", json.RawMessage(`"str"`)},
		{"数字", json.RawMessage(`42`)},
		{"非法JSON", json.RawMessage(`{bad`)},
		{"截断JSON", json.RawMessage(`{"a":`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Create(ctx, &projectdto.CreateReq{Name: "工程", Settings: tc.settings})
			wantErrContain(t, err, projectenums.ErrInvalidSettings)
		})
	}

	t.Run("空兜底为空对象", func(t *testing.T) {
		res, err := svc.Create(ctx, &projectdto.CreateReq{Name: "工程", Settings: nil})
		if err != nil {
			t.Fatalf("settings 为空应兜底 {}: %v", err)
		}
		if string(res.Settings) != "{}" {
			t.Fatalf("空 settings 应规范化为 {}，实际: %s", string(res.Settings))
		}
	})

	t.Run("合法对象", func(t *testing.T) {
		res, err := svc.Create(ctx, &projectdto.CreateReq{Name: "工程", Settings: json.RawMessage(`{"locale":"zh-CN","theme":"light"}`)})
		if err != nil {
			t.Fatalf("合法对象应放行: %v", err)
		}
		var obj map[string]any
		if err := json.Unmarshal(res.Settings, &obj); err != nil {
			t.Fatalf("返回 settings 应为合法 JSON: %v", err)
		}
		if obj["locale"] != "zh-CN" || obj["theme"] != "light" {
			t.Fatalf("settings 内容丢失: %s", string(res.Settings))
		}
	})
}

// TestProjectDetailEdge Detail：空请求/空 ID/不存在。
func TestProjectDetailEdge(t *testing.T) {
	svc := newProjectService(t)
	ctx := context.Background()

	t.Run("Nil请求", func(t *testing.T) {
		_, err := svc.Detail(ctx, nil)
		wantErrContain(t, err, projectenums.ErrProjectNotFound)
	})

	t.Run("空ID", func(t *testing.T) {
		_, err := svc.Detail(ctx, &projectdto.DetailReq{ID: "  "})
		wantErrContain(t, err, projectenums.ErrProjectNotFound)
	})

	t.Run("不存在ID", func(t *testing.T) {
		_, err := svc.Detail(ctx, &projectdto.DetailReq{ID: "00000000-0000-0000-0000-000000000000"})
		wantErrContain(t, err, projectenums.ErrProjectNotFound)
	})

	t.Run("存在", func(t *testing.T) {
		created, err := svc.Create(ctx, &projectdto.CreateReq{Name: "官网"})
		if err != nil {
			t.Fatalf("预置工程失败: %v", err)
		}
		got, err := svc.Detail(ctx, &projectdto.DetailReq{ID: created.ID})
		if err != nil {
			t.Fatalf("Detail 应成功: %v", err)
		}
		if got.ID != created.ID || got.Name != "官网" {
			t.Fatalf("Detail 返回不一致: %+v", got)
		}
	})
}

// TestProjectUpdateEdge Update：空请求/空 ID/不存在/空白名/非法 settings/正常更新。
func TestProjectUpdateEdge(t *testing.T) {
	svc := newProjectService(t)
	ctx := context.Background()
	created, err := svc.Create(ctx, &projectdto.CreateReq{Name: "原工程", Settings: json.RawMessage(`{"a":1}`)})
	if err != nil {
		t.Fatalf("预置工程失败: %v", err)
	}

	t.Run("Nil请求", func(t *testing.T) {
		_, err := svc.Update(ctx, nil)
		wantErrContain(t, err, projectenums.ErrInvalidParam)
	})

	t.Run("空ID", func(t *testing.T) {
		_, err := svc.Update(ctx, &projectdto.UpdateReq{ID: "  ", Name: "新名"})
		wantErrContain(t, err, projectenums.ErrProjectNotFound)
	})

	t.Run("不存在ID", func(t *testing.T) {
		_, err := svc.Update(ctx, &projectdto.UpdateReq{ID: "00000000-0000-0000-0000-000000000000", Name: "新名"})
		wantErrContain(t, err, projectenums.ErrProjectNotFound)
	})

	t.Run("空白名", func(t *testing.T) {
		_, err := svc.Update(ctx, &projectdto.UpdateReq{ID: created.ID, Name: "   "})
		wantErrContain(t, err, projectenums.ErrInvalidName)
	})

	t.Run("Settings非法", func(t *testing.T) {
		_, err := svc.Update(ctx, &projectdto.UpdateReq{ID: created.ID, Name: "新名", Settings: json.RawMessage(`[1,2]`)})
		wantErrContain(t, err, projectenums.ErrInvalidSettings)
	})

	t.Run("正常更新", func(t *testing.T) {
		res, err := svc.Update(ctx, &projectdto.UpdateReq{ID: created.ID, Name: "新工程", Settings: json.RawMessage(`{"theme":"dark"}`)})
		if err != nil {
			t.Fatalf("正常更新失败: %v", err)
		}
		if res.Name != "新工程" {
			t.Fatalf("更新名称错误: %+v", res)
		}
		assertJSONEqual(t, res.Settings, `{"theme":"dark"}`)
		// 回读验证持久化。
		got, err := svc.Detail(ctx, &projectdto.DetailReq{ID: created.ID})
		if err != nil {
			t.Fatalf("回读失败: %v", err)
		}
		if got.Name != "新工程" {
			t.Fatalf("DB 中名称不一致: %+v", got)
		}
		assertJSONEqual(t, got.Settings, `{"theme":"dark"}`)
	})
}

// TestProjectExistsEdge Exists：空 ID/不存在/存在。
func TestProjectExistsEdge(t *testing.T) {
	svc := newProjectService(t)
	ctx := context.Background()

	t.Run("空ID", func(t *testing.T) {
		exists, err := svc.Exists(ctx, "  ")
		if err != nil || exists {
			t.Fatalf("空 ID 应返回 (false,nil)，实际 exists=%t err=%v", exists, err)
		}
	})

	t.Run("不存在", func(t *testing.T) {
		exists, err := svc.Exists(ctx, "00000000-0000-0000-0000-000000000000")
		if err != nil || exists {
			t.Fatalf("不存在应返回 (false,nil)，实际 exists=%t err=%v", exists, err)
		}
	})

	t.Run("存在", func(t *testing.T) {
		created, err := svc.Create(ctx, &projectdto.CreateReq{Name: "官网"})
		if err != nil {
			t.Fatalf("预置工程失败: %v", err)
		}
		exists, err := svc.Exists(ctx, created.ID)
		if err != nil || !exists {
			t.Fatalf("存在应返回 (true,nil)，实际 exists=%t err=%v", exists, err)
		}
	})
}

// TestNormalizeSettingsThroughCreate normalizeSettings 纯逻辑覆盖：
// normalizeSettings 为 service 包私有函数且仅被 Create 调用，
// 故经由 Create 入口逐分支断言其行为。
func TestNormalizeSettingsThroughCreate(t *testing.T) {
	svc := newProjectService(t)
	ctx := context.Background()

	t.Run("空与nil兜底为空对象", func(t *testing.T) {
		for _, raw := range []json.RawMessage{nil, json.RawMessage(""), json.RawMessage("{}")} {
			res, err := svc.Create(ctx, &projectdto.CreateReq{Name: "工程", Settings: raw})
			if err != nil {
				t.Fatalf("settings=%q 应兜底 {}: %v", string(raw), err)
			}
			if string(res.Settings) != "{}" {
				t.Fatalf("settings=%q 规范化后应 {}, 实际: %s", string(raw), string(res.Settings))
			}
		}
	})

	t.Run("非对象拒绝", func(t *testing.T) {
		for _, raw := range []json.RawMessage{
			json.RawMessage("null"), json.RawMessage("[]"), json.RawMessage(`"str"`),
			json.RawMessage("42"), json.RawMessage("true"), json.RawMessage("{bad"),
		} {
			_, err := svc.Create(ctx, &projectdto.CreateReq{Name: "工程", Settings: raw})
			wantErrContain(t, err, projectenums.ErrInvalidSettings)
		}
	})

	t.Run("对象重新序列化", func(t *testing.T) {
		res, err := svc.Create(ctx, &projectdto.CreateReq{Name: "工程", Settings: json.RawMessage(`{"b":1,"a":2}`)})
		if err != nil {
			t.Fatalf("合法对象应放行: %v", err)
		}
		// Go encoding/json 对 map 键排序，输出应为 {"a":2,"b":1}。
		if string(res.Settings) != `{"a":2,"b":1}` {
			t.Fatalf("对象应被规范化（键排序），实际: %s", string(res.Settings))
		}
	})

	t.Run("嵌套对象保留", func(t *testing.T) {
		res, err := svc.Create(ctx, &projectdto.CreateReq{Name: "工程", Settings: json.RawMessage(`{"outer":{"inner":[1,2,3]}}`)})
		if err != nil {
			t.Fatalf("嵌套对象应放行: %v", err)
		}
		var obj map[string]any
		if err := json.Unmarshal(res.Settings, &obj); err != nil {
			t.Fatalf("结果应为合法 JSON: %v", err)
		}
		inner, ok := obj["outer"].(map[string]any)
		if !ok {
			t.Fatalf("嵌套结构丢失: %s", string(res.Settings))
		}
		if _, ok := inner["inner"]; !ok {
			t.Fatalf("嵌套 inner 丢失: %s", string(res.Settings))
		}
	})
}
