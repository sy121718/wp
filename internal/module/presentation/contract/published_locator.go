package presentationcontract

// published_locator.go — 「实体 → 已上线详情页路径」的收窄只读端口（BIZ-2 站内搜索）。
//
// 为什么单开一条端口：搜索片段需要的是「这批实体里哪些真的有线上页面，在哪」，
// 而 PresentationService 同时持有创建 / 重建 / 删除 / 预览等写能力。
// 接口形状即越权防护 —— 访问面的 anonymous 片段拿不到发布能力。

import "context"

// PublishedEntityLocator 按实体 id 批量解析**已上线**详情页的线上路径。
type PublishedEntityLocator interface {
	// PublishedEntityPaths 返回 entity_id → 线上路径（url_path），只含已上线（active 指针非空）
	// 的实例。未发布 / 已删除 / 查不到的实体不会出现在结果里 —— 调用方按「没有条目」处理。
	//
	// projectID / entityType 为空或 entityIDs 为空时返回空表且不报错：这是正常状态
	// （站内还没这类内容），不是错误。lang 为空时取站点默认语言（I18N-013）。
	// 路径已是最终访问路径（含语言前缀），调用方不要再拼语言前缀。
	PublishedEntityPaths(ctx context.Context, projectID, entityType, lang string, entityIDs []string) (map[string]string, error)
}
