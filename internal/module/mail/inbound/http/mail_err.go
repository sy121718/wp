package mailhttp

// mail_err.go — 邮箱模块后台页面 handler 的错误文案归口（审计 CQ-009 / 第三波收口）。
//
// 背景：本模块的页面 handler 此前一律把 err.Error() 拼进 ?err= 或直接塞进渲染数据。
// 两个方向都不对：
//   · service 的业务错误**本身就是 i18n key**（mailenums.ErrAccountNotFound =
//     "mail.err.accountNotFound"），页面直接拼出来的是裸 key，运营看到的是
//     「mail.err.campaignNotDraft」而不是「活动不在草稿状态」；
//   · service 上抛的基础设施错误（PostgreSQL 原文带表名 / 约束名 / SQLSTATE，
//     SMTP 客户端原文带主机与响应码）会一起直出到页面上 —— 响应不是可信边界。
//
// 三件套（对齐 AGENTS.md「响应与错误处理」）：
//   ① **判定** —— pkg/response.IsBusinessError（`a.b.c` 形态的 key，或 Err* / Msg* 常量名），
//      统一经 adminui.PageErrorFact 出口；模块**不再**维护 XxxFacingMessages 白名单 ——
//      那是一份必须与 enums 常量逐条对齐的第二份真相，漏一条的症状是「业务错误在页面上
//      变成一句通用提示」（不报错、不记日志），而这句话本来就能从错误本身判定出来；
//   ② **归口文案** mailenums.ErrInternal（mail.err.internal，迁移 276 seed 中英各一行）；
//   ③ **结构化日志** —— 未命中时原文只进日志，带场景、user_id 与请求路径。
//
// 分工：判定归**共享设施**（adminui），归口话术归**本模块** —— adminui 的归口句是
// 「系统内部错误，请稍后重试」，本模块的是「操作失败，请稍后重试」（与 membership_err.go
// 同一分工：那边的归口句也是模块自己的 ErrInternal）。
//
// 出口形态：提示页交**事实**（mailErrMsg → mailMsg，取词在模板渲染期发生）；需要成品文本的
// 槽位（页面 data 的 Err 键、JSON 逐条原因、片段槽位）走 mailErrPageText。

import (
	"errors"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/builtin"
	mailenums "go_wp/internal/module/mail/enums"
	"go_wp/pkg/bulkids"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/mailer"
	"go_wp/pkg/reqparams"
)

// mailErrScene 日志场景名（与 mail 模块其它 logger.Scene("mail") 调用点一致）。
const mailErrScene = "mail"

// mailErrMsg 页面路径的错误**事实**（提示页出口用它，取词在模板渲染期发生）。
//
// 判定只有一条：adminui.PageErrorFact 内部走 pkg/response.IsBusinessError —— 命中即
// 「这句话是本模块能对外说的文案」，未命中即基础设施原文（记结构化日志后归口）。
// 要成品文本的槽位用 mailErrPageText。
func mailErrMsg(c *gin.Context, err error) mailMsg {
	if err == nil {
		return mailMsg{}
	}
	fact := adminui.PageErrorFact(c, mailErrScene, err)
	if !fact.Business {
		return mailInternalMsg()
	}
	if fact.Tail != "" {
		// 「key：明细」是一段 key 表达不了的复合句（明细来自 i18n.ErrorDetail 词条族），
		// 收成成品串（Key 空 → 取词方原样输出），与 page 的 pageErrMsgOf 同一取舍。
		if detail := mailDetailText(mailTranslate(c), fact.Tail); detail != "" {
			return mailMsg{Fallback: adminui.TranslateArgs(c, fact.Key, fact.Fallback, nil) + "：" + detail}
		}
	}
	return mailMsg{Key: fact.Key, Fallback: fact.Fallback}
}

// mailErrPageText 错误文案的**成品文本**形态（页面 data 的 Err 键、列表提示条、
// JSON 逐条原因、片段槽位用）。提示页出口不用它 —— 那里交事实。
//
// 为什么必须给文本：这些槽位是**直接渲染的文本**（模板 {{.Err}} 与 JSON body 都不经过
// pkg/response 的 translate）。不取词的话运营会看到 mail.err.accountNotFound 这样的裸 key。
//
// 为什么不能让调用点直传 err.Error()：service 一旦把 PostgreSQL / SMTP 原文上抛，
// 它就会随提示页正文摆到运营面前，与 JSON body 一样不是可信边界。
func mailErrPageText(c *gin.Context, err error) string {
	return adminui.MsgText(c, mailErrMsg(c, err))
}

