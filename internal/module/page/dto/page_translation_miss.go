package pagedto

// page_translation_miss.go — 缺译报告的跨模块形状（U2）。

// TranslationMissRow 一个（页面 × 语言）的缺译情况。
//
// **两个量的量纲不同、不能相除**（U0 结论）：
//   - Candidates 是去重后的可翻译**字段数**；
//   - Misses 是渲染期取词**未命中的调用次数**（同一字段渲染多次计多次）。
//
// 所以展示只给条数（`misses`），不给「缺失率」——`misses > candidates` 是正常现象
// （开发库实测：48 / 45）。要算比例得先有「按字段去重的缺失数」，当前实现没有这个量。
type TranslationMissRow struct {
	PageID string `json:"pageId"`
	// DraftPath 页面草稿路径（作者在页面列表里认得的那个名字）。
	DraftPath string `json:"draftPath"`
	Lang      string `json:"lang"`
	// Misses 取词未命中次数（> 0 才会进报告）。
	Misses int64 `json:"misses"`
	// Candidates 该次构建的可翻译字段数（仅作参考，不是分母）。
	Candidates int64 `json:"candidates"`
}
