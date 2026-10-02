package mailservice

// mail_contact_import.go — 联系人导入（issue #37）。
//
// 导入不是「上传 CSV 就完事」，要做五件事，缺一件都会在真实使用中出问题：
//
//	1. 逐行校验并**逐行报错**（不让一行坏数据毁掉整批）；
//	2. 批内去重（同一批里重复的邮箱只留一次，否则批量 upsert 会自相冲突）；
//	3. **查抑制名单**（退订 / 硬退信的地址直接不进 —— 导进来也发不出去）；
//	4. 同意状态按操作者的声明落库（没声明就是 pending，不可发营销）；
//	5. 集合式写库：新增走 CreateInBatches（一次语句几百行），已存在的更新走单条语句批量写回，
//	   失败才回退逐条（见 updateExistingContacts）。

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"net/mail"
	"sort"
	"strings"
	"time"

	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	mailmodel "go_wp/internal/module/mail/model"
	"go_wp/pkg/logger"
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
	toUpdate := make([]mailmodel.ContactImportUpdate, 0, len(list))
	// 触发相关的粗判（**一次查询**）：有没有启用中的 tag_added 流程。
	// 只有存在时才值得为「标签差集」读每个联系人的旧标签（每行一次主键读）。
	// 触发本身不在这里做，由写库后的批量入口各自匹配一次流程。
	needTagDiff := hasTriggerType(s.activeAutomations(ctx), mailmodel.TriggerTagAdded)
	// tagAdds 记录「本次真正**新增**的标签」（email → 新增标签）：tag_added 触发只认新增部分。
	// 少了这层过滤，一次「标签一个都没变」的重新导入会把所有人重新推进 tag_added 流程。
	// 差集必须在写库**之前**算 —— 写完之后就查不到旧值了。
	tagAdds := make(map[string][]string, len(list))
	for _, e := range list {
		key := strings.ToLower(e.Email)
		if _, exists := existing[key]; !exists {
			toInsert = append(toInsert, e)
			// 新联系人的全部标签都是新增的（旧集合为空），无需读旧值。
			if len(e.Tags) > 0 {
				tagAdds[key] = []string(e.Tags)
			}
			continue
		}
		if !req.UpdateExisting {
			res.Skipped++
			continue
		}
		// 已存在且要求更新：收集成一批，稍后由单条语句写回；**只动非同意字段** ——
		// 同意状态（status / subscribed_at / consent_source）不能被一次导入悄悄改写，
		// 否则「已退订的人」会被导入变回订阅，等于自己造投诉。
		toUpdate = append(toUpdate, mailmodel.ContactImportUpdate{Email: e.Email, Name: e.Name, Tags: e.Tags})
		if needTagDiff {
			if added := s.addedTags(ctx, existing[key], e.Tags); len(added) > 0 {
				tagAdds[key] = added
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

	// ---- 事件触发（issue #38 P3 的入口侧）----
	//
	// 触发统一放在**写库之后**：事件描述的是已经发生的事，而 StartRun 内部要按
	// contact_id 反查联系人 —— 写之前那个 id 还不存在。顺序也决定了失败语义：
	// 导入是用户要的结果、触发是附加行为（事件入口自带 recover、不返回错误），
	// 所以下面这些调用不会让一次成功的导入变成失败。
	//
	// 为什么必须在这里接：这两个触发器此前**零调用方**（后台 UI 却提供了
	// 「新联系人产生」「被打上某个标签」两个选项），配了它们的流程永不启动，
	// 列表页看不出任何异常。
	//
	// 为什么是「按规模分派」而不是统统走批量 / 统统走单条：
	//   · 单条入口每人查一次流程表 —— 2000 行导入会发出 2000 条查询（护栏测试
	//     TestImportWriteStatementProfile 实测 2006 条语句，阈值 20）；
	//   · 单条源（注册 / 手工建号）仍走单条入口，语义与批量入口共用同一个
	//     startRunsWithAutomations，不存在两套启动规则。
	// 所以：1 条走单条入口，多条走批量入口；标签按标签分组（trigger_params 的 tag 条件按标签匹配）。
	failed := failedImportEmails(res)
	newIDs := make(map[string]uint64, len(toInsert))
	createdIDs := make([]uint64, 0, len(toInsert))
	for _, e := range toInsert {
		if e.ID == 0 {
			continue // 主键未回填：没有 id 就触发不了（StartRun 要求 contactID > 0）
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
			continue // 这一行的标签没写进去，不该按「被打上标签」触发
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

// addedTags 算出「本次新增的标签」：读旧标签后取差集。
//
// 为什么需要旧值：tag_added 触发只认**新增**（见 OnTagsAdded 的调用约定）；
// 少了这层过滤，一次「标签没变」的重新导入会把所有联系人重新推进 tag_added 流程，
// 表现是重复发信。
//
// 读法是主键单行读：模型层没有「按邮箱批量取标签」的方法，而这里只对
// 「本次带了标签、且命中已存在联系人」的行读 —— 导入是低频人工操作，
// 主键读是最便宜的可用手段。读失败一律当作「没有新增」：触发是附加行为，
// 不能让它把导入本身打失败（与 fireTrigger 同一取舍）。
func (s *Service) addedTags(ctx context.Context, contactID uint64, incoming []string) []string {
	if contactID == 0 || len(incoming) == 0 {
		return nil
	}
	row, err := s.m.GetContact(ctx, contactID)
	if err != nil {
		logger.Scene("mail").With("contact_id", contactID).
			Warn("读取联系人旧标签失败，本次导入不触发 tag_added（导入本身不受影响）")
		return nil
	}
	return diffTags(row.Tags, incoming)
}

// diffTags 返回 incoming 中**不在** old 里的标签（去空白、保序）。
//
// 抽成纯函数是为了能就近单测：这一段错了不会报错，只会让「重复导入同标签」
// 每次都重新触发一次 tag_added（表现是重复发信），而那种偏差在集成测试里很容易被
// 别的原因掩盖。标签比较是精确匹配（大小写敏感）——与按标签筛人群的口径一致。
func diffTags(old []string, incoming []string) []string {
	if len(incoming) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(old))
	for _, t := range old {
		seen[strings.TrimSpace(t)] = struct{}{}
	}
	// 没有新增时返回 nil（而不是空切片）：调用方一律按 len() 判断，
	// 而 nil 能让「这次没有新增标签」这件事在日志/调试时一眼可辨。
	var out []string
	for _, t := range incoming {
		v := strings.TrimSpace(t)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{} // 同一批里的重复标签只算一次
		out = append(out, v)
	}
	return out
}

// failedImportEmails 收集本次导入报错的行（email → true），供触发侧跳过。
//
// 逐行报告里的 email 已经归一化（用例见 parseContactRows / updateExistingContacts），
// 与 tagAdds 的键口径一致（都走 strings.ToLower）。
func failedImportEmails(res *maildto.ImportContactsResp) map[string]bool {
	out := make(map[string]bool, len(res.Errors))
	for _, e := range res.Errors {
		if email := strings.ToLower(strings.TrimSpace(e.Email)); email != "" {
			out[email] = true
		}
	}
	return out
}

// sortedKeys 按字典序返回 map 的键：触发顺序因此与 map 迭代顺序无关（可复现）。
// 泛型：同一份实现同时服务「email → 标签」与「标签 → 联系人 id」两张表。
func sortedKeys[T any](m map[string][]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// updateExistingContacts 写回导入命中的已存在联系人（审计 DB-006）。
//
// 快路径是**一条语句写完整批**（把 N 次往返压成 1 次）；批量失败时**回退逐条**，
// 因为对外承诺的语义是「逐行报错 + Updated 精确计数」，批量只是同一语义的快路径，
// 不能改变对外行为。批量为空的差集（前置查询判定存在、写回时已消失，即并发删除）
// 同样逐行报告，不让它静默少算。
func (s *Service) updateExistingContacts(ctx context.Context, rows []mailmodel.ContactImportUpdate, existing map[string]uint64, now time.Time, res *maildto.ImportContactsResp) (err error) {
	hit, uerr := s.m.BatchUpdateContactImportFields(ctx, rows, mailmodel.ContactSourceImport, now)
	if uerr != nil {
		// 集合语句整体失败（罕见的约束/连接问题）：退回逐条，保住逐行报告能力。
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

// updateExistingContactsOneByOne 逐条写回（批量路径失败时的回退）。
//
// 字段口径与批量路径逐项一致：name / tags 仅在非空时覆盖，source 与 update_time 恒写；
// tags 走 StringArray 的 driver.Valuer（裸 []string 会被 pgx 编成元组字面量报 22P02）。
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