// mailInternalMsg 归口事实（拿不到具体 err 的失败分支用）：提示页不能显示空串。
func mailInternalMsg() mailMsg {
	return mailMsg{Key: mailenums.ErrInternal, Fallback: mailInternalFallback}
}

// mailInternalFallback 归口文案的中文原文（词条缺失时的兜底）。
//
// 与 adminui 的 internalErrorFallback（「系统内部错误，请稍后重试」）是同一条边界的两种
// 措辞：本模块这句话被既有用例逐字断言（public/test/mail/feature 的 mail_error_leak_test.go），
// 改字属于独立的文案迁移，不夹在本步里做。
const mailInternalFallback = "操作失败，请稍后重试"

// mailDetailText 业务错误的**补充说明** → 当前语言文案（不是词条时返回空串）。
//
// 接受的形态只有一种：i18n.ErrorDetail 的产物（控制字符开头的「明细词条 key + 具名参数」，
// 可多段）。其余一律**丢弃并落日志** —— 与 product / inventory 的读侧口径有意不同：
// 那两处的 `return tail` 是为了兼容改造前就已存在的纯文本明细，而 mail 这一族从本批起
// 全部改走 ErrorDetail（写侧见 service/mail_graph_err.go），所以不再保留「原样透出」这条
// 无界通道 —— 它正是英文界面上中文混排的来源。
//
// 未登记的明细 key（拼错 / 新加漏登记）跳过该段并记一条日志：少一句补充说明，
// 好过把编码串或半截占位符摆到页面上。
func mailDetailText(tr func(key, fallback string) string, tail string) string {
	parts, ok := i18n.ParseErrorDetails(tail)
	if !ok {
		logger.Scene(mailErrScene).With("tail", i18n.DetailTailForLog(tail)).
			Warn("业务错误的补充说明不是登记的词条形态，已丢弃（不再原样透出）")
		return ""
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		fallback, registered := mailenums.ErrDetailFallbacks[p.Key]
		if !registered || fallback == "" {
			logger.Scene(mailErrScene).With("detail_key", p.Key).
				Warn("业务错误的补充说明词条未登记，已省略该段")
			continue
		}
		out = append(out, i18n.FillTranslate(tr, p.Key, fallback, p.Args))
	}
	return strings.Join(out, "；")
}

// mailFormErrText 本页表单校验的错误文案（由 parseAutomationForm 组装，带「第 N 行」定位）。
//
// 与 mailErrPageText 分开的理由：那一份管的是「service 的 error 能不能透出」，
// 而这里的原文**由本页生成**（「第 3 行：节点标识与类型都要填」「流程名称不能为空」），
// 结构上不可能带库表 / SMTP 信息，而且正是运营照着改的依据 ——
// 落进归口文案会把「第 3 行缺节点标识」变成「操作失败，请稍后重试」，把可行动的提示弄丢。
//
// 与批量 id 超限那条是同一判据：**看文案来自哪里**，而不是看它是不是 error 类型
// （超限那条走类型判定 + adminui.BulkIDsFacingText，不再需要门禁豁免；本页自造的文案
// 仍由本函数原样透出）。
func mailFormErrText(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}

// mailBulkIDsText 批量 id 读取失败的**成品文本**（JSON 出口用：那边要一句话，界面在前端）。
//
// 判据是**类型**而不是文案比对：超限错误是 bulkids 的类型（*bulkids.IDsError），
// 「一次最多操作 N 项，当前 M 项，请分批进行」按当前语言生成，其中**当前 M 项**
// （去重后的条数）只有错误类型知道 —— 调用方用 bulkids.MaxIDs 重算会丢掉 Count
// （旧的实现正是如此）。留痕（谁在哪个页面触发）由 adminui 的出口统一记日志。
func mailBulkIDsText(c *gin.Context, err error) string {
	return adminui.BulkIDsFacingText(c, err)
}

