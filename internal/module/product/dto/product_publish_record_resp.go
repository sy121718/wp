package productdto

// 商品与发布登记的业务查询结果；不包含格式化时间或状态文案。

import (
	"time"
)

// PublishRecordRowResp 一个商品的发布现状（多语言在 Langs 里逐条）。
type PublishRecordRowResp struct {
	ProductID   string
	ProductName string
	ProductSlug string

	// HasInstance 是否已有发布实例。
	HasInstance bool
	// Online 任一语言已上线。
	Online bool
	// Stale 依赖已变更、待重建。
	Stale bool
	// PublishedAt 最近一次激活时刻；零值 = 从没发布成功过。
	PublishedAt time.Time
	// ArtifactVersion 当前最大产物版本号（0 = 没有产物 / 产物行已被回收）。
	ArtifactVersion int64
	ArtifactHash    string

	// Langs 逐语言发布登记。
	Langs []PublishRecordLangResp
}

// PublishRecordLangResp 一个语言上的发布登记。
type PublishRecordLangResp struct {
	Lang string
	// OnlinePath 线上真实访问路径（含语言前缀），可直接点开看。空 = 未上线。
	OnlinePath string
	// Online 该语言是否已上线。
	Online bool
	// Stale 依赖已变更、待重建。这是**实例级**事实（同一实例的所有语言同值），
	// 留在这里是为了让「有实例但从未发布」那一行也带着它（那种行 Lang 为空串）。
	Stale bool
	// PublishedAt 该语言激活时刻；零值 = 未发布。
	PublishedAt time.Time
	// ArtifactVersion 该语言激活产物的版本号（0 = 没有产物）。
	ArtifactVersion int64
	ArtifactHash    string
}
