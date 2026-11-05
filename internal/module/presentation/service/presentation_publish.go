package presentationservice

import (
	"context"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	publish "go_wp/pkg/publish"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationenums "go_wp/internal/module/presentation/enums"
	presentationmodel "go_wp/internal/module/presentation/model"
	pubcontract "go_wp/internal/module/publication/contract"
	"go_wp/internal/pipeline"
	"go_wp/pkg/builder"
	"go_wp/pkg/logger"
	"go_wp/pkg/rls"
	"go_wp/pkg/utils"
)

const publishAttempts = 3

var errPublishStateMoved = errors.New("实例状态已被并发的发布批次推进")

var errInputVersionMoved = errors.New("实例的编译输入代次已被推进")

var errPublishInputSuperseded = errors.New("改 URL 的输入意图已持久化，但本次产物基于过期输入")

type publishInputError struct{ err error }

func (e *publishInputError) Error() string { return e.err.Error() }
func (e *publishInputError) Unwrap() error { return e.err }

func publishFailedErr(err error) error {
	var inputErr *publishInputError
	if errors.As(err, &inputErr) {
		return inputErr.err
	}
	return presentationenums.ErrDetail(presentationenums.ErrBuildFailed, err.Error())
}

type beginReceiptInput struct {
	inst                 *presentationmodel.InstanceEntity
	path                 string
	oldPath              string
	redirect             bool
	lang                 string
	fromArtifactID       string
	toArtifactID         string
	capturedInputVersion int64
	action               string

	oldPaths []string

	successfulReceipts map[string]string

	fromReceiptID string
}

type oldPathIntent struct {
	lang    string
	oldPath string

	newPath string

	fromArtifactID string

	newArtifactID string
	redirect      bool
}

type publishIntent struct {
	resolveDoc func(ctx context.Context, inst *presentationmodel.InstanceEntity) (*contenttemplatecontract.ResolvedTemplate, error)

	logicalPath string

	oldPaths []oldPathIntent

	inputPath string

	trigger string

	mode *instanceModePending
}

type publishFreeze struct {
	references *pipeline.ReferenceSnapshot

	langs []string

	rule publish.LangURLRule

	logicalPath string

	defaultLang string

	tpl *contenttemplatecontract.ResolvedTemplate

	mode *instanceModePending

	inputVersion int64

	fingerprint instanceStateFingerprint

	inputPersisted bool
}

func (s *Service) freezePublish(ctx context.Context, inst *presentationmodel.InstanceEntity,
	intent publishIntent) (frozen *publishFreeze, timing phaseTiming, err error) {
	if inst == nil || strings.TrimSpace(inst.ID) == "" {
		return nil, timing, presentationenums.Err(presentationenums.ErrNotFound)
	}
	lock := s.lockInstance(inst.EntityType, inst.EntityID)
	waitStart := time.Now()
	lock.Lock()
	timing.lockWait = time.Since(waitStart)
	workStart := time.Now()

	defer lock.Unlock()
	defer func() { timing.work = time.Since(workStart) }()

	fresh, gerr := s.m.GetInstance(ctx, inst.ProjectID, inst.ID)
	if gerr != nil {
		if errors.Is(gerr, gorm.ErrRecordNotFound) {
			return nil, timing, presentationenums.Err(presentationenums.ErrNotFound)
		}
		return nil, timing, gerr
	}
	*inst = *fresh

	inputPersisted := false

	if in := strings.TrimSpace(intent.inputPath); in != "" {
		at := time.Now().UTC()
		var newVersion int64
		terr := s.m.Transaction(ctx, func(tx *gorm.DB) error {
			v, perr := s.m.PersistPublishInputURLTx(tx, fresh.ProjectID, fresh.ID, in, at)
			if perr != nil {
				return perr
			}
			newVersion = v
			return nil
		})
		if terr != nil {
			return nil, timing, terr
		}
		fresh.InputVersion = newVersion
		fresh.URLPath = in

		fresh.Stale = true
		*inst = *fresh
		inputPersisted = true
	}

	langs, lerr := s.publishLangsOf(ctx, inst.ProjectID)
	if lerr != nil {
		return nil, timing, fmt.Errorf("站点语言清单不可读，多语言整批发布中止: %w", lerr)
	}
	tpl, terr := intent.resolveDoc(ctx, inst)
	if terr != nil {

		return nil, timing, &publishInputError{err: terr}
	}
	references, refErr := s.freezeReferences(ctx, inst.ProjectID, tpl.Document)
	if refErr != nil {
		return nil, timing, refErr
	}
	logicalPath := pipeline.LogicalPathOf(ctx, s.project, inst.ProjectID, intent.logicalPath)
	if logicalPath == "" {
		logicalPath = s.instanceLogicalPath(ctx, inst)
	}
	return &publishFreeze{
		references:  references,
		langs:       langs,
		rule:        pipeline.LangURLRuleForProject(ctx, s.project, inst.ProjectID),
		logicalPath: logicalPath,
		defaultLang: pipeline.DefaultLocale(ctx, s.project, inst.ProjectID),
		tpl:         tpl,
		mode:        intent.mode,

		inputVersion:   fresh.InputVersion,
		fingerprint:    fingerprintOfInstance(fresh),
		inputPersisted: inputPersisted,
	}, timing, nil
}

func (s *Service) compileLangs(ctx context.Context, inst *presentationmodel.InstanceEntity,
	frozen *publishFreeze) (results []langBuildResult, work time.Duration, err error) {
	start := time.Now()
	defer func() { work = time.Since(start) }()
	ctx = pipeline.WithReferenceSnapshot(ctx, frozen.references)
	results = make([]langBuildResult, 0, len(frozen.langs))
	for _, lang := range frozen.langs {
		accessPath, perr := pipeline.SitePath(frozen.rule, lang, frozen.logicalPath)
		if perr != nil {
			return nil, work, perr
		}

		built, berr := s.buildArtifact(ctx, inst.EntityType, inst.EntityID, accessPath,
			inst.ProjectID, lang, frozen.langs, frozen.tpl)
		if berr != nil {
			return nil, work, berr
		}
		results = append(results, langBuildResult{lang: lang, accessPath: accessPath, built: built})
	}
	return results, work, nil
}

