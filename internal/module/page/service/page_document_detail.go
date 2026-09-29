package pageservice

// page_document_detail.go — 页面文档校验失败 → **可翻译的补充说明**（i18n.ErrorDetail 编码）。
//
// 写侧为什么需要它：page_draft.go 的保存/创建路径把 builder 的校验错误拼进
//
//	fmt.Errorf("%w: %v", ErrInvalidDocument, builderErr)
//
// 而 ErrInvalidDocument 在 page_handle.go 的 pageErrorStatus 里落 **400**（不是 500），
// 于是两个出口都会把它给用户看：JSON 出口的 pageErrorMessage、页面出口的 pageFacingText。
// 改造前那半句是 builder 的硬编码中文（「顶级节点 0: 组件树深度 11 超过上限 10…」），
// 英文界面上必然中英混排。
//
// 判据为什么是**类型**而不是错误文本：文本嗅探（strings.Contains(err, "深度")）改一个字
// 就静默失效，失效方向还是「明细消失」；builder 侧已把三类致命校验错误做成类型
//（internal/builder/page_validate_err.go），这里用 errors.As 取参数即可，错误文本一字不改。
//
// 兜底：识别不出类别时给一条概括性明细（DetailDocStructureInvalid），而不是空串 ——
// 空串会让「页面文档不合法」变成一句没头没尾的话，用户不知道该改哪里。

import (
	"errors"
	"strconv"

	"go_wp/internal/builder"
	pageenums "go_wp/internal/module/page/enums"
	"go_wp/pkg/i18n"
)

// pageDocumentDetail 把 builder 的文档校验错误映射成一条可翻译的补充说明编码串。
//
// 返回空串只在 i18n.ErrorDetail 判定「参数值含协议分隔符」时发生（本函数的参数都是
// strconv 出来的数字，实际不会）；这时调用点得到的是「key：」形态的业务错误，
// 读侧按协议丢弃空明细 —— 不显示半截句子。
func pageDocumentDetail(err error) string {
	if err == nil {
		return i18n.ErrorDetail(pageenums.DetailDocStructureInvalid)
	}
	var depthErr *builder.NodeDepthError
	if errors.As(err, &depthErr) {
		return i18n.ErrorDetail(pageenums.DetailDocNodeDepthExceed,
			"index", strconv.Itoa(depthErr.Index),
			"depth", strconv.Itoa(depthErr.Depth),
			"max", strconv.Itoa(depthErr.Max))
	}
	var nodeErr *builder.NodeInvalidError
	if errors.As(err, &nodeErr) {
		return i18n.ErrorDetail(pageenums.DetailDocNodeInvalid, "index", strconv.Itoa(nodeErr.Index))
	}
	var setErr *builder.PageSettingsError
	if errors.As(err, &setErr) {
		return i18n.ErrorDetail(pageenums.DetailDocSettingsInvalid)
	}
	if errors.Is(err, builder.ErrPageDocumentEmpty) {
		return i18n.ErrorDetail(pageenums.DetailDocEmpty)
	}
	return i18n.ErrorDetail(pageenums.DetailDocStructureInvalid)
}
