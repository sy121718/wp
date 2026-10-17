package productdto

// product_translations_req.go — 商品译文工作台保存的 JSON 入参（批次 C）。
//
// 三组并行数组按行一一对应（与页面表单字段同名）：length 对齐与逐行校验都在读模型侧做
// （那里才有候选原文），控制器只负责把请求读成三组数组。
//
// 空 target 表示「本行不写入」（不删除库中已有译文）：因此数组里必须保留空串占位，
// 不能在建数组时把空行剔掉 —— 剔掉会让后面所有行错位一格，而错位在服务端看起来只是
// 「这一行的译文与原文对不上」。
type TranslationSaveReq struct {
	ProjectID string `json:"projectId"`
	ProductID string `json:"productId" binding:"required"`
	Lang      string `json:"lang"`
	// 字段名与表单一致（rowTarget / rowContext / rowHash），前端只声明 data-write-array。
	Targets  []string `json:"rowTarget"`
	Contexts []string `json:"rowContext"`
	Hashes   []string `json:"rowHash"`
}
