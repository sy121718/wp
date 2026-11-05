package builder

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

	third, err := ComponentSchemas()
	if err != nil {
		t.Fatalf("第三次生成 schema 失败: %v", err)
	}
	if _, leaked := third[probe]; leaked {
		t.Error("探针出现在第三次调用的返回值里：存在一条进程级缓存路径")
	}
}

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

	schemas, err := ComponentSchemas()
	if err != nil {
		t.Fatalf("生成 schema 失败: %v", err)
	}
	if _, leaked := schemas["plugin.example.button"]; leaked {
		t.Error("并发调用的写入泄漏到了后续请求：存在共享 map")
	}
}
