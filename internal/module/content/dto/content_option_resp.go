package contentdto

import "go_wp/pkg/pagination"

// content_option_resp.go — 内容模块的下拉选项形状。
//
// 三个下拉（工程 / 内容模板 / 页面）形状相同：ID + 已取好词的 Name。
// 合成一个类型比三份 gin.H 更可靠 —— 手写 map 里每处都可能拼错一个键，
// 而拼错的表现是选项渲染成空白（不报错）。
type OptionResp struct {
	ID   string
	Name string
}

// TemplateOptionResp 内容模板下拉（带草稿版本号：发布时要落定版本）。
type TemplateOptionResp struct {
	ID      string
	Name    string
	Version int64
}

// StalePageRowResp 待重建页面的一行（模板读 .ID / .Path / .ProjectName）。
type StalePageRowResp struct {
	ID          string
	Path        string
	ProjectID   string
	ProjectName string
	// Published 区分「已发布但有更新」与「从未上线」：后者的下一步不是重建而是发布。
	Published bool
	// RebuildFailure 失败痕迹（迁移 474）：有痕迹说明这页**不是还没轮到，而是重建失败过**。
	// 是**事实**（阶段 key + 时刻），整句由片段模板按同一顺序拼回。
	RebuildFailure any
}

// StaleImpactResp 「待重建页面影响面」的渲染数据。
//
// 降级语义：失败 → Available=false（显示「读不到」），**绝不**渲染成「0 个待重建」
// （那会把一次读取失败伪装成一切正常）。
//
// Hint 是**词条事实**（key + 中文兜底）不是拼好的句子：取词在模板渲染期发生。
// 控制器不取词 —— 语言是请求级的，在那里取词意味着「英文后台显示中文」这类
// 不会报错、也不会被测出来的缺陷。
type StaleImpactResp struct {
	Available bool
	Pages     []StalePageRowResp
	Total     int
	Truncated bool
	Limit     int
	Hint      any
}

// ArticleFormResp 文章编辑表单的字段集合（新建页给全空值）。
//
// 与 articleForm（handler 内部类型）分开：那一份是「从 POST 读什么」，
// 这一份是「模板要读什么」。两者今天同形，但混用会让改表单字段时
// 分不清是改入参还是改视图。
type ArticleFormResp struct {
	ID        string
	Slug      string
	Title     string
	Body      string
	Revision  int64
	UpdatedAt string
}

// ArticleNewPageResp 文章新建页（不取数，只渲染空表单）。
type ArticleNewPageResp struct {
	Title string `view:"title"`
	Menu  string `view:"menu"`
	Form  *ArticleFormResp
}

// ArticleSeoDrawerResp 文章 SEO 抽屉片段。
type ArticleSeoDrawerResp struct {
	Score any
	IsNew bool
}

// ArticleListRowResp 文章列表的一行。
//
// 发布状态只有两种取值来源：查到了线上路径（已发布）或没查到（未发布 / 未建实例）。
// 不区分「未发布」与「查询失败」—— 列表页不是排查页，编辑页会给出完整状态。
type ArticleListRowResp struct {
	ID        string
	Title     string
	Slug      string
	Excerpt   string
	Revision  int64
	UpdatedAt any
	Published bool
	URLPath   string
	PublicURL string
	EditURL   string
	// StateKey / StateFallback 状态文案的**事实**，取词由模板用注入的 tr 完成。
	StateKey      string
	StateFallback string
}

// ArticleListPageResp 文章列表整页。
type ArticleListPageResp struct {
	Title string `view:"title"`
	Menu  string `view:"menu"`
	Rows  []ArticleListRowResp
	Total int
	Empty bool
	Err   any
	// BlogBase 文章详情路径前缀（模板拼列表页的「查看」链接）。
	BlogBase string
	Keyword  string
	// ClearFilterURL 清空筛选后的地址。
	ClearFilterURL string
	Limit          int
	// FilterQuery 写动作表单 action 的筛选 query。
	FilterQuery string
	// StaleImpact 依赖失效影响面（哪些页面正在等待重建）。
	StaleImpact *StaleImpactResp

	Pagination pagination.Page
}

// ArticleStaleImpactResp 文章列表页的「依赖失效影响面」区块。
//
// 与 page.StaleOverviewResp 同形但独立声明：两者回答的是同一件事（哪些页面待重建），
// 但一个是文章列表页的页头区块、一个是页面管理页的页头区块 ——
// 合成一个会让「改这页的区块」牵动那页。
type ArticleStaleImpactResp struct {
	Available bool
	Pages     any
	Total     int
	Truncated bool
	Limit     int
	Hint      any
}
