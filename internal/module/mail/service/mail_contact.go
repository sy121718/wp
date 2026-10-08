package mailservice

// 导入不是「上传 CSV 就完事」，要做五件事，缺一件都会在真实使用中出问题：
//
//	1. 逐行校验并**逐行报错**（不让一行坏数据毁掉整批）；
//	2. 批内去重（同一批里重复的邮箱只留一次，否则批量 upsert 会自相冲突）；
//	3. **查抑制名单**（退订 / 硬退信的地址直接不进 —— 导进来也发不出去）；
//	4. 同意状态按操作者的声明落库（没声明就是 pending，不可发营销）；
//	5. 集合式写库：新增走 CreateInBatches（一次语句几百行），已存在的更新走单条语句批量写回，
//	   失败才回退逐条（见 updateExistingContacts）。

// 为什么单独一个文件：这三件事与「列表 + 改状态」（mail_contact.go）、
// 「批量打标签」（mail_contact_tag.go）的入口不同、失败形态不同，但共享同一组边界：
// 邮箱归一化、抑制名单连带、状态与抑制名单同事务。边界写在下面的注释里，
// 复制到别处就会分叉。

// 标签是 text[] 列上的**精确匹配**值（筛选与投递目标都走 tags @> ...），
// 所以这里的每一处去空白 / 去重 / 大小写处理都必须与 splitTags / mergeTags / diffTags 同口径：
// 口径一旦放宽（比如把标签统一转小写），「按标签筛人群」就会开始漏人，而且不报错。

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
)

// 编译期断言：本模块服务满足对外契约。
var _ mailcontract.MailService = (*Service)(nil)

// ListContacts 联系人列表（分页）。
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
		Offset:  (page - 1) * size,
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

// UpdateContactStatus 后台手工改同意状态（订阅 / 退订）。
//
// 这是**人工留痕**操作，与自动回写（退信 / 投诉）走的路径不同，但落库字段一致：
// 改成 subscribed 会写 subscribed_at，改成 unsubscribed 同时进抑制名单 ——
// 后台点了退订却还能发出去，是比不点退订更糟的结果。
func (s *Service) UpdateContactStatus(ctx context.Context, req *maildto.UpdateContactStatusReq) (err error) {
	if req == nil || req.ID == 0 || !contactStatusValid(req.Status) {
		return errors.New(mailenums.ErrInvalidParam)
	}
	row, err := s.m.GetContact(ctx, req.ID)
	if err != nil {
		return errors.New(mailenums.ErrContactNotFound)
	}
	// 状态变更与抑制名单是同一件事的两面，**必须同事务**（AGENTS.md「写操作的事务与回滚」）：
	// 分开提交时第二步失败会留下「后台点了退订、地址却不在抑制名单里」——换一个活动照样会发出去，
	// 这比「没点退订」更糟（联系人以为已经退订了）。退订 / 投诉 / 硬退信三种终态都进名单。
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		return s.applyContactStatusTx(ctx, tx, req.ID, row.Email, req.Status, req.Note, time.Now())
	}); err != nil {
		return err
	}
	// 变为已订阅时才触发自动化（#38 P3）。判断「原来不是订阅」而不是「现在是订阅」——
	// 否则重复保存一次订阅状态就会给人再塞进一条欢迎流程。触发失败不影响状态变更。
	//
	// 触发**留在事务外**：它要建实例、可能入队发信（外部副作用），不属于本次状态写入的原子范围。
	s.fireContactSubscribedIfChanged(ctx, req.ID, row.Status, req.Status)
	return nil
}

// —— 同意状态的共享实现 ——
//
// 手工改状态（UpdateContactStatus）、编辑抽屉保存（UpdateContact）都要写「状态 + 抑制名单」，
// 但它们的入口、校验与回执完全不同。共用的部分抽在这里，**不是**为了让文件短一点：
// 两份实现的第一天就等价，第二次改（比如加一种终态）时必然分叉 ——
// 分叉的后果是某条路径点了退订却没进抑制名单，换一个活动照样发出去。

// contactStatusValid 手工可设的同意状态白名单（单条抽屉 / 批量 / 编辑抽屉三条入口共用一份）。
//
// bounced / complained 也在白名单里：单条抽屉允许人工把它们改回去（投递反馈是事实，
// 但事实可能已经过时 —— 换过邮件服务商之后原地址可能又能收了）。批量路径另有限制，
// 见 MailContactsBulkStatus。
func contactStatusValid(status string) bool {
	switch status {
	case mailmodel.ContactStatusSubscribed, mailmodel.ContactStatusPending,
		mailmodel.ContactStatusUnsubscribed, mailmodel.ContactStatusBounced,
		mailmodel.ContactStatusComplained:
		return true
	}
	return false
}

