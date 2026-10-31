package pagedto

import "net/url"

type PageLangFormReq struct {
	PageID string `form:"pageId"`
	Lang   string `form:"lang"`
}

type ProjectCreateFormReq struct {
	Name string `form:"name"`
}

type PageCreateFormReq struct {
	ProjectID   string `form:"projectId"`
	DraftPath   string `form:"draftPath"`
	BlueprintID string `form:"blueprintId"`
}

type PageDeleteFormReq struct {
	ID string `form:"id"`
}

type PageTranslationsSaveFormReq struct {
	PageID     string `form:"pageId"`
	Lang       string `form:"lang"`
	RowContext []string
	RowSource  []string
	RowHash    []string
	RowTarget  []string
}

func (r *PageTranslationsSaveFormReq) BindForm(v url.Values) error {
	r.RowContext = append([]string(nil), v["rowContext"]...)
	r.RowSource = append([]string(nil), v["rowSource"]...)
	r.RowHash = append([]string(nil), v["rowHash"]...)
	r.RowTarget = append([]string(nil), v["rowTarget"]...)
	return nil
}

type PageHistoryRestoreFormReq struct {
	PageID  string `form:"pageId"`
	Version string `form:"version"`
}
