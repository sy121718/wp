package mailenums

const (
	DetailGraphEmptyDefinition = "admin.mail.detail.graphEmpty"

	DetailGraphRequestNotJSON = "admin.mail.detail.requestNotJSON"

	DetailGraphNotGraph = "admin.mail.detail.notGraph"

	DetailGraphNoNode         = "admin.mail.detail.noNode"
	DetailGraphNodeKeyMissing = "admin.mail.detail.nodeKeyMissing"
	DetailGraphNodeKeyDup     = "admin.mail.detail.nodeKeyDuplicate"

	DetailGraphNeedMinutes    = "admin.mail.detail.needMinutes"
	DetailGraphNeedTemplate   = "admin.mail.detail.needTemplate"
	DetailGraphNeedTwoArms    = "admin.mail.detail.needTwoArms"
	DetailGraphNeedCondition  = "admin.mail.detail.needCondition"
	DetailGraphNeedTagAction  = "admin.mail.detail.needTagAction"
	DetailGraphUnknownNodeTyp = "admin.mail.detail.unknownNodeType"

	DetailGraphUnknownOutlet = "admin.mail.detail.unknownOutlet"
	DetailGraphBadCondition  = "admin.mail.detail.badCondition"

	DetailGraphOutletMissing    = "admin.mail.detail.outletMissing"
	DetailGraphNeedVariants     = "admin.mail.detail.needVariants"
	DetailGraphVariantDup       = "admin.mail.detail.variantDuplicate"
	DetailGraphVariantWeightBad = "admin.mail.detail.variantWeightBad"
	DetailGraphNeedWebhookURL   = "admin.mail.detail.needWebhookURL"
	DetailGraphWebhookDenied    = "admin.mail.detail.webhookDenied"

	DetailGraphEntryMissing      = "admin.mail.detail.entryMissing"
	DetailGraphEntryNotExist     = "admin.mail.detail.entryNotExist"
	DetailGraphEdgeTargetMissing = "admin.mail.detail.edgeTargetMissing"
	DetailGraphCycle             = "admin.mail.detail.cycle"
	DetailGraphUnreachable       = "admin.mail.detail.unreachable"
)

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
	DetailGraphUnknownOutlet,
	DetailGraphBadCondition,
	DetailGraphOutletMissing,
	DetailGraphNeedVariants,
	DetailGraphVariantDup,
	DetailGraphVariantWeightBad,
	DetailGraphNeedWebhookURL,
	DetailGraphWebhookDenied,
	DetailGraphEntryMissing,
	DetailGraphEntryNotExist,
	DetailGraphEdgeTargetMissing,
	DetailGraphCycle,
	DetailGraphUnreachable,
}

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

	DetailGraphUnknownOutlet: "节点 {node}: 未知出口名: {outlet}",
	DetailGraphBadCondition:  "节点 {node}: 条件不合法: {reason}",

	DetailGraphOutletMissing:    "节点 {node}: 缺少出口 {outlet}",
	DetailGraphNeedVariants:     "节点 {node}: 分流转需要 2–5 个变体",
	DetailGraphVariantDup:       "节点 {node}: 变体 id 重复: {variant}",
	DetailGraphVariantWeightBad: "节点 {node}: 变体权重之和必须是 100（当前 {weight}）",
	DetailGraphNeedWebhookURL:   "节点 {node}: 回调节点需要 https 地址",
	DetailGraphWebhookDenied:    "节点 {node}: 回调地址不允许指向内网",

	DetailGraphEntryMissing:      "没有指定入口节点",
	DetailGraphEntryNotExist:     "入口节点不存在: {node}",
	DetailGraphEdgeTargetMissing: "节点 {from} 指向了不存在的节点 {to}",
	DetailGraphCycle:             "流程里有环: {path}",
	DetailGraphUnreachable:       "有节点从入口走不到: {nodes}",
}
