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

// 行列表必须带 form tag：契约生成器按它出线上字段表，缺 tag 会回落到 Go 字段名
// （RowContext），与模板真实提交的 rowContext 对不上。
type PageTranslationsSaveFormReq struct {
	PageID     string   `form:"pageId"`
	Lang       string   `form:"lang"`
	RowContext []string `form:"rowContext"`
	RowSource  []string `form:"rowSource"`
	RowHash    []string `form:"rowHash"`
	RowTarget  []string `form:"rowTarget"`
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
