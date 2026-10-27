package productdto

// product_publish_record_resp.go — 发布记录页的渲染视图（B9）。
//
// **一组事实，不是历史**：库里没有「每次发布尝试成功/失败」的明细表（回执
// publication_receipts 只覆盖访问面切换那一段，完成后只留终态），所以这一屏给的是
// 「现在每个商品发布成什么样」，不是「发布过几次、哪次失败了」。数据来源三张表：
// presentation_instances（active 指针 / stale）、presentation_publications（每语言
// 激活路径 / 发布时间）、presentation_artifacts（产物版本号）；拼装在 presentation
// 模块的 PublishRecordPort 里完成，本视图只做「商品 × 发布登记」的合并与过滤。

import (
	"time"

	projectdto "go_wp/internal/module/project/dto"
	"go_wp/pkg/pagination"
)

// ProductPublishRecordPageResp 发布记录整页。
//
// **一行一个商品**（多语言折叠成子行）：分页单位必须与「共 N 条」的口径一致，
// 而按语言分行会让同一个商品占掉几行、页码随语言数漂移。
type ProductPublishRecordPageResp struct {
	Projects        []projectdto.ProjectResp
	SelectedProject string
	// SiteName 本次选中工程的名字（「站点」那一列的值）。
	//
	// 由读模型给而不是模板按 id 去 Projects 里找：Jet 没有「按条件取数组元素」的写法，
	// 而在模板里 range 一遍工程去比 id 就是业务判断（template_logic_test.go 与
	// templates/CLAUDE.md 都禁）。
	SiteName string

	FilterKeyword string
	FilterStatus  string
	Page          int
	Limit         int
	Total         int64

	// Ready 发布记录端口是否装配：假 → 模板只给一句「发布能力未装配」，
	// 不渲染任何行（否则空的表格会被读成「一个商品都没发布」）。
	Ready bool

	Records    []PublishRecordRowResp
	Pagination pagination.Page
}

// PublishRecordRowResp 一个商品的发布现状（多语言在 Langs 里逐条）。
type PublishRecordRowResp struct {
	ProductID   string
	ProductName string
	ProductSlug string

	// HasInstance 是否已有发布实例：假 → 该商品从没发布过，模板禁用「重新发布」
	// 并指路到详情页模板页的「首次发布」。
	HasInstance bool
	// Online 任一语言已上线。
	Online bool
	// Stale 依赖已变更、待重建（这一屏最有价值的一列：它表示「线上是旧的」）。
	Stale bool
	// PublishedAt 最近一次激活时刻；零值 = 从没发布成功过。
	PublishedAt time.Time
	// ArtifactVersion 当前最大产物版本号（0 = 没有产物 / 产物行已被回收）。
	ArtifactVersion int64
	ArtifactHash    string

	// Langs 逐语言明细（已上线在前、其次按语言名）。模板只 range 显示。
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

// Paging 见 product_page_resp.go 里那组同类实现（视图自报分页事实）。
func (v ProductPublishRecordPageResp) Paging() pagination.Page { return v.Pagination }
