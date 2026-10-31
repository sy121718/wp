package presentationservice

import (
	"context"
	"strings"
	"time"

	presentationmodel "go_wp/internal/module/presentation/model"
	"go_wp/pkg/actor"
	"go_wp/pkg/logger"
)

const publishAttemptMaxErrorLen = 2000

const publishAttemptWriteTimeout = 5 * time.Second

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

func rebuildTrigger(switching bool) string {
	if switching {
		return presentationmodel.PublishTriggerSwitch
	}
	return presentationmodel.PublishTriggerManual
}

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
