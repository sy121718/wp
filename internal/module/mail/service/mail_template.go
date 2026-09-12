package mailservice

// mail_template.go — 邮件模板 CRUD、渲染与事务发送（issue #37）。

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"text/template"
	"time"

	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	mailmodel "go_wp/internal/module/mail/model"
)

// 模板渲染选项。
//
// **missingkey=error 是刻意的**：Go 模板默认把缺失变量渲染成 "<no value>"，
// 营销群发时一个变量拼错就会静默发出去几千封带着 `<no value>` 的邮件 ——
// 那种事故没人会及时发现。宁可现在报错。
const templateOption = "missingkey=error"

// UpsertTemplate 新建或覆盖模板（key + locale 唯一）。
func (s *Service) UpsertTemplate(ctx context.Context, req *maildto.SaveTemplateReq) (res *maildto.TemplateItem, err error) {
	if req == nil || strings.TrimSpace(req.TemplateKey) == "" || strings.TrimSpace(req.Subject) == "" {
		return nil, errors.New(mailenums.ErrInvalidParam)
	}
	// 保存前**先试渲染一次**（用空变量）：模板语法错、变量名写错当场暴露，
	// 而不是等到真发信时才失败。
	if _, _, _, rerr := RenderTemplate(req.Subject, req.BodyHTML, req.BodyText, map[string]any{}); rerr != nil && !isMissingKeyErr(rerr) {
		return nil, errors.New(mailenums.ErrTemplateSyntax + ": " + rerr.Error())
	}
	e := &mailmodel.MailTemplateEntity{
		TemplateKey: strings.TrimSpace(req.TemplateKey),
		Locale:      strings.TrimSpace(req.Locale),
		Name:        strings.TrimSpace(req.Name),
		Subject:     req.Subject,
		BodyHTML:    req.BodyHTML,
		BodyText:    req.BodyText,
		Status:      mailmodel.TemplateStatusEnabled,
	}
	if len(req.Variables) > 0 {
		e.Variables = mailmodel.StringArray(req.Variables)
	}
	if err = s.m.UpsertTemplate(ctx, e); err != nil {
		return nil, err
	}
	return templateItemOf(e), nil
}

// ListTemplates 列出模板。
func (s *Service) ListTemplates(ctx context.Context, key string) (res []*maildto.TemplateItem, err error) {
	list, err := s.m.ListTemplates(ctx, key)
	if err != nil {
		return nil, err
	}
	res = make([]*maildto.TemplateItem, 0, len(list))
	for _, e := range list {
		res = append(res, templateItemOf(e))
	}
	return res, nil
}

// DeleteTemplate 删除模板（按 key + locale）。
func (s *Service) DeleteTemplate(ctx context.Context, key, locale string) (err error) {
	return s.m.DeleteTemplate(ctx, key, locale)
}

// RenderTemplate 渲染模板（导出以便单测直接验证渲染语义）。
//
// 三个串（主题 / HTML / 纯文本）共用同一份变量；纯文本为空时由 HTML 兜底不需要，
// 发送时若只有其一也能发（multipart/alternative 的两种形态由 mailer 决定）。
func RenderTemplate(subject, bodyHTML, bodyText string, vars map[string]any) (sSubject, sHTML, sText string, err error) {
	if vars == nil {
		vars = map[string]any{}
	}
	render := func(name, src string) (string, error) {
		if strings.TrimSpace(src) == "" {
			return "", nil
		}
		t, perr := template.New(name).Option(templateOption).Parse(src)
		if perr != nil {
			return "", perr
		}
		var buf bytes.Buffer
		if perr = t.Execute(&buf, vars); perr != nil {
			return "", perr
		}
		return buf.String(), nil
	}
	if sSubject, err = render("subject", subject); err != nil {
		return "", "", "", err
	}
	if sHTML, err = render("html", bodyHTML); err != nil {
		return "", "", "", err
	}
	if sText, err = render("text", bodyText); err != nil {
		return "", "", "", err
	}
	return sSubject, sHTML, sText, nil
}

