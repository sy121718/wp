package workbenchhttp

// workbench_schemas.go — 画布「组件 Inspector schema」的唯一取数口。
//
// 为什么有这个文件：builder.ComponentSchemas() 是纯函数（每次返回新 map），而
// 已注册组件的 PropsSpec 是**编译期静态**的 —— 每来一个画布请求就重新反射 46 份
// schema，纯属浪费。所以缓存留在这里，但形态变了：
//
//	· **只读快照**：建好后没有任何代码写它。写它曾经是活的 data race（工作台 handler
//	  直接往全局 map 里 merge 插件 schema），也是「插件禁用后 schema 永久残留」的来源。
//	· **每次发副本**：componentSchemaSnapshot() 返回 maps.Clone，调用方写自己那张。
//	  插件 schema 因此永远进不了缓存，与「一次请求取几次」无关。
//
// 只读是靠纪律而不是靠类型（Go 的 map 没有只读形态）：本文件之外不许出现对返回值的写入，
// 唯一的写点是 componentsForCanvas 合并插件 schema 之前的 maps.Clone。测试要造 500 也不再
// 靠污染这张 map —— 注入点见 schemaSnapshotForTest。

import (
	"maps"
	"sync"

	json "go_wp/pkg/jsonx"

	"go_wp/internal/builder"
	plugincontract "go_wp/internal/module/plugin/contract"
)

// componentSchemaSnapshot 进程级只读快照（见文件头）。
//
// **单实例前提**（架构文档「部署形态」）：快照在进程内缓存一次，插件启停后只有收到那次
// 请求的实例会重建（实际上连它也不会 —— 进程重启才重建）。多实例时各实例的检查器 schema
// 可能不同，表现为「同一个组件在两台机器上字段不一样」。
//
// 改造方向：以 plugincontract.Assembly.Fingerprint 为键做带失效的缓存，指纹变化即重建。
var componentSchemaSnapshot = sync.OnceValues(func() (map[string]json.RawMessage, error) {
	return builder.ComponentSchemas()
})

// componentSchemaSnapshotFunc 快照取数的间接层：生产恒为 componentSchemaSnapshot，
// 测试可替换（造「schema 生成 / 序列化失败」两条不可端到端注入的分支）。
//
// 上一版测试往共享 map 里塞非法 JSON 来触发 500 —— 那套手法要求缺陷存在。现在分支由
// 注入点驱动，测的是 handler 自己的失败路径，与缓存实现解耦。
var componentSchemaSnapshotFunc = componentSchemaSnapshot

// componentsForCanvas 画出画布用的 schema 表：内置组件快照 + 启用插件的检查器 schema。
//
// 返回的是**调用方自有的一份**（快照的 clone），插件合并只发生在这份上。
func componentsForCanvas(asm *plugincontract.Assembly) (map[string]json.RawMessage, error) {
	base, err := componentSchemaSnapshotFunc()
	if err != nil {
		return nil, err
	}
	out := maps.Clone(base)
	if out == nil {
		// base 为 nil（理论上不会：错误已在上面返回），clone 也就 nil —— 显式兜一个空表，
		// 让调用方的 json.Marshal 得到 "{}" 而不是 "null"（前端 JSON.parse('null') 是 null，
		// （接 .x 会抛 TypeError，与空表是两种故障形态）。
		out = map[string]json.RawMessage{}
	}
	if asm == nil {
		return out, nil
	}
	for t, data := range asm.InspectorSchemas {
		out[t] = data
	}
	return out, nil
}
