package builder

import (
	mediacontract "go_wp/internal/module/media/contract"
)

// assetProbeMemo 把「按 URL 探测媒体变体」包成**单次编译作用域**的 memoize。
//
// 为什么需要：探测函数每渲染一个 <img> 节点就被调用一次（core.image 生成 srcset），
// 而它的实现方（media.Service.ProbeImageVariants）每次调用要发**两条 SQL**
// （按 file_path 取附件 → 取该附件的变体清单）。同一张图出现在 50 个节点上就是 100 次
// 查询；一次全量构建（数千页 × 每页若干图）能到十万次量级。一次编译内媒体变体不变
// —— 「确定性构建」（AGENTS.md 不变量 5）本来就建立在这个前提上 —— 命中率接近 100%。
//
// 为什么挂载点必须是「每次 Compile 新建一个」：
// 装配层（pipeline.CompileOptions、presentation_render、page_assemble 注入的 MediaProbe
// 闭包）构造的探测函数生命周期**跨构建**。而变体 URL 带 generation + 内容指纹
// （<stem>_<type>-<generation>-<hash8>.jpg）：换图、重生成变体后 generation 变化。
// 若把缓存挂在那个跨构建的闭包上，第二次构建起会继续返回旧 generation 的 URL，
// 症状是「换了图，产物里新图在所有后续构建中持续不可见」，而构建、发布、测试全绿
// —— 本仓库刚修过同源的变体引用失效问题，这里不能重犯。
// 因此缓存的有效期必须精确等于「变体不可能变」的窗口：一次 Compile 调用。
// Compile 每次都 newAssetProbeMemo 新建，编译结束即随该次 RenderContext 一起不可达，
// 进程内不存在任何跨构建残留（没有包级变量、没有 sync.Once、没有共享闭包）。
//
// 并发：Compile 内的渲染是串行的（builder.go 里 roots 逐节点渲染），故不加锁。
// 若将来渲染改为并发，这里必须补锁或换 sync.Map —— 普通 map 并发写会直接崩。
//
// 内存：缓存上限是「单个文档里出现过的图片 URL 数」（几十条），随编译整体回收；
// 挂到跨构建闭包上则会随进程见过的 URL 总量单调增长（构建 worker 常驻）。
type assetProbeMemo struct {
	probe func(string) []mediacontract.VariantRef
	cache map[string][]mediacontract.VariantRef
}

// newAssetProbeMemo 包装探测函数；probe 为 nil 时返回 nil，
// 使「未注入 WithAssetProbe」的语义原样传递（组件据此完全不输出 srcset）。
func newAssetProbeMemo(probe func(string) []mediacontract.VariantRef) func(string) []mediacontract.VariantRef {
	if probe == nil {
		return nil
	}
	m := &assetProbeMemo{
		probe: probe,
		cache: make(map[string][]mediacontract.VariantRef, 8),
	}
	return m.lookup
}

// lookup 命中直接返回缓存值，未命中才真正探测并记录结果。
//
// 用 comma-ok 判命中而不是判 len(refs)>0：**空结果也是答案**（库里绝大多数图没有变体），
// 只缓存非空结果等于对最常见的那类 URL 每次都重新查库 —— 缓存等于没做。
func (m *assetProbeMemo) lookup(url string) []mediacontract.VariantRef {
	if refs, ok := m.cache[url]; ok {
		return refs
	}
	refs := m.probe(url)
	m.cache[url] = refs
	return refs
}
