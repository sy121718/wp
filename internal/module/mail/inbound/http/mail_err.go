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
// 三件套（对齐 AGENTS.md「响应与错误处理」，样板见 admin_err.go / navigation_err.go）：
//   ① 可透出业务文案的**白名单** mailenums.MailFacingMessages（与 enums 常量一一对应，
//      由 enums 包内的 AST 对账测试钉住）；
//   ② **归口文案** mailenums.ErrInternal（mail.err.internal，迁移 276 seed 中英各一行）；
//   ③ **结构化日志** —— 未命中时原文只进日志，带场景、user_id 与请求路径。
//
// 为什么页面路径不直接用 response.ErrorAuto：那一份走**形态**判定（本模块的 JSON 接口在用，
// 效果不变），而页面路径要的是「这句话是不是本模块的文案」；判定同源才能保证
// 「JSON 出口与页面出口对同一个错误给出同一个结论」。

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"

	mailenums "go_wp/internal/module/mail/enums"
	"go_wp/internal/web/shell"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/mailer"
)

// mailErrScene 日志场景名（与 mail 模块其它 logger.Scene("mail") 调用点一致）。
const mailErrScene = "mail"

// mailFacingText 命中白名单 → 返回可对外文案（key 或 `key: 细节` 形态）；未命中 → ("", false)。
//
// 带参协议：本模块的 service 用 `key + ": " + 细节` 承载定位信息
// （mail_automation.go 的 ErrAutomationGraphInvalid + ": " + uerr.Error() —— 那半句是
// 图校验器给的「第几个节点、哪条边」），所以除整串相等外还认 `key: ` 前缀：
// 只放行 key 命中白名单的那些参数串，key 之外的原文不会随它一起透出。
//
// 拆出这个**不记日志**的版本，是给「调用点已经记过一条更具体的日志」的场景用
// （与 navigation_err.go 的 navigationFacingText 同一取舍）。
func mailFacingText(err error) (string, bool) {
	msg := ""
	if err != nil {
		msg = strings.TrimSpace(err.Error())
	}
	if msg == "" {
		return "", false
	}
	for _, m := range mailenums.MailFacingMessages {
		if msg == m || strings.HasPrefix(msg, m+": ") {
			return msg, true
		}
	}
	return "", false
}

// mailErrPageText 页面路径（?err= 回带、渲染数据 Err、302 回跳）的错误文案归口。
//
// 命中白名单 → 把 i18n key 翻成当前语言的文案后返回；
// 未命中 → 记一条结构化日志，返回 mailenums.ErrInternal 的归口译文。
//
// 为什么必须翻译：页面提示是**直接渲染的文本**（模板 {{.Err}} 与 ?err= 都不经过
// pkg/response 的 translate）。不翻的话运营会看到 mail.err.accountNotFound 这样的裸 key。
//
// 为什么不能让调用点直传 err.Error()：service 一旦把 PostgreSQL / SMTP 原文上抛，
// 它就会随 302 的 Location 或页面正文摆到运营面前，与 JSON body 一样不是可信边界。
func mailErrPageText(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	translate := shell.TranslateFor(c)
	if msg, ok := mailFacingText(err); ok {
		return translateMailFacing(translate, msg)
	}

	logger.Scene(mailErrScene).
		With("user_id", shell.CurrentUserID(c)).
		With("path", c.Request.URL.Path).
		Error(err, "邮箱后台页处理失败")
	return translate(mailenums.ErrInternal, "操作失败，请稍后重试")
}

// translateMailFacing 把命中的白名单文案翻成当前语言。
//
// `key: 明细` 只翻能识别的部分：key 取词条，明细按 i18n.ErrorDetail 协议取词并填 {name}。
// **明细不是 ErrorDetail 词条就丢弃并落日志** —— 改造前这里是 `text + ": " + detail`
// （原样拼回 service 的中文原文），英文界面上必然中英混排，而那句话会经 302 的 ?err=
// 进页面与浏览器历史（见 mail_automation.go 的 graphInvalidError 调用点）。
func translateMailFacing(translate func(key, fallback string) string, msg string) string {
	key, detail, hasDetail := strings.Cut(msg, ": ")
	// 兜底给 key 本身：词条缺失时页面显示的是 key（一眼可见），而不是把整句吞掉。
	text := translate(key, key)
	if hasDetail && detail != "" {
		if d := mailDetailText(translate, detail); d != "" {
			return text + "：" + d
		}
	}
	return text
}

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
// 与 shell.BulkIDs 的受控提示是同一判据：**看文案来自哪里**，而不是看它是不是 error 类型
// （超限那条已改成类型判定 + shell.BulkIDsFacingText，不再需要门禁豁免；本页自造的文案
// 仍由本函数原样透出）。
func mailFormErrText(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}