// contactStatusFields 状态变更要写的列（不含邮箱等身份列）。
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

// suppressionReasonForStatus 状态 → 抑制名单原因；不需要进名单的状态返回 false。
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

// contactStatusForSuppression 抑制名单原因 → 联系人应当呈现的状态。
//
// 用于「这个地址已经在抑制名单里」的那条分支（新建联系人或改邮箱时命中）：
// 列表上写着「已订阅」而发信时被名单拦下，运营会以为系统坏了 —— 状态必须与事实一致。
// manual（人工加进名单的）按退订处理：名单的语义就是「不要再发」。
func contactStatusForSuppression(reason string) string {
	switch reason {
	case mailmodel.SuppressionReasonComplaint:
		return mailmodel.ContactStatusComplained
	case mailmodel.SuppressionReasonHardBounce:
		return mailmodel.ContactStatusBounced
	}
	return mailmodel.ContactStatusUnsubscribed
}

// applyContactStatusTx 写状态字段 + 终态进抑制名单，**调用方事务内**。
//
// 抑制记录的 Note 只在退订时写：投诉 / 硬退信是投递反馈带来的终态，
// 那句「来源备注」是人工退订留痕用的（与既有实现逐字一致）。
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

// fireContactSubscribedIfChanged 从非订阅变为订阅时触发自动化，**在事务外调用**。
//
// 判据是「原来不是订阅」而不是「现在是订阅」——否则重复保存一次订阅状态
// 就会给人再塞进一条欢迎流程（与 UpdateContactStatus 的既有口径一致）。
func (s *Service) fireContactSubscribedIfChanged(ctx context.Context, contactID uint64, was, now string) {
	if now != mailmodel.ContactStatusSubscribed || was == mailmodel.ContactStatusSubscribed {
		return
	}
	s.OnContactSubscribed(ctx, contactID)
}

