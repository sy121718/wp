package buildservice

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	buildcontract "go_wp/internal/module/build/contract"
	builddto "go_wp/internal/module/build/dto"
	buildenums "go_wp/internal/module/build/enums"
	buildmodel "go_wp/internal/module/build/model"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"

	"github.com/google/uuid"
)

var _ buildcontract.BuildService = (*Service)(nil)

const (
	defaultWorkerIdle = time.Second

	defaultLeaseTTL = 15 * time.Minute

	defaultJobTimeout = 10 * time.Minute
)

type Service struct {
	m *buildmodel.Model

	mu        sync.RWMutex
	executors map[string]buildcontract.Executor

	workerIdle time.Duration
	leaseTTL   time.Duration
	jobTimeout time.Duration
	now        func() time.Time
}

func NewService(m *buildmodel.Model) *Service {
	return &Service{
		m:          m,
		executors:  map[string]buildcontract.Executor{},
		workerIdle: defaultWorkerIdle,
		leaseTTL:   defaultLeaseTTL,
		jobTimeout: defaultJobTimeout,
		now:        func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) Model() *buildmodel.Model { return s.m }

func (s *Service) SetLeaseTTL(d time.Duration) { s.leaseTTL = d }

func (s *Service) RegisterExecutor(sourceType string, fn buildcontract.Executor) {
	if s == nil || strings.TrimSpace(sourceType) == "" || fn == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.executors[sourceType] = fn
}

func (s *Service) executorFor(sourceType string) (buildcontract.Executor, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	fn, ok := s.executors[sourceType]
	return fn, ok
}

func normalizeIntent(raw string) (string, bool) {
	switch strings.TrimSpace(raw) {
	case "":
		return buildmodel.IntentDependency, true
	case buildmodel.IntentManual:
		return buildmodel.IntentManual, true
	case buildmodel.IntentDependency:
		return buildmodel.IntentDependency, true
	default:
		return "", false
	}
}

func canonicalSourceID(raw string) (string, bool) {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", false
	}
	return id.String(), true
}

func (s *Service) Enqueue(ctx context.Context, req *builddto.EnqueueReq) (job *builddto.Job, created bool, err error) {
	if s == nil || s.m == nil || req == nil {
		return nil, false, buildenums.Err(buildenums.ErrInvalidParam)
	}
	sourceType := strings.TrimSpace(req.SourceType)
	switch sourceType {
	case buildmodel.SourceTypePage, buildmodel.SourceTypePresentation:
	default:

		return nil, false, buildenums.Err(buildenums.ErrInvalidSource)
	}
	sourceID, ok := canonicalSourceID(req.SourceID)
	if !ok {
		return nil, false, buildenums.Err(buildenums.ErrInvalidParam)
	}
	intent, ok := normalizeIntent(req.Intent)
	if !ok {
		return nil, false, buildenums.Err(buildenums.ErrInvalidParam)
	}
	e := &buildmodel.Entity{
		SourceType: sourceType, SourceID: sourceID,

		ProjectID:      nullableString(req.ProjectID),
		Lang:           strings.TrimSpace(req.Lang),
		Intent:         intent,
		DraftVersion:   req.DraftVersion,
		BuildInputHash: strings.TrimSpace(req.BuildInputHash),
		Status:         buildmodel.StatusPending, CreateTime: s.now(),
	}
	created, err = s.m.Enqueue(ctx, e)
	if err != nil {
		return nil, false, err
	}
	if !created {
		return nil, false, nil
	}
	job = toDto(e)
	return job, true, nil
}

func (s *Service) EnqueuePageBuild(ctx context.Context, pageID, projectID, lang, intent string, draftVersion int64, buildInputHash string) error {
	_, _, err := s.Enqueue(ctx, &builddto.EnqueueReq{
		SourceType: buildmodel.SourceTypePage, SourceID: pageID, ProjectID: projectID,
		Lang: strings.TrimSpace(lang), Intent: intent,
		DraftVersion: draftVersion, BuildInputHash: buildInputHash,
	})
	return err
}

func (s *Service) EnqueuePresentationBuild(ctx context.Context, presentationID, projectID string) error {
	_, _, err := s.Enqueue(ctx, &builddto.EnqueueReq{
		SourceType: buildmodel.SourceTypePresentation, SourceID: presentationID, ProjectID: projectID,
		BuildInputHash: "",
		Intent:         buildmodel.IntentDependency,
	})
	return err
}

