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
// `key: 细节` 只翻 key，细节原样保留 —— 细节是本模块 service 自己生成的定位信息
// （「节点 b 的下一步 c 不存在」），不是词条、也不该按词条去查表。
func translateMailFacing(translate func(key, fallback string) string, msg string) string {
	key, detail, hasDetail := strings.Cut(msg, ": ")
	// 兜底给 key 本身：词条缺失时页面显示的是 key（一眼可见），而不是把整句吞掉。
	text := translate(key, key)
	if hasDetail {
		return text + ": " + detail
	}
	return text
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
const (
	mailTemplateListFailedText = "读取模板列表失败，本次没有删除任何模板。"
	mailContactStatusBadText   = "目标状态不合法，本次没有处理任何联系人。"
	mailAutomationDeletedText  = "已删除"
	mailStatusLabelActive      = "已启用"
	mailStatusLabelPaused      = "已暂停"
	mailStatusLabelDraft       = "草稿"
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

// mailTestSendFailedTemplates 测试发送失败的受控文案（SMTP 原文只进日志）。
//
// 分类取自 mailer 的 Kind（temporary / permanent / configuration）—— 有限的枚举，
// 而 SMTP 的响应码与主机名一律不出现在页面上：它们既不是给运营看的，
// 也不该经浏览器历史与 Referer 留在 URL 里。
var mailTestSendFailedTemplates = map[string]string{
	string(mailer.KindTemporary):     "测试邮件发送失败（可重试的临时故障），详情见服务端日志。",
	string(mailer.KindPermanent):     "测试邮件发送失败（被对方永久拒绝），详情见服务端日志。",
	string(mailer.KindConfiguration): "测试邮件发送失败（配置问题，需人工处理），详情见服务端日志。",
}

// mailTestSendFailedText 测试发送失败的受控回执 + 结构化日志。
func mailTestSendFailedText(c *gin.Context, kind, detail string) string {
	if strings.TrimSpace(detail) != "" {
		logger.Scene(mailErrScene).
			With("user_id", shell.CurrentUserID(c)).
			With("path", c.Request.URL.Path).
			With("kind", kind).
			Error(errors.New(detail), "测试邮件发送失败")
	}
	if tpl, ok := mailTestSendFailedTemplates[strings.TrimSpace(kind)]; ok {
		return tpl
	}
	return "测试邮件发送失败（未分类），详情见服务端日志。"
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
		"状态已更新为 "+mailStatusLabelActive+"。",
		"状态已更新为 "+mailStatusLabelPaused+"。",
		"状态已更新为 "+mailStatusLabelDraft+"。",
		"状态已更新为 未知状态。",
		mailTestSendFailedText(c, string(mailer.KindTemporary), ""),
		mailTestSendFailedText(c, string(mailer.KindPermanent), ""),
		mailTestSendFailedText(c, string(mailer.KindConfiguration), ""),
	)
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