// isContactEmailConflict 判断是不是「邮箱唯一索引撞车」。
//
// 双判据（与 ai_provider_crud.go / product_crud.go 同款）：生产连接开了 gorm 的
// TranslateError 时拿到 gorm.ErrDuplicatedKey，未翻译的连接（部分测试 fixture）拿到的是
// PG 原始 23505 文本。两条都认，否则并发撞车时运营看到的是 SQLSTATE 原文。
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
	// 没有的话整块标签差集都不必算 —— 零配置站点不为它付任何代价。
	// 触发本身不在这里做，由写库后的批量入口各自匹配一次流程。
	needTagDiff := hasTriggerType(s.activeAutomations(ctx), mailmodel.TriggerTagAdded)
	// tagAdds 记录「本次真正**新增**的标签」（email → 新增标签）：tag_added 触发只认新增部分。
	// 少了这层过滤，一次「标签一个都没变」的重新导入会把所有人重新推进 tag_added 流程。
	// 差集必须在写库**之前**算 —— 写完之后就查不到旧值了。
	tagAdds := make(map[string][]string, len(list))
	// pendingTags 收集「已存在、要更新、带了标签」的行，稍后**一次**批量读旧标签。
	// 逐条读是 N+1（每个这样的联系人一次主键读）；这里天然有 id 集合可批。
	pendingTags := make([]pendingTagDiff, 0, len(list))
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
		if needTagDiff && len(e.Tags) > 0 && existing[key] != 0 {
			pendingTags = append(pendingTags, pendingTagDiff{email: key, contactID: existing[key], tags: e.Tags})
		}
	}

	// 一次批量读旧标签，再在内存里取差集（差集必须在写库之前算完）。
	if len(pendingTags) > 0 {
		ids := make([]uint64, 0, len(pendingTags))
		for _, p := range pendingTags {
			ids = append(ids, p.contactID)
		}
		oldTags, terr := s.m.TagsByContactIDs(ctx, ids)
		if terr != nil {
			// 读失败一律当作「没有新增」：触发是附加行为，不能让它把导入本身打失败
			// （与事件入口自带 recover 同一取舍）。
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

// pendingTagDiff 待算标签差集的一行（已存在、要更新、带了标签）。
//
// 收集成一批是为了**一次**读旧标签（mailmodel.TagsByContactIDs）：逐条读是 N+1，
// 而这里天然有 id 集合可批。差集口径（diffTags）不变，只是旧值的取法从逐条变批量。
type pendingTagDiff struct {
	email     string
	contactID uint64
	tags      []string
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

// CreateContact 新建联系人（ID = 0 的保存语义）。
//
// 三条边界在这里定死：
//
//	· 邮箱一律走 normalizeEmail —— 非法地址进了库也发不出去，还会让日后的导入报告里
//	  冒出「这个地址格式非法」而没人知道是谁塞进来的；
//	· 重复邮箱返回**可读错误**（ErrContactEmailExists）：唯一索引是表达式索引 lower(email)，
//	  撞车时 PG 抛的是 23505 原文（带索引名），既不能给运营看，也不能当 500。
//	  先查一次是为了给出可读文案，事务内再兜一次并发撞车（isContactEmailConflict）；
//	· 状态缺省 pending：没有同意证据的人不进可发名单（与导入路径同一口径）。
func (s *Service) CreateContact(ctx context.Context, req *maildto.SaveContactReq) (id uint64, err error) {
	if req == nil {
		return 0, errors.New(mailenums.ErrInvalidParam)
	}
	email, err := normalizeContactEmail(req.Email)
	if err != nil {
		return 0, err
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = mailmodel.ContactStatusPending
	} else if !contactStatusValid(status) {
		return 0, errors.New(mailenums.ErrInvalidParam)
	}
	if _, gerr := s.m.GetContactByEmail(ctx, email); gerr == nil {
		return 0, errors.New(mailenums.ErrContactEmailExists)
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

	// 新建 + 状态副作用（订阅态写 subscribed_at / 终态进抑制名单）是两处写，必须同事务。
	// 抑制名单的判断也在事务内：这个地址已经在名单里，却把状态写成「已订阅」，
	// 列表说的就是假话 —— 发信时照样被名单拦下（见 applyContactStatusTx）。
	now := time.Now()
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		sup, serr := s.m.FindSuppressionByEmailTx(ctx, tx, email)
		if serr == nil {
			status = contactStatusForSuppression(sup.Reason)
		} else if !errors.Is(serr, gorm.ErrRecordNotFound) {
			return serr
		}
		e.Status = status
		// 判据是**最终状态**（上面那步可能把 subscribed 降级成抑制终态），不是提交上来的状态：
		// 命中抑制名单的人不该因为「没写同意来源」被拒 —— 他本来就不进可发名单。
		// 这条底线与导入路径的 ConsentDeclared 是同一件事的两个入口：那边靠勾选强制，
		// 这边只能由服务端强制（页面文案承诺过，不执行就是文案在撒谎）。
		if status == mailmodel.ContactStatusSubscribed && strings.TrimSpace(req.ConsentSource) == "" {
			return errors.New(mailenums.ErrConsentSourceRequired)
		}
		if cerr := s.m.CreateContact(ctx, e); cerr != nil {
			if isContactEmailConflict(cerr) {
				return errors.New(mailenums.ErrContactEmailExists)
			}
			return cerr
		}
		id = e.ID
		return s.applyContactStatusTx(ctx, tx, e.ID, email, status, req.ConsentSource, now)
	}); err != nil {
		return 0, err
	}

	// 两个触发**都在事务外**：建实例、可能入队发信是外部副作用，不属于本次写入的原子范围。
	// 「新建即订阅」按「从非订阅变为订阅」处理（此前状态视为不存在）——否则手工加进来的
	// 订阅者永远拿不到欢迎流程，而同一批人在导入路径里是能拿到的。
	s.fireContactSubscribedIfChanged(ctx, id, "", status)
	s.fireContactTagsAdded(ctx, id, nil, tags)
	return id, nil
}

// UpdateContact 编辑联系人（ID > 0 的保存语义）。
//
// 邮箱是抑制名单的**关联键**（mail_suppressions 只有 email，没有 contact_id），
// 所以「改邮箱」这件事必须显式表态，本实现选的是：
//
//	· **不搬抑制记录** —— 把「这个地址说过不要再发」的记录跟着搬到新地址，等于让退订者
//	  换个地址继续收，正是反垃圾邮件规则要禁止的事（与「后台点了退订却还能被发出去」同一根）；
//	· 反过来，新地址若已经在抑制名单里，就把这个联系人落到名单原因对应的**终态** ——
//	  否则列表上写着「已订阅」，发信时照样被名单拦下，运营会以为系统坏了；
//	· 旧地址的抑制记录原样留在名单里：那是**地址**的历史事实，与哪一行联系人无关。
//
// 状态若不改（表单没提交或与原值相同）不写状态列；要改则复用 UpdateContactStatus 的
// 内部实现（applyContactStatusTx），状态字段与抑制名单永远同一事务。
func (s *Service) UpdateContact(ctx context.Context, req *maildto.SaveContactReq) (err error) {
	if req == nil || req.ID == 0 {
		return errors.New(mailenums.ErrInvalidParam)
	}
	email, err := normalizeContactEmail(req.Email)
	if err != nil {
		return err
	}
	status := strings.TrimSpace(req.Status)
	if status != "" && !contactStatusValid(status) {
		return errors.New(mailenums.ErrInvalidParam)
	}
	row, err := s.m.GetContact(ctx, req.ID)
	if err != nil {
		return errors.New(mailenums.ErrContactNotFound)
	}
	emailChanged := !strings.EqualFold(strings.TrimSpace(row.Email), email)
	if emailChanged {
		other, gerr := s.m.GetContactByEmail(ctx, email)
		switch {
		case gerr == nil && other.ID != req.ID:
			return errors.New(mailenums.ErrContactEmailExists)
		case gerr != nil && !errors.Is(gerr, gorm.ErrRecordNotFound):
			return gerr
		}
	}
	tags := normalizeTagList(req.Tags)
	now := time.Now()
	fields := map[string]any{
		"email": email,
		// 姓名留空 = 清空（表单预填了原值，用户删掉它就是要清掉）。
		"name":        strPtr(strings.TrimSpace(req.Name)),
		"tags":        mailmodel.StringArray(tags),
		"update_time": now,
	}
	// 来源留空时保持原值：来源是事实记录，不该因为这次没在表单里填就被抹掉。
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
		// 置为「已订阅」必须写清同意来源（合规留痕）。判据取**最终**状态与**最终**来源：
		//   · 最终状态：表单没提交状态时就是库里原状态；改邮箱撞抑制名单时上面已把它降级成终态，
		//     那种情况不该要求来源（他进不了可发名单）；
		//   · 最终来源：本次提交的来源，没提交则沿用库里已有的 —— 否则「只改姓名」会被无谓挡住。
		finalStatus := nextStatus
		if finalStatus == "" {
			finalStatus = row.Status
		}
		finalConsent := strings.TrimSpace(req.ConsentSource)
		if finalConsent == "" && row.ConsentSource != nil {
			finalConsent = strings.TrimSpace(*row.ConsentSource)
		}
		if finalStatus == mailmodel.ContactStatusSubscribed && finalConsent == "" {
			return errors.New(mailenums.ErrConsentSourceRequired)
		}
		if uerr := s.m.UpdateContactFieldsTx(ctx, tx, req.ID, fields); uerr != nil {
			if isContactEmailConflict(uerr) {
				return errors.New(mailenums.ErrContactEmailExists)
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

// DeleteContacts 删除联系人（单条与批量同一条路径）。
//
// **只删 mail_contacts 行，绝不碰 mail_suppressions**：抑制名单是地址级的合规事实
// （这个地址说过「不要再发」）。删联系人时顺手删掉抑制记录，下次导入名单就会把退订者
// 复活 —— 一个退订过的人重新收到群发，是这类系统里最贵的缺陷（投诉 + 域名声誉）。
// 误判需要放行时走抑制名单页的单独入口（DeleteSuppression），不是这里。
//
// 返回实际删除行数：调用方据此如实说明「点选的 id 里有几个已经不存在了」。
func (s *Service) DeleteContacts(ctx context.Context, req *maildto.DeleteContactsReq) (deleted int64, err error) {
	if req == nil || len(req.IDs) == 0 {
		return 0, errors.New(mailenums.ErrInvalidParam)
	}
	// 批删是一条语句（DeleteContactsByIDsTx）而不是循环单条：循环会让「删了 3 个、
	// 第 4 个报错」留下半截状态，调用方只能把整批当失败，重试时前 3 条已不存在。
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		var derr error
		deleted, derr = s.m.DeleteContactsByIDsTx(ctx, tx, req.IDs)
		return derr
	}); err != nil {
		return 0, err
	}
	return deleted, nil
}

// normalizeContactEmail 归一化并校验邮箱，失败时给出可读原因。
//
// 空与非法分开报：空是「忘了填」，非法是「填错了」——合成一句会让运营反复试同一个错。
func normalizeContactEmail(raw string) (string, error) {
	email, ok := normalizeEmail(raw)
	if ok {
		return email, nil
	}
	if strings.TrimSpace(raw) == "" {
		return "", errors.New(mailenums.ErrEmailRequired)
	}
	return "", errors.New(mailenums.ErrEmailInvalid)
}

// contactSourceOr 来源缺省（手工新建的联系人来源就是 manual）。
func contactSourceOr(raw string) string {
	if v := strings.TrimSpace(raw); v != "" {
		return v
	}
	return mailmodel.ContactSourceManual
}

// fireContactTagsAdded 对本次**真正新增**的标签触发 tag_added（事务外）。
//
// 口径与导入路径一致（见 mail_contact_import.go 的 tagAdds / OnTagsAddedBatch）：
// 导入会触发、这里不触发的话，同一个动作走两条入口只有一条会发信 —— 静默不一致，
// 而运营的预期是「打上这个标签的人就会进那条流程」。
// 差集用 diffTags（精确匹配、大小写敏感），与按标签筛人群的口径相同。
func (s *Service) fireContactTagsAdded(ctx context.Context, contactID uint64, before, after []string) {
	for _, tag := range diffTags(before, after) {
		s.OnTagsAddedBatch(ctx, tag, []uint64{contactID})
	}
}

// TagContacts 批量给联系人加 / 减标签。
//
// 逐条「锁读当前标签 → 算新值 → 写回」：数组列上没有既做减法又保序去重的单语句写法，
// 而丢掉行锁就会让并发写互相覆盖（见 model.LockContactTagsTx 的说明）。
// 批量 IN 预读只能省一次读、不能省锁 —— 锁外读到的旧值随时会变，写回去就是丢更新。
//
// 牺牲是 N 次往返（N = 点选条数）。为什么可接受：点选上限由 shell.BulkIDs 封顶
// （MaxBulkIDs），不是无界输入；将来上限若放大到几百，再把这里改成「按 id 升序一次锁多行」。
//
// changed 计「已处理」而不是「标签真的变了」：全部人本来就带着该标签时，
// 回执说「已处理 N 个」比两个数字都是 0、页面没有任何反馈要好（后者会被当成点了没反应）。
// skipped 只计失败与不存在的行，逐条失败不中断整批。
func (s *Service) TagContacts(ctx context.Context, req *maildto.TagContactsReq) (changed, skipped int, err error) {
	if req == nil || len(req.IDs) == 0 {
		return 0, 0, errors.New(mailenums.ErrInvalidParam)
	}
	add, remove := normalizeTagList(req.Add), normalizeTagList(req.Remove)
	if len(add) == 0 && len(remove) == 0 {
		return 0, 0, errors.New(mailenums.ErrContactTagEmpty)
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
				// 幂等：目标状态已经是这样，不写库。
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
		// 触发在事务外（自动化要建实例、可能入队发信）。
		for _, tag := range addedTags {
			s.OnTagsAddedBatch(ctx, tag, []uint64{id})
		}
	}
	return changed, skipped, nil
}

// ListContactTags 全库去重后的标签集合（筛选区 datalist 候选）。
//
// 只读、且只服务列表页的筛选项，权限复用 mail:contact_list，不新增权限点
// （新增权限点 = 已授权角色静默缩权，见 521 的注释）。
func (s *Service) ListContactTags(ctx context.Context) (tags []string, err error) {
	return s.m.ListContactTags(ctx)
}

// applyTagDelta 在 cur 之上加 add、去 remove，返回新标签与本次**真正新增**的标签。
//
// 口径与 splitTags / mergeTags / diffTags 一致：去空白、去重、保序、精确匹配（大小写敏感）。
// 同一条请求里既在 add 又在 remove 的标签以 **remove 为准**（「去掉」是更强的诉求：
// 运营同时勾了两个字段时，宁可少打一个标签，也不该给一个明确要去掉的人打上）。
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

// normalizeTagList 去空白、去空项、去重（保序）—— 表单与 CSV 两条输入共用。
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

// tagListEqual 逐项比较两组标签（顺序敏感：新值由 applyTagDelta 按原顺序生成）。
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

// dedupIDs 去零、去重（保序）。批量入口的 id 集合来自 shell.BulkIDs，
// 但单条路径与直接调用 service 的测试也会走这里，重复 id 会让同一条被处理两次
// （第二次数出的 changed/skipped 就是假的）。
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
