package mailservice

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"net/mail"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/internal/module/mail/contract"
	"go_wp/internal/module/mail/dto"
	"go_wp/internal/module/mail/enums"
	mailmodel "go_wp/internal/module/mail/model"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

var _ mailcontract.MailService = (*Service)(nil)

func (s *Service) ListContacts(ctx context.Context, req *maildto.ContactFilterReq) (res *maildto.ContactListResp, err error) {
	if req == nil {
		req = &maildto.ContactFilterReq{}
	}
	page, size := req.Page, req.PageSize
	if page <= 0 {
		page = 1
	}
	if size <= 0 || size > 200 {
		size = 20
	}
	list, total, err := s.m.ListContacts(ctx, mailmodel.ContactFilter{
		Keyword: req.Keyword,
		Status:  req.Status,
		Tags:    req.Tags,
		Offset:  utils.PageOffset(page, size),
		Limit:   size,
	})
	if err != nil {
		return nil, err
	}
	res = &maildto.ContactListResp{Items: make([]maildto.ContactItem, 0, len(list)), Total: total}
	for _, e := range list {
		res.Items = append(res.Items, contactItemOf(e))
	}
	return res, nil
}

func (s *Service) UpdateContactStatus(ctx context.Context, req *maildto.UpdateContactStatusReq) (err error) {
	if req == nil || req.ID == 0 || !contactStatusValid(req.Status) {
		return mailenums.Err(mailenums.ErrInvalidParam)
	}
	row, err := s.m.GetContact(ctx, req.ID)
	if err != nil {
		return mailenums.Err(mailenums.ErrContactNotFound)
	}

	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		return s.applyContactStatusTx(ctx, tx, req.ID, row.Email, req.Status, req.Note, time.Now())
	}); err != nil {
		return err
	}

	s.fireContactSubscribedIfChanged(ctx, req.ID, row.Status, req.Status)
	return nil
}

func contactStatusValid(status string) bool {
	switch status {
	case mailmodel.ContactStatusSubscribed, mailmodel.ContactStatusPending,
		mailmodel.ContactStatusUnsubscribed, mailmodel.ContactStatusBounced,
		mailmodel.ContactStatusComplained:
		return true
	}
	return false
}

func contactStatusFields(status, note string, at time.Time) map[string]any {
	fields := map[string]any{"status": status, "update_time": at}
	if status == mailmodel.ContactStatusSubscribed {
		fields["subscribed_at"] = at
		if src := strings.TrimSpace(note); src != "" {
			fields["consent_source"] = src
		}
	}
	return fields
}

func suppressionReasonForStatus(status string) (string, bool) {
	switch status {
	case mailmodel.ContactStatusUnsubscribed:
		return mailmodel.SuppressionReasonUnsubscribe, true
	case mailmodel.ContactStatusComplained:
		return mailmodel.SuppressionReasonComplaint, true
	case mailmodel.ContactStatusBounced:
		return mailmodel.SuppressionReasonHardBounce, true
	}
	return "", false
}

func contactStatusForSuppression(reason string) string {
	switch reason {
	case mailmodel.SuppressionReasonComplaint:
		return mailmodel.ContactStatusComplained
	case mailmodel.SuppressionReasonHardBounce:
		return mailmodel.ContactStatusBounced
	}
	return mailmodel.ContactStatusUnsubscribed
}

func (s *Service) applyContactStatusTx(ctx context.Context, tx *gorm.DB, contactID uint64, email, status, note string, at time.Time) error {
	if err := s.m.UpdateContactFieldsTx(ctx, tx, contactID, contactStatusFields(status, note, at)); err != nil {
		return err
	}
	reason, need := suppressionReasonForStatus(status)
	if !need {
		return nil
	}
	sup := &mailmodel.MailSuppressionEntity{
		Email: email, Reason: reason, Source: strPtr("manual"),
	}
	if reason == mailmodel.SuppressionReasonUnsubscribe {
		sup.Note = strPtr(note)
	}
	return s.m.AddSuppressionTx(ctx, tx, sup)
}

func (s *Service) fireContactSubscribedIfChanged(ctx context.Context, contactID uint64, was, now string) {
	if now != mailmodel.ContactStatusSubscribed || was == mailmodel.ContactStatusSubscribed {
		return
	}
	s.OnContactSubscribed(ctx, contactID)
}

