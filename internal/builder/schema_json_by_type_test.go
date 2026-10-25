package builder

// schema_json_by_type_test.go — ComponentSchemas 的**纯函数契约**。
//
// 这条契约是修一个活的 data race 时定下来的：该函数曾是 sync.Once 缓存的共享 map，
// 工作台 handler 直接往里 merge 插件 schema，于是（1）并发读写撞 race，
// （2）插件 schema 写进进程级缓存后永不消失 —— 插件禁用后仍留在 wb-schemas 里。
//
// 判据只有两条，但两条都会在「把缓存加回去」时立刻变红：
//   - 两次调用返回**不同的 map**（按值返回，不按引用）；
//   - 改第一张 map，第二张与后续调用都看不见。

import (
	json "go_wp/pkg/jsonx"
	"testing"
)

func TestComponentSchemasReturnsFreshMap(t *testing.T) {
	first, err := ComponentSchemas()
	if err != nil {
		t.Fatalf("生成 schema 失败: %v", err)
	}
	if len(first) == 0 {
		t.Fatal("schema 为空：组件注册表可能未初始化")
	}
	const probe = "__schema_purity_probe__"
	first[probe] = json.RawMessage("{ 这不是 JSON")

	second, err := ComponentSchemas()
	if err != nil {
		t.Fatalf("第二次生成 schema 失败: %v", err)
	}
	if _, leaked := second[probe]; leaked {
		t.Error("改第一次的返回值污染了第二次的返回值：ComponentSchemas 返回了共享 map，" +
			"调用方（工作台 handler 合并插件 schema）会再次读写同一张全局 map —— data race 与跨请求残留都回来了")
	}
	if _, ok := first[probe]; !ok {
		t.Fatal("探针没写进 first：本次测试的注入手法失效，断言变得没有意义")
	}
	// 第三次调用同样干净：污染不能顺着任何缓存传下去。
	third, err := ComponentSchemas()
	if err != nil {
		t.Fatalf("第三次生成 schema 失败: %v", err)
	}
	if _, leaked := third[probe]; leaked {
		t.Error("探针出现在第三次调用的返回值里：存在一条进程级缓存路径")
	}
}

// TestComponentSchemasConcurrentCallers 并发调用不得撞 race（-race 下才有意义）。
//
// 覆盖的是**真实调用形态**：多个请求各自取 schema、各自往自己的表里 merge 一条
// 插件 schema、再序列化。修复前这一段是 runtime.mapassign_faststr vs reflect.maplen。
func TestComponentSchemasConcurrentCallers(t *testing.T) {
	const workers = 8
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func() {
			schemas, err := ComponentSchemas()
			if err != nil {
				errs <- err
				return
			}
			schemas["plugin.example.button"] = json.RawMessage(`[]`)
			_, err = json.Marshal(schemas)
			errs <- err
		}()
	}
	for i := 0; i < workers; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("并发调用失败: %v", err)
		}
	}
	// 收尾：并发写完之后，正常调用仍然只看到内置组件。
	schemas, err := ComponentSchemas()
	if err != nil {
		t.Fatalf("生成 schema 失败: %v", err)
	}
	if _, leaked := schemas["plugin.example.button"]; leaked {
		t.Error("并发调用的写入泄漏到了后续请求：存在共享 map")
	}
}
