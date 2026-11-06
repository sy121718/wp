package mailservice

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"net/mail"
	"slices"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/internal/module/mail/contract"
	"go_wp/internal/module/mail/dto"
	"go_wp/internal/module/mail/enums"
	mailmodel "go_wp/internal/module/mail/model"
	"go_wp/pkg/facing"
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

// UpdateContactStatus 与批量改状态共用同一条规则：事务内按 id 加锁、缺行 404、写库错误原样上抛。
func (s *Service) UpdateContactStatus(ctx context.Context, req *maildto.UpdateContactStatusReq) (err error) {
	if req == nil || req.ID == 0 || !contactStatusValid(req.Status) {
		return mailenums.Err(mailenums.ErrInvalidParam)
	}
	was := ""
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		rows, lerr := s.m.ContactsByIDsTx(ctx, tx, []uint64{req.ID})
		if lerr != nil {
			return lerr
		}
		if len(rows) != 1 {
			return rejectBatch(1, blockedIDs([]uint64{req.ID}, mailenums.ErrContactNotFound))
		}
		was = rows[0].Status
		return s.applyContactStatusTx(ctx, tx, req.ID, rows[0].Email, req.Status, req.Note, time.Now())
	})
	if err != nil {
		return err
	}
	s.fireContactSubscribedIfChanged(ctx, req.ID, was, req.Status)
	return nil
}

