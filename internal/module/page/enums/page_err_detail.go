package pageenums

// page_err_detail.go — page 域业务错误的**补充说明**词条（key + 具名参数 + 中文兜底）。
//
// 背景（系统性通道，见 pkg/i18n/errdetail.go）：写侧习惯写成
//
//	fmt.Errorf("%w: %v", ErrInvalidDocument, builderErr)
//
// 读侧只按「key：」前缀认出前半截业务 key 去取词，**后半截原样拼在译文后面** —— 英文界面上
// 那半句永远是中文，而它恰好是信息量最大的一半（第几个顶级节点、深了多少层、上限多少）。
//
// 本文件是那半句的真源：page/service 用 i18n.ErrorDetail(常量, name, value, …) 产出
//「key + 具名参数」编码，读侧（page/inbound/http 的 pageDetailText）取词并填 {name}。
// 中文兜底留在这里（i18n 未初始化 / 词条缺失时用），与 product / inventory 的
// ErrDetailFallbacks 同一形态。
//
// 名字**不带 Err 前缀**：它们不是可抛出的业务错误消息（不参与 pageErrorStatus 的哨兵分类），
// 而是「补充说明」的词条 key，由 i18n.ErrorDetail 编码进业务错误的 tail。
const (
	// DetailDocEmpty 页面文档为空（builder.ErrPageDocumentEmpty）。
	DetailDocEmpty = "admin.page.detail.documentEmpty"
	// DetailDocSettingsInvalid 页面设置校验失败（builder.PageSettingsError）。
	//
	// 不含内层细节：设置校验器的原文是组件层的自由文本，未词条化 ——
	// 按读侧协议「不是词条就丢弃」，这里给一句概括，内层原文只进日志。
	DetailDocSettingsInvalid = "admin.page.detail.settingsInvalid"
	// DetailDocNodeDepthExceed 组件树深度超限（builder.NodeDepthError）。
	DetailDocNodeDepthExceed = "admin.page.detail.nodeDepthExceed"
	// DetailDocNodeInvalid 顶级节点配置非法（builder.NodeInvalidError）。
	DetailDocNodeInvalid = "admin.page.detail.nodeInvalid"
	// DetailDocStructureInvalid 兜底：识别不出具体类别的文档校验失败。
	DetailDocStructureInvalid = "admin.page.detail.structureInvalid"
)

// DetailKeys 上面那组词条的**全量清单**（登记对账的唯一来源）。
//
// 读侧只认 ErrDetailFallbacks 里登记过的 key，而这份清单与兜底表由用例
//（page/inbound/http 的 page_err_detail_test.go）双向对账：新增词条漏了任何一边都会变红。
// 少登记的表现是「明细被静默丢弃」——不报错、不 500，只是页面上少一句话。
var DetailKeys = []string{
	DetailDocEmpty,
	DetailDocSettingsInvalid,
	DetailDocNodeDepthExceed,
	DetailDocNodeInvalid,
	DetailDocStructureInvalid,
}

// ErrDetailFallbacks 上面那组词条的中文兜底（i18n 未初始化 / 该 key 没有词条时用）。
//
// 占位符与词条一一对应（{index} / {depth} / {max}），且**值与 builder 的中文原文同义**：
// 词条缺失时页面显示的就是这句，运营照着它去改文档。
var ErrDetailFallbacks = map[string]string{
	DetailDocEmpty:           "页面文档为空，没有可保存的内容",
	DetailDocSettingsInvalid: "页面设置不合法",
	DetailDocNodeDepthExceed: "顶级节点 {index} 的组件树深度 {depth} 超过上限 {max}（嵌套失控，请简化结构）",
	DetailDocNodeInvalid:     "顶级节点 {index} 的配置不合法",
	DetailDocStructureInvalid: "页面文档结构不合法",
}
