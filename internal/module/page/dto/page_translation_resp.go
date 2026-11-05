package pagedto

type TranslationMissResp struct {
	PageID string `json:"pageId"`

	DraftPath string `json:"draftPath"`
	Lang      string `json:"lang"`

	Misses int64 `json:"misses"`

	Candidates int64 `json:"candidates"`
}

// PageTranslationFailureItem 是写基座 data.items 的一条（对齐 pkg/response 的
// blockedDataOf：{id, message}）。id 是行标识（哪一行），页面级错误为空串。
type PageTranslationFailureItem struct {
	ID      string `json:"id,omitempty"`
	Message string `json:"message,omitempty"`
}

// PageTranslationFailureList 是译文保存失败响应的 data 形状。
type PageTranslationFailureList struct {
	Items []PageTranslationFailureItem `json:"items"`
}
