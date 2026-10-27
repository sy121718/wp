package presentationservice

// presentation_publish_attempt.go — 把每一次发布尝试写进 presentation_publish_attempts（迁移 673）。
//
// ## 为什么在事务外写
//
// 失败路径要把记录留下，而失败时发布事务整体回滚 —— 同事务写进去的那行会跟着消失。
// 所以写入点在 publishAllLangs 的 defer 里，事务已经结束。代价是进程被 kill 时最后一次
// 尝试的记录会丢（拿「记录必然存在」换「记录完整」，这是刻意的取舍）。
//
// ## 为什么用 context.WithoutCancel
//
// 请求被客户端断开时 ctx 已取消，而这条记录恰恰是「那次没发成功」的证据 —— 丢掉它等于
// 把最需要看的那一行删了。超时用 5s 兜住：写历史不该拖住调用方返回。

import (
	"context"
	"strings"
	"time"

	presentationmodel "go_wp/internal/module/presentation/model"
	"go_wp/pkg/actor"
	"go_wp/pkg/logger"
)

// publishAttemptMaxErrorLen 失败原因原文的落库上限（列是 text，这里只求可读）。
const publishAttemptMaxErrorLen = 2000

// publishAttemptWriteTimeout 写一条历史的时间上限。
const publishAttemptWriteTimeout = 5 * time.Second

// publishTriggerOf 归一触发源：空串与未知取值都落成 manual。
//
// 为什么未知值不原样写：ck_publish_attempts_trigger 会拒掉它，整条记录都写不进去 ——
// 宁可把这条记成「人工发布」（人看的结果是「有一次发布」），也不要因为一个枚举写错
// 就让历史断档。
func publishTriggerOf(trigger string) string {
	switch trigger {
	case presentationmodel.PublishTriggerCreate,
		presentationmodel.PublishTriggerManual,
		presentationmodel.PublishTriggerSwitch,
		presentationmodel.PublishTriggerURL,
		presentationmodel.PublishTriggerRollback,
		presentationmodel.PublishTriggerOverride,
		presentationmodel.PublishTriggerAuto:
		return trigger
	default:
		return presentationmodel.PublishTriggerManual
	}
}

// rebuildTrigger 重建入口的触发源：带 templateId = 换模板，其余按人工发布。
func rebuildTrigger(switching bool) string {
	if switching {
		return presentationmodel.PublishTriggerSwitch
	}
	return presentationmodel.PublishTriggerManual
}

// recordPublishAttempt 写一条发布尝试。cause 为 nil = 成功。
//
// 本方法**永不向上返回错误**：它是观测通道，写失败只记日志。反过来（写历史失败导致
// 发布被判失败）会让一次成功的发布对用户显示成失败 —— 观测不该改变被观测的行为。
func (s *Service) recordPublishAttempt(ctx context.Context, inst *presentationmodel.InstanceEntity,
	intent publishIntent, langs []string, attempts int, artifactID string, cause error, dur time.Duration) {
	if s == nil || inst == nil || s.m == nil {
		return
	}
	outcome := presentationmodel.PublishOutcomeSuccess
	errorText := ""
	if cause != nil {
		outcome = presentationmodel.PublishOutcomeFailed
		errorText = truncateRunes(cause.Error(), publishAttemptMaxErrorLen)
	}

	artifactVersion, artifactHash := int64(0), ""
	if cause == nil && strings.TrimSpace(artifactID) != "" {
		// 产物行是成功路径写的；读不到只说明产物已被 GC（真实状态），按 0 版本记。
		if facts, ferr := s.m.ListArtifactVersionFacts(ctx, []string{artifactID}); ferr == nil && len(facts) > 0 {
			artifactVersion, artifactHash = facts[0].Version, facts[0].ArtifactHash
		}
	}

	who := actor.From(ctx)
	if attempts < 1 {
		attempts = 1
	}
	row := &presentationmodel.PublishAttemptEntity{
		ProjectID:       inst.ProjectID,
		InstanceID:      inst.ID,
		EntityType:      inst.EntityType,
		EntityID:        inst.EntityID,
		Trigger:         publishTriggerOf(intent.trigger),
		Outcome:         outcome,
		ErrorText:       errorText,
		Langs:           presentationmodel.LangArray(langs),
		Attempts:        attempts,
		ArtifactVersion: artifactVersion,
		ArtifactHash:    artifactHash,
		OperatorID:      who.ID,
		OperatorName:    who.Name,
		DurationMs:      dur.Milliseconds(),
		CreatedAt:       time.Now().UTC(),
	}

	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), publishAttemptWriteTimeout)
	defer cancel()
	if err := s.m.RecordPublishAttempt(writeCtx, row); err != nil {
		logger.Scene("publication").With("instanceId", inst.ID).With("outcome", outcome).
			Error(err, "发布历史写入失败（本次发布行为不受影响）")
	}
}

// truncateRunes 按字符截断（不切断多字节字符）。
func truncateRunes(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}
