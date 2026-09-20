package source

// entity_source.go — 实体类型与单字段解析契约。
//
// 与集合（列表）互补：这里是「单个实体的一个字段」，详情页按字段绑定取它就够了。
// 同样只读：能读白名单内的字段，读不到就返回空串，没有任何写入口。

import "context"

// ContentResolver CMS 内容解析契约：绑定字段 → 构建期字符串值。
// 规范 docs/02-C1 §2（Dynamic Binding）：发布期数据完全静态填入。
type ContentResolver interface {
	// ResolveString 按字段路径（如 "product.name"）解析字符串值；不存在返回空串。
	ResolveString(field string) (string, error)
}

// EntityFieldSource 一种实体类型的数据来源（如 product / article）。
//
// 白名单的唯一来源：实现方给出「这个实体类型允许绑定哪些字段」，
// 构建层据此在保存模板与编译两处做校验（不变量 4）。
type EntityFieldSource interface {
	// EntityType 类型标识（如 article / product）。
	EntityType() string
	// FieldWhitelist 该类型允许绑定的字段路径（不变量 4 的唯一来源，只读拷贝）。
	FieldWhitelist() []string
	// ResolverFor 返回绑定单个实体的字段解析器。
	ResolverFor(ctx context.Context, entityID string) (ContentResolver, error)
}

// FieldRef 组件声明的实体字段绑定引用（实体类型 + 字段名）。
//
// 由组件经 FieldBindingProvider 自报，构建层据此在「模板保存」与「编译」两处
// 按实体类型注册表做白名单校验（不变量 4：Document 只保存白名单绑定）。
type FieldRef struct {
	// EntityType 实体类型标识（如 product）。
	EntityType string
	// Field 字段名（不含类型前缀，如 name）。
	Field string
	// CollectionSource 声明该绑定的集合组件所在的集合源（如 content:product）。
	//
	// 空串 = 非集合来源（普通组件、或集合组件尚未选源），此时按**模板实体类型**校验。
	// 非空 = 该绑定由集合组件按集合项渲染（见 core.CollectionProvider / ItemScope），
	// 校验口径是「集合源实体类型」而不是模板实体类型 —— 否则「商品分类归档模板里的
	// 商品列表」会被判成跨数据源绑定：模板实体是 product_category，而列表绑的是
	// product.*，两者本来就不是一个数据源。
	//
	// 填的是**集合源标识**而不是实体类型：标识 → 实体类型的推导只该有一处
	// （core.CollectionEntityType），由收集方决定何时生效、校验方决定怎么用。
	CollectionSource string
}

// EntitySourceRegistry 实体类型注册表（进程级；装配期注册，运行期只读）。
type EntitySourceRegistry interface {
	// Register 注册一个实体类型来源；重复类型或非法来源返回错误。
	Register(src EntityFieldSource) error
	// Lookup 按类型取来源。
	Lookup(entityType string) (EntityFieldSource, bool)
	// IsValidType 类型是否已注册。
	IsValidType(entityType string) bool
	// FieldWhitelist 该类型字段白名单（只读拷贝；未知类型返回 nil）。
	FieldWhitelist(entityType string) []string
	// ResolverFor 取该类型某实体的字段解析器。
	ResolverFor(ctx context.Context, entityType, entityID string) (ContentResolver, error)
}
