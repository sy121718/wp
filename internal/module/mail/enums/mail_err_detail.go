package mailenums

// mail_err_detail.go — mail 域业务错误的**补充说明**词条（key + 具名参数 + 中文兜底）。
//
// 背景（系统性通道，见 pkg/i18n/errdetail.go）：本模块的图校验错误此前是
//
//	errors.New(mailenums.ErrAutomationGraphInvalid + ": " + 图校验器的中文原文)
//
// 读侧（mail_err.go）按「key: 」前缀认出前半截业务 key 去取词，**后半截中文原样拼回** ——
// 英文界面上永远是中文，而它恰好是作者照着改图的唯一线索（哪条边指向了不存在的节点、
// 环的路径、哪些节点走不到）。它还会经 302 的 ?err= 进页面与浏览器历史。
//
// 本文件是那半句的真源：service 侧用 i18n.ErrorDetail(常量, name, value, …) 产出
//「key + 具名参数」，读侧取词并填 {name} 占位符。中文兜底留在这里（i18n 未初始化 /
// 词条缺失时用）。词条的 zh-CN 值与图校验器的中文原文同义 —— 后者仍留在
// mail_automation_graph.go（它是日志与单测断言的文本），两处都必须改是这次的已知代价：
// 文本进日志、词条进界面，判据不同所以不能只留一份。
//
// 名字**不带 Err 前缀**：它们不是可抛出的业务错误消息（不参与 response.ErrorAuto 的形态判据），
// 而是「补充说明」的词条 key，由 i18n.ErrorDetail 编码进业务错误的 tail。
const (
	// —— 定义本身（ParseDefinition / 请求体）——
	//
	// DetailGraphEmptyDefinition 传进来的定义是空对象。
	DetailGraphEmptyDefinition = "admin.mail.detail.graphEmpty"
	// DetailGraphRequestNotJSON 请求体里的 definition 不是合法 JSON（{reason} 是解析器原文）。
	DetailGraphRequestNotJSON = "admin.mail.detail.requestNotJSON"
	// DetailGraphNotGraph 定义不是合法的图结构（{reason} 是 JSON 解码器原文）。
	DetailGraphNotGraph = "admin.mail.detail.notGraph"

	// —— 节点集合 ——
	DetailGraphNoNode         = "admin.mail.detail.noNode"
	DetailGraphNodeKeyMissing = "admin.mail.detail.nodeKeyMissing"
	DetailGraphNodeKeyDup     = "admin.mail.detail.nodeKeyDuplicate"

	// —— 节点形状（{node} 是节点 key）——
	DetailGraphNeedMinutes    = "admin.mail.detail.needMinutes"
	DetailGraphNeedTemplate   = "admin.mail.detail.needTemplate"
	DetailGraphNeedTwoArms    = "admin.mail.detail.needTwoArms"
	DetailGraphNeedCondition  = "admin.mail.detail.needCondition"
	DetailGraphNeedTagAction  = "admin.mail.detail.needTagAction"
	DetailGraphUnknownNodeTyp = "admin.mail.detail.unknownNodeType"

	// —— 入口与边 ——
	DetailGraphEntryMissing       = "admin.mail.detail.entryMissing"
	DetailGraphEntryNotExist      = "admin.mail.detail.entryNotExist"
	DetailGraphEdgeTargetMissing  = "admin.mail.detail.edgeTargetMissing"
	DetailGraphCycle              = "admin.mail.detail.cycle"
	DetailGraphUnreachable        = "admin.mail.detail.unreachable"
)

// DetailKeys 上面那组词条的**全量清单**（登记对账的唯一来源）。
//
// 读侧只认 ErrDetailFallbacks 里登记过的 key，而这份清单与兜底表由用例
//（mail/inbound/http 的 mail_err_detail_test.go）双向对账：新增词条漏了任何一边都会变红。
// 少登记的表现是「明细被静默丢弃」——不报错，只是页面上少一句话。
var DetailKeys = []string{
	DetailGraphEmptyDefinition,
	DetailGraphRequestNotJSON,
	DetailGraphNotGraph,
	DetailGraphNoNode,
	DetailGraphNodeKeyMissing,
	DetailGraphNodeKeyDup,
	DetailGraphNeedMinutes,
	DetailGraphNeedTemplate,
	DetailGraphNeedTwoArms,
	DetailGraphNeedCondition,
	DetailGraphNeedTagAction,
	DetailGraphUnknownNodeTyp,
	DetailGraphEntryMissing,
	DetailGraphEntryNotExist,
	DetailGraphEdgeTargetMissing,
	DetailGraphCycle,
	DetailGraphUnreachable,
}

// ErrDetailFallbacks 上面那组词条的中文兜底（i18n 未初始化 / 该 key 没有词条时用）。
//
// 占位符与词条一一对应（{node} / {from} / {to} / {type} / {path} / {nodes} / {reason}），
// 且**值就是图校验器原先那句中文**：词条缺失时页面上显示的就是它，不丢可行动信息。
var ErrDetailFallbacks = map[string]string{
	DetailGraphEmptyDefinition: "流程定义不能为空",
	DetailGraphRequestNotJSON:  "流程定义的 JSON 解析失败: {reason}",
	DetailGraphNotGraph:        "流程定义不是合法的图结构: {reason}",

	DetailGraphNoNode:         "流程里至少要有一个节点",
	DetailGraphNodeKeyMissing: "存在没有 key 的节点",
	DetailGraphNodeKeyDup:     "节点 key 重复: {node}",

	DetailGraphNeedMinutes:    "节点 {node}: 等待节点需要正数的 minutes",
	DetailGraphNeedTemplate:   "节点 {node}: 发信节点需要 template_key",
	DetailGraphNeedTwoArms:    "节点 {node}: 条件分支需要 yes 与 no 两条出边",
	DetailGraphNeedCondition:  "节点 {node}: 条件分支需要至少一个条件",
	DetailGraphNeedTagAction:  "节点 {node}: 标签节点需要 add 或 remove",
	DetailGraphUnknownNodeTyp: "节点 {node}: 未知节点类型: {type}",

	DetailGraphEntryMissing:      "没有指定入口节点",
	DetailGraphEntryNotExist:     "入口节点不存在: {node}",
	DetailGraphEdgeTargetMissing: "节点 {from} 指向了不存在的节点 {to}",
	DetailGraphCycle:             "流程里有环: {path}",
	DetailGraphUnreachable:       "有节点从入口走不到: {nodes}",
}