func isContactEmailConflict(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "23505") || strings.Contains(msg, "duplicate key value")
}

func contactItemOf(e *mailmodel.MailContactEntity) maildto.ContactItem {
	item := maildto.ContactItem{
		ID:     e.ID,
		Email:  e.Email,
		Source: e.Source,
		Status: e.Status,
		Tags:   e.Tags,
	}
	if e.Name != nil {
		item.Name = *e.Name
	}
	if e.UserID != nil {
		item.UserID = *e.UserID
	}
	if e.ConsentSource != nil {
		item.ConsentSource = *e.ConsentSource
	}
	if e.SubscribedAt != nil {
		item.SubscribedAt = e.SubscribedAt.Format(time.RFC3339)
	}
	if e.CreateTime != nil {
		item.CreateTime = e.CreateTime.Format(time.RFC3339)
	}
	return item
}

const maxImportBytes = 8 << 20

const importBatchSize = 500

type contactRow struct {
	Line  int
	Email string
	Name  string
	Tags  []string
}

func (s *Service) ImportContacts(ctx context.Context, req *maildto.ImportContactsReq) (res *maildto.ImportContactsResp, err error) {
	if req == nil || len(req.Content) == 0 {
		return nil, mailenums.Err(mailenums.ErrImportEmpty)
	}
	if len(req.Content) > maxImportBytes {
		return nil, mailenums.Err(mailenums.ErrImportTooLarge)
	}

	res = &maildto.ImportContactsResp{Errors: []maildto.ImportRowError{}}
	rows := parseContactRows(req.Content, res)
	res.Total = len(rows)
	if len(rows) == 0 {
		return res, nil
	}

	seen := make(map[string]struct{}, len(rows))
	unique := make([]contactRow, 0, len(rows))
	for _, r := range rows {
		key := strings.ToLower(r.Email)
		if _, dup := seen[key]; dup {
			res.Skipped++
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, r)
	}

	emails := make([]string, 0, len(unique))
	for _, r := range unique {
		emails = append(emails, r.Email)
	}
	blocked, err := s.m.SuppressedEmails(ctx, emails)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	list := make([]*mailmodel.MailContactEntity, 0, len(unique))
	for _, r := range unique {
		if blocked[strings.ToLower(r.Email)] {

			res.Suppressed++
			continue
		}
		tags := mailmodel.StringArray(mergeTags(r.Tags, req.DefaultTags))
		name := r.Name
		e := &mailmodel.MailContactEntity{
			Email:  r.Email,
			Source: mailmodel.ContactSourceImport,
			Status: mailmodel.ContactStatusPending,
			Tags:   tags,
		}
		if name != "" {
			e.Name = &name
		}
		if req.ConsentDeclared {

			e.Status = mailmodel.ContactStatusSubscribed
			e.SubscribedAt = &now
			if src := strings.TrimSpace(req.ConsentSource); src != "" {
				e.ConsentSource = &src
			}
		}
		list = append(list, e)
	}

	if len(list) == 0 {
		return res, nil
	}

	existing, err := s.m.ExistingContactIDs(ctx, emails)
	if err != nil {
		return nil, err
	}

	toInsert := make([]*mailmodel.MailContactEntity, 0, len(list))
	toUpdate := make([]mailmodel.ContactImportUpdate, 0, len(list))

	needTagDiff := hasTriggerType(s.activeAutomations(ctx), mailmodel.TriggerTagAdded)

	tagAdds := make(map[string][]string, len(list))

	pendingTags := make([]pendingTagDiff, 0, len(list))
	for _, e := range list {
		key := strings.ToLower(e.Email)
		if _, exists := existing[key]; !exists {
			toInsert = append(toInsert, e)

			if len(e.Tags) > 0 {
				tagAdds[key] = []string(e.Tags)
			}
			continue
		}
		if !req.UpdateExisting {
			res.Skipped++
			continue
		}

		toUpdate = append(toUpdate, mailmodel.ContactImportUpdate{Email: e.Email, Name: e.Name, Tags: e.Tags})
		if needTagDiff && len(e.Tags) > 0 && existing[key] != 0 {
			pendingTags = append(pendingTags, pendingTagDiff{email: key, contactID: existing[key], tags: e.Tags})
		}
	}

	if len(pendingTags) > 0 {
		ids := make([]uint64, 0, len(pendingTags))
		for _, p := range pendingTags {
			ids = append(ids, p.contactID)
		}
		oldTags, terr := s.m.TagsByContactIDs(ctx, ids)
		if terr != nil {

			logger.Scene("mail").With("contacts", len(ids)).
				Warn("批量读取联系人旧标签失败，本次导入不触发 tag_added（导入本身不受影响）")
		} else {
			for _, p := range pendingTags {
				if added := diffTags(oldTags[p.contactID], p.tags); len(added) > 0 {
					tagAdds[p.email] = added
				}
			}
		}
	}

	if len(toUpdate) > 0 {
		if err = s.updateExistingContacts(ctx, toUpdate, existing, now, res); err != nil {
			return nil, err
		}
	}

	if err = s.m.BatchInsertContacts(ctx, toInsert, importBatchSize); err != nil {
		return nil, err
	}
	res.Imported = len(toInsert)

	failed := failedImportEmails(res)
	newIDs := make(map[string]uint64, len(toInsert))
	createdIDs := make([]uint64, 0, len(toInsert))
	for _, e := range toInsert {
		if e.ID == 0 {
			continue
		}
		newIDs[strings.ToLower(e.Email)] = e.ID
		createdIDs = append(createdIDs, e.ID)
	}
	switch {
	case len(createdIDs) == 1:
		s.OnContactCreated(ctx, createdIDs[0])
	case len(createdIDs) > 1:
		s.OnContactsCreated(ctx, createdIDs)
	}

	tagContacts := make(map[string][]uint64, len(tagAdds))
	for _, email := range sortedKeys(tagAdds) {
		if failed[email] {
			continue
		}
		id := existing[email]
		if id == 0 {
			id = newIDs[email]
		}
		if id == 0 {
			continue
		}
		for _, tag := range tagAdds[email] {
			tagContacts[tag] = append(tagContacts[tag], id)
		}
	}
	for _, tag := range sortedKeys(tagContacts) {
		ids := tagContacts[tag]
		if len(ids) == 1 {
			s.OnTagsAdded(ctx, ids[0], []string{tag})
			continue
		}
		s.OnTagsAddedBatch(ctx, tag, ids)
	}
	return res, nil
}

