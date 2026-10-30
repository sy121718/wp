package navigationcontract

// navigation_translation_source.go — 「菜单项原文 → 译文寻址」的**共享来源解析**契约。
//
// 为什么在这里而不是 service：索引结果跨层被消费者使用，而模块之间与 inbound 之间只走
// contract 的形状。实现只有一份（navigationservice.Service.OpenSourceIndex）。
//
// 索引回答三件事：
//  1. 某个原文 hash 对应哪段服务端原文、哪个语境 —— 客户端能发的只有 hash + 译文，
//     原文一律由服务端算，绝不采信请求里带来的文本；
//  2. 这段原文出现在哪些菜单位置（kind）—— 同一段文字可以同时出现在页眉与页脚，
//     下游失效派发必须拿到全部位置；
//  3. 每个位置读取**失败**了什么 —— 读侧逐位置降级（局部展示），写侧严格整体失败。
//
// 本文件**只有事实形状**：没有一个错误 key 常量、没有一个把错误反解成 kind 的解析函数。
// 错误 key 与中文兜底住在 enums 声明处（navigationenums.ErrSourceKindFailed /
// ErrSourceKindDetail），状态码也在那里给；这里若再写一份 key，或者按 \"<key>: <kind>\"
// 反解错误文本，就是第二份协议 —— 判据会随措辞漂移且没人发现。
//
// 层归属：contract 只放接口与不可变 DTO（不 import 别的 service/model/enums、不取词）。

// TranslationSourceEntry 一个原文 hash 在本次解析中的唯一事实（不可变，值语义）。
//
// 同一 hash 只有一个语境（与 i18n.ContentIndexKey 的寻址口径一致：hash + context 唯一）；
// **位置可以有多个**：同一段文字同时出现在 header 与 footer 时，一份来源对应两个 kind。
// 这里不放「哪个节点 / 哪一行」：那是展示形状，属读侧。
type TranslationSourceEntry struct {
	// Hash 原文指纹（pkg/i18n.ContentHash，sha256hex）。
	Hash string
	// Source 服务端原文（菜单项解析后的标题）。是否参与翻译由调用方用
	// i18n.ShouldTranslateContent 判定（判据不在这里复制第二份）。
	Source string
	// Context 语境（导航标签固定为 i18n.ContentContext("navigation", "label")）。
	Context string
	// Kinds 该原文出现过的全部菜单位置，已去重，按 header → header_mobile → footer →
	// footer_mobile 的固定顺序排列（与输入顺序、map 遍历顺序无关 → 结果确定）。
	Kinds []string
	// NodeID 该原文**首次出现**的菜单项 id（读侧展示定位用；不参与 hash 与合并判定）。
	//
	// 为什么放在条目上而不是只走 SourceIndexNodes：读侧要为每一行找「这一行的节点」，
	// 走可选接口需要 type assert 一次、再过一遍节点表；首现节点已经在这里，直接可用。
	// 完整节点表（同一段原文出现在多个节点时）仍由 SourceIndexNodes.Nodes 给出。
	NodeID string
	// Path 首现节点解析后的链接（同上，展示用）。
	Path string
}

// SourceIndex 一次来源解析的不可变结果。
//
// 两种取用方式各自保留错误事实，绝不互相掩盖：
//   - 读：Locate / KindErrors —— 逐位置降级（某位置读失败只是那一组不展示）；
//   - 写：Require —— 任一位置读失败即整体失败（AGENTS.md「批量要么全成要么全败」）。
//
// 「某个位置没有菜单」与「某个位置读失败」是两件事，只有后者能让 Require 失败。
type SourceIndex interface {
	// Locate 按原文 hash 取唯一事实。未收录返回 ok=false（调用方据此判定「原文已变化」，
	// 不得回退用请求里带的文本）。
	Locate(hash string) (TranslationSourceEntry, bool)
	// Entries 全部条目，按 Hash 升序（跨次调用字节级确定，便于对拍与快照）。
	Entries() []TranslationSourceEntry
	// Kinds 本次解析涉及的菜单位置（按 kind 规范顺序；一个都没有时是空切片而不是 nil）。
	Kinds() []string
	// KindErrors 逐位置读取失败的事实（按 kind 规范顺序，只含失败的位置；无失败时为空切片）。
	KindErrors() []KindReadError
	// Require 写侧严格前缀：任一位置读失败即返回该位置业务错误（聚合全部失败位置），
	// 全部成功返回 nil。聚合错误由 enums 的 ErrSourceKindDetail 构造，调用方用
	// errors.Is(err, enums.Err(enums.ErrSourceKindFailed)) 判定（key 只在 enums 声明一次）。
	Require() error
}

// SourceIndexNodes 索引的可选展示接口：某段原文在菜单树里出现在哪些节点。
//
// 为什么是**可选**接口而不是 SourceIndex 的方法：展示形状（哪个节点、哪个链接）属读侧，
// 而 SourceIndex 的五个方法只回答事实。实现方（navigationservice）在收集时顺手记下了
// 节点引用，读侧按需断言；断言不到就少展示一列链接，不影响任何事实。
//
// 返回切片按树内前序、跨位置按规范 kind 顺序；调用方不得修改（值语义由实现保证）。
type SourceIndexNodes interface {
	// Nodes 该 hash 对应的全部节点（同一段原文在多个位置/多个菜单项里出现时为多条）。
	Nodes(hash string) []NavigationNodeRef
}

// NavigationNodeRef 一个菜单节点的最小引用（展示用的链接与定位，不含业务字段）。
type NavigationNodeRef struct {
	// ID 菜单项 id（页面只用它做展示定位，不做写）。
	ID string
	// Kind 该节点所在的菜单位置。
	Kind string
	// Path 该节点解析后的链接（来源实体解析成功才是来源的 URL，否则是记录自身路径）。
	Path string
}

// KindReadError 一个菜单位置读取失败的事实（key + 中文兜底 + 状态码来自 enums）。
type KindReadError struct {
	// Kind 失败的位置（header / header_mobile / footer / footer_mobile）。
	Kind string
	// Err 面向调用方的业务错误（enums.ErrSourceKindDetail 派生，带 500 状态码）。
	// 原始端口错误（表名 / SQLSTATE）只进 service 日志，不在这里透出。
	Err error
}
