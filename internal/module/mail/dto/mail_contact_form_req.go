package maildto

// ContactImportFormReq 联系人导入的表单入参（transport 形状，不是业务契约）。
//
// 为什么不直接把表单绑到 ImportContactsReq：那份契约的 Content 是 []byte
// （上传或原始正文灌入，不是表单文本），ConsentDeclared / UpdateExisting 是 bool，
// 而 HTML 表单只能给字符串 —— checkbox 给 "true" / 空，与 <select> 给 "1" 这类
// 表示法在传输层，不该渗进业务契约。所以在这里收字符串，再由 handler 归一。
type ContactImportFormReq struct {
	// Content 导入内容（form-urlencoded 文本）；空时 handler 回落到原始请求体。
	Content string `form:"content"`
	// ConsentDeclared 是否声明「这批人已同意接收营销」（"true" 视为真）。
	ConsentDeclared string `form:"consentDeclared"`
	// ConsentSource 合规留痕来源（口头 / 网页表单 / 已购名单等）。
	ConsentSource string `form:"consentSource"`
	// UpdateExisting 已存在邮箱是否更新（"true" 视为真）。
	UpdateExisting string `form:"updateExisting"`
	// Tags 逗号分隔的默认标签。
	Tags string `form:"tags"`
}