// SendTemplate 发送一封事务邮件：抑制检查 → 渲染 → 落日志 → 入队。
//
// **抑制检查与渲染都在入队前同步完成**：
//
//	· 抑制名单命中的直接落 suppressed 日志（留痕「为什么没发」），连队都不入；
//	· 渲染失败当场返回错误（模板语法 / 变量缺失），不会入队后才在 worker 里炸。
//
// 真正的投递在 worker 里（mail:send 任务），失败按错误分类回写状态。
func (s *Service) SendTemplate(ctx context.Context, req *maildto.SendTemplateReq) (res *maildto.SendResult, err error) {
	if req == nil || strings.TrimSpace(req.To) == "" || strings.TrimSpace(req.TemplateKey) == "" {
		return nil, errors.New(mailenums.ErrInvalidParam)
	}
	to := strings.TrimSpace(req.To)

	// 1. 抑制名单（批量接口，单个也走它，口径一致）
	blocked, err := s.m.SuppressedEmails(ctx, []string{to})
	if err != nil {
		return nil, err
	}
	if blocked[strings.ToLower(to)] {
		_, _ = s.writeLog(ctx, &maildto.SendTemplateReq{To: to, TemplateKey: req.TemplateKey}, mailmodel.LogStatusSuppressed, "", nil)
		return &maildto.SendResult{To: to, Suppressed: true}, nil
	}

	// 2. 取模板并按语言回退（指定语言没有就用空 locale 的通用模板）
	tpl, err := s.m.GetTemplate(ctx, req.TemplateKey, req.Locale)
	if err != nil && strings.TrimSpace(req.Locale) != "" {
		tpl, err = s.m.GetTemplate(ctx, req.TemplateKey, "")
	}
	if err != nil {
		return nil, errors.New(mailenums.ErrTemplateNotFound)
	}

	// 3. 渲染（缺失变量在这里报错，而不是发出去 <no value>）
	subject, html, text, rerr := RenderTemplate(tpl.Subject, tpl.BodyHTML, tpl.BodyText, req.Vars)
	if rerr != nil {
		return nil, rerr
	}

	// 4. 选账号：显式指定优先，否则用该用途的默认账号
	accountID := req.AccountID
	if accountID == 0 {
		acc, aerr := s.m.DefaultAccount(ctx, mailmodel.AccountPurposeTransactional)
		if aerr != nil {
			return nil, errors.New(mailenums.ErrAccountNotFound)
		}
		accountID = acc.ID
	}

	// 5. 落日志（pending）+ 入队
	logID, err := s.writeLog(ctx, req, mailmodel.LogStatusPending, subject, &accountID)
	if err != nil {
		return nil, err
	}
	if err = enqueueMailSend(MailSendPayload{
		LogID: logID, AccountID: accountID, To: to,
		Subject: subject, HTML: html, Text: text,
	}); err != nil {
		// 入队失败：把日志标成失败并回传错误（不静默丢信）。
		_ = s.m.UpdateLogResult(ctx, logID, map[string]any{"status": mailmodel.LogStatusFailed, "error_message": err.Error()})
		return nil, err
	}
	return &maildto.SendResult{LogID: logID, To: to, Queued: true}, nil
}

// writeLog 写一条发送日志，返回日志 id。
func (s *Service) writeLog(ctx context.Context, req *maildto.SendTemplateReq, status, subject string, accountID *uint64) (id uint64, err error) {
	e := &mailmodel.MailLogEntity{
		ToEmail:     strings.TrimSpace(req.To),
		Status:      status,
		TemplateKey: strPtr(strings.TrimSpace(req.TemplateKey)),
	}
	if strings.TrimSpace(subject) != "" {
		e.Subject = strPtr(subject)
	}
	if accountID != nil {
		e.AccountID = accountID
	}
	if err = s.m.CreateLog(ctx, e); err != nil {
		return 0, err
	}
	return e.ID, nil
}

// isMissingKeyErr 判断是不是「变量缺失」类错误（保存模板时的空变量试渲染会命中它，
// 那不算语法错 —— 变量要到发送时才有值）。
func isMissingKeyErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "map has no entry for key")
}

func templateItemOf(e *mailmodel.MailTemplateEntity) *maildto.TemplateItem {
	item := &maildto.TemplateItem{
		ID:          e.ID,
		TemplateKey: e.TemplateKey,
		Locale:      e.Locale,
		Name:        e.Name,
		Subject:     e.Subject,
		BodyHTML:    e.BodyHTML,
		BodyText:    e.BodyText,
		Status:      e.Status,
	}
	if len(e.Variables) > 0 {
		item.Variables = append(item.Variables, e.Variables...)
	}
	if e.UpdateTime != nil {
		item.UpdateTime = e.UpdateTime.Format(time.RFC3339)
	}
	return item
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