func (s *Service) commitPublish(ctx context.Context, inst *presentationmodel.InstanceEntity,
	frozen *publishFreeze, results []langBuildResult, oldPathPlan []oldPathIntent) (primaryArtifactID string, timing phaseTiming, err error) {
	lock := s.lockInstance(inst.EntityType, inst.EntityID)
	waitStart := time.Now()
	lock.Lock()
	timing.lockWait = time.Since(waitStart)
	workStart := time.Now()

	defer lock.Unlock()
	defer func() { timing.work = time.Since(workStart) }()

	current, gerr := s.m.GetInstance(ctx, inst.ProjectID, inst.ID)
	if gerr != nil {
		if errors.Is(gerr, gorm.ErrRecordNotFound) {
			return "", timing, presentationenums.Err(presentationenums.ErrNotFound)
		}
		return "", timing, gerr
	}
	if !frozen.fingerprint.equal(fingerprintOfInstance(current)) {
		return "", timing, errPublishStateMoved
	}

	if current.InputVersion != frozen.inputVersion {

		if frozen.inputPersisted {
			return "", timing, errPublishInputSuperseded
		}
		return "", timing, errInputVersionMoved
	}

	previousArtifacts := s.publishedArtifactsByLang(ctx, inst.ID)

	currentReceiptIDs := s.publishedReceiptIDsByLang(ctx, inst.ID, results)

	for i := range results {
		b := &results[i]
		if b.artifactID != "" {
			continue
		}
		existingID, aerr := s.m.FindArtifactIDByHash(ctx, inst.ID, b.built.Hash)
		switch {
		case aerr == nil:
			b.artifactID = existingID
		case errors.Is(aerr, gorm.ErrRecordNotFound):
			b.artifactID = uuid.NewString()
		default:
			return "", timing, fmt.Errorf("解析 %s 的产物身份失败（未切换访问面）: %w", b.accessPath, aerr)
		}
	}

	receipts := make(map[string]string, len(results))
	for i := range results {
		b := &results[i]

		id, berr := s.beginPublishReceipt(ctx, beginReceiptInput{
			inst: inst,
			path: b.accessPath, lang: b.lang, fromArtifactID: previousArtifacts[b.lang],
			toArtifactID: b.artifactID, capturedInputVersion: frozen.inputVersion,
			fromReceiptID: currentReceiptIDs[b.lang],
		})
		if berr != nil {

			for _, rid := range receipts {
				s.abortPublishReceipt(ctx, rid, "同批次前序回执登记失败")
			}
			return "", timing, fmt.Errorf("登记 %s 的发布回执失败（未切换访问面）: %w", b.accessPath, berr)
		}
		receipts[b.lang] = id
	}

	oldPathReceiptID := ""
	if len(oldPathPlan) > 0 {
		first := oldPathPlan[0]
		paths := make([]string, 0, len(oldPathPlan))
		for _, plan := range oldPathPlan {
			paths = append(paths, plan.oldPath)
		}

		id, oerr := s.beginPublishReceipt(ctx, beginReceiptInput{
			inst: inst,
			path: first.newPath, oldPath: first.oldPath, redirect: first.redirect,
			oldPaths: paths, successfulReceipts: receipts,
			lang: first.lang, fromArtifactID: first.fromArtifactID, toArtifactID: first.newArtifactID,
			capturedInputVersion: frozen.inputVersion, action: pubcontract.ReceiptActionUpdateURL,

			fromReceiptID: currentReceiptIDs[first.lang],
		})
		if oerr != nil {
			for _, rid := range receipts {
				s.abortPublishReceipt(ctx, rid, "同批次旧路径处置回执登记失败")
			}
			return "", timing, fmt.Errorf("登记 %s 的旧路径处置失败（未切换访问面）: %w", first.oldPath, oerr)
		}
		oldPathReceiptID = id
	}

	now := time.Now().UTC()
	var primaryArtifactID2, snapID string

	txErr := s.m.Transaction(ctx, func(tx *gorm.DB) error {
		locked, lerr := s.m.LockInstanceForActivationTx(ctx, tx, inst.ProjectID, inst.ID)
		if lerr != nil {
			if errors.Is(lerr, gorm.ErrRecordNotFound) {
				return presentationenums.Err(presentationenums.ErrNotFound)
			}
			return lerr
		}

		if locked.InputVersion != frozen.inputVersion {
			return errInputVersionMoved
		}

		primaryID, ssid, perr := s.persistMultiLangArtifactsTx(ctx, tx, inst, frozen.tpl, results,
			now, frozen.logicalPath, frozen.defaultLang, frozen.mode)
		if perr != nil {
			return perr
		}
		primaryArtifactID2, snapID = primaryID, ssid

		for i := range results {
			b := &results[i]
			if aerr := s.activate(b.accessPath, b.built); aerr != nil {
				return aerr
			}
			if rerr := s.registerRouteTx(ctx, tx, inst, b.accessPath, b.artifactID); rerr != nil {
				return fmt.Errorf("登记 %s 的路由占用失败: %w", b.accessPath, rerr)
			}

			if merr := s.m.MarkPublishedLangTx(tx, presentationmodel.PublicationRecord{
				PresentationID: inst.ID, Lang: b.lang, ActivePath: b.accessPath,
				ArtifactID: b.artifactID, ArtifactHash: b.built.Hash,
				ReceiptID: receipts[b.lang], PublishedAt: now,
			}); merr != nil {
				return fmt.Errorf("写入 %s 的发布账本失败: %w", b.accessPath, merr)
			}
		}

		return s.finalizeMultiLangBatchTx(ctx, tx, inst, snapID, primaryArtifactID2, now, frozen.inputVersion)
	})
	if txErr != nil {

		s.abortBatchAfterFailure(ctx, inst, receipts, txErr, frozen.inputPersisted)

		if oldPathReceiptID != "" || frozen.inputPersisted {
			s.NotifyPendingReceipt()
		}
		return "", timing, txErr
	}

	for _, rid := range receipts {
		if cerr := s.completePublishReceipt(ctx, rid); cerr != nil {
			logger.Scene("build").With("instanceId", inst.ID).
				Warn("多语言发布回执结案失败（状态已一致，留待收敛幂等补齐）: " + cerr.Error())
			s.NotifyPendingReceipt()
		}
	}

	oldPathPending := 0
	if oldPathReceiptID != "" {

		pending, derr := s.runOldPathDisposal(ctx, pubcontract.PendingReceiptResp{
			ID: oldPathReceiptID, SourceType: pubcontract.ReceiptSourcePresentation,
			SourceID: inst.ID, ProjectID: inst.ProjectID,
			Path: inst.URLPath, OldPath: firstOldPath(oldPathPlan), OldPaths: oldPathList(oldPathPlan),
			Redirect:     len(oldPathPlan) > 0 && oldPathPlan[0].redirect,
			ToArtifactID: primaryArtifactID2, Action: pubcontract.ReceiptActionUpdateURL,
		}, primaryArtifactID2, receipts)
		if !pending {
			oldPathPending++
			s.lastOldPathPending.Store(time.Now().Unix())
			s.NotifyPendingReceipt()
			if derr != nil {
				logger.Scene("build").With("instanceId", inst.ID).With("receiptId", oldPathReceiptID).
					Warn("旧路径处置未完成（新路径已生效，回执保留待收敛重放）: " + derr.Error())
			}
		}
	}
	pinged := make([]string, 0, len(results))
	for i := range results {
		pinged = append(pinged, results[i].accessPath)
	}
	s.notifyIndexNow(ctx, inst, pinged...)
	return primaryArtifactID2, timing, nil
}

func (s *Service) abortBatchAfterFailure(ctx context.Context, inst *presentationmodel.InstanceEntity,
	receipts map[string]string, cause error, inputPersisted bool) {
	if inputPersisted {
		return
	}
	if errors.Is(cause, errInputVersionMoved) || errors.Is(cause, errPublishStateMoved) {

		for _, rid := range receipts {
			s.abortPublishReceipt(ctx, rid, "版本或状态复验未通过，未切换访问面")
		}
		return
	}
	s.markBatchUnconverged(ctx, inst, cause)
	s.NotifyPendingReceipt()
}

func (s *Service) registerRouteTx(ctx context.Context, tx *gorm.DB,
	inst *presentationmodel.InstanceEntity, urlPath, artifactID string) error {
	if s.routes == nil {
		return nil
	}
	_, err := s.routes.ActivateTx(ctx, tx, &pubcontract.ActivateReq{
		ProjectID:      inst.ProjectID,
		Path:           urlPath,
		PresentationID: inst.ID,
		ArtifactID:     artifactID,
	})
	return err
}

func (s *Service) finalizeMultiLangBatchTx(ctx context.Context, tx *gorm.DB,
	inst *presentationmodel.InstanceEntity, snapID, primaryArtifactID string,
	now time.Time, expectInputVersion int64) error {
	if strings.TrimSpace(primaryArtifactID) == "" || strings.TrimSpace(snapID) == "" {
		return errors.New("多语言发布缺少默认语言产物或快照，无法推进实例指针")
	}
	inst.CurrentSnapshotID = &snapID
	inst.StagedSnapshotID = &snapID
	inst.StagedArtifactID = &primaryArtifactID
	inst.ActiveArtifactID = &primaryArtifactID
	inst.Stale = false
	inst.PublishedAt = &now
	inst.UpdatedAt = now
	if err := s.m.WriteInstanceActivationTx(tx, inst.ProjectID, inst, expectInputVersion); err != nil {
		if errors.Is(err, presentationmodel.ErrInputVersionMoved) {
			return errInputVersionMoved
		}
		return err
	}
	return nil
}

type instanceStateFingerprint struct {
	templateID        string
	urlPath           string
	renderMode        string
	overrideDocument  string
	currentSnapshotID string
	stagedSnapshotID  string
	stagedArtifactID  string
	activeArtifactID  string
	stale             bool
	publishedAt       time.Time
}

func fingerprintOfInstance(e *presentationmodel.InstanceEntity) instanceStateFingerprint {
	if e == nil {
		return instanceStateFingerprint{}
	}
	f := instanceStateFingerprint{
		templateID:        e.TemplateID,
		urlPath:           e.URLPath,
		renderMode:        presentationmodel.NormalizeRenderMode(e.RenderMode),
		overrideDocument:  string(e.OverrideDocument),
		currentSnapshotID: derefString(e.CurrentSnapshotID),
		stagedSnapshotID:  derefString(e.StagedSnapshotID),
		stagedArtifactID:  derefString(e.StagedArtifactID),
		activeArtifactID:  derefString(e.ActiveArtifactID),
		stale:             e.Stale,
	}
	if e.PublishedAt != nil {
		f.publishedAt = e.PublishedAt.UTC()
	}
	return f
}

func (f instanceStateFingerprint) equal(o instanceStateFingerprint) bool {
	return f.templateID == o.templateID &&
		f.urlPath == o.urlPath &&
		f.renderMode == o.renderMode &&
		f.overrideDocument == o.overrideDocument &&
		f.currentSnapshotID == o.currentSnapshotID &&
		f.stagedSnapshotID == o.stagedSnapshotID &&
		f.stagedArtifactID == o.stagedArtifactID &&
		f.activeArtifactID == o.activeArtifactID &&
		f.stale == o.stale &&
		f.publishedAt.Equal(o.publishedAt)
}

