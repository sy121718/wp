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
	if _, cerr := s.m.GetContact(ctx, contactID); cerr != nil {
		return false, errors.New(mailenums.ErrContactNotFound)
	}
	def, derr := ParseDefinition(automation.Definition)
	if derr != nil {
		return false, errors.New(mailenums.ErrAutomationGraphInvalid + ": " + derr.Error())
	}

	if _, aerr := s.m.ActiveRun(ctx, automationID, contactID); aerr == nil {
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
func (s *Service) StartRunsByTrigger(ctx context.Context, triggerType string, contactID uint64, extra map[string]any) (started int, err error) {
	list, lerr := s.m.ListActiveAutomations(ctx)
	if lerr != nil {
		return 0, lerr
	}
	for _, a := range list {
		if a.TriggerType != triggerType {
			continue
		}
		if !triggerParamsMatch(a.TriggerParams, extra) {
			continue
		}
		ok, serr := s.StartRun(ctx, a.ID, contactID, triggerType)
		if serr != nil {
			continue
		}
		if ok {
			started++
		}
	}
	return started, nil
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

// OnContactCreated 新联系人产生（导入 / 拉系统用户 / 注册）。
func (s *Service) OnContactCreated(ctx context.Context, contactID uint64) {
	s.fireTrigger(ctx, mailmodel.TriggerContactCreated, contactID, nil)
}

// OnContactSubscribed 联系人变为已订阅。
func (s *Service) OnContactSubscribed(ctx context.Context, contactID uint64) {
	s.fireTrigger(ctx, mailmodel.TriggerContactSubscribed, contactID, nil)
}

// OnTagsAdded 联系人被外部加上标签（自动化内部的标签动作不调用它）。
func (s *Service) OnTagsAdded(ctx context.Context, contactID uint64, tags []string) {
	for _, tag := range tags {
		s.fireTrigger(ctx, mailmodel.TriggerTagAdded, contactID, map[string]any{"tag": tag})
	}
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