// mailBulkIDsText shell.BulkIDs 的失败文案（单次提交的 id 超过上限）。
//
// 只是转调 shell 的受控出口：超限错误是 shell 的类型（shell.BulkIDsError），
// 「一次最多操作 N 项，当前 M 项，请分批进行」按当前语言生成，其中**当前 M 项**
// （去重后的条数）只有 shell 知道 —— 本模块不再用 shell.MaxBulkIDs 重算一遍：
// 那是第二份真相，而且必然丢掉 Count（旧的实现正是如此）。
// 判据也不再是「文案来自哪里」而是类型：出口只认 sentinel，认不出就回落归口文案。
// 留痕（谁在哪个页面触发）由 shell 的出口统一记日志。
func mailBulkIDsText(c *gin.Context, err error) string {
	return shell.BulkIDsFacingText(c, err)
}

// —— 访客面出口：邮件里的公开链接（点击追踪 / 一键退订）——
//
// 这一段的消费者与上面**完全不同**，判据也不同：
//
//	· 上面（mailErrPageText 等）面向登录后的运营：有后台壳、有 ?err=、有归口文案；
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
	return shell.TranslateFor(c)(key, fallback)
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

// —— 读侧回执的收口（?err= / ?ok= / ?done=）——
//
// 写侧早已把错误收敛过（mailErrPageText / mailBulkIDsText / mailBulkOutcome），但页面
// 此前是把 query 参数**原样**塞进渲染数据（`"Err": c.Query("err")`）：任何人手拼一个
// /admin/mail?err=任意文案 就能在页面上塞一条顶着「上一次操作未完成」样式的伪造消息。
// 查询参数与响应体、模板数据一样**不是可信边界**。
//
// 判定用 shell.FacingNotice（受控形状：逐字相等 / 数字归一相等 / 文案 + "：" + 定位信息），
// 候选文案由 mailNoticeTexts 给出 —— 它是写侧全部出口的**镜像**：
// 改了写侧文案就要在这里同步，否则运营看到的会从「已删除 3 个发信账号。」退化成
// 「系统内部错误」（错误通道）或什么都不显示（成功通道）。

// mailOkToken 单条写动作成功时回带的固定 token（/admin/mail?ok=1）。
//
// 它对运营没有意义（页面上会渲染成一个裸 "1"），所以读侧把它**收敛成一句翻译过的
// 成功文案**再进模板 —— 这正是「已经是内部 token 的那条路」：不让 raw 原样透出。
const mailOkToken = "1"

// mailBulkVerbs / mailBulkNouns 批量结论里的动作与对象（写侧 mailBulkOutcome 的全部取值）。
var (
	mailBulkVerbs = []string{"删除", "更新"}
	mailBulkNouns = []string{"发信账号", "邮件模板", "联系人", "群发活动"}
)

// mailBulkResultTemplates 批量结论文案模板（与写侧 mailBulkOutcome 共用同一份字面量）。
var mailBulkResultTemplates = []string{
	"已%s %d 个%s。",
	"0 个%s被%s：%d 个被跳过（不存在或被服务端拒绝）。",
	"已%s %d 个%s，另有 %d 个被跳过（不存在或被服务端拒绝）。",
}

// mail自造回执文案（不是 enums key、也不来自 shell —— 但它们会进 ?err= / ?ok=）。
//
// 状态标签与测试发送文案**不在这里**：那两组是 (key, 中文兜底) 形态，真源在
// mailenums（mail_status_labels.go）—— 因为它们的读侧白名单要用同一份取值重拼候选集。
const (
	mailTemplateListFailedText = "读取模板列表失败，本次没有删除任何模板。"
	mailContactStatusBadText   = "目标状态不合法，本次没有处理任何联系人。"
	mailAutomationDeletedText  = "已删除"
)