type phaseTiming struct {
	lockWait time.Duration
	work     time.Duration
}

type publishSessionMetrics struct {
	startedAt      time.Time
	attempts       int
	langs          int
	lockWaitFreeze time.Duration
	freeze         time.Duration
	compile        time.Duration
	lockWaitCommit time.Duration
	commit         time.Duration
}

func (m publishSessionMetrics) retried() bool { return m.attempts > 1 }

func (m publishSessionMetrics) total() time.Duration { return time.Since(m.startedAt) }

const (
	publishOutcomeOK     = "ok"
	publishOutcomeFailed = "failed"
)

func publishOutcomeOf(cause error) string {
	if cause != nil {
		return publishOutcomeFailed
	}
	return publishOutcomeOK
}

func publishSessionFields(instanceID string, m publishSessionMetrics, outcome string) []any {
	return []any{
		"instanceId", instanceID,
		"langs", m.langs,
		"attempts", m.attempts,
		"retried", m.retried(),
		"lockWaitFreezeMs", m.lockWaitFreeze.Milliseconds(),
		"freezeMs", m.freeze.Milliseconds(),
		"compileMs", m.compile.Milliseconds(),
		"lockWaitCommitMs", m.lockWaitCommit.Milliseconds(),
		"commitMs", m.commit.Milliseconds(),
		"totalMs", m.total().Milliseconds(),
		"outcome", outcome,
	}
}

func (s *Service) logPublishSession(inst *presentationmodel.InstanceEntity, m publishSessionMetrics, cause error) {
	if s == nil {
		return
	}
	instanceID := ""
	if inst != nil {
		instanceID = inst.ID
	}
	fields := publishSessionFields(instanceID, m, publishOutcomeOf(cause))
	entry := logger.Scene("publication")
	for i := 0; i+1 < len(fields); i += 2 {
		key, _ := fields[i].(string)
		entry = entry.With(key, fields[i+1])
	}
	if cause != nil {
		entry = entry.With("error", cause.Error())
	}
	entry.Info("发布会话分阶段耗时")
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func (s *Service) Rebuild(ctx context.Context, req *presentationdto.RebuildReq) (res *presentationdto.InstanceResp, err error) {
	if req == nil || req.EntityID == "" {
		return nil, presentationenums.Err(presentationenums.ErrInvalidParam)
	}

	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}

	inst, err := s.findByEntityID(ctx, projectID, req.EntityID)
	if err != nil {
		return nil, presentationenums.Err(presentationenums.ErrNotFound)
	}

	switching := strings.TrimSpace(req.TemplateID) != ""
	return s.rebuildInstance(ctx, inst, publishIntent{
		resolveDoc: func(ctx context.Context, fresh *presentationmodel.InstanceEntity) (*contenttemplatecontract.ResolvedTemplate, error) {
			tpl, rerr := s.resolveBoundTemplate(ctx, fresh, req.TemplateID)
			if rerr != nil {
				return nil, rerr
			}

			if !switching {
				tpl = instanceDocumentFor(fresh, tpl)
			}
			return tpl, nil
		},

		trigger: rebuildTrigger(switching),
	})
}

func (s *Service) ListArtifactHashes(ctx context.Context) (hashes []string, err error) {
	return s.m.ListArtifactHashes(ctx)
}

func (s *Service) rebuildInstance(ctx context.Context, inst *presentationmodel.InstanceEntity,
	intent publishIntent) (res *presentationdto.InstanceResp, err error) {
	if _, err = s.publishAllLangs(ctx, inst, intent); err != nil {
		return nil, publishFailedErr(err)
	}
	return s.toResp(ctx, inst)
}

type builtArtifact struct {
	Hash     string
	Loc      publish.Locator
	Manifest publish.Manifest
}

func (s *Service) recordArtifactTx(ctx context.Context, tx *gorm.DB, inst *presentationmodel.InstanceEntity,
	snapID string, built builtArtifact, lang string, version int64, presetID string, now time.Time) (artifactID string, err error) {
	if existing, gerr := s.m.GetArtifactByHashTx(tx, inst.ID, built.Hash); gerr == nil {
		return existing.ID, nil
	} else if !errors.Is(gerr, gorm.ErrRecordNotFound) {
		return "", gerr
	}

	manifestJSON, err := json.Marshal(built.Manifest)
	if err != nil {
		return "", err
	}
	artifactUUID := strings.TrimSpace(presetID)
	if artifactUUID == "" {
		artifactUUID = uuid.NewString()
	}
	e := &presentationmodel.ArtifactEntity{
		ID: artifactUUID, PresentationInstanceID: inst.ID, SnapshotID: snapID,
		Version: version, Lang: strings.TrimSpace(lang), SourceHash: built.Manifest.SourceHash,
		BuildInputHash:   built.Manifest.BuildInputHash,
		ArtifactProvider: "local", ArtifactKey: built.Loc.Key, ArtifactHash: built.Hash,
		CompilerVersion: built.Manifest.CompilerVersion,

		RegistryVersion: builder.RegistryVersion(),
		Manifest:        manifestJSON, PayloadState: "available", Note: "",
		CreatedBy: systemCreator, CreatedAt: now,
	}
	if err = s.m.CreateArtifactTx(tx, e); err != nil {
		return "", err
	}
	return e.ID, nil
}

func (s *Service) persistDependenciesTx(tx *gorm.DB, instanceID, artifactID string, deps []publish.Dependency, now time.Time) error {
	if strings.TrimSpace(instanceID) == "" || strings.TrimSpace(artifactID) == "" {
		return nil
	}
	rows := make([]presentationmodel.DependencyEntity, 0, len(deps))
	seen := map[[2]string]bool{}
	for _, d := range deps {
		kind, key := strings.TrimSpace(d.Kind), strings.TrimSpace(d.Key)
		if kind == "" || key == "" {
			continue
		}
		if seen[[2]string{kind, key}] {
			continue
		}
		seen[[2]string{kind, key}] = true
		row := presentationmodel.DependencyEntity{
			PresentationID: instanceID, ArtifactID: artifactID,
			DependencyKind: kind, DependencyKey: key, LastChecked: now,
		}
		if rev := strings.TrimSpace(d.Revision); rev != "" {
			r := rev
			row.Revision = &r
		}
		rows = append(rows, row)
	}
	return s.m.ReplaceDependenciesTx(tx, artifactID, rows)
}

const (
	pendingReceiptConvergeBatch = 20

	pendingReceiptConvergeMaxBatches = 50

	pendingReceiptStartupMaxBatches = 0

	pendingReceiptConvergeInterval = time.Minute

	pendingReceiptConvergeTimeout = 2 * time.Minute

	pendingReceiptStartupTimeout = 5 * time.Minute
)

var presentationReceiptActions = []string{

	presentationReceiptAction,

	pubcontract.ReceiptActionDeactivate,

	pubcontract.ReceiptActionUpdateURL,
}

func isPresentationReceiptAction(action string) bool {
	for _, known := range presentationReceiptActions {
		if known == action {
			return true
		}
	}
	return false
}

type pendingReceiptConvergeResult struct {
	claimed    int
	completed  int
	rolledBack int
	failed     int
	batches    int
}

func (r pendingReceiptConvergeResult) converged() int { return r.completed + r.rolledBack }

func (s *Service) ConvergePendingReceipts(ctx context.Context) (converged int, err error) {
	res, claimErr, itemErrs := s.convergePendingReceipts(ctx, pendingReceiptConvergeMaxBatches)
	return res.converged(), errors.Join(append([]error{claimErr}, itemErrs...)...)
}

func (s *Service) RecoverPendingPublications(ctx context.Context) (recovered, rolledBack int, err error) {
	res, claimErr, _ := s.convergePendingReceipts(ctx, pendingReceiptStartupMaxBatches)
	return res.completed, res.rolledBack, claimErr
}