type pendingTagDiff struct {
	email     string
	contactID uint64
	tags      []string
}

func diffTags(old []string, incoming []string) []string {
	if len(incoming) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(old))
	for _, t := range old {
		seen[strings.TrimSpace(t)] = struct{}{}
	}

	var out []string
	for _, t := range incoming {
		v := strings.TrimSpace(t)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func failedImportEmails(res *maildto.ImportContactsResp) map[string]bool {
	out := make(map[string]bool, len(res.Errors))
	for _, e := range res.Errors {
		if email := strings.ToLower(strings.TrimSpace(e.Email)); email != "" {
			out[email] = true
		}
	}
	return out
}

func sortedKeys[T any](m map[string][]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (s *Service) updateExistingContacts(ctx context.Context, rows []mailmodel.ContactImportUpdate, existing map[string]uint64, now time.Time, res *maildto.ImportContactsResp) (err error) {
	hit, uerr := s.m.BatchUpdateContactImportFields(ctx, rows, mailmodel.ContactSourceImport, now)
	if uerr != nil {

		return s.updateExistingContactsOneByOne(ctx, rows, existing, now, res)
	}
	written := make(map[string]struct{}, len(hit))
	for _, e := range hit {
		written[e] = struct{}{}
	}
	for _, r := range rows {
		if _, ok := written[strings.ToLower(r.Email)]; ok {
			res.Updated++
			continue
		}
		res.Errors = append(res.Errors, maildto.ImportRowError{Email: r.Email, Reason: "更新失败: 联系人已不存在"})
	}
	return nil
}

func (s *Service) updateExistingContactsOneByOne(ctx context.Context, rows []mailmodel.ContactImportUpdate, existing map[string]uint64, now time.Time, res *maildto.ImportContactsResp) (err error) {
	for _, r := range rows {
		id, ok := existing[strings.ToLower(r.Email)]
		if !ok {
			res.Errors = append(res.Errors, maildto.ImportRowError{Email: r.Email, Reason: "更新失败: 联系人已不存在"})
			continue
		}
		fields := map[string]any{"source": mailmodel.ContactSourceImport, "update_time": now}
		if r.Name != nil {
			fields["name"] = *r.Name
		}
		if len(r.Tags) > 0 {
			fields["tags"] = mailmodel.StringArray(r.Tags)
		}
		if uerr := s.m.UpdateContactFields(ctx, id, fields); uerr != nil {
			res.Errors = append(res.Errors, maildto.ImportRowError{Email: r.Email, Reason: "更新失败: " + uerr.Error()})
			continue
		}
		res.Updated++
	}
	return nil
}

func parseContactRows(content []byte, res *maildto.ImportContactsResp) []contactRow {
	text := strings.TrimPrefix(string(content), "\ufeff")
	firstLine := firstNonEmptyLine(text)
	if strings.Contains(firstLine, ",") {
		return parseCSVRows(text, res)
	}
	return parsePlainRows(text, res)
}

func parsePlainRows(text string, res *maildto.ImportContactsResp) []contactRow {
	rows := make([]contactRow, 0, 64)
	for i, line := range strings.Split(text, "\n") {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		addr, ok := normalizeEmail(s)
		if !ok {
			res.Errors = append(res.Errors, maildto.ImportRowError{Line: i + 1, Email: s, Reason: mailenums.ErrEmailInvalid})
			continue
		}
		rows = append(rows, contactRow{Line: i + 1, Email: addr})
	}
	return rows
}

func parseCSVRows(text string, res *maildto.ImportContactsResp) []contactRow {
	reader := csv.NewReader(strings.NewReader(text))
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true
	records, err := reader.ReadAll()
	if err != nil {
		res.Errors = append(res.Errors, maildto.ImportRowError{Line: 0, Reason: "CSV 解析失败: " + err.Error()})
		return nil
	}
	if len(records) == 0 {
		return nil
	}

	emailCol, nameCol, tagsCol := 0, -1, -1
	header := records[0]
	hasHeader := false
	for idx, cell := range header {
		switch strings.ToLower(strings.TrimSpace(cell)) {
		case "email", "邮箱", "mail":
			emailCol, hasHeader = idx, true
		case "name", "姓名", "nickname", "昵称":
			nameCol, hasHeader = idx, true
		case "tags", "标签":
			tagsCol, hasHeader = idx, true
		}
	}
	body := records
	if hasHeader {
		body = records[1:]
	}

	rows := make([]contactRow, 0, len(body))
	for i, rec := range body {
		lineNo := i + 1
		if hasHeader {
			lineNo++
		}
		if len(rec) <= emailCol {
			continue
		}
		addr, ok := normalizeEmail(rec[emailCol])
		if !ok {
			res.Errors = append(res.Errors, maildto.ImportRowError{Line: lineNo, Email: rec[emailCol], Reason: mailenums.ErrEmailInvalid})
			continue
		}
		row := contactRow{Line: lineNo, Email: addr}
		if nameCol >= 0 && len(rec) > nameCol {
			row.Name = strings.TrimSpace(rec[nameCol])
		}
		if tagsCol >= 0 && len(rec) > tagsCol {
			row.Tags = splitTags(rec[tagsCol])
		}
		rows = append(rows, row)
	}
	return rows
}

func normalizeEmail(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", false
	}

	if addr, err := mail.ParseAddress(s); err == nil {
		s = addr.Address
	}
	at := strings.LastIndex(s, "@")
	if at <= 0 || at == len(s)-1 {
		return "", false
	}
	if strings.ContainsAny(s, " \t") {
		return "", false
	}
	return s[:at] + "@" + strings.ToLower(s[at+1:]), true
}

func splitTags(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ';' || r == '|' || r == ',' })
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func mergeTags(rowTags, defaultTags []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(rowTags)+len(defaultTags))
	for _, group := range [][]string{rowTags, defaultTags} {
		for _, t := range group {
			v := strings.TrimSpace(t)
			if v == "" {
				continue
			}
			if _, ok := seen[v]; ok {
				continue
			}
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	if out == nil {
		return []string{}
	}
	return out
}

func firstNonEmptyLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			return s
		}
	}
	return ""
}

