package mailservice

// mail_contact_import.go — 联系人导入（issue #37）。
//
// 导入不是「上传 CSV 就完事」，要做五件事，缺一件都会在真实使用中出问题：
//
//	1. 逐行校验并**逐行报错**（不让一行坏数据毁掉整批）；
//	2. 批内去重（同一批里重复的邮箱只留一次，否则批量 upsert 会自相冲突）；
//	3. **查抑制名单**（退订 / 硬退信的地址直接不进 —— 导进来也发不出去）；
//	4. 同意状态按操作者的声明落库（没声明就是 pending，不可发营销）；
//	5. 批量 upsert（一次语句几百行，不是逐行 insert）。

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"net/mail"
	"strings"
	"time"

	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	mailmodel "go_wp/internal/module/mail/model"
)

// maxImportBytes 单次导入内容上限（防止把内存吃满）。
const maxImportBytes = 8 << 20 // 8 MiB

// importBatchSize 批量写入的批大小（一次语句多少行）。
const importBatchSize = 500

// contactRow 解析出来的一行。
type contactRow struct {
	Line  int
	Email string
	Name  string
	Tags  []string
}

// ImportContacts 导入联系人。
func (s *Service) ImportContacts(ctx context.Context, req *maildto.ImportContactsReq) (res *maildto.ImportContactsResp, err error) {
	if req == nil || len(req.Content) == 0 {
		return nil, errors.New(mailenums.ErrImportEmpty)
	}
	if len(req.Content) > maxImportBytes {
		return nil, errors.New(mailenums.ErrImportTooLarge)
	}

	res = &maildto.ImportContactsResp{Errors: []maildto.ImportRowError{}}
	rows := parseContactRows(req.Content, res)
	res.Total = len(rows)
	if len(rows) == 0 {
		return res, nil
	}

	// 批内去重：同一邮箱只保留第一次出现（后面的算重复）。
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

	// 查抑制名单：**一次 SQL 判一批**（并发/大批量场景下这是关键，不能每人查一次）。
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
			// 抑制名单里的地址不进联系人表（进了也发不出去，还会误导运营）。
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
			// 操作者声明已同意：直接 subscribed 并留痕（谁声明、什么时候）。
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

	// 先批量查已存在的联系人（一条 SQL），再分两批写。
	//
	// 不用 ON CONFLICT：唯一索引是**表达式索引** lower(email)，ON CONFLICT (email) 匹配不到它，
	// PG 直接报 42P10（实测）。先查后写还有个好副作用：新增 / 更新的计数是**准确值**，不是估算。
	existing, err := s.m.ExistingContactIDs(ctx, emails)
	if err != nil {
		return nil, err
	}

	toInsert := make([]*mailmodel.MailContactEntity, 0, len(list))
	for _, e := range list {
		id, exists := existing[strings.ToLower(e.Email)]
		if !exists {
			toInsert = append(toInsert, e)
			continue
		}
		if !req.UpdateExisting {
			res.Skipped++
			continue
		}
		// 已存在且要求更新：逐条更新，且**只动非同意字段** ——
		// 同意状态（status / subscribed_at / consent_source）不能被一次导入悄悄改写，
		// 否则「已退订的人」会被导入变回订阅，等于自己造投诉。
		//
		// 逐条而不批量：tags 因人而异，一条 SQL 覆盖不了不同人的标签。
		fields := map[string]any{"source": e.Source, "update_time": now}
		if e.Name != nil {
			fields["name"] = *e.Name
		}
		if len(e.Tags) > 0 {
			fields["tags"] = e.Tags
		}
		if uerr := s.m.UpdateContactFields(ctx, id, fields); uerr != nil {
			res.Errors = append(res.Errors, maildto.ImportRowError{Email: e.Email, Reason: "更新失败: " + uerr.Error()})
			continue
		}
		res.Updated++
	}

	if err = s.m.BatchInsertContacts(ctx, toInsert, importBatchSize); err != nil {
		return nil, err
	}
	res.Imported = len(toInsert)
	return res, nil
}

// parseContactRows 解析导入内容（CSV 与纯文本自动识别）。
func parseContactRows(content []byte, res *maildto.ImportContactsResp) []contactRow {
	text := strings.TrimPrefix(string(content), "\ufeff") // 去掉 Excel 导出的 BOM
	firstLine := firstNonEmptyLine(text)
	if strings.Contains(firstLine, ",") {
		return parseCSVRows(text, res)
	}
	return parsePlainRows(text, res)
}

// parsePlainRows 纯文本：每行一个地址，忽略空行与 # 注释。
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

// parseCSVRows CSV：识别 email / name / tags 表头；无表头时第一列当邮箱。
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

	// 表头映射
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

// normalizeEmail 校验并归一化邮箱（用户名部分保留大小写，域名小写）。
//
// 用 net/mail 解析而不是自己写正则：邮箱的合法形态比正则能表达的多
// （带引号的本地部分、IPv6 字面量域名等），标准库已经处理过这些边界。
func normalizeEmail(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", false
	}
	// 允许 "张三 <a@b.com>" 这种写法：取尖括号里的地址。
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

// mergeTags 合并行内标签与默认标签（去重，保持稳定顺序）。
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

// bytesReader 保留给未来从 io.Reader 流式导入（大文件）。
var _ = bytes.NewReader