func (s *Service) convergePendingReceipts(ctx context.Context, maxBatches int) (res pendingReceiptConvergeResult, claimErr error, itemErrs []error) {

	if s == nil || s.routes == nil || s.publication == nil {
		return res, nil, nil
	}
	start := time.Now()

	s.converging.Add(1)
	defer s.converging.Add(-1)

	touched := make(map[string]string)

	lastBatch := ""
	for batch := 0; maxBatches <= 0 || batch < maxBatches; batch++ {
		if cerr := ctx.Err(); cerr != nil {
			itemErrs = append(itemErrs, cerr)
			break
		}
		items, cerr := s.routes.ClaimPendingReceipts(ctx, &pubcontract.ReceiptsQueryReq{
			SourceType: pubcontract.ReceiptSourcePresentation,
			Actions:    presentationReceiptActions,
			Limit:      pendingReceiptConvergeBatch,
		})
		if cerr != nil {
			claimErr = cerr
			logger.Scene("publication").Error(cerr, "领取未结案的多语言发布回执失败（本轮收敛中止）")
			break
		}
		if len(items) == 0 {
			break
		}

		batchKey := presentationReceiptBatchKey(items)
		if batchKey == lastBatch {
			logger.Scene("publication").With("batch", batch).With("items", len(items)).
				Debug("多语言发布回执收敛：本批与上一批完全相同（本轮无进展，收手等下一轮）")
			break
		}
		lastBatch = batchKey
		res.claimed += len(items)
		res.batches++
		handled, failed := 0, 0
		for _, item := range items {

			if item.SourceType != pubcontract.ReceiptSourcePresentation || !isPresentationReceiptAction(item.Action) {
				continue
			}
			handled++
			var done bool
			var rerr error
			switch item.Action {
			case pubcontract.ReceiptActionUpdateURL:
				done, rerr = s.replayOldPathReceipt(ctx, item)
			case pubcontract.ReceiptActionDeactivate:
				done, rerr = s.recoverDeactivateReceipt(ctx, item)
			default:
				done, rerr = s.recoverOneReceipt(ctx, item)
			}
			if rerr != nil {
				failed++
				logger.Scene("publication").With("receiptId", item.ID).With("action", item.Action).
					With("path", item.Path).Error(rerr, "多语言发布回执收敛失败（保留 pending，下一轮重试）")
				itemErrs = append(itemErrs, fmt.Errorf("回执 %s(%s): %w", item.ID, item.Action, rerr))
				continue
			}
			touched[item.SourceID] = item.ProjectID
			if done {
				res.completed++
			} else {
				res.rolledBack++
			}
		}
		res.failed += failed

		if failed > 0 {
			break
		}

		if handled == 0 {
			break
		}
	}

	for instanceID, projectID := range touched {
		if ctx.Err() != nil {
			break
		}
		s.convergeInstanceBatch(ctx, projectID, instanceID)
	}
	s.lastConvergeAt.Store(time.Now().Unix())
	s.logPendingReceiptConverge(res, time.Since(start))
	return res, claimErr, itemErrs
}

func presentationReceiptBatchKey(items []pubcontract.PendingReceiptResp) string {
	if len(items) == 0 {
		return ""
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	sort.Strings(ids)
	return strings.Join(ids, ",")
}

func (s *Service) logPendingReceiptConverge(res pendingReceiptConvergeResult, cost time.Duration) {
	entry := logger.Scene("publication").
		With("sourceType", pubcontract.ReceiptSourcePresentation).
		With("claimed", res.claimed).
		With("converged", res.converged()).
		With("completed", res.completed).
		With("rolledBack", res.rolledBack).
		With("failed", res.failed).
		With("batches", res.batches).
		With("costMs", cost.Milliseconds())
	switch {
	case res.failed > 0:
		entry.Warn("多语言发布回执收敛完成（有仍未成功的行，保留 pending 等待下一轮）")
	case res.claimed > 0:
		entry.Info("多语言发布回执收敛完成")
	default:
		entry.Debug("多语言发布回执收敛空转（没有未结案的回执）")
	}
}

func (s *Service) NotifyPendingReceipt() {
	if s == nil || s.convergeWake == nil || s.converging.Load() > 0 {
		return
	}
	select {
	case s.convergeWake <- struct{}{}:
	default:
	}
}

func (s *Service) PendingReceiptStatus(ctx context.Context) (pending int64, lastConvergeAt time.Time, err error) {
	if s == nil || s.routes == nil {
		return 0, time.Time{}, nil
	}
	pending, err = s.routes.CountPendingReceipts(ctx, &pubcontract.ReceiptsQueryReq{
		SourceType: pubcontract.ReceiptSourcePresentation,
		Actions:    presentationReceiptActions,
	})
	if err != nil {
		return 0, time.Time{}, err
	}
	if unix := s.lastConvergeAt.Load(); unix > 0 {
		lastConvergeAt = time.Unix(unix, 0).UTC()
	}
	return pending, lastConvergeAt, nil
}

func (s *Service) PendingReceiptBacklog(ctx context.Context) (pending int64, oldestAge time.Duration, lastConvergeAt time.Time, err error) {
	if s == nil || s.routes == nil {
		return 0, 0, time.Time{}, nil
	}
	pending, err = s.routes.CountPendingReceipts(ctx, &pubcontract.ReceiptsQueryReq{
		SourceType: pubcontract.ReceiptSourcePresentation,
		Actions:    presentationReceiptActions,
	})
	if err != nil {
		return 0, 0, time.Time{}, err
	}
	if unix := s.lastConvergeAt.Load(); unix > 0 {
		lastConvergeAt = time.Unix(unix, 0).UTC()
	}
	if pending <= 0 {
		return 0, 0, lastConvergeAt, nil
	}
	return pending, s.oldestPendingReceiptAge(ctx), lastConvergeAt, nil
}

func (s *Service) oldestPendingReceiptAge(ctx context.Context) time.Duration {
	items, err := s.routes.ListPendingReceipts(ctx)
	if err != nil {
		logger.Scene("publication").With("err", err).
			Warn("读取最老未结案的多语言发布回执失败（积压条数仍可用，年龄暂缺）")
		return 0
	}
	for _, item := range items {

		if item.SourceType != pubcontract.ReceiptSourcePresentation || !isPresentationReceiptAction(item.Action) {
			continue
		}
		ts, perr := time.Parse(time.RFC3339, item.CreatedAt)
		if perr != nil || ts.IsZero() {
			return 0
		}
		if age := time.Since(ts); age > 0 {
			return age
		}
		return 0
	}
	return 0
}

func StartPendingReceiptConvergenceScheduler(svc *Service) {
	if utils.IsTestProcess() {
		return
	}
	startPendingReceiptConvergenceScheduler(svc, pendingReceiptConvergeInterval)
}

func StartPendingReceiptConvergenceSchedulerWithInterval(svc *Service, interval time.Duration) {
	startPendingReceiptConvergenceScheduler(svc, interval)
}

func startPendingReceiptConvergenceScheduler(svc *Service, interval time.Duration) {
	if svc == nil {
		return
	}
	if interval <= 0 {
		interval = pendingReceiptConvergeInterval
	}
	go func() {

		startupCtx, cancelStartup := context.WithTimeout(context.Background(), pendingReceiptStartupTimeout)
		_, _, _ = svc.RecoverPendingPublications(startupCtx)
		cancelStartup()

		converge := func() {
			ctx, cancel := context.WithTimeout(context.Background(), pendingReceiptConvergeTimeout)
			defer cancel()
			_, _ = svc.ConvergePendingReceipts(ctx)
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				converge()
			case <-svc.convergeWake:
				converge()
			}
		}
	}()
}

const (
	presentationReceiptAction = pubcontract.ReceiptActionSwitchActive

	artifactLocatorKeyPrefix = "artifacts/"
)

func (s *Service) beginPublishReceipt(ctx context.Context, in beginReceiptInput) (receiptID string, err error) {
	if s.routes == nil {
		return "", nil
	}
	inst := in.inst
	id, berr := s.routes.BeginPublishReceipt(ctx, &pubcontract.BeginPublishReceiptReq{
		ProjectID:      inst.ProjectID,
		Path:           in.path,
		PresentationID: inst.ID,
		FromArtifactID: strings.TrimSpace(in.fromArtifactID),
		ToArtifactID:   in.toArtifactID,
		Lang:           in.lang,
		Action:         in.action,
		OldPath:        strings.TrimSpace(in.oldPath),
		OldPaths:       normalizePathList(in.oldPaths),
		Redirect:       in.redirect,

		CapturedInputVersion: in.capturedInputVersion,

		SuccessfulReceipts: in.successfulReceipts,

		FromReceiptID: in.fromReceiptID,
	})
	if berr != nil {
		logger.Scene("publication").With("instanceId", inst.ID).With("url", in.path).
			Error(berr, "多语言发布回执登记失败（不切换访问面）")
		return "", berr
	}
	return id, nil
}

func (s *Service) completePublishReceipt(ctx context.Context, receiptID string) error {
	if s == nil || s.routes == nil || strings.TrimSpace(receiptID) == "" {
		return nil
	}
	return s.routes.CompletePublishReceipt(ctx, receiptID)
}

