package contentcontract

// search.go — 内容检索的**收窄只读端口**（BIZ-2 站内搜索）。
//
// 为什么单独一条端口而不是把方法挂到 ContentService 上：搜索的消费方是**访问面片段**
// （anonymous GET），它只需要「按关键词取一批文章」这一条只读能力。ContentService 有
// 十来个方法且含全部写方法（Create / Update / Delete / RegisterEntityTypes）——
// 依赖面越大，越容易在不经意间用上不该用的能力。接口形状即越权防护。

import "context"

// EntityTypeArticle 文章实体类型（contents.entity_type）。
//
// 与 fieldWhitelist 的键同名；提取成常量是为了让检索路径不必再写一遍字面量
// （写错类型名不会报错，只会永远搜不到东西）。
const EntityTypeArticle = "article"

// ArticleSearchHit 一篇文章的检索命中（只读展示事实）。
//
// 只给「结果条目」需要的四样：id（供发布面反查线上路径）、slug、标题、摘要。
// 正文与 SEO 字段不进来 —— 搜索结果不渲染全文，多传的字段总有一天会被拿去拼进页面。
type ArticleSearchHit struct {
	ID      string
	Slug    string
	Title   string
	Excerpt string
}

// SearchPort 内容检索能力（只读、限量）。
type SearchPort interface {
	// SearchArticles 按关键词检索文章，最多返回 limit 条（limit ≤ 0 时由实现取默认上限）。
	//
	// **不判定「已发布」**：contents 表没有状态列 —— 内容的线上可用性由「有没有已上线的
	// 详情页产物」决定（presentation 实例的 active 指针）。调用方（搜索片段）拿本端口的
	// 命中 id 去问发布面，只把有线上路径的那些显示出来。把发布判定塞进内容模块会
	// 让内容域反向依赖发布域，而且两处判定的口径迟早分叉。
	SearchArticles(ctx context.Context, keyword string, limit int) (hits []*ArticleSearchHit, err error)
}