var _ = bytes.NewReader

func (s *Service) CreateContact(ctx context.Context, req *maildto.SaveContactReq) (id uint64, err error) {
	if req == nil {
		return 0, mailenums.Err(mailenums.ErrInvalidParam)
	}
	email, err := normalizeContactEmail(req.Email)
	if err != nil {
		return 0, err
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = mailmodel.ContactStatusPending
	} else if !contactStatusValid(status) {
		return 0, mailenums.Err(mailenums.ErrInvalidParam)
	}
	if _, gerr := s.m.GetContactByEmail(ctx, email); gerr == nil {
		return 0, mailenums.Err(mailenums.ErrContactEmailExists)
	} else if !errors.Is(gerr, gorm.ErrRecordNotFound) {
		return 0, gerr
	}
	tags := normalizeTagList(req.Tags)
	e := &mailmodel.MailContactEntity{
		Email:  email,
		Source: contactSourceOr(req.Source),
		Status: status,
		Tags:   mailmodel.StringArray(tags),
	}
	if name := strings.TrimSpace(req.Name); name != "" {
		e.Name = strPtr(name)
	}
	if src := strings.TrimSpace(req.ConsentSource); src != "" {
		e.ConsentSource = strPtr(src)
	}

	now := time.Now()
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		sup, serr := s.m.FindSuppressionByEmailTx(ctx, tx, email)
		if serr == nil {
			status = contactStatusForSuppression(sup.Reason)
		} else if !errors.Is(serr, gorm.ErrRecordNotFound) {
			return serr
		}
		e.Status = status

		if status == mailmodel.ContactStatusSubscribed && strings.TrimSpace(req.ConsentSource) == "" {
			return mailenums.Err(mailenums.ErrConsentSourceRequired)
		}
		if cerr := s.m.CreateContact(ctx, e); cerr != nil {
			if isContactEmailConflict(cerr) {
				return mailenums.Err(mailenums.ErrContactEmailExists)
			}
			return cerr
		}
		id = e.ID
		return s.applyContactStatusTx(ctx, tx, e.ID, email, status, req.ConsentSource, now)
	}); err != nil {
		return 0, err
	}

	s.fireContactSubscribedIfChanged(ctx, id, "", status)
	s.fireContactTagsAdded(ctx, id, nil, tags)
	return id, nil
}