func (s *Service) abortPublishReceipt(ctx context.Context, receiptID, reason string) {
	if s == nil || s.routes == nil || strings.TrimSpace(receiptID) == "" {
		return
	}
	if err := s.routes.AbortPublishReceipt(ctx, receiptID); err != nil {
		logger.Scene("publication").With("receiptId", receiptID).
			Error(err, "多语言发布回执结案失败（"+reason+"）")
	}
}

func (s *Service) convergeInstanceBatch(ctx context.Context, projectID, instanceID string) {
	inst, ierr := s.locateInstanceForReceipt(ctx, projectID, instanceID)
	if ierr != nil {
		return
	}
	if s.batchConverged(ctx, inst) {
		return
	}
	s.markBatchUnconverged(ctx, inst, errors.New("多语言发布批次未收敛（启动恢复重跑一次）"))
	if s.buildQueue != nil {
		if qerr := s.buildQueue.EnqueuePresentationBuild(ctx, instanceID, projectID); qerr != nil {
			logger.Scene("publication").With("instanceId", instanceID).
				Error(qerr, "启动恢复入队重建失败（实例保持 stale）")
		}
		return
	}
	if rerr := s.RebuildInstance(ctx, instanceID); rerr != nil {
		logger.Scene("publication").With("instanceId", instanceID).
			Error(rerr, "启动恢复重建失败（实例保持 stale）")
		return
	}
	logger.Scene("publication").With("instanceId", instanceID).
		Info("启动恢复：多语言发布批次已收敛")
}

func (s *Service) batchConverged(ctx context.Context, inst *presentationmodel.InstanceEntity) bool {

	langs, lerr := s.publishLangsOf(ctx, inst.ProjectID)
	if lerr != nil {
		logger.Scene("publication").With("instanceId", inst.ID).
			Error(lerr, "读取站点语言清单失败，跳过本轮批次收敛判定（不宣称收敛，下轮重来）")
		return true
	}
	if len(langs) == 0 {
		return true
	}
	pubs, perr := s.m.ListPublications(ctx, inst.ID)
	if perr != nil {
		logger.Scene("publication").With("instanceId", inst.ID).
			Error(perr, "读取语言账本失败，跳过批次收敛判定")
		return true
	}
	byLang := make(map[string]string, len(pubs))
	for _, pub := range pubs {
		if pub.ArtifactID != nil && strings.TrimSpace(*pub.ArtifactID) != "" {
			byLang[pub.Lang] = *pub.ArtifactID
		}
	}
	defaultLang := pipeline.DefaultLocale(ctx, s.project, inst.ProjectID)
	primary := byLang[defaultLang]
	if primary == "" {
		primary = byLang[langs[0]]
	}
	for _, lang := range langs {
		if byLang[lang] == "" {
			return false
		}
	}
	if primary == "" || inst.ActiveArtifactID == nil || *inst.ActiveArtifactID != primary {
		return false
	}
	return !inst.Stale
}

func (s *Service) recoverOneReceipt(ctx context.Context, item pubcontract.PendingReceiptResp) (bool, error) {
	inst, ierr := s.locateInstanceForReceipt(ctx, item.ProjectID, item.SourceID)
	if ierr != nil {
		s.abortPublishReceipt(ctx, item.ID, "实例不存在")
		return false, nil
	}
	expectedHash, herr := s.artifactHashByID(ctx, item.ToArtifactID)
	if herr != nil || expectedHash == "" {
		s.abortPublishReceipt(ctx, item.ID, "回执记录的产物行不存在")
		return false, nil
	}

	if !s.receiptPathStillCurrent(ctx, inst, item) {
		logger.Scene("publication").With("instanceId", inst.ID).With("url", item.Path).
			Warn("未结案的多语言发布回执路径已过期，标记回滚")
		s.abortPublishReceipt(ctx, item.ID, "回执路径已非该语言的当前访问路径")
		return false, nil
	}

	actualHash := s.activeArtifactHash(item.Path)
	if actualHash == "" || actualHash != expectedHash {

		logger.Scene("publication").With("instanceId", inst.ID).With("url", item.Path).
			With("actual", actualHash).With("expected", expectedHash).
			Warn("未结案的多语言发布回执判定为「未生效」，标记回滚")
		s.abortPublishReceipt(ctx, item.ID, "访问面未指向本次产物")
		return false, nil
	}

	var converged bool
	txErr := s.m.Transaction(ctx, func(tx *gorm.DB) error {
		locked, lerr := s.m.LockInstanceForActivationTx(ctx, tx, inst.ProjectID, inst.ID)
		if lerr != nil {
			return lerr
		}
		*inst = *locked

		captured := capturedInputVersionOf(item)
		versionMoved := captured > 0 && captured != locked.InputVersion
		superseded := s.langLedgerSupersededTx(ctx, tx, inst.ID, item)
		if versionMoved || superseded {

			return s.convergeSupersededAttemptTx(ctx, tx, inst, item, expectedHash, versionMoved, superseded)
		}

		if rerr := s.registerRouteTx(ctx, tx, inst, item.Path, item.ToArtifactID); rerr != nil {
			return rerr
		}

		if merr := s.m.MarkPublishedLangTx(tx, presentationmodel.PublicationRecord{
			PresentationID: inst.ID, Lang: strings.TrimSpace(item.Lang), ActivePath: item.Path,
			ArtifactID: item.ToArtifactID, ArtifactHash: expectedHash,
			ReceiptID: strings.TrimSpace(item.ID), PublishedAt: time.Now().UTC(),
		}); merr != nil {
			return merr
		}
		converged = true
		return nil
	})
	if txErr != nil {

		return false, txErr
	}
	if cerr := s.completePublishReceipt(ctx, item.ID); cerr != nil {
		return false, cerr
	}
	if converged {
		logger.Scene("publication").With("instanceId", inst.ID).With("url", item.Path).
			Info("多语言发布在失败/崩溃前已切换访问面，已补齐路由登记与语言账本")
	}
	return true, nil
}

func (s *Service) replayOldPathReceipt(ctx context.Context, item pubcontract.PendingReceiptResp) (bool, error) {
	return s.runOldPathDisposal(ctx, item, "", nil)
}

type oldPathOutcome int

const (
	oldPathRetry oldPathOutcome = iota

	oldPathAbandon

	oldPathExecute
)

func (s *Service) runOldPathDisposal(ctx context.Context, item pubcontract.PendingReceiptResp,
	artifactID string, fastPathReceipts map[string]string) (bool, error) {
	if s == nil || s.publication == nil {
		return false, nil
	}

	newPath := strings.TrimSpace(item.Path)
	oldPaths := receiptOldPaths(item)
	if len(oldPaths) == 0 || newPath == "" {
		s.abortPublishReceipt(ctx, item.ID, "旧路径处置回执缺少旧/新路径事实")
		return false, nil
	}
	inst, ierr := s.locateInstanceForReceipt(ctx, item.ProjectID, item.SourceID)
	if ierr != nil {
		s.abortPublishReceipt(ctx, item.ID, "实例不存在")
		return false, nil
	}

	links := fastPathReceipts
	if len(links) == 0 {
		links = item.SuccessfulReceipts
	}
	if len(links) == 0 {
		logger.Scene("publication").With("instanceId", inst.ID).With("receiptId", item.ID).
			Warn("旧路径处置回执没有关联的成功回执，无法证明激活成功（保留 pending 不处置）")
		return false, errOldPathWithoutSuccessEvidence
	}

	var (
		outcome  = oldPathRetry
		earlyErr error
	)
	txErr := s.m.Transaction(ctx, func(tx *gorm.DB) error {
		locked, lerr := s.m.LockInstanceForActivationTx(ctx, tx, inst.ProjectID, inst.ID)
		if lerr != nil {
			if errors.Is(lerr, gorm.ErrRecordNotFound) {

				outcome = oldPathAbandon
				return nil
			}
			return lerr
		}

		owned := make(map[string]string, len(links))
		for lang := range links {
			got, gerr := s.m.PublishedReceiptIDTx(tx, locked.ID, lang)
			if gerr != nil {
				return gerr
			}
			owned[lang] = got
		}
		verdict, verr := s.routes.ReceiptAttemptVerdictTx(ctx, tx, links, owned)
		if verr != nil {
			return verr
		}
		switch verdict {
		case pubcontract.ReceiptAttemptSucceeded:

		case pubcontract.ReceiptAttemptFailed:

			outcome = oldPathAbandon
			return nil
		default:

			outcome = oldPathRetry
			return nil
		}

		idVerdict, yerr := s.receiptIdentityVerdictTx(ctx, tx, locked.ID, item, links)
		if yerr != nil {
			return yerr
		}
		switch idVerdict {
		case receiptIdentityOwned, receiptIdentityCurrent:

		case receiptIdentitySuperseded:
			logger.Scene("publication").With("instanceId", locked.ID).With("receiptId", item.ID).
				Warn("旧路径处置意图已被后来的成功 attempt 取代，结案不处置")
			outcome = oldPathAbandon
			return nil
		default:

			logger.Scene("publication").With("instanceId", locked.ID).With("receiptId", item.ID).
				Warn("旧路径处置意图的身份事实无法判定（保留 pending）")
			outcome = oldPathRetry
			return nil
		}

		wantLogical := pipeline.LogicalPathOf(ctx, s.project, locked.ProjectID, newPath)
		current := pipeline.LogicalPathOf(ctx, s.project, locked.ProjectID, locked.URLPath)
		if current != wantLogical && current != newPath {
			logger.Scene("publication").With("instanceId", locked.ID).With("wanted", newPath).
				With("current", current).
				Warn("旧路径处置回执的目标路径已过期，结案不再重放")
			outcome = oldPathAbandon
			return nil
		}

		if derr := s.disposeOldPathsTx(ctx, tx, locked, oldPaths, newPath,
			firstNonEmpty(artifactID, strings.TrimSpace(item.ToArtifactID)), item.Redirect); derr != nil {
			earlyErr = derr
			outcome = oldPathRetry
			return nil
		}
		outcome = oldPathExecute
		return nil
	})
	if txErr != nil {
		return false, txErr
	}
	switch outcome {
	case oldPathAbandon:
		s.abortPublishReceipt(ctx, item.ID, "关联的激活回执未成功或目标已过期，旧路径处置不再执行")
		return false, nil
	case oldPathExecute:
		if cerr := s.completePublishReceipt(ctx, item.ID); cerr != nil {
			return false, cerr
		}
		logger.Scene("publication").With("instanceId", inst.ID).With("oldPaths", oldPaths).
			With("newPath", newPath).With("redirect", item.Redirect).
			Info("旧路径处置已在实体锁内执行并结案")
		return true, nil
	}
	if earlyErr != nil {
		logger.Scene("publication").With("instanceId", inst.ID).With("oldPaths", oldPaths).
			With("receiptId", item.ID).Error(earlyErr, "旧路径处置失败（保留 pending，下一轮重试）")
		return false, earlyErr
	}
	return false, errOldPathWithoutSuccessEvidence
}

