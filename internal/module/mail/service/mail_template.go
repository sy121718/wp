package mailservice

import (
	"bytes"
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"strings"
	"text/template"
	"time"

	mailcontract "go_wp/internal/module/mail/contract"
	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	mailmodel "go_wp/internal/module/mail/model"
)

const templateOption = "missingkey=error"

func (s *Service) UpsertTemplate(ctx context.Context, req *maildto.SaveTemplateReq) (res *maildto.TemplateItem, err error) {
	if req == nil || strings.TrimSpace(req.TemplateKey) == "" || strings.TrimSpace(req.Subject) == "" {
		return nil, mailenums.Err(mailenums.ErrInvalidParam)
	}

	var (
		bodyJSON     mailmodel.JSONMap
		compiledHTML string
		compiledText string
	)
	if len(req.BodyJSON) > 0 {
		var tree TemplateTree
		if uerr := json.Unmarshal(req.BodyJSON, &tree); uerr != nil {
			return nil, errors.New(mailenums.ErrTemplateSyntax + ": " + uerr.Error())
		}
		html, text, cerr := CompileTemplate(tree)
		if cerr != nil {
			return nil, errors.New(mailenums.ErrTemplateSyntax + ": " + cerr.Error())
		}
		if uerr := json.Unmarshal(req.BodyJSON, &bodyJSON); uerr != nil {
			return nil, errors.New(mailenums.ErrTemplateSyntax + ": " + uerr.Error())
		}
		compiledHTML, compiledText = html, text
	}

	checkHTML, checkText := req.BodyHTML, req.BodyText
	if compiledHTML != "" {
		checkHTML, checkText = compiledHTML, compiledText
	}
	if _, _, _, rerr := RenderTemplate(req.Subject, checkHTML, checkText, map[string]any{}); rerr != nil && !isMissingKeyErr(rerr) {
		return nil, errors.New(mailenums.ErrTemplateSyntax + ": " + rerr.Error())
	}
	e := &mailmodel.MailTemplateEntity{
		TemplateKey:      strings.TrimSpace(req.TemplateKey),
		Locale:           strings.TrimSpace(req.Locale),
		Name:             strings.TrimSpace(req.Name),
		Subject:          req.Subject,
		BodyHTML:         req.BodyHTML,
		BodyText:         req.BodyText,
		BodyJSON:         bodyJSON,
		BodyCompiledHTML: compiledHTML,
		BodyCompiledText: compiledText,
		Status:           mailmodel.TemplateStatusEnabled,
	}
	if len(req.Variables) > 0 {
		e.Variables = mailmodel.StringArray(req.Variables)
	}
	if err = s.m.UpsertTemplate(ctx, e); err != nil {
		return nil, err
	}
	return templateItemOf(e), nil
}

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

func (s *Service) ListTemplatesPage(ctx context.Context, key string, page, limit int) (res []*maildto.TemplateItem, total int64, current int, err error) {
	list, total, current, err := s.m.ListTemplatesPage(ctx, key, page, limit)
	if err != nil {
		return nil, 0, 0, err
	}
	res = make([]*maildto.TemplateItem, 0, len(list))
	for _, e := range list {
		res = append(res, templateItemOf(e))
	}
	return res, total, current, nil
}

func (s *Service) GetTemplate(ctx context.Context, key, locale string) (*maildto.TemplateItem, error) {
	if strings.TrimSpace(key) == "" {
		return nil, mailenums.Err(mailenums.ErrInvalidParam)
	}
	e, err := s.m.GetTemplate(ctx, strings.TrimSpace(key), strings.TrimSpace(locale))
	if err != nil {
		return nil, mailenums.Err(mailenums.ErrTemplateNotFound)
	}
	return templateItemOf(e), nil
}

func (s *Service) DeleteTemplate(ctx context.Context, key, locale string) (err error) {
	return s.m.DeleteTemplate(ctx, key, locale)
}

func bodyOf(tpl *mailmodel.MailTemplateEntity) (html, text string) {
	if tpl == nil {
		return "", ""
	}
	if len(tpl.BodyJSON) > 0 {
		return tpl.BodyCompiledHTML, tpl.BodyCompiledText
	}
	return tpl.BodyHTML, tpl.BodyText
}

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

func (s *Service) SendTemplate(ctx context.Context, req *maildto.SendTemplateReq) (res *maildto.SendResult, err error) {
	if req == nil || strings.TrimSpace(req.To) == "" || strings.TrimSpace(req.TemplateKey) == "" {
		return nil, mailenums.Err(mailenums.ErrInvalidParam)
	}
	to := strings.TrimSpace(req.To)

	blocked, err := s.m.SuppressedEmails(ctx, []string{to})
	if err != nil {
		return nil, err
	}
	if blocked[strings.ToLower(to)] {
		_, _ = s.writeLog(ctx, &maildto.SendTemplateReq{To: to, TemplateKey: req.TemplateKey}, mailmodel.LogStatusSuppressed, "", nil)
		return &maildto.SendResult{To: to, Suppressed: true}, nil
	}

	tpl, err := s.m.GetTemplate(ctx, req.TemplateKey, req.Locale)
	if err != nil && strings.TrimSpace(req.Locale) != "" {
		tpl, err = s.m.GetTemplate(ctx, req.TemplateKey, "")
	}
	if err != nil {
		return nil, mailenums.Err(mailenums.ErrTemplateNotFound)
	}

	htmlSrc, textSrc := bodyOf(tpl)
	subject, html, text, rerr := RenderTemplate(tpl.Subject, htmlSrc, textSrc, req.Vars)
	if rerr != nil {
		return nil, rerr
	}

	accountID := req.AccountID
	if accountID == 0 {
		acc, aerr := s.m.DefaultAccount(ctx, mailmodel.AccountPurposeTransactional)
		if aerr != nil {
			return nil, mailenums.Err(mailenums.ErrAccountNotFound)
		}
		accountID = acc.ID
	}

	logID, err := s.writeLog(ctx, req, mailmodel.LogStatusPending, subject, &accountID)
	if err != nil {
		return nil, err
	}
	if err = enqueueMailSend(MailSendPayload{
		LogID: logID, AccountID: accountID, To: to,
		Subject: subject, HTML: html, Text: text,
	}); err != nil {

		if serr := s.sendNow(ctx, logID, accountID, to, subject, html, text); serr != nil {
			_ = s.m.UpdateLogResult(ctx, logID, map[string]any{"status": mailmodel.LogStatusFailed, "error_message": serr.Error()})
			return nil, serr
		}
	}
	return &maildto.SendResult{LogID: logID, To: to, Queued: true}, nil
}

func (s *Service) SendTransactional(ctx context.Context, in *mailcontract.SendInput) (res *mailcontract.SendOutcome, err error) {
	if in == nil {
		return nil, mailenums.Err(mailenums.ErrInvalidParam)
	}
	sent, serr := s.SendTemplate(ctx, &maildto.SendTemplateReq{
		TemplateKey: in.TemplateKey,
		Locale:      in.Locale,
		To:          in.To,
		Vars:        in.Vars,
	})
	if serr != nil {
		return nil, serr
	}
	return &mailcontract.SendOutcome{Queued: sent.Queued, Suppressed: sent.Suppressed}, nil
}

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
	if len(e.BodyJSON) > 0 {
		if b, err := json.Marshal(e.BodyJSON); err == nil {
			item.BodyJSON = b
		}
	}
	item.BodyCompiledHTML = e.BodyCompiledHTML
	item.BodyCompiledText = e.BodyCompiledText
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
