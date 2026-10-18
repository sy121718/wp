package productservice

// product_detail_template_text.go — 详情页模板页「对端错误」的展示兜底。
//
// 这一页的写动作（预览 / 发布 / 切换模板 / 改 URL）会调两个对端能力，错误的**声明处**在
// 对端：presentation（发布实例与路径）与 contenttemplate（模板契约）。它们现在还是老式
// 常量（`ErrInvalidParam = "ErrInvalidParam"`，值就是常量名），enums 里没有兜底表，
// pkg/facing.FactOf 只能给出「兜底 = key」，页面就会显示 ErrPathOccupied 这种标识符。
//
// 因此这里有两张**补丁表**，按「哪个对端声明的」分开：
//
//   - 同名 key 在两个对端含义不同（contenttemplate 的 ErrNotFound 是「模板不存在」，
//     presentation 的是「还没有发布实例」），合成一张表必然吃掉一边；
//   - 调用点知道这次错误来自哪个对端（哪一次 service 调用失败的），所以按来源查表是
//     有依据的，不是猜。
//
// 归 service 不归控制器的理由同 product_foreign_text.go：跨模块错误的语义属于「谁调用
// 产生的」。控制器只负责取词与出口。
//
// 清理条件：对端 enums 补上 errFallbacks + Err(key) 工厂（参照 membership / content /
// navigation 已迁的形态）之后，本文件整体删除，出口直接吃 fact.Fallback。

import (
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	presentationenums "go_wp/internal/module/presentation/enums"
	"go_wp/pkg/facing"
)

// presentationErrFallbacks 发布实例 / 路径类错误的展示兜底。
var presentationErrFallbacks = map[string]string{
	presentationenums.ErrInvalidParam:         "参数不完整，请检查工程、商品与路径。",
	presentationenums.ErrNotFound:             "这个商品还没有详情页实例，先发布一次。",
	presentationenums.ErrNoTemplate:           "该类型没有可用的内容模板，先建一套商品详情模板。",
	presentationenums.ErrBuildFailed:          "构建失败，请检查模板与商品数据后重试。",
	presentationenums.ErrTemplateTypeMismatch: "这套模板不是商品类型的，换一套再试。",
	presentationenums.ErrInvalidPath:          "访问路径不合法：必须以 / 开头，且不含空格、引号与 .. 路径段。",
	presentationenums.ErrSamePath:             "新路径与当前路径相同，没有需要修改的地方。",
	presentationenums.ErrPathOccupied:         "这个路径已被其他页面或详情页占用，换一个再试。",
}

// contentTemplateErrFallbacks 模板契约（contenttemplate）错误的展示兜底。
var contentTemplateErrFallbacks = map[string]string{
	contenttemplateenums.ErrInvalidParam:        "参数不完整，请检查工程、模板名与复制来源。",
	contenttemplateenums.ErrNotFound:            "这套模板不存在，可能已被删除。",
	contenttemplateenums.ErrTemplateInUse:       "这套模板仍被自动发布实例引用，不能删除。",
	contenttemplateenums.ErrInvalidType:         "不支持的内容类型（本页只处理商品详情模板）。",
	contenttemplateenums.ErrDataInvalid:         "模板文档格式非法：请回到工作台重新保存后再复制。",
	contenttemplateenums.ErrFieldBindingInvalid: "模板里的字段绑定越界：请回到工作台改用本模板数据源内的字段。",
	contenttemplateenums.ErrProjectRequired:     "站点里有多个工程，请显式选择这套模板所属的工程。",
	contenttemplateenums.ErrProjectNotFound:     "选择的站点工程不存在，请刷新后重试。",
}

// PresentationErrText presentation 错误 → 展示兜底；未登记时回落事实自身兜底。
func PresentationErrText(fact facing.Fact) string {
	if fb := presentationErrFallbacks[fact.Key]; fb != "" {
		return fb
	}
	return fact.Fallback
}

// ContentTemplateErrText contenttemplate 错误 → 展示兜底；未登记时回落事实自身兜底。
func ContentTemplateErrText(fact facing.Fact) string {
	if fb := contentTemplateErrFallbacks[fact.Key]; fb != "" {
		return fb
	}
	return fact.Fallback
}
