package mcp

// write_test.go — 写工具地基的断言（docs/17 D8：确认 + 幂等）。
//
// 这一批要挡的两类故障都不报错：
//   · 没有确认位时，模型自己觉得该写就写了 —— 调用流水里看不出「有没有人要这个改动」；
//   · 没有幂等键时，一次重试落两条 —— 两条都合法。
// 所以判据必须落在「handler 被调用了几次」「store 里记了什么」上，而不是「返回值对不对」。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"go_wp/internal/permission"
)

// memStore 幂等台账的内存实现（只给测试用）。
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

// writeProbe 造一个写工具：handler 记调用次数，并按 needErr 决定成功还是失败。
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

// invoke 走工具自己的 Invoke（与运行期同一条路径）。
func invoke(t *testing.T, tool Tool, body string) (Result, error) {
	t.Helper()
	return tool.Invoke(context.Background(), json.RawMessage(body))
}

// TestWriteSchemaInjectsConfirmAndKey 地基必须把两个字段塞进 schema 与 required。
//
// 判据落在这里而不是「文档说了要写」：让工具作者在自己的结构体里声明，漏声明的
// 失败模式是「这个工具没有确认位」，而它在接口列表里与别人长得一模一样。
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
	// 业务字段是**追加**在作者给的 required 之后的，不能被顶掉。
	if !strings.Contains(required, "name") {
		t.Errorf("作者声明的 required 被覆盖了：%v", schema.Required)
	}
}

// TestWriteRejectsWithoutConfirm confirm 缺失或 false 时**不执行 handler**。
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

// TestWriteRejectsWithoutKey 缺幂等键时同样不执行。
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

// TestWriteIdempotentReplay 同键第二次直接回上次结果，**不碰业务代码**。
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
	// 必须告诉模型「这次没真的再写一遍」，否则它会向用户汇报两次改动。
	if !strings.Contains(second.Text, "没有重复写入") {
		t.Errorf("回放结果要说明本次没有重复写入，实得 %q", second.Text)
	}
}

// TestWriteFailureIsNotRemembered 失败不落幂等，重试能真的重试。
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

// TestWriteStoreAbsentStillWorks 台账缺失时不阻断写操作（有意的降级）。
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