// UpdateContactsStatus 整批改联系人状态：同一事务内按 id 升序锁定、逐条校验，
// 任一条不存在就整批回滚；订阅类自动化在提交后才触发。
func (s *Service) UpdateContactsStatus(ctx context.Context, req *maildto.UpdateContactsStatusReq) (err error) {
	if req == nil || len(req.IDs) == 0 || !contactStatusValid(req.Status) {
		return mailenums.Err(mailenums.ErrInvalidParam)
	}
	ids, ok := dedupIDs(req.IDs)
	if !ok || len(ids) == 0 {
		return mailenums.Err(mailenums.ErrInvalidParam)
	}
	now := time.Now()
	subscribed := make([]uint64, 0, len(ids))

	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		rows, lerr := s.m.ContactsByIDsTx(ctx, tx, ids)
		if lerr != nil {
			return lerr
		}
		byID := make(map[uint64]*mailmodel.MailContactEntity, len(rows))
		for _, row := range rows {
			byID[row.ID] = row
		}
		missing := make([]uint64, 0, len(ids)-len(rows))
		for _, id := range ids {
			if _, ok := byID[id]; !ok {
				missing = append(missing, id)
			}
		}
		if len(missing) > 0 {
			return rejectBatch(len(ids), blockedIDs(missing, mailenums.ErrContactNotFound))
		}
		for _, id := range ids {
			row := byID[id]
			if aerr := s.applyContactStatusTx(ctx, tx, id, row.Email, req.Status, req.Note, now); aerr != nil {
				return aerr
			}
			if req.Status == mailmodel.ContactStatusSubscribed && row.Status != mailmodel.ContactStatusSubscribed {
				subscribed = append(subscribed, id)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, id := range subscribed {
		s.fireContactSubscribedIfChanged(ctx, id, "", req.Status)
	}
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

// ImportContacts 是一次批量写：解析出的任一行非法、或持久化中途任何一步失败，
// 都整批回滚（零提交）。多批 SQL 共用同一个事务句柄；触发在提交后统一发出。
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
	if len(res.Errors) > 0 {
		return nil, importRowBlocked(res.Errors)
	}
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
	needTagDiff := hasTriggerType(s.activeAutomations(ctx), mailmodel.TriggerTagAdded)
	now := time.Now()

	var (
		createdIDs []uint64
		tagAdds    map[string][]string
		newIDs     map[string]uint64
		existing   map[string]uint64
		toInsert   []*mailmodel.MailContactEntity
	)
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		blocked, berr := s.m.SuppressedEmailsTx(ctx, tx, emails)
		if berr != nil {
			return berr
		}

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
			return nil
		}

		var xerr error
		// 先按邮箱锁定已存在行，再读旧标签、再更新：差集与更新基于同一份锁内快照。
		existing, xerr = s.m.ExistingContactIDsForUpdateTx(ctx, tx, emails)
		if xerr != nil {
			return xerr
		}

		toInsert = make([]*mailmodel.MailContactEntity, 0, len(list))
		toUpdate := make([]mailmodel.ContactImportUpdate, 0, len(list))
		tagAdds = make(map[string][]string, len(list))
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
			oldTags, terr := s.m.TagsByContactIDsTx(ctx, tx, ids)
			if terr != nil {
				return terr
			}
			for _, p := range pendingTags {
				if added := diffTags(oldTags[p.contactID], p.tags); len(added) > 0 {
					tagAdds[p.email] = added
				}
			}
		}

		if len(toUpdate) > 0 {
			hit, uerr := s.m.BatchUpdateContactImportFieldsTx(ctx, tx, toUpdate, mailmodel.ContactSourceImport, now)
			if uerr != nil {
				return uerr
			}
			written := make(map[string]struct{}, len(hit))
			for _, email := range hit {
				written[email] = struct{}{}
			}
			missing := make([]maildto.ImportRowError, 0)
			for _, row := range toUpdate {
				if _, ok := written[strings.ToLower(row.Email)]; ok {
					res.Updated++
					continue
				}
				missing = append(missing, maildto.ImportRowError{Email: row.Email, Reason: mailenums.ErrContactNotFound})
			}
			if len(missing) > 0 {
				return importRowBlocked(missing)
			}
		}

		if xerr = s.m.BatchInsertContactsTx(ctx, tx, toInsert, importBatchSize); xerr != nil {
			return xerr
		}
		res.Imported = len(toInsert)

		newIDs = make(map[string]uint64, len(toInsert))
		createdIDs = make([]uint64, 0, len(toInsert))
		for _, e := range toInsert {
			if e.ID == 0 {
				continue
			}
			newIDs[strings.ToLower(e.Email)] = e.ID
			createdIDs = append(createdIDs, e.ID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// 提交成功后才触发：回滚的导入不会留下自动化实例或 tag_added 副作用。
	switch {
	case len(createdIDs) == 1:
		s.OnContactCreated(ctx, createdIDs[0])
	case len(createdIDs) > 1:
		s.OnContactsCreated(ctx, createdIDs)
	}
	tagContacts := make(map[string][]uint64, len(tagAdds))
	for _, email := range sortedKeys(tagAdds) {
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

// importRowBlocked 把逐行解析/校验失败交给共享拒绝出口，整批零提交。
func importRowBlocked(rows []maildto.ImportRowError) error {
	items := make([]facing.BlockedItem, 0, len(rows))
	for _, row := range rows {
		id := ""
		if row.Line > 0 {
			id = strconv.Itoa(row.Line)
		}
		items = append(items, blockedRow(id, row.Email, row.Reason))
	}
	return rejectBatch(len(items), items)
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
		res.Errors = append(res.Errors, maildto.ImportRowError{Line: 0, Reason: mailenums.ErrImportCsvBad})
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

// DeleteContacts 是整批原子删除：先按 id 升序锁定取回，任一条不存在就整批拒绝，
// 逐条原因放进 data.items；数据库错误不改写成 notfound。
func (s *Service) DeleteContacts(ctx context.Context, req *maildto.DeleteContactsReq) (deleted int64, err error) {
	if req == nil || len(req.IDs) == 0 {
		return 0, mailenums.Err(mailenums.ErrInvalidParam)
	}
	ids, ok := dedupIDs(req.IDs)
	if !ok || len(ids) == 0 {
		return 0, mailenums.Err(mailenums.ErrInvalidParam)
	}

	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		rows, lerr := s.m.ContactsByIDsTx(ctx, tx, ids)
		if lerr != nil {
			return lerr
		}
		found := make(map[uint64]struct{}, len(rows))
		for _, row := range rows {
			found[row.ID] = struct{}{}
		}
		missing := make([]uint64, 0, len(ids)-len(rows))
		for _, id := range ids {
			if _, ok := found[id]; !ok {
				missing = append(missing, id)
			}
		}
		if len(missing) > 0 {
			return rejectBatch(len(ids), blockedIDs(missing, mailenums.ErrContactNotFound))
		}
		var derr error
		deleted, derr = s.m.DeleteContactsByIDsTx(ctx, tx, ids)
		if derr != nil {
			return derr
		}
		if deleted != int64(len(ids)) {
			return rejectBatch(len(ids), blockedIDs(ids, mailenums.ErrContactNotFound))
		}
		return nil
	})
	if err != nil {
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

// TagContacts 是整批原子打标签：同一事务内按 id 升序加锁、逐条校验；
// 任一条不存在就整批回滚（同值 no-op 仍视为成功），触发在提交后统一发出。
func (s *Service) TagContacts(ctx context.Context, req *maildto.TagContactsReq) (changed int, err error) {
	if req == nil || len(req.IDs) == 0 {
		return 0, mailenums.Err(mailenums.ErrInvalidParam)
	}
	add, remove := normalizeTagList(req.Add), normalizeTagList(req.Remove)
	if len(add) == 0 && len(remove) == 0 {
		return 0, mailenums.Err(mailenums.ErrContactTagEmpty)
	}
	ids, ok := dedupIDs(req.IDs)
	if !ok || len(ids) == 0 {
		return 0, mailenums.Err(mailenums.ErrInvalidParam)
	}

	type tagChange struct {
		id    uint64
		added []string
	}
	changes := make([]tagChange, 0, len(ids))
	now := time.Now()

	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		rows, lerr := s.m.ContactsByIDsTx(ctx, tx, ids)
		if lerr != nil {
			return lerr
		}
		current := make(map[uint64]*mailmodel.MailContactEntity, len(rows))
		for _, row := range rows {
			current[row.ID] = row
		}
		missing := make([]uint64, 0, len(ids)-len(rows))
		for _, id := range ids {
			if _, ok := current[id]; !ok {
				missing = append(missing, id)
			}
		}
		if len(missing) > 0 {
			return rejectBatch(len(ids), blockedIDs(missing, mailenums.ErrContactNotFound))
		}
		for _, id := range ids {
			// 行已在同一事务里按 id 升序加锁，直接用锁内读到的标签，避免二次查询。
			cur := current[id].Tags
			next, added := applyTagDelta(cur, add, remove)
			if tagListEqual(cur, next) {
				// 合法同值 no-op：不改行，也不计入 changed。
				continue
			}
			if uerr := s.m.UpdateContactFieldsTx(ctx, tx, id, map[string]any{
				"tags": mailmodel.StringArray(next), "update_time": now,
			}); uerr != nil {
				return uerr
			}
			changed++
			changes = append(changes, tagChange{id: id, added: added})
		}
		return nil
	})
	if err != nil {
		return 0, err
	}

	// 提交后才发触发：回滚的事务不会再留下半截自动化实例。
	for _, change := range changes {
		for _, tag := range change.added {
			s.OnTagsAddedBatch(ctx, tag, []uint64{change.id})
		}
	}
	return changed, nil
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

// dedupIDs 去重并按升序返回；任一 id 为 0 时整批参数非法。
// 不能静默过滤 0：那会把「含非法项的批量」变成部分成功。
func dedupIDs(ids []uint64) (out []uint64, ok bool) {
	out = make([]uint64, 0, len(ids))
	seen := make(map[uint64]struct{}, len(ids))
	for _, id := range ids {
		if id == 0 {
			return nil, false
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	slices.Sort(out)
	return out, true
}