// mailFormNoticeTemplates 本页自造的表单校验文案模板（parseAutomationForm / 组装层）。
//
// 它们带「第 N 行」定位，是运营照着改的依据 —— 判据与 mailErrPageText 同源：
// 能进响应的只有受控文案；这里的受控性来自「整句都由本页拼出 + 逐条登记」。
var mailFormNoticeTemplates = []string{
	"流程名称不能为空",
	"第 %d 行：节点标识与类型都要填",
	"第 %d 行：等待分钟数要填正整数",
	"第 %d 行：发信节点要选邮件模板",
	"第 %d 行：条件分支至少填一个条件",
	"第 %d 行：标签节点要填要加的标签",
	"至少要填一个节点",
	"流程定义组装失败，请检查各行的填写内容后重试",
}

// mailCountedNoticeTemplates 带计数的回执文案模板（自动化与营销页）。
var mailCountedNoticeTemplates = []string{
	"已保存（版本 %d）",
	"已补投 %d 个到点实例",
	"导入完成：新增 %d，更新 %d，跳过 %d，抑制名单命中 %d，非法 %d",
	"活动已开始发送，目标 %d 人；进度可在下方列表刷新查看。",
}

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

// mailTestSendFailedText 测试发送失败的受控回执 + 结构化日志。
//
// 文案按当前语言取词：这一句既会进 302 的 ?err=，也会作为读侧白名单的候选参与比对
// （mailNoticeTexts 调它生成候选）—— 两边都必须走这里，否则英文后台写侧写英文、
// 读侧认中文，运营看到的是「系统内部错误」。
func mailTestSendFailedText(c *gin.Context, kind, detail string) string {
	if strings.TrimSpace(detail) != "" {
		logger.Scene(mailErrScene).
			With("user_id", shell.CurrentUserID(c)).
			With("path", c.Request.URL.Path).
			With("kind", kind).
			Error(errors.New(detail), "测试邮件发送失败")
	}
	return mailLabel(shell.TranslateFor(c), mailTestSendFailedLabel(kind))
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
// 为什么这三句是中文硬编码而不是 mailenums 的 i18n key：与上面的
// mailTemplateListFailedText / mailContactStatusBadText 同一取舍 —— 它们是**本页自造**
// 的受控文案（不是 service 上抛、也不来自 shell），受控性来自「整句由本模块写出 +
// 逐条登记进 mailNoticeTexts」。**漏登记的后果是读侧把它丢弃**（shell.FacingNotice
// 未命中 → shell.PageInternalText），运营看到的是「系统内部错误」——
// 新增回执文案时务必同步登记，这是上一批踩过的点。
const (
	mailCampaignIDRequiredText   = "请先从活动列表选择一条活动，再查看它的报表。"
	mailRunIDRequiredText        = "请先从实例列表选择一条实例，再查看它的排障详情。"
	mailAutomationIDRequiredText = "请先从流程列表选择一个流程，再查看它的画布。"
)

// mailIDRequiredTexts 上面三句的集合（mailNoticeTexts 登记用；新增一句必须加进来）。
var mailIDRequiredTexts = []string{
	mailCampaignIDRequiredText,
	mailRunIDRequiredText,
	mailAutomationIDRequiredText,
}

// mailQueryID 读取 ?id= 并区分「没给」与「给了」——第二返回值 false 表示**缺参**。
//
// 为什么不能写 `id := shell.ParseUint(c.Query("id")); if id == 0 { 缺参 }`：
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
	return shell.ParseUint(raw), true
}