type receiptIdentityVerdict int

const (
	receiptIdentityUnknown receiptIdentityVerdict = iota

	receiptIdentityOwned

	receiptIdentityCurrent

	receiptIdentitySuperseded
)

func (s *Service) receiptIdentityVerdictTx(ctx context.Context, tx *gorm.DB, instanceID string,
	item pubcontract.PendingReceiptResp, links map[string]string) (receiptIdentityVerdict, error) {
	langs := make([]string, 0, len(links))
	for lang := range links {
		langs = append(langs, lang)
	}
	sort.Strings(langs)
	if len(langs) == 0 {
		return receiptIdentityUnknown, nil
	}
	own := strings.TrimSpace(item.ID)
	fromRaw := item.FromReceiptID
	var from int64 = -1
	if v := strings.TrimSpace(fromRaw); v != "" {
		parsed, perr := strconv.ParseInt(v, 10, 64)
		if perr != nil || parsed < 0 {

			return receiptIdentityUnknown, nil
		}
		from = parsed
	}
	sawOwn, sawFrom, sawOther := false, false, false
	for _, lang := range langs {
		ledger, lerr := s.m.PublishedReceiptIDTx(tx, instanceID, lang)
		if lerr != nil {
			return receiptIdentityUnknown, lerr
		}
		ledger = strings.TrimSpace(ledger)
		switch {
		case ledger == "":

			continue
		case own != "" && ledger == own:
			sawOwn = true
		case links[lang] != "" && ledger == strings.TrimSpace(links[lang]):

			sawOwn = true
		case from >= 0 && ledger == strconv.FormatInt(from, 10):
			sawFrom = true
		case from == -1:

			ledgerArtifact, aerr := s.m.PublishedArtifactIDTx(tx, instanceID, lang)
			if aerr != nil {
				return receiptIdentityUnknown, aerr
			}
			if strings.TrimSpace(ledgerArtifact) != "" &&
				strings.TrimSpace(ledgerArtifact) != strings.TrimSpace(item.ToArtifactID) {
				sawOther = true
			} else {
				sawFrom = true
			}
		default:
			sawOther = true
		}
	}
	switch {
	case sawOther:

		return receiptIdentitySuperseded, nil
	case sawOwn:
		return receiptIdentityOwned, nil
	default:
		_ = sawFrom
		return receiptIdentityCurrent, nil
	}
}

