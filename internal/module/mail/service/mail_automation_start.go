package mailservice

// mail_automation_start.go — 启动实例（issue #38 P3 的触发器侧）。
//
// 幂等由**部分唯一索引**兜底：同一人同流程只允许一个进行中的实例。
// 应用层先查一次是为了给出「已在流程中」这种可读结果，但真正的保证在数据库 ——
// 并发触发（两个事件同时到达）时应用层的「先查再插」必然漏，唯一索引才拦得住。

import (
	"context"
	"errors"
	"strings"
	"time"

	mailenums "go_wp/internal/module/mail/enums"
	mailmodel "go_wp/internal/module/mail/model"
	"go_wp/pkg/logger"
)

// StartRun 启动一个实例。返回是否新启动（false 表示该联系人已在此流程中）。
func (s *Service) StartRun(ctx context.Context, automationID, contactID uint64, triggerEvent string) (newRun bool, err error) {
	if automationID == 0 || contactID == 0 {
		return false, errors.New(mailenums.ErrInvalidParam)
	}
	automation, err := s.m.GetAutomation(ctx, automationID)
	if err != nil {
		return false, errors.New(mailenums.ErrAutomationNotFound)
	}
	// 只有启用中的流程接受新实例（暂停/草稿不收）。
	if automation.Status != mailmodel.AutomationStatusActive {
		return false, nil
	}
	return s.startRunWithAutomation(ctx, automation, contactID, triggerEvent)
}

// startRunWithAutomation 用**已取到的**流程实体启动实例。
//
// 与 StartRun 的唯一差异：调用方已经确认过流程存在且启用（批量路径用
// automationsForTrigger 过滤），所以这里不再重查流程行 —— 那次查询正是批量路径要消掉的
// 每人一条。启动规则本身只有这一份实现，StartRun 委托到这里，不存在第二份真源。
func (s *Service) startRunWithAutomation(ctx context.Context, automation *mailmodel.MailAutomationEntity,
	contactID uint64, triggerEvent string) (newRun bool, err error) {
	if automation == nil || automation.ID == 0 || contactID == 0 {
		return false, errors.New(mailenums.ErrInvalidParam)
	}
	if _, cerr := s.m.GetContact(ctx, contactID); cerr != nil {
		return false, errors.New(mailenums.ErrContactNotFound)
	}
	def, derr := ParseDefinition(automation.Definition)
	if derr != nil {
		return false, graphInvalidError(derr)
	}

	if _, aerr := s.m.ActiveRun(ctx, automation.ID, contactID); aerr == nil {
		// 已在流程中：不重复启动。
		return false, nil
	}

	now := time.Now()
	entry := def.Entry
	e := &mailmodel.MailAutomationRunEntity{
		AutomationID:      automation.ID,
		AutomationVersion: automation.Version,
		ContactID:         contactID,
		Status:            mailmodel.RunStatusRunning,
		StartedAt:         &now,
	}
	if entry != "" {
		e.CurrentNode = &entry
	}
	if ev := strings.TrimSpace(triggerEvent); ev != "" {
		e.TriggerEvent = &ev
	}
	if cerr := s.m.CreateRun(ctx, e); cerr != nil {
		// 唯一索引冲突 = 并发下已有人建了同一个实例，按「已在流程中」处理，不是错误。
		return false, nil
	}
	enqueueAutomationRun(e.ID, time.Time{})
	return true, nil
}

// StartRunsByTrigger 按触发方式批量启动（事件进入时调用）。
//
// 匹配规则：流程启用中、trigger_type 相符、trigger_params 的条件也满足。
// 目前只支持 tag_added 的标签匹配；其余触发方式（opened / clicked 等）不带附加条件。
//
// ⚠️ 这个入口**每个联系人查一次流程表**。只处理单个联系人的事件（打开 / 点击 / 订阅）
// 可以用它；**批量场景（导入）必须走下面的批量入口** —— 否则 2000 行导入会发出 2000 条
// 流程查询（护栏测试 TestImportWriteStatementProfile 实测 2006 条语句）。
func (s *Service) StartRunsByTrigger(ctx context.Context, triggerType string, contactID uint64, extra map[string]any) (started int, err error) {
	autos, lerr := s.activeAutomationsFor(ctx, triggerType, extra)
	if lerr != nil {
		return 0, lerr
	}
	ok, serr := s.startRunsWithAutomations(ctx, autos, []uint64{contactID}, triggerType)
	return ok, serr
}

