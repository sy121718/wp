// 商品预览、评分与外部查询契约沿用的对象。
package productdto

import (
	"go_wp/internal/seo/scoreview"
)

// ScoreFragmentResp 评分片段的数据根（片段按 Score.OK 分流）。
type ScoreFragmentResp struct {
	Score ScoreResp
}

// SeoDrawerResp SEO 抽屉片段的数据根：目标容器 id + 隐藏字段 + 评分视图。
type SeoDrawerResp struct {
	TargetID     string
	HiddenFields []SeoHiddenFieldResp
	Score        ScoreResp
}

// SeoHiddenFieldResp 抽屉「评分」表单的一个隐藏字段。
type SeoHiddenFieldResp struct {
	Name  string
	Value string
}

// WarehouseOptionResp 仓库身份、名称与默认仓事实；显示格式由模板决定。
type WarehouseOptionResp struct {
	ID        string
	Code      string
	Name      string
	IsDefault bool
}

// WarehouseSKUItemResp 「从仓库选」的一个候选货。
type WarehouseSKUItemResp struct {
	// SKUCode 一律是**裸码**（仓库侧的名字，不带仓码前缀）：前端把它填进「SKU 编码」框时，
	// 服务端按 (仓, 裸码) 复核并复用那一行 —— 带上前缀会被当成一条新货去建行。
	SKUCode string
	// ExternalSKU 该仓登记的对方编码（可能为空）。
	ExternalSKU string
	// StockID 是那条货行的 id：仓库明细按它认行（编码可以改，行 id 不会）。
	// 加行时带上它，服务端就不用按 (仓, 裸码) 再猜一次是哪一行。
	StockID string
	// Name / ImageURL 是迁移 642 的货品主档：商品表单的仓库明细按名称认货、带缩略图。
	// 空串 = 那条货还没填主档，不是读失败 —— 模板退化成显示编码。
	Name     string
	ImageURL string
	// Quantity / TrackQuantity 是选中之后的初值：数量框直接显示当前在库，
	// 不跟踪（无限）的行按 TrackQuantity 关掉输入框 —— 无限不是「0 件」。
	Quantity      int
	TrackQuantity bool
}

// WarehouseSKUGroupResp 一个仓的候选货集合（模板按仓分组渲染 optgroup / datalist）。
type WarehouseSKUGroupResp struct {
	WarehouseID string
	// WarehouseCode 仓短码：前端预览「将要生成的主体 SKU」用（服务端才是真源）。
	WarehouseCode string
	WarehouseName string
	IsDefault     bool
	Items         []WarehouseSKUItemResp
}

// EntityTemplateOptionResp 商品详情模板候选的一项（卡片区只展示名字与「默认」徽标）。
type EntityTemplateOptionResp struct {
	ID   string
	Name string
}

// ScoreResp SEO 评分片段视图：与页面 / 文章共用同一份结构（internal/seo/scoreview）。
//
// 这里只是别名：三处各定义一份视图时「哪些检查项算未达标」会分叉，而分叉后每处单独看都对。
// 视图里只有事实（维度 key、分数、实测值、跳转目标、冲突页面），文案在模板按 key 取词。
type ScoreResp = scoreview.View

// PreviewNoticeResp 预览 iframe 里的替代页（还没保存 / 还没有详情页模板）。
//
// 只给事实：Key + Fallback 由模板取词，Lang 落 html 的 lang 属性。
// 条件与样式在 admin/preview_notice.html —— 这里只是数据。
type PreviewNoticeResp struct {
	Key      string
	Fallback string
	Lang     string
}