func receiptOldPaths(item pubcontract.PendingReceiptResp) []string {
	seen := make(map[string]bool, len(item.OldPaths)+1)
	out := make([]string, 0, len(item.OldPaths)+1)
	for _, p := range item.OldPaths {
		v := strings.TrimSpace(p)
		if v == "" || seen[v] || strings.EqualFold(v, strings.TrimSpace(item.Path)) {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	if v := strings.TrimSpace(item.OldPath); v != "" && !seen[v] &&
		!strings.EqualFold(v, strings.TrimSpace(item.Path)) {
		out = append(out, v)
	}
	return out
}

func normalizePathList(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		v := strings.TrimSpace(raw)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func firstOldPath(plan []oldPathIntent) string {
	if len(plan) == 0 {
		return ""
	}
	return plan[0].oldPath
}

func oldPathList(plan []oldPathIntent) []string {
	out := make([]string, 0, len(plan))
	for _, p := range plan {
		out = append(out, p.oldPath)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func (s *Service) disposeOldPathsTx(ctx context.Context, tx *gorm.DB,
	inst *presentationmodel.InstanceEntity, oldPaths []string, newPath, artifactID string, withRedirect bool) error {
	return s.withPaths(oldPaths, func() error {
		for _, oldPath := range oldPaths {
			if oerr := s.reverifyOldPathOwnershipTx(ctx, tx, inst, oldPath); oerr != nil {
				return oerr
			}
			if derr := s.disposeOldPathTx(ctx, tx, inst, oldPath, newPath, artifactID, withRedirect); derr != nil {
				return derr
			}
		}
		return nil
	})
}

func (s *Service) reverifyOldPathOwnershipTx(ctx context.Context, tx *gorm.DB,
	inst *presentationmodel.InstanceEntity, oldPath string) error {
	if s.routes != nil {
		owner, oerr := s.routes.RouteOwnerTx(ctx, tx, inst.ProjectID, oldPath)
		if oerr != nil && !errors.Is(oerr, gorm.ErrRecordNotFound) {
			return oerr
		}
		if oerr == nil && owner != nil {
			if owner.ProjectID != inst.ProjectID {
				return fmt.Errorf("%w: 旧路径 %s 已属于其它工程", errOldPathFSTargetForeign, oldPath)
			}
			if owner.PresentationID == nil || *owner.PresentationID != inst.ID {
				return fmt.Errorf("%w: 旧路径 %s 的路由占用已转给其它实体", errOldPathFSTargetForeign, oldPath)
			}
		}
	}
	return nil
}

func (s *Service) disposeOldPathTx(ctx context.Context, tx *gorm.DB,
	inst *presentationmodel.InstanceEntity, oldPath, newPath, artifactID string, withRedirect bool) error {
	if withRedirect {
		ra, rerr := publish.NewRedirectArtifact(newPath, 301)
		if rerr != nil {
			return rerr
		}
		loc, perr := s.store.PutRedirect(ra)
		if perr != nil {
			return perr
		}
		if s.routes != nil {
			if _, derr := s.routes.RedirectTx(ctx, tx, &pubcontract.RedirectReq{
				ProjectID: inst.ProjectID, OldPath: oldPath,
				PresentationID: inst.ID, ArtifactID: artifactID,
			}); derr != nil {
				return derr
			}
		}
		if oerr := s.oldPathFSTargetAllowsRedirect(oldPath, newPath); oerr != nil {
			return oerr
		}
		return s.publication.Activate(oldPath, loc)
	}
	if s.routes != nil {
		if derr := s.routes.DeactivateTx(ctx, tx, &pubcontract.DeactivateReq{
			ProjectID: inst.ProjectID, Path: oldPath, PresentationID: inst.ID,
		}); derr != nil {
			return derr
		}
	}
	if oerr := s.oldPathFSTargetAllowsDeactivate(oldPath); oerr != nil {
		return oerr
	}
	return s.publication.Deactivate(oldPath)
}

var errOldPathFSTargetForeign = errors.New("旧路径的访问面链接已被其他产物接管")

func (s *Service) oldPathFSTargetAllowsDeactivate(oldPath string) error {
	if s.publication == nil {
		return nil
	}
	st, ierr := s.publication.Inspect(oldPath)
	if ierr != nil {

		return ierr
	}
	if st == nil || st.Kind == publish.PublicationNone {
		return nil
	}

	if st.Kind == publish.PublicationPage || st.Kind == publish.PublicationRedirect {
		return nil
	}
	logger.Scene("publication").With("oldPath", oldPath).With("kind", st.Kind).
		Warn("旧路径的访问面占位无法归属本实例，跳过下线（保留 pending 重放）")
	return errOldPathFSTargetForeign
}

func (s *Service) oldPathFSTargetAllowsRedirect(oldPath, newPath string) error {
	if s.publication == nil {
		return nil
	}
	st, ierr := s.publication.Inspect(oldPath)
	if ierr != nil {
		return ierr
	}
	if st == nil || st.Kind == publish.PublicationNone {
		return nil
	}

	if st.Kind != publish.PublicationRedirect {
		return nil
	}

	if st.Redirect != nil && strings.TrimSpace(st.Redirect.TargetPath) == strings.TrimSpace(newPath) {
		return nil
	}
	logger.Scene("publication").With("oldPath", oldPath).With("newPath", newPath).
		With("kind", st.Kind).
		Warn("旧路径的 301 目标已指向别处，跳过覆盖（保留 pending 重放）")
	return errOldPathFSTargetForeign
}

var errOldPathWithoutSuccessEvidence = errors.New("旧路径处置回执缺少关联的成功回执证据")

func (s *Service) convergeSupersededAttemptTx(ctx context.Context, tx *gorm.DB,
	inst *presentationmodel.InstanceEntity, item pubcontract.PendingReceiptResp,
	expectedHash string, versionMoved, superseded bool) error {

	if lang := strings.TrimSpace(item.Lang); lang != "" {

		if merr := s.m.MarkPublishedLangTx(tx, presentationmodel.PublicationRecord{
			PresentationID: inst.ID, Lang: lang, ActivePath: item.Path,
			ArtifactID: item.ToArtifactID, ArtifactHash: expectedHash,
			ReceiptID: strings.TrimSpace(item.ID), PublishedAt: time.Now().UTC(),
		}); merr != nil {
			return merr
		}
	}

	if _, serr := s.m.MarkStaleWithoutVersionTx(tx, inst.ProjectID, []string{inst.ID}, time.Now().UTC()); serr != nil {
		return serr
	}
	logger.Scene("publication").With("instanceId", inst.ID).With("url", item.Path).
		With("versionMoved", versionMoved).With("superseded", superseded).
		Warn("未结案的多语言发布回执被同版本的新 attempt 或更新的输入取代；链接仍指向本产物，已对齐账本并安排重建")
	return nil
}

func (s *Service) langLedgerSupersededTx(ctx context.Context, tx *gorm.DB,
	instanceID string, item pubcontract.PendingReceiptResp) bool {
	lang := strings.TrimSpace(item.Lang)
	if lang == "" || strings.TrimSpace(item.ToArtifactID) == "" {
		return false
	}

	ledgerReceiptID, rerr := s.m.PublishedReceiptIDTx(tx, instanceID, lang)
	if rerr != nil {
		return false
	}
	if ledgerReceiptID != "" {
		return ledgerReceiptID != strings.TrimSpace(item.ID)
	}
	artifactID, err := s.m.PublishedArtifactIDTx(tx, instanceID, lang)
	if err != nil || artifactID == "" {
		return false
	}
	return artifactID != strings.TrimSpace(item.ToArtifactID)
}

func capturedInputVersionOf(item pubcontract.PendingReceiptResp) int64 {
	if v := item.CapturedInputVersion; v > 0 {
		return v
	}
	return 0
}

func (s *Service) artifactHashByID(ctx context.Context, artifactID string) (string, error) {
	id := strings.TrimSpace(artifactID)
	if id == "" {
		return "", nil
	}
	hash, err := s.m.GetArtifactHash(ctx, id)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(hash), nil
}

func (s *Service) activeArtifactHash(path string) string {
	if s == nil || s.publication == nil {
		return ""
	}
	state, err := s.publication.Inspect(path)
	if err != nil || state == nil || state.Locator == nil {
		return ""
	}

	if state.Kind != publish.PublicationPage {
		return ""
	}
	return strings.TrimPrefix(state.Locator.Key, artifactLocatorKeyPrefix)
}

func (s *Service) receiptPathStillCurrent(ctx context.Context, inst *presentationmodel.InstanceEntity,
	item pubcontract.PendingReceiptResp) bool {
	lang := strings.TrimSpace(item.Lang)
	if lang == "" {
		return true
	}
	rule := pipeline.LangURLRuleForProject(ctx, s.project, inst.ProjectID)
	path, perr := pipeline.SitePath(rule, lang, s.instanceLogicalPath(ctx, inst))
	if perr != nil {
		return false
	}
	return path == item.Path
}

func (s *Service) locateInstanceForReceipt(ctx context.Context, projectID, instanceID string) (*presentationmodel.InstanceEntity, error) {
	if id := strings.TrimSpace(projectID); id != "" {
		inst, err := s.m.GetInstance(ctx, id, instanceID)
		if err == nil {
			return inst, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	return s.findOneInstanceAnyProject(ctx, instanceID)
}

type deactivationIntent struct {
	ReceiptID     string
	ProjectID     string
	InstanceID    string
	Paths         []string
	PathArtifacts map[string]pubcontract.ReceiptPathIdentity
}

func (s *Service) pathIdentities(ctx context.Context, inst *presentationmodel.InstanceEntity,
	paths []string) map[string]pubcontract.ReceiptPathIdentity {
	out := make(map[string]pubcontract.ReceiptPathIdentity, len(paths))
	pubs, perr := s.m.ListPublicationsForInstance(ctx, inst.ID)
	if perr != nil {
		logger.Scene("build").With("instanceId", inst.ID).
			Warn("读取实例语言账本失败，下线意图的路径归属按链接事实记录: " + perr.Error())
		pubs = nil
	}
	for _, p := range paths {
		key := strings.TrimSpace(p)
		if key == "" {
			continue
		}
		var id pubcontract.ReceiptPathIdentity
		for _, pub := range pubs {
			if strings.TrimSpace(pub.ActivePath) != key {
				continue
			}
			if pub.ArtifactID != nil {
				id.ArtifactID = strings.TrimSpace(*pub.ArtifactID)
			}
			if pub.ReceiptID != nil {
				id.ReceiptID = strings.TrimSpace(*pub.ReceiptID)
			}
			break
		}
		if s.publication != nil {
			if st, ierr := s.publication.Inspect(key); ierr == nil && st != nil && st.Locator != nil {
				id.Locator = strings.TrimSpace(st.Locator.Provider + ":" + st.Locator.Key)
			}
		}
		if id.ArtifactID != "" || id.ReceiptID != "" || id.Locator != "" {
			out[key] = id
		}
	}
	return out
}

func (s *Service) registerDeactivationIntent(ctx context.Context, inst *presentationmodel.InstanceEntity,
	paths []string) (*deactivationIntent, error) {
	if s.routes == nil {
		return nil, errors.New("下线意图登记端口未装配")
	}

	if strings.TrimSpace(inst.ProjectID) == "" || strings.TrimSpace(inst.ID) == "" {
		return nil, errors.New("下线意图缺少实例归属事实")
	}
	ids := s.pathIdentities(ctx, inst, paths)
	id, rerr := s.routes.BeginPublishReceipt(ctx, &pubcontract.BeginPublishReceiptReq{
		ProjectID:      inst.ProjectID,
		Path:           strings.TrimSpace(inst.URLPath),
		PresentationID: inst.ID,
		Action:         pubcontract.ReceiptActionDeactivate,
		OldPaths:       append([]string(nil), paths...),
		OldPath:        firstPath(paths),
		PathArtifacts:  ids,
	})
	if rerr != nil {
		return nil, fmt.Errorf("登记下线意图失败（未改动访问面）: %w", rerr)
	}
	return &deactivationIntent{
		ReceiptID: id, ProjectID: inst.ProjectID, InstanceID: inst.ID,
		Paths: append([]string(nil), paths...), PathArtifacts: ids,
	}, nil
}

func firstPath(paths []string) string {
	for _, p := range paths {
		if v := strings.TrimSpace(p); v != "" {
			return v
		}
	}
	return ""
}

func deactivationPathsUnchanged(registered []string, current []string) error {
	want := make(map[string]struct{}, len(registered))
	for _, p := range normalizePathList(registered) {
		want[p] = struct{}{}
	}
	got := make(map[string]struct{}, len(current))
	for _, p := range normalizePathList(current) {
		got[p] = struct{}{}
	}
	if len(want) != len(got) {
		return fmt.Errorf("%w: 登记的下线路径集合与当前不一致", errPublishStateMoved)
	}
	for p := range want {
		if _, ok := got[p]; !ok {
			return fmt.Errorf("%w: 登记的下线路径集合与当前不一致", errPublishStateMoved)
		}
	}
	return nil
}

func (s *Service) completeDeactivationIntent(ctx context.Context,
	inst *presentationmodel.InstanceEntity, intent *deactivationIntent) error {
	if intent == nil {
		return nil
	}
	if len(intent.Paths) == 0 {
		return s.completePublishReceipt(ctx, intent.ReceiptID)
	}

	executed, werr := s.withdrawDeactivatedLinksLocked(ctx, inst.ProjectID, inst.ID, intent.ReceiptID)
	if werr != nil {
		s.lastOldPathPending.Store(1)
		s.NotifyPendingReceipt()
		return fmt.Errorf("实例已删除，但访问面链接下线失败（回执 %s 保持 pending 待重放）: %w",
			intent.ReceiptID, werr)
	}
	if !executed {
		return nil
	}
	return s.completePublishReceipt(ctx, intent.ReceiptID)
}

func (s *Service) ensureDeactivationPathsUnchanged(ctx context.Context,
	inst *presentationmodel.InstanceEntity, registered []string) error {
	return deactivationPathsUnchanged(registered, s.instanceActivePaths(ctx, inst))
}

func (s *Service) withdrawDeactivatedLinks(ctx context.Context, tx *gorm.DB, projectID, instanceID string,
	ids map[string]pubcontract.ReceiptPathIdentity, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	return s.withPaths(paths, func() error {
		for _, p := range paths {
			if strings.TrimSpace(p) == "" {
				continue
			}
			if oerr := s.verifyDeactivatedPathOwner(ctx, tx, projectID, instanceID, p, ids[p]); oerr != nil {
				return oerr
			}
			if derr := s.publication.Deactivate(p); derr != nil {
				return fmt.Errorf("撤掉访问路径 %s 失败: %w", p, derr)
			}
		}
		return nil
	})
}

func (s *Service) verifyDeactivatedPathOwner(ctx context.Context, tx *gorm.DB, projectID, instanceID, path string,
	registered pubcontract.ReceiptPathIdentity) error {
	if s.routes != nil {

		owner, oerr := s.routes.RouteOwnerTx(ctx, tx, projectID, path)
		if oerr != nil && !errors.Is(oerr, gorm.ErrRecordNotFound) {
			return oerr
		}
		if oerr == nil && owner != nil {
			if owner.ProjectID != projectID {
				return fmt.Errorf("%w: 路径 %s 已属于其它工程", errOldPathFSTargetForeign, path)
			}
			if owner.PresentationID == nil || *owner.PresentationID != instanceID {
				return fmt.Errorf("%w: 路径 %s 的路由占用已转给其它实体", errOldPathFSTargetForeign, path)
			}
		}
	}
	if s.publication == nil || registered.Locator == "" {
		return nil
	}
	st, ierr := s.publication.Inspect(path)
	if ierr != nil {
		return ierr
	}
	if st == nil || st.Kind == publish.PublicationNone || st.Locator == nil {

		return nil
	}
	if current := strings.TrimSpace(st.Locator.Provider + ":" + st.Locator.Key); current != registered.Locator {
		return fmt.Errorf("%w: 路径 %s 的访问面链接已指向别的产物", errOldPathFSTargetForeign, path)
	}
	return nil
}

func (s *Service) recoverDeactivateReceipt(ctx context.Context, item pubcontract.PendingReceiptResp) (bool, error) {
	if s == nil || s.publication == nil || s.routes == nil {
		return false, nil
	}
	receiptID, perr := strconv.ParseInt(strings.TrimSpace(item.ID), 10, 64)
	if perr != nil || receiptID <= 0 {
		return false, fmt.Errorf("下线回执 id 非法: %q", item.ID)
	}
	projectID := strings.TrimSpace(item.ProjectID)
	if projectID == "" || item.SourceID == "" {

		s.abortPublishReceipt(ctx, item.ID, "下线意图缺少工程/实例事实")
		return false, nil
	}

	lock := s.lockInstance("presentation", item.SourceID)
	lock.Lock()
	defer lock.Unlock()

	var (
		abandon bool
		done    bool
	)
	txErr := s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
		if aerr := s.lockPresentationEntityOrAdvisory(ctx, tx, projectID, item.SourceID); aerr != nil {
			return aerr
		}
		row, lerr := s.routes.LoadDeactivationReceiptTx(ctx, tx,
			pubcontract.ReceiptOwner{SourceType: pubcontract.ReceiptSourcePresentation, SourceID: item.SourceID},
			projectID, item.ID)
		if lerr != nil {
			if errors.Is(lerr, gorm.ErrRecordNotFound) {
				abandon = true
				return nil
			}
			return lerr
		}
		if row.ReceiptState != "pending" {

			abandon = true
			return nil
		}
		if !deactivationBusinessCommitted(row.ReceiptData) {

			return nil
		}
		done = true
		return nil
	})
	if txErr != nil {

		return false, txErr
	}
	if abandon {
		s.abortPublishReceipt(ctx, item.ID, "下线意图对应的业务变更未提交（或已被并发结案），链接保持原样")
		return false, nil
	}
	if done {

		executed, werr := s.withdrawDeactivatedLinksLocked(ctx, projectID, item.SourceID, item.ID)
		if werr != nil {
			return false, werr
		}
		if !executed {

			return false, nil
		}
		if cerr := s.completePublishReceipt(ctx, item.ID); cerr != nil {
			return false, cerr
		}
		return true, nil
	}
	return false, nil
}

func (s *Service) withdrawDeactivatedLinksLocked(ctx context.Context, projectID, instanceID,
	receiptID string) (executed bool, err error) {
	txErr := s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
		if aerr := s.lockPresentationEntityOrAdvisory(ctx, tx, projectID, instanceID); aerr != nil {
			return aerr
		}
		row, lerr := s.routes.LoadDeactivationReceiptTx(ctx, tx,
			pubcontract.ReceiptOwner{SourceType: pubcontract.ReceiptSourcePresentation, SourceID: instanceID},
			projectID, receiptID)
		if lerr != nil {
			return lerr
		}
		if row.ReceiptState != "pending" {

			return nil
		}
		if !deactivationBusinessCommitted(row.ReceiptData) {
			return fmt.Errorf("%w: 下线回执缺少已提交事实", errPublishStateMoved)
		}
		ids := deactivationPathArtifacts(row.ReceiptData)

		lockedPaths := deactivationReceiptPaths(row.ReceiptData)
		if len(lockedPaths) == 0 {
			return fmt.Errorf("%w: 下线回执缺少路径事实", errPublishStateMoved)
		}
		executed = true
		return s.withdrawDeactivatedLinks(ctx, tx, projectID, instanceID, ids, lockedPaths)
	})
	if txErr != nil {
		return false, txErr
	}
	return executed, nil
}

func (s *Service) lockPresentationEntityOrAdvisory(ctx context.Context, tx *gorm.DB,
	projectID, instanceID string) error {

	if aerr := s.m.LockPresentationEntityAdvisoryTx(ctx, tx, instanceID); aerr != nil {
		return aerr
	}

	if _, lerr := s.m.LockInstanceForActivationTx(ctx, tx, projectID, instanceID); lerr != nil {
		if errors.Is(lerr, gorm.ErrRecordNotFound) {
			return nil
		}
		return lerr
	}
	return nil
}

func deactivationReceiptPaths(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var probe struct {
		Path     string   `json:"path"`
		OldPath  string   `json:"oldPath"`
		OldPaths []string `json:"oldPaths"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil
	}
	main := strings.TrimSpace(probe.Path)
	out := make([]string, 0, len(probe.OldPaths)+1)
	for _, p := range normalizePathList(probe.OldPaths) {
		if p != main {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		if single := strings.TrimSpace(probe.OldPath); single != "" && single != main {
			out = append(out, single)
		}
	}
	return out
}

func deactivationBusinessCommitted(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var probe struct {
		BusinessCommitted bool `json:"businessCommitted"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	return probe.BusinessCommitted
}

func deactivationPathArtifacts(raw json.RawMessage) map[string]pubcontract.ReceiptPathIdentity {
	if len(raw) == 0 {
		return nil
	}
	var probe struct {
		PathArtifacts map[string]pubcontract.ReceiptPathIdentity `json:"pathArtifacts"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil
	}
	return probe.PathArtifacts
}