// ActiveAutomations 取一次「启用中的流程全集」（批量路径的预检与触发共用同一份结果）。
//
// 读失败不返回错误：触发是业务动作的附加行为，读不到流程表不该让导入 / 注册失败
// （与 fireTrigger 同一取舍），失败记结构化日志后按「没有流程」处理。
func (s *Service) activeAutomations(ctx context.Context) []*mailmodel.MailAutomationEntity {
	list, err := s.m.ListActiveAutomations(ctx)
	if err != nil {
		logger.Scene("mail").Error(err, "读取启用中的自动化流程失败，本次不触发（业务动作本身不受影响）")
		return nil
	}
	return list
}

// activeAutomationsFor 取启用中的流程里匹配该触发器的那些（**一次查询**）。
func (s *Service) activeAutomationsFor(ctx context.Context, triggerType string, extra map[string]any) ([]*mailmodel.MailAutomationEntity, error) {
	list, err := s.m.ListActiveAutomations(ctx)
	if err != nil {
		return nil, err
	}
	return automationsForTrigger(list, triggerType, extra), nil
}

// automationsForTrigger 从流程全集里挑出匹配的（纯函数，便于单测）。
func automationsForTrigger(list []*mailmodel.MailAutomationEntity, triggerType string, extra map[string]any) []*mailmodel.MailAutomationEntity {
	out := make([]*mailmodel.MailAutomationEntity, 0, len(list))
	for _, a := range list {
		if a == nil || a.TriggerType != triggerType {
			continue
		}
		if !triggerParamsMatch(a.TriggerParams, extra) {
			continue
		}
		out = append(out, a)
	}
	return out
}

// hasTriggerType 流程全集里是否有该触发器类型的流程（**不判 trigger_params**）。
//
// 用途是「粗判有没有这一类流程」：调用方据此决定要不要做那些「只有存在流程时才有意义」
// 的准备工作（例如导入时读每个联系人的旧标签来算差集）。用带参数匹配的过滤函数做这件事是错的 ——
// extra 为 nil 时带 tag 条件的流程不会被匹配上，粗判会假阴性。
func hasTriggerType(list []*mailmodel.MailAutomationEntity, triggerType string) bool {
	for _, a := range list {
		if a != nil && a.TriggerType == triggerType {
			return true
		}
	}
	return false
}

// startRunsWithAutomations 用**已取到的**流程集合给一批联系人各自建实例。
//
// 这是批量路径的实例层写入主体：流程查询由调用方一次完成，这里每个联系人只剩
// 「联系人存在性 + 幂等检查 + 建实例」三件事。无流程时（len==0）一次查询都不发。
func (s *Service) startRunsWithAutomations(ctx context.Context, autos []*mailmodel.MailAutomationEntity,
	contactIDs []uint64, triggerEvent string) (started int, err error) {
	if len(autos) == 0 || len(contactIDs) == 0 {
		return 0, nil
	}
	seen := make(map[uint64]bool, len(contactIDs))
	var firstErr error
	for _, contactID := range contactIDs {
		if contactID == 0 || seen[contactID] {
			continue
		}
		seen[contactID] = true
		if ctx.Err() != nil {
			break
		}
		for _, a := range autos {
			ok, serr := s.startRunWithAutomation(ctx, a, contactID, triggerEvent)
			if serr != nil {
				// 单人失败不中断整批（与 StartRunsByTrigger 一致），但要留一个错误出口：
				// 全部失败时调用方至少能看出「这一次触发整体没成」。
				if firstErr == nil {
					firstErr = serr
				}
				continue
			}
			if ok {
				started++
			}
		}
	}
	return started, firstErr
}

// StartRunsByTriggerBatch 批量触发：一次流程查询，然后逐联系人 × 匹配流程建实例。
//
// 与 StartRunsByTrigger 的分工就是这个「一次」：批量导入调用它，避免每个联系人各查一次
// 流程表。无匹配流程时**不做任何实例层查询**（零配置站点的批量导入不该为触发付费）。
func (s *Service) StartRunsByTriggerBatch(ctx context.Context, triggerType string, contactIDs []uint64, extra map[string]any) (started int, err error) {
	autos, lerr := s.activeAutomationsFor(ctx, triggerType, extra)
	if lerr != nil {
		return 0, lerr
	}
	return s.startRunsWithAutomations(ctx, autos, contactIDs, triggerType)
}