// mailBulkIDsMsg 批量 id 超限的**事实**（提示页出口用）。
//
// 与 page 的 pageBulkIDsMsg 同形：两个数字（上限 Max 与去重后的本次条数 Count）由错误
// 类型给出，调用方用 bulkids.MaxIDs 重算会丢掉 Count —— 也就不会有第二份真相。
func mailBulkIDsMsg(c *gin.Context, err error) mailMsg {
	var tooMany *bulkids.IDsError
	if !errors.As(err, &tooMany) {
		// 不是本模块认识的错误：原文只进日志（adminui.BulkIDsFacingText 已记），对外归口。
		logger.Scene(mailErrScene).With("path", c.Request.URL.Path).
			Error(err, "批量 id 读取失败：未命中受控错误，只对外给归口文案")
		return mailInternalMsg()
	}
	logger.Scene(mailErrScene).With("path", c.Request.URL.Path).
		With("count", tooMany.Count).With("max", tooMany.Max).
		Warn("批量操作被拒：提交的 id 数量超过上限")
	return mailMsg{Key: bulkids.MsgTooMany, Fallback: bulkids.TooManyTemplate}.
		WithArgs(strconv.Itoa(tooMany.Max), strconv.Itoa(tooMany.Count))
}

// —— 访客面出口：邮件里的公开链接（点击追踪 / 一键退订）——
//
// 这一段的消费者与上面**完全不同**，判据也不同：
//
//	· 上面（mailErrPageText 等）面向登录后的运营：有后台壳、有提示页、有归口文案；
//	· 这里面向**收件人**：邮件客户端里点开链接，浏览器直接打开 /_t/c/{token} 或
//	  /_t/u/{token} —— 无登录态、无后台页面壳、也没有任何表单可改。
//
// 所以「失败时给一句受控短句 / 成功时给一张自带样式的整页 HTML」是**对的形态**，
// 改成 303 + ?err= 或 JSON 反而错（访客没有可回归的列表页，也没有解析 JSON 的客户端）。
// 要收口的只是**文案来源**：这几句此前是 Go 里的硬编码中文，而收件人可能是英文用户。
//
// 语言从哪来：response.RequestLanguage 的协商链（Cookie lang → query lang →
// Accept-Language → 默认语言）**不需要登录态**，裸引擎路由（SetupTrackingRoutes）直接用得上。
// 这也是为什么这里不经过 shell.PageError* —— 那是给后台页面（带壳）准备的。

// mailVisitorText 访客面文案：按请求语言取词条，**缺词条回落中文原文**。
//
// 与 mailErrPageText 的兜底方向恰好相反（那一份兜底给 key）：后台页面上出现裸 key 时
// 运营会来报「页面显示 mail.err.accountNotFound」，而访客面只有收件人一个人看见 ——
// 没人会来报，所以宁可给中文原文也不能给 key。pkg/i18n.Translate 的兜底链是
// 「命中 → 默认语言 → fallback → key」，传了 fallback 就轮不到 key 出场。
func mailVisitorText(c *gin.Context, key, fallback string) string {
	return mailTranslate(c)(key, fallback)
}

// mailVisitorLog 访客端点失败留痕（外部输入类：签名不符 / 链接过期 / 载荷不完整）。
//
// 记 Warn 而不是 Error：点击端点是最热的访客路径，而它的失败**几乎全是**
// 「访客拿着被客户端截断或已过期的链接」—— 记 Error 会把它变成日志噪声，
// 真正的故障反而被淹掉。
//
// **不记 token**：token 是能直接伪造「退订别人」的凭据（验签密钥就握在本服务手里），
// 落进日志等于把它复制到了另一个系统里。日志只需要「哪个端点、什么原因」。
func mailVisitorLog(c *gin.Context, err error, msg string) {
	if err == nil {
		return
	}
	logger.Scene(mailErrScene).
		With("path", c.Request.URL.Path).
		With("detail", err.Error()).
		Warn(msg)
}

// mailVisitorLogError 访客端点失败留痕（涉及持久化写入的一类：退订的状态 + 抑制名单事务）。
//
// 与上面分开的理由是**判据不同**：退订失败可能来自那次事务写（连接池 / 约束 / 驱动原文），
// 那是真正的系统故障，必须落在错误日志里；而「token 无效」这类纯验签失败不该。
// 原文（可能带表名 / SQLSTATE）只到这里，响应里永远只有 mailVisitorText 给的那句短句。
func mailVisitorLogError(c *gin.Context, err error, msg string) {
	if err == nil {
		return
	}
	logger.Scene(mailErrScene).
		With("path", c.Request.URL.Path).
		Error(err, msg)
}

