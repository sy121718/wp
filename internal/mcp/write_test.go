package mcp

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	"go_wp/internal/permission"
)

type memStore struct {
	saved map[string]Result
	calls int
}

func newMemStore() *memStore { return &memStore{saved: map[string]Result{}} }

func (m *memStore) Lookup(_ context.Context, tool, key string) (Result, bool, error) {
	m.calls++
	res, ok := m.saved[tool+"|"+key]
	return res, ok, nil
}

func (m *memStore) Save(_ context.Context, tool, key string, res Result) error {
	m.saved[tool+"|"+key] = res
	return nil
}

func writeProbe(store IdempotencyStore, calls *int, needErr bool) Tool {
	return NewWrite("probe_create", "探针", "只用于测试的写工具",
		permission.ContentCreate,
		Object("业务参数", map[string]Schema{"name": String("名称")}, "name"),
		store,
		func(_ context.Context, args struct {
			Name string `json:"name"`
		}) (Result, error) {
			*calls++
			if needErr {
				return Result{}, errors.New("业务失败")
			}
			return Result{Text: "已新建 " + args.Name}, nil
		},
	)
}

func invoke(t *testing.T, tool Tool, body string) (Result, error) {
	t.Helper()
	return tool.Invoke(context.Background(), json.RawMessage(body))
}

func TestWriteSchemaInjectsConfirmAndKey(t *testing.T) {
	tool := writeProbe(newMemStore(), new(int), false)
	if tool.Name() != "probe_create" {
		t.Fatalf("工具名不对：%s", tool.Name())
	}
	schema := tool.Schema()
	props := schema.Properties
	for _, k := range []string{WriteArgConfirm, WriteArgKey, "name"} {
		if _, ok := props[k]; !ok {
			t.Errorf("schema 里缺少 %q", k)
		}
	}
	required := strings.Join(schema.Required, ",")
	for _, k := range []string{WriteArgConfirm, WriteArgKey} {
		if !strings.Contains(required, k) {
			t.Errorf("%q 必须在 required 里（否则模型不会主动给）", k)
		}
	}

	if !strings.Contains(required, "name") {
		t.Errorf("作者声明的 required 被覆盖了：%v", schema.Required)
	}
}

func TestWriteRejectsWithoutConfirm(t *testing.T) {
	calls := 0
	tool := writeProbe(newMemStore(), &calls, false)
	for _, body := range []string{
		`{"name":"a","idempotencyKey":"k1"}`,
		`{"name":"a","confirm":false,"idempotencyKey":"k1"}`,
	} {
		_, err := invoke(t, tool, body)
		var ae *ArgsError
		if !errors.As(err, &ae) {
			t.Fatalf("%s 应被拒（ArgsError），实得 %v", body, err)
		}
		if !strings.Contains(ae.Msg, "confirm") {
			t.Errorf("错误文案要告诉模型怎么改，实得：%s", ae.Msg)
		}
	}
	if calls != 0 {
		t.Fatalf("被拒时不得调用业务代码，实得 %d 次", calls)
	}
}

func TestWriteRejectsWithoutKey(t *testing.T) {
	calls := 0
	tool := writeProbe(newMemStore(), &calls, false)
	_, err := invoke(t, tool, `{"name":"a","confirm":true}`)
	var ae *ArgsError
	if !errors.As(err, &ae) || !strings.Contains(ae.Msg, "idempotencyKey") {
		t.Fatalf("应因缺幂等键被拒，实得 %v", err)
	}
	if calls != 0 {
		t.Fatal("被拒时不得调用业务代码")
	}
}

func TestWriteIdempotentReplay(t *testing.T) {
	calls := 0
	store := newMemStore()
	tool := writeProbe(store, &calls, false)

	first, err := invoke(t, tool, `{"name":"甲","confirm":true,"idempotencyKey":"same"}`)
	if err != nil {
		t.Fatal(err)
	}
	second, err := invoke(t, tool, `{"name":"甲","confirm":true,"idempotencyKey":"same"}`)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("同键只应执行一次，实得 %d 次 —— 重试会重复写入", calls)
	}
	if !strings.HasPrefix(second.Text, first.Text) {
		t.Errorf("回放要返回上次结果，实得 %q", second.Text)
	}

	if !strings.Contains(second.Text, "没有重复写入") {
		t.Errorf("回放结果要说明本次没有重复写入，实得 %q", second.Text)
	}
}

func TestWriteFailureIsNotRemembered(t *testing.T) {
	calls := 0
	store := newMemStore()
	tool := writeProbe(store, &calls, true)

	for i := 0; i < 2; i++ {
		if _, err := invoke(t, tool, `{"name":"甲","confirm":true,"idempotencyKey":"k"}`); err == nil {
			t.Fatal("业务失败应向上返回")
		}
	}
	if calls != 2 {
		t.Fatalf("失败不落幂等，两次都该真的执行，实得 %d 次", calls)
	}
	if len(store.saved) != 0 {
		t.Fatal("失败的结果不该被记住")
	}
}

func TestWriteStoreAbsentStillWorks(t *testing.T) {
	calls := 0
	tool := writeProbe(nil, &calls, false)
	if _, err := invoke(t, tool, `{"name":"甲","confirm":true,"idempotencyKey":"k"}`); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("台账缺失不应阻断写操作")
	}
}