func (s *Service) List(ctx context.Context, req *builddto.ListReq) (list []*builddto.Job, err error) {
	projectID, status, limit := "", "", 0
	if req != nil {
		projectID, status, limit = strings.TrimSpace(req.ProjectID), strings.TrimSpace(req.Status), req.Limit
	}
	rows, err := s.m.ListRecent(ctx, projectID, status, limit)
	if err != nil {
		return nil, err
	}
	out := make([]*builddto.Job, 0, len(rows))
	for _, r := range rows {
		out = append(out, toDto(r))
	}
	return out, nil
}

func (s *Service) Retry(ctx context.Context, projectID, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return buildenums.Err(buildenums.ErrInvalidParam)
	}
	jid, perr := strconv.ParseInt(id, 10, 64)
	if perr != nil {
		return buildenums.Err(buildenums.ErrJobNotFound)
	}
	ok, err := s.m.RetryFailed(ctx, jid, strings.TrimSpace(projectID))
	if err != nil {
		return err
	}
	if !ok {
		return buildenums.Err(buildenums.ErrJobNotFound)
	}
	return nil
}

func (s *Service) ReclaimStale(ctx context.Context) (reclaimed int64, err error) {
	res, err := s.reclaimStale(ctx)
	return res.Reclaimed, err
}

func (s *Service) reclaimStale(ctx context.Context) (buildmodel.ReclaimResult, error) {
	if s == nil || s.m == nil {
		return buildmodel.ReclaimResult{}, nil
	}
	res, err := s.m.ReclaimStale(ctx, s.now())
	if err != nil {
		return res, err
	}
	if res.Merged > 0 {
		logger.Scene("build").With("merged", res.Merged).
			Warn("陈旧构建任务与队列里的待办重复，已合并为 superseded")
	}
	return res, nil
}

func (s *Service) Stats(ctx context.Context, projectID string) (res *builddto.QueueStatsResp, err error) {
	res = &builddto.QueueStatsResp{RecentFailed: []builddto.Job{}}
	if s == nil || s.m == nil {
		return res, nil
	}
	projectID = strings.TrimSpace(projectID)

	if r, rerr := s.reclaimStale(ctx); rerr == nil {
		res.StaleReclaimed = int(r.Reclaimed)
		res.StaleMerged = int(r.Merged)
	}
	rows, err := s.m.CountByStatus(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		res.Total += r.Count
		switch r.Status {
		case buildmodel.StatusPending:
			res.Pending = r.Count
		case buildmodel.StatusRunning:
			res.Running = r.Count
		case buildmodel.StatusFailed:
			res.Failed = r.Count
		case buildmodel.StatusSucceeded:
			res.Succeeded = r.Count
		case buildmodel.StatusSuperseded:
			res.Superseded = r.Count
		}
	}
	if res.Failed > 0 {
		failed, ferr := s.m.ListRecent(ctx, projectID, buildmodel.StatusFailed, 10)
		if ferr == nil {
			for _, r := range failed {
				res.RecentFailed = append(res.RecentFailed, *toDto(r))
			}
		}
	}
	return res, nil
}

func toDto(e *buildmodel.Entity) *builddto.Job {
	if e == nil {
		return nil
	}
	msg := ""
	if e.ErrorMessage != nil {
		msg = *e.ErrorMessage
	}
	projectID := ""
	if e.ProjectID != nil {
		projectID = *e.ProjectID
	}
	inputVersion := int64(0)
	if e.InputVersion != nil {
		inputVersion = *e.InputVersion
	}
	return &builddto.Job{
		ID: strconv.FormatInt(e.ID, 10), SourceType: e.SourceType, SourceID: e.SourceID,
		ProjectID: projectID, Lang: e.Lang, Intent: e.Intent,
		DraftVersion: e.DraftVersion, BuildInputHash: e.BuildInputHash,
		InputVersion: inputVersion, Status: e.Status,
		ArtifactID: e.ArtifactID, ErrorMessage: msg, Attempt: e.Attempt,
		CreatedAt: utils.NewJSONTime(e.CreateTime), StartedAt: utils.NewJSONTimePtr(e.StartedAt), CompletedAt: utils.NewJSONTimePtr(e.CompletedAt),
	}
}

func nullableString(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}