// —— 结论文案的**事实**（key + 中文兜底 + 位置参数）——
//
// 落到这里的三类文案都不是 service 上抛的 error，也不是 shell 的类型，而是**本模块自己
// 写出的受控句**：批量结论、带计数的回执、前置判定与取数失败结论。改造前它们是 Go 里
// 拼好的中文字符串（模板常量 + fmt.Sprintf），现在是事实 —— 语言在渲染期由模板的 tf 决定。
//
// 复用现有词条的地方一律复用（保存成功 / 已删除 / 导入完成 / 活动开始发送都是 enums 里
// 已有的 Msg*）；其余走 enums 的 Notice*（新增的 mail.* key，清单见 mail_enums.go
// 的那一块，词条由迁移另行 seed）；词条缺失时回落这里的中文兜底，与改造前逐字相同。
//
// 占位符必须是位置占位符（百分号 s）：adminui.TranslateArgs 只认这一种形态，词条里混进
// %d 之类的写法会回落兜底 —— 所以数字一律 strconv 后作为参数传，不写进句子。
var (
	// 批量结论（mailBulkOutcome 的三档）。
	mailBulkAllDoneMsg    = mailMsg{Key: mailenums.NoticeBulkAllDone, Fallback: "已%s %s 个%s。"}
	mailBulkAllSkippedMsg = mailMsg{Key: mailenums.NoticeBulkAllSkipped, Fallback: "0 个%s被%s：%s 个被跳过（不存在或被服务端拒绝）。"}
	mailBulkPartialMsg    = mailMsg{Key: mailenums.NoticeBulkPartial, Fallback: "已%s %s 个%s，另有 %s 个被跳过（不存在或被服务端拒绝）。"}

	// 带计数的写动作回执（自动化与营销页）。
	mailAutomationSavedMsg = mailMsg{Key: mailenums.MsgSaveSuccess, Fallback: "已保存（版本 %s）"}
	mailAutomationTickMsg  = mailMsg{Key: mailenums.NoticeAutomationTick, Fallback: "已补投 %s 个到点实例"}
	mailContactImportMsg   = mailMsg{Key: mailenums.MsgImportSuccess, Fallback: "导入完成：新增 %s，更新 %s，跳过 %s，抑制名单命中 %s，非法 %s"}
	mailCampaignStartedMsg = mailMsg{Key: mailenums.MsgCampaignStarted, Fallback: "活动已开始发送，目标 %s 人；进度可在下方列表刷新查看。"}

	// 单条写动作回执（复用 enums 的 MsgDeleteSuccess）。
	mailAutomationDeletedMsg = mailMsg{Key: mailenums.MsgDeleteSuccess, Fallback: "已删除"}

	// 本模块自造的前置判定与取数失败结论（不是 service 上抛的 error）。
	mailTemplateListFailedMsg = mailMsg{Key: mailenums.NoticeTemplateListFailed, Fallback: "读取模板列表失败，本次没有删除任何模板。"}
	mailContactStatusBadMsg   = mailMsg{Key: mailenums.NoticeContactStatusBad, Fallback: "目标状态不合法，本次没有处理任何联系人。"}

	// 缺 id 的三句引导（分档判据见 mailQueryID 的说明）。
	mailCampaignIDRequiredMsg   = mailMsg{Key: mailenums.NoticeCampaignIDRequired, Fallback: "请先从活动列表选择一条活动，再查看它的报表。"}
	mailRunIDRequiredMsg        = mailMsg{Key: mailenums.NoticeRunIDRequired, Fallback: "请先从实例列表选择一条实例，再查看它的排障详情。"}
	mailAutomationIDRequiredMsg = mailMsg{Key: mailenums.NoticeAutomationIDRequired, Fallback: "请先从流程列表选择一个流程，再查看它的画布。"}
)

// mailTestSendFailedLabel 发送失败分类 → 受控文案标签。
//
// 取值映射用 mailer 的 Kind 常量（不写字面量）：Kind 改了值而这里写死字符串的话，
// 表现是「所有失败都落进未分类」—— 页面照常显示，只是永远说不出原因。
func mailTestSendFailedLabel(kind string) mailenums.LabelPair {
	switch strings.TrimSpace(kind) {
	case string(mailer.KindTemporary):
		return mailenums.TestSendFailedTemporary
	case string(mailer.KindPermanent):
		return mailenums.TestSendFailedPermanent
	case string(mailer.KindConfiguration):
		return mailenums.TestSendFailedConfiguration
	default:
		return mailenums.TestSendFailedUnknown
	}
}

