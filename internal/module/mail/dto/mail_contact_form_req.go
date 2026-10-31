package maildto

type ContactImportFormReq struct {
	Content string `form:"content"`

	ConsentDeclared string `form:"consentDeclared"`

	ConsentSource string `form:"consentSource"`

	UpdateExisting string `form:"updateExisting"`

	Tags string `form:"tags"`
}
