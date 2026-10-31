package pagedto

type TranslationMissResp struct {
	PageID string `json:"pageId"`

	DraftPath string `json:"draftPath"`
	Lang      string `json:"lang"`

	Misses int64 `json:"misses"`

	Candidates int64 `json:"candidates"`
}