// mailTestSendFailedMsg 测试发送失败的受控**事实** + 结构化日志。
//
// 分类标签本身就是「key + 中文兜底」（mailTestSendFailedLabel），所以这里交事实、
// 取词在模板渲染期发生（见 mail_jump.go）—— 英文后台拿到的是英文分类，而不是中文兜底。
func mailTestSendFailedMsg(c *gin.Context, kind, detail string) mailMsg {
	if strings.TrimSpace(detail) != "" {
		logger.Scene(mailErrScene).
			With("user_id", builtin.CurrentUserID(c)).
			With("path", c.Request.URL.Path).
			With("kind", kind).
			Error(errors.New(detail), "测试邮件发送失败")
	}
	return mailLabelMsg(mailTestSendFailedLabel(kind))
}

// —— 缺 id 的引导文案与前置判据（审计 P0：三处「无 id 前置判定」）——
//
// 背景：活动报表页（?id）/ 实例排障页（?id）/ 流程画布页（?id）三个入口都必须带 id，
// 而它们此前都是「直接调 service，靠查询失败兜底」—— 于是「没给 id」与「id 给错」
// 被压成同一句含糊文案（service 的 mail.err.invalidParam → 「参数不合法」）。
// 最刺眼的一处是活动报表页：sys_menus 里「邮件活动」是**正式菜单项**且 path 不带参数
// （id=137 → /admin/mail/campaign），运营点菜单进来必然看到「参数不合法」，
// 而那个页面上**没有任何参数可改** —— 用户没有任何出路。
//
// 分档判据（两档的差别不是措辞，而是「用户下一步该干什么」）：
//
//	· 没给 id → 这是**引导**：说清去哪找（本页列出的三句话都点名了列表页）；
//	· 给了 id 但查不到 → 保留 service 的业务文案（活动不存在 / 自动化实例不存在 /
//	  自动化流程不存在）—— 那句话是对的，缺的是前面那一档。
//
// 这三句文案的形状是**事实**（定义见上面的 mailCampaignIDRequiredMsg 一族）。
//
// 与改造前的差别只有一处：那时「是本模块自造的受控文案」要靠一张中文白名单来证明，
// 所以只能硬编码中文、也不带 key；现在判定归 response.IsBusinessError（见 mailErrMsg），
// 这些句子带上自己的 key 就能按请求语言渲染 —— 受控性由「整句由本模块写出」保证，
// 不靠「没有 key」保证。结论由 shell.RenderJump 直接渲染（见 mail_jump.go），
// 不再经查询参数回带，所以也不需要一份「读侧候选集」来证明这条提示出自本仓。

// mailQueryID 读取 ?id= 并区分「没给」与「给了」——第二返回值 false 表示**缺参**。
//
// 为什么不能写 `id := reqparams.ParseUint(c.Query("id")); if id == 0 { 缺参 }`：
// ParseUint 对 ""、"abc"、"0" 一律返回 0，那样「没给 id」与「给了个查不到的 id」
// 又被压回同一档 —— 正是本轮要拆开的那对情况。判据必须是**原始 query 是否为空**：
// 非空值（含非法值）交给 service，由它按自己的口径给业务文案 ——
// `?id=99999999`（合法但不存在的 id）落「活动不存在 / 自动化实例不存在 / 自动化流程不存在」，
// `?id=abc`（非法值，解析成 0）落 service 的 mail.err.invalidParam（「参数不合法」）。
// 两句都比改前多了一层信息：**没给 id 才走引导**，给了 id 的话文案说的是那个 id 怎么了。
//
// 为什么放在 mail_err.go 而不是某个 handler：三个 handler（活动报表 / 实例排障 /
// 流程画布）共用同一份判据与同一套文案档位，它们的改动必须是一个整体，散开必然漂移。
func mailQueryID(c *gin.Context) (uint64, bool) {
	raw := strings.TrimSpace(c.Query("id"))
	if raw == "" {
		return 0, false
	}
	return reqparams.ParseUint(raw), true
}

// mailAutomationStatusNotice 状态变更回执（当前语言）。
//
// 整句外壳也是词条（AutomationStatusChanged）：只把状态标签词条化的话，
// 英文界面会显示成「状态已更新为 Enabled.」—— 半句中文比全句中文更像渲染故障。
// 结论由 shell.RenderJump 渲染进响应体（见 mail_jump.go），不再经 ?ok= 回带。
func mailAutomationStatusNotice(c *gin.Context, status string) string {
	tr := mailTranslate(c)
	return i18n.FillTranslate(tr,
		mailenums.AutomationStatusChanged, mailenums.AutomationStatusChangedFallback,
		map[string]string{"status": mailLabel(tr, mailenums.AutomationStatusLabel(status))})
}