// mailAutomationStatusNotice 状态变更回执（当前语言）。
//
// **写侧与读侧共用这一份**：写侧（MailAutomationStatus）把它拼进 302 的 ?ok=，
// 读侧（mailNoticeTexts）用它生成白名单候选 —— 两边只要有一处自己拼字面量，
// 就会出现「写侧写了个读侧不认的值」：运营点完按钮，页面上什么都没有，
// 而服务端不报错、日志里也看不出来。
//
// 整句外壳也是词条（AutomationStatusChanged）：只把状态标签词条化的话，
// 英文界面会显示成「状态已更新为 Enabled.」—— 半句中文比全句中文更像渲染故障。
func mailAutomationStatusNotice(c *gin.Context, status string) string {
	tr := shell.TranslateFor(c)
	return i18n.FillTranslate(tr,
		mailenums.AutomationStatusChanged, mailenums.AutomationStatusChangedFallback,
		map[string]string{"status": mailLabel(tr, mailenums.AutomationStatusLabel(status))})
}

// mailNoticeTexts 本页可以原样展示的回执文案（当前语言）。
func mailNoticeTexts(c *gin.Context) []string {
	translate := shell.TranslateFor(c)
	out := make([]string, 0, len(mailenums.MailFacingMessages)+32)
	for _, key := range mailenums.MailFacingMessages {
		out = append(out, translate(key, key))
	}
	out = append(out,
		translate(mailenums.ErrInternal, "操作失败，请稍后重试"),
		shell.BulkIDsNoticeTemplate(c),
		mailTemplateListFailedText,
		mailContactStatusBadText,
		mailAutomationDeletedText,
		// 状态变更回执：与**写侧同一份拼装**（mailAutomationStatusNotice）。
		// 三条已知状态 + 未知档各生成一条候选 —— 少一条的表现是那一档回执
		// 在页面上静默消失（成功通道未命中回落空串）。
		mailAutomationStatusNotice(c, "active"),
		mailAutomationStatusNotice(c, "paused"),
		mailAutomationStatusNotice(c, "draft"),
		mailAutomationStatusNotice(c, ""),
		mailTestSendFailedText(c, string(mailer.KindTemporary), ""),
		mailTestSendFailedText(c, string(mailer.KindPermanent), ""),
		mailTestSendFailedText(c, string(mailer.KindConfiguration), ""),
		// 未分类档（Kind 为空 / 取值漂移）：写侧会落到它，读侧不登记就会把那条回执丢掉。
		mailTestSendFailedText(c, "", ""),
	)
	// 缺 id 的引导文案（见 mailIDRequiredTexts）：不登记的话读侧会把它丢掉，
	// 运营看到的会从「请先从活动列表选择一条活动」退化成「系统内部错误」。
	out = append(out, mailIDRequiredTexts...)
	for _, tpl := range mailFormNoticeTemplates {
		out = append(out, shell.NoticeTemplate(tpl))
	}
	for _, tpl := range mailCountedNoticeTemplates {
		out = append(out, shell.NoticeTemplate(tpl))
	}
	for _, verb := range mailBulkVerbs {
		for _, noun := range mailBulkNouns {
			out = append(out,
				shell.NoticeTemplate(fmt.Sprintf(mailBulkResultTemplates[0], verb, 0, noun)),
				shell.NoticeTemplate(fmt.Sprintf(mailBulkResultTemplates[1], noun, verb, 0)),
				shell.NoticeTemplate(fmt.Sprintf(mailBulkResultTemplates[2], verb, 0, noun, 0)),
			)
		}
	}
	return out
}

// mailPageErr 页面 ?err= 的统一出口（未命中落归口文案）。
func mailPageErr(c *gin.Context) string {
	return shell.FacingQueryText(c.Query("err"), shell.PageInternalText(c), func(raw string) string {
		return shell.FacingNotice(raw, mailNoticeTexts(c))
	})
}

// mailPageOk 页面 ?ok= 的统一出口（固定 token 收敛成一句成功文案；其余过白名单，未命中落空串）。
func mailPageOk(c *gin.Context) string {
	raw := strings.TrimSpace(c.Query("ok"))
	if raw == "" {
		return ""
	}
	if raw == mailOkToken {
		return shell.TranslateFor(c)(mailenums.MsgSaveSuccess, "保存成功")
	}
	return shell.FacingNotice(raw, mailNoticeTexts(c))
}

// mailPageDone 页面 ?done= 的统一出口（成功提示：未命中落空串）。
func mailPageDone(c *gin.Context) string {
	return shell.FacingQueryText(c.Query("done"), "", func(raw string) string {
		return shell.FacingNotice(raw, mailNoticeTexts(c))
	})
}