func (s *Service) UpdateContact(ctx context.Context, req *maildto.SaveContactReq) (err error) {
	if req == nil || req.ID == 0 {
		return mailenums.Err(mailenums.ErrInvalidParam)
	}
	email, err := normalizeContactEmail(req.Email)
	if err != nil {
		return err
	}
	status := strings.TrimSpace(req.Status)
	if status != "" && !contactStatusValid(status) {
		return mailenums.Err(mailenums.ErrInvalidParam)
	}
	row, err := s.m.GetContact(ctx, req.ID)
	if err != nil {
		return mailenums.Err(mailenums.ErrContactNotFound)
	}
	emailChanged := !strings.EqualFold(strings.TrimSpace(row.Email), email)
	if emailChanged {
		other, gerr := s.m.GetContactByEmail(ctx, email)
		switch {
		case gerr == nil && other.ID != req.ID:
			return mailenums.Err(mailenums.ErrContactEmailExists)
		case gerr != nil && !errors.Is(gerr, gorm.ErrRecordNotFound):
			return gerr
		}
	}
	tags := normalizeTagList(req.Tags)
	now := time.Now()
	fields := map[string]any{
		"email": email,

		"name":        strPtr(strings.TrimSpace(req.Name)),
		"tags":        mailmodel.StringArray(tags),
		"update_time": now,
	}

	if src := strings.TrimSpace(req.Source); src != "" {
		fields["source"] = src
	}
	if cs := strings.TrimSpace(req.ConsentSource); cs != "" {
		fields["consent_source"] = cs
	}

	nextStatus := status
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if emailChanged {
			sup, serr := s.m.FindSuppressionByEmailTx(ctx, tx, email)
			switch {
			case serr == nil:
				nextStatus = contactStatusForSuppression(sup.Reason)
			case errors.Is(serr, gorm.ErrRecordNotFound):
				if nextStatus == "" {
					nextStatus = row.Status
				}
			default:
				return serr
			}
		}

		finalStatus := nextStatus
		if finalStatus == "" {
			finalStatus = row.Status
		}
		finalConsent := strings.TrimSpace(req.ConsentSource)
		if finalConsent == "" && row.ConsentSource != nil {
			finalConsent = strings.TrimSpace(*row.ConsentSource)
		}
		if finalStatus == mailmodel.ContactStatusSubscribed && finalConsent == "" {
			return mailenums.Err(mailenums.ErrConsentSourceRequired)
		}
		if uerr := s.m.UpdateContactFieldsTx(ctx, tx, req.ID, fields); uerr != nil {
			if isContactEmailConflict(uerr) {
				return mailenums.Err(mailenums.ErrContactEmailExists)
			}
			return uerr
		}
		if nextStatus != "" && nextStatus != row.Status {
			return s.applyContactStatusTx(ctx, tx, req.ID, email, nextStatus, req.ConsentSource, now)
		}
		return nil
	}); err != nil {
		return err
	}

	if nextStatus == "" {
		nextStatus = row.Status
	}
	s.fireContactSubscribedIfChanged(ctx, req.ID, row.Status, nextStatus)
	s.fireContactTagsAdded(ctx, req.ID, row.Tags, tags)
	return nil
}