// ---- 事件触发（issue #38 P3）----
//
// 每个业务动作结束后调用对应方法，把「发生了什么」交给引擎去匹配流程。
// **触发失败绝不影响主流程**：注册 / 导入 / 订阅 / 追踪这些动作本身是用户要的结果，
// 自动化只是附加行为 —— 它出问题不该让注册失败。
//
// 一条边界：**自动化内部的标签节点不再触发 tag_added**。否则「流程 A 加了 tag X」
// 会触发「流程 B」，B 又加 tag Y 触发 A，形成跨流程的递归。标签触发只认**外部**改动
//（后台手工、导入、前台订阅）。

// OnContactCreated 新联系人产生（单条事件源：注册 / 后台手工建号）。
func (s *Service) OnContactCreated(ctx context.Context, contactID uint64) {
	s.fireTrigger(ctx, mailmodel.TriggerContactCreated, contactID, nil)
}

// OnContactsCreated 一批新联系人产生（导入路径；**一次流程匹配**，不做 N+1）。
//
// 与 OnContactCreated 的关系：同一个触发器、同一个引擎，差别只在「流程表查几次」。
// 导入 2000 行时逐人调用单条入口会发出 2000 条流程查询（护栏测试
// TestImportWriteStatementProfile 守的就是这条），所以批量生产者必须走这里。
func (s *Service) OnContactsCreated(ctx context.Context, contactIDs []uint64) {
	defer func() { _ = recover() }()
	_, _ = s.StartRunsByTriggerBatch(ctx, mailmodel.TriggerContactCreated, contactIDs, nil)
}

// OnContactSubscribed 联系人变为已订阅。
func (s *Service) OnContactSubscribed(ctx context.Context, contactID uint64) {
	s.fireTrigger(ctx, mailmodel.TriggerContactSubscribed, contactID, nil)
}

// OnTagsAdded 联系人被外部加上标签（单条）。
//
// 自动化内部的标签动作不调用它（见上方边界说明）；导入路径**多联系人时**走
// OnTagsAddedBatch（同触发器、同引擎，只是流程表查一次）。
func (s *Service) OnTagsAdded(ctx context.Context, contactID uint64, tags []string) {
	for _, tag := range tags {
		s.fireTrigger(ctx, mailmodel.TriggerTagAdded, contactID, map[string]any{"tag": tag})
	}
}

// OnTagsAddedBatch 一批联系人被打上**同一个**标签（导入路径；一次流程匹配）。
//
// 按标签分组调用：trigger_params 的 tag 条件是按标签匹配的，不同标签要各自匹配一次。
func (s *Service) OnTagsAddedBatch(ctx context.Context, tag string, contactIDs []uint64) {
	defer func() { _ = recover() }()
	_, _ = s.StartRunsByTriggerBatch(ctx, mailmodel.TriggerTagAdded, contactIDs, map[string]any{"tag": tag})
}

// OnEmailOpened / OnEmailClicked 追踪事件（在事件落库之后调用）。
func (s *Service) OnEmailOpened(ctx context.Context, contactID uint64) {
	if contactID > 0 {
		s.fireTrigger(ctx, mailmodel.TriggerEmailOpened, contactID, nil)
	}
}

// OnEmailClicked 点击事件触发。
func (s *Service) OnEmailClicked(ctx context.Context, contactID uint64) {
	if contactID > 0 {
		s.fireTrigger(ctx, mailmodel.TriggerEmailClicked, contactID, nil)
	}
}

// fireTrigger 内部入口：吞掉错误（触发不该影响调用方的主流程）。
func (s *Service) fireTrigger(ctx context.Context, triggerType string, contactID uint64, extra map[string]any) {
	defer func() { _ = recover() }()
	_, _ = s.StartRunsByTrigger(ctx, triggerType, contactID, extra)
}

// triggerParamsMatch 判断流程的触发条件是否与本次事件相符。
//
// 目前只认 tag_added 的 tag 字段：不匹配就跳过。其余触发方式不带条件。
func triggerParamsMatch(params map[string]any, extra map[string]any) bool {
	if len(params) == 0 {
		return true
	}
	wantTag := strings.TrimSpace(toString(params["tag"]))
	if wantTag == "" {
		return true
	}
	gotTag := ""
	if extra != nil {
		gotTag = strings.TrimSpace(toString(extra["tag"]))
	}
	return wantTag == gotTag
}