func (s *Service) DeleteContacts(ctx context.Context, req *maildto.DeleteContactsReq) (deleted int64, err error) {
	if req == nil || len(req.IDs) == 0 {
		return 0, mailenums.Err(mailenums.ErrInvalidParam)
	}

	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		var derr error
		deleted, derr = s.m.DeleteContactsByIDsTx(ctx, tx, req.IDs)
		return derr
	}); err != nil {
		return 0, err
	}
	return deleted, nil
}

func normalizeContactEmail(raw string) (string, error) {
	email, ok := normalizeEmail(raw)
	if ok {
		return email, nil
	}
	if strings.TrimSpace(raw) == "" {
		return "", mailenums.Err(mailenums.ErrEmailRequired)
	}
	return "", mailenums.Err(mailenums.ErrEmailInvalid)
}

func contactSourceOr(raw string) string {
	if v := strings.TrimSpace(raw); v != "" {
		return v
	}
	return mailmodel.ContactSourceManual
}

func (s *Service) fireContactTagsAdded(ctx context.Context, contactID uint64, before, after []string) {
	for _, tag := range diffTags(before, after) {
		s.OnTagsAddedBatch(ctx, tag, []uint64{contactID})
	}
}

func (s *Service) TagContacts(ctx context.Context, req *maildto.TagContactsReq) (changed, skipped int, err error) {
	if req == nil || len(req.IDs) == 0 {
		return 0, 0, mailenums.Err(mailenums.ErrInvalidParam)
	}
	add, remove := normalizeTagList(req.Add), normalizeTagList(req.Remove)
	if len(add) == 0 && len(remove) == 0 {
		return 0, 0, mailenums.Err(mailenums.ErrContactTagEmpty)
	}
	for _, id := range dedupIDs(req.IDs) {
		var addedTags []string
		werr := s.m.Transaction(ctx, func(tx *gorm.DB) error {
			cur, lerr := s.m.LockContactTagsTx(ctx, tx, id)
			if lerr != nil {
				return lerr
			}
			next, added := applyTagDelta(cur, add, remove)
			addedTags = added
			if tagListEqual(cur, next) {

				return nil
			}
			return s.m.UpdateContactFieldsTx(ctx, tx, id, map[string]any{
				"tags": mailmodel.StringArray(next), "update_time": time.Now(),
			})
		})
		if werr != nil {
			skipped++
			continue
		}
		changed++

		for _, tag := range addedTags {
			s.OnTagsAddedBatch(ctx, tag, []uint64{id})
		}
	}
	return changed, skipped, nil
}

func (s *Service) ListContactTags(ctx context.Context) (tags []string, err error) {
	return s.m.ListContactTags(ctx)
}

func applyTagDelta(cur, add, remove []string) (next []string, added []string) {
	drop := make(map[string]struct{}, len(remove))
	for _, t := range remove {
		if v := strings.TrimSpace(t); v != "" {
			drop[v] = struct{}{}
		}
	}
	seen := make(map[string]struct{}, len(cur)+len(add))
	next = make([]string, 0, len(cur)+len(add))
	for _, t := range cur {
		v := strings.TrimSpace(t)
		if v == "" {
			continue
		}
		if _, ok := drop[v]; ok {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		next = append(next, v)
	}
	for _, t := range add {
		v := strings.TrimSpace(t)
		if v == "" {
			continue
		}
		if _, ok := drop[v]; ok {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		next = append(next, v)
		added = append(added, v)
	}
	return next, added
}

func normalizeTagList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func tagListEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func dedupIDs(ids []uint64) []uint64 {
	out := make([]uint64, 0, len(ids))
	seen := make(map[uint64]struct{}, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
