package pipeline

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	"go_wp/internal/templates"
	"go_wp/pkg/logger"
)

const (
	StateDraft = "draft"

	StateBuilding = "building"

	StateReady = "ready"

	StateFailed = "failed"

	StatePublished = "published"

	StateSuperseded = "superseded"
)

var (
	ErrPageNotFound = errors.New("页面不存在")

	ErrVersionConflict = errors.New("草稿版本冲突：写入基于旧版本")

	ErrNoStagedArtifact = errors.New("无就绪产物，请先构建")

	ErrRollbackPathMismatch = errors.New("回滚产物 URL 与当前页面路径不一致，请先按 URL 修改流程重建")
)

type HistoryEntry struct {
	Hash string

	Path string

	Status string

	Order int
}

type PageRecord struct {
	ID string

	Path string

	Lang string

	Plan *PublicationPlan

	Version int

	Status string

	DocumentJSON []byte

	StagedHash string

	ActiveHash string

	FailedReason string

	Histories []*HistoryEntry
}

type BuildInput struct {
	PageID string

	Lang string

	Path string

	DocJSON []byte

	Usage *CompileUsage

	Diagnostics *builder.DegradeCollector

	Plan *PublicationPlan
}

type CompileUsage struct {
	Menus map[string]bool

	Navigations map[string]bool

	Blocks map[string]bool

	ActionTargets map[core.ActionKind]map[string]bool

	siteLangs []string

	plan *PublicationPlan

	contentCandidates int
	contentTranslator ContentMissCounter

	ContentTranslation bool
}

func (u *CompileUsage) UseContentTranslation() {
	if u == nil {
		return
	}
	u.ContentTranslation = true
}

func (u *CompileUsage) UseMenu(kind string) {
	if u == nil || kind == "" {
		return
	}
	if u.Menus == nil {
		u.Menus = map[string]bool{}
	}
	u.Menus[kind] = true
}

func (u *CompileUsage) UseNavigation(navigationID string) {
	if u == nil || navigationID == "" {
		return
	}
	if u.Navigations == nil {
		u.Navigations = map[string]bool{}
	}
	u.Navigations[navigationID] = true
}

func (u *CompileUsage) UseBlock(blockID string) {
	if u == nil || blockID == "" {
		return
	}
	if u.Blocks == nil {
		u.Blocks = map[string]bool{}
	}
	u.Blocks[blockID] = true
}

func (u *CompileUsage) BlockList() []string {
	if u == nil || len(u.Blocks) == 0 {
		return nil
	}
	out := make([]string, 0, len(u.Blocks))
	for id := range u.Blocks {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func (u *CompileUsage) NavigationList() []string {
	if u == nil || len(u.Navigations) == 0 {
		return nil
	}
	out := make([]string, 0, len(u.Navigations))
	for id := range u.Navigations {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func (u *CompileUsage) MenuList() []string {
	if u == nil || len(u.Menus) == 0 {
		return nil
	}
	out := make([]string, 0, len(u.Menus))
	for kind := range u.Menus {
		out = append(out, kind)
	}
	sort.Strings(out)
	return out
}

type ContentMissCounter interface {
	Misses() int64
}

func (u *CompileUsage) SetSiteLangs(langs []string) {
	if u == nil || len(langs) == 0 {
		return
	}
	u.siteLangs = append([]string(nil), langs...)
}

func (u *CompileUsage) SiteLangs() []string {
	if u == nil || len(u.siteLangs) == 0 {
		return nil
	}
	return append([]string(nil), u.siteLangs...)
}

func (u *CompileUsage) SetPublicationPlan(plan PublicationPlan) {
	if u == nil {
		return
	}
	n := plan.Normalize()
	if n.Empty() {
		return
	}
	cp := n
	u.plan = &cp
	if len(n.SiteLangs) > 0 {
		u.siteLangs = append([]string(nil), n.SiteLangs...)
	}
}

func (u *CompileUsage) PublicationPlan() *PublicationPlan {
	if u == nil || u.plan == nil {
		return nil
	}
	cp := u.plan.Normalize()
	return &cp
}

func (u *CompileUsage) RecordContentTranslation(candidates int, counter ContentMissCounter) {
	if u == nil {
		return
	}
	if candidates > u.contentCandidates {
		u.contentCandidates = candidates
	}
	if counter != nil {
		u.contentTranslator = counter
	}
}

func (u *CompileUsage) ContentTranslationMisses() (candidates int, misses int64) {
	if u == nil {
		return 0, 0
	}
	if u.contentTranslator != nil {
		misses = u.contentTranslator.Misses()
	}
	return u.contentCandidates, misses
}

type compileUsageKey struct{}

func WithCompileUsage(ctx context.Context, u *CompileUsage) context.Context {
	if ctx == nil || u == nil {
		return ctx
	}
	return context.WithValue(ctx, compileUsageKey{}, u)
}

func CompileUsageFromContext(ctx context.Context) *CompileUsage {
	if ctx == nil {
		return nil
	}
	u, _ := ctx.Value(compileUsageKey{}).(*CompileUsage)
	return u
}

type CompileFn func(ctx context.Context, in BuildInput) (html []byte, err error)

func DefaultCompile(ctx context.Context, in BuildInput) ([]byte, error) {
	page, err := builder.ParsePage(in.DocJSON)
	if err != nil {
		return nil, err
	}

	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		return nil, err
	}
	opts := append(ClientAssetOptions(),
		builder.WithContext(ctx), builder.WithCompileMode(builder.CompileModePublish),
		builder.WithComponentSet(set), builder.WithLanguage(in.Lang))
	compiled, err := builder.Compile(page, opts...)
	if err != nil {
		return nil, err
	}
	doc, err := builder.RenderDocument(compiled)
	if err != nil {
		return nil, err
	}
	return []byte(doc), nil
}

type Draft struct {
	Path string

	Lang string

	DocJSON []byte

	Plan *PublicationPlan
}

type Option func(*Publisher)

func WithCompile(fn CompileFn) Option {
	return func(p *Publisher) { p.compile = fn }
}

type DependencyProvider func(ctx context.Context, in BuildInput) []Dependency

func WithDependencies(fn DependencyProvider) Option {
	return func(p *Publisher) { p.deps = fn }
}

type Publisher struct {
	store Store
	pub   PublicationStore

	paths   PublicationPathLocker
	compile CompileFn

	deps DependencyProvider

	mu    sync.Mutex
	pages map[string]*PageRecord
	order int
}

func NewPublisher(store Store, pub PublicationStore, opts ...Option) *Publisher {
	p := &Publisher{
		store:   store,
		pub:     pub,
		paths:   DefaultPathLocker(),
		compile: DefaultCompile,
		pages:   map[string]*PageRecord{},
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p *Publisher) SetPublicationPathLocker(locker PublicationPathLocker) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.paths = locker
}

func (p *Publisher) withPaths(paths []string, fn func() error) error {
	if fn == nil {
		return nil
	}
	return WithPathLock(p.paths, paths, fn)
}

func (p *Publisher) oldPathWithPaths(newPath, oldPath string, pathLockHeld bool, fn func() error) error {
	if pathLockHeld {
		return fn()
	}
	return p.withPaths([]string{newPath, oldPath}, fn)
}

func (p *Publisher) activatePath(path string, loc Locator) error {
	return p.withPaths([]string{path}, func() error { return p.activatePathInLock(path, loc) })
}

func (p *Publisher) activatePathInLock(path string, loc Locator) error {
	return p.pub.Activate(path, loc)
}

func (p *Publisher) deactivatePathInLock(path string) error {
	return p.pub.Deactivate(path)
}

func (p *Publisher) SetCompile(fn CompileFn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if fn != nil {
		p.compile = fn
	}
}

func (p *Publisher) SaveDraft(pageID string, expectedVersion int, path string, docJSON []byte) (version int, err error) {
	return p.SaveDraftInput(pageID, expectedVersion, Draft{Path: path, DocJSON: docJSON})
}

func (p *Publisher) SaveDraftInput(pageID string, expectedVersion int, d Draft) (version int, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.saveDraftLocked(pageID, expectedVersion, d)
}

func (p *Publisher) saveDraftLocked(pageID string, expectedVersion int, d Draft) (version int, err error) {
	if pageID == "" {
		return 0, errors.New("页面 ID 不能为空")
	}
	nPath, err := NormalizeURL(d.Path)
	if err != nil {
		return 0, err
	}
	lang := strings.TrimSpace(d.Lang)
	if lang != "" {
		if lang, err = NormalizeLang(lang); err != nil {
			return 0, err
		}
	}
	docJSON := d.DocJSON
	rec, ok := p.pages[pageID]
	if !ok {
		if expectedVersion != 0 {
			return 0, ErrVersionConflict
		}
		rec = &PageRecord{ID: pageID, Path: nPath, Lang: lang, Version: 1, Status: StateDraft}
		p.pages[pageID] = rec
	} else {
		if expectedVersion != rec.Version {
			return 0, ErrVersionConflict
		}
		rec.Version++
		rec.Path = nPath
		rec.Lang = lang
	}

	rec.Plan = draftPlan(d.Plan)

	snap := make([]byte, len(docJSON))
	copy(snap, docJSON)
	rec.DocumentJSON = snap

	rec.Status = rec.currentStatus()
	return rec.Version, nil
}

func (p *Publisher) Build(ctx context.Context, pageID string, expectedVersion int) (hash string, err error) {

	p.mu.Lock()
	rec, ok := p.pages[pageID]
	if !ok {
		p.mu.Unlock()
		return "", ErrPageNotFound
	}
	if expectedVersion != rec.Version {
		p.mu.Unlock()
		return "", ErrVersionConflict
	}
	rec.Status = StateBuilding
	docSnapshot := append([]byte(nil), rec.DocumentJSON...)
	in := BuildInput{
		PageID: pageID, Lang: rec.Lang, Path: rec.Path, DocJSON: docSnapshot,
		Plan:  draftPlan(rec.Plan),
		Usage: &CompileUsage{}, Diagnostics: builder.NewDegradeCollector(),
	}
	p.mu.Unlock()

	a, cerr := p.compileArtifact(ctx, in)
	if cerr != nil {
		p.mu.Lock()
		if rec.Version == expectedVersion {
			rec.FailedReason = cerr.Error()
			rec.Status = rec.currentStatus()
		}
		p.mu.Unlock()
		logger.Scene("build").With("pageId", rec.ID).Error(cerr, "页面编译失败")
		return "", cerr
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if rec.Version != expectedVersion {
		return "", ErrVersionConflict
	}
	rec.StagedHash = a.Hash
	rec.FailedReason = ""
	rec.Status = rec.currentStatus()
	p.order++
	rec.Histories = append(rec.Histories, &HistoryEntry{
		Hash: a.Hash, Path: in.Path, Status: StateReady, Order: p.order,
	})
	rec.trimHistory()
	logger.Scene("build").With("pageId", rec.ID).With("hash", a.Hash).Info("构建完成")
	return a.Hash, nil
}

func (p *Publisher) Publish(pageID string) (hash string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, err = p.stageArtifactLocked(pageID); err != nil {
		return "", err
	}
	if err = p.activateStagedLocked(pageID); err != nil {
		return "", err
	}
	return p.pages[pageID].ActiveHash, nil
}

func (p *Publisher) StageArtifact(pageID string) (hash string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stageArtifactLocked(pageID)
}

func (p *Publisher) stageArtifactLocked(pageID string) (hash string, err error) {
	rec, ok := p.pages[pageID]
	if !ok {
		return "", ErrPageNotFound
	}
	if rec.StagedHash == "" {
		return "", ErrNoStagedArtifact
	}
	loc := ArtifactLocator(rec.StagedHash)

	a, err := p.store.GetArtifact(loc)
	if err != nil {
		return "", err
	}
	if a.CanonicalPath != rec.Path {
		return "", fmt.Errorf("暂存产物路径 %s 与当前页面路径 %s 不一致，请重新构建", a.CanonicalPath, rec.Path)
	}
	return rec.StagedHash, nil
}

func (p *Publisher) ActivateStagedArtifact(pageID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.activateStagedLocked(pageID)
}

func (p *Publisher) activateStagedLocked(pageID string) error {
	rec, ok := p.pages[pageID]
	if !ok {
		return ErrPageNotFound
	}
	if rec.StagedHash == "" {
		return ErrNoStagedArtifact
	}
	loc := ArtifactLocator(rec.StagedHash)
	if err := p.activatePath(rec.Path, loc); err != nil {

		logger.Scene("build").With("pageId", rec.ID).Error(err, "激活失败（线上保持不变）")
		rec.Status = rec.currentStatus()
		return fmt.Errorf("激活失败（线上版本保持不变）: %w", err)
	}
	p.supersedeActiveLocked(rec)
	rec.ActiveHash = rec.StagedHash
	rec.Status = StatePublished
	rec.markLatestHistory(rec.StagedHash, StatePublished, rec.Path)
	return nil
}

func (p *Publisher) Rollback(pageID string, targetHash string) (err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	rec, ok := p.pages[pageID]
	if !ok {
		return ErrPageNotFound
	}
	entry := rec.findHistory(targetHash)
	if entry == nil {
		return fmt.Errorf("目标版本不存在于历史: %s", targetHash)
	}
	if entry.Path != rec.Path {
		return ErrRollbackPathMismatch
	}

	loc := ArtifactLocator(targetHash)
	if err = p.store.VerifyArtifact(loc, targetHash); err != nil {
		return err
	}
	if err = p.activatePath(rec.Path, loc); err != nil {
		logger.Scene("build").With("pageId", rec.ID).Error(err, "回滚失败")
		return fmt.Errorf("回滚激活失败（线上版本保持不变）: %w", err)
	}

	p.supersedeActiveLocked(rec)
	rec.ActiveHash = targetHash

	rec.Status = StatePublished
	rec.markLatestHistory(targetHash, StatePublished, rec.Path)
	return nil
}

func (p *Publisher) UpdateURL(ctx context.Context, pageID string, newPath string, withRedirect bool) (oldPath string, err error) {
	return p.updateURL(ctx, pageID, newPath, withRedirect, false)
}

func (p *Publisher) UpdateURLInLock(ctx context.Context, pageID string, newPath string, withRedirect bool) (oldPath string, err error) {
	return p.updateURL(ctx, pageID, newPath, withRedirect, true)
}

func (p *Publisher) updateURL(ctx context.Context, pageID string, newPath string, withRedirect bool, pathLockHeld bool) (oldPath string, err error) {
	p.mu.Lock()
	rec, ok := p.pages[pageID]
	if !ok {
		p.mu.Unlock()
		return "", ErrPageNotFound
	}
	oldPath = rec.Path

	nPath, err := NormalizeURL(newPath)
	if err != nil {
		p.mu.Unlock()
		return oldPath, err
	}
	if nPath == oldPath {
		p.mu.Unlock()
		return oldPath, errors.New("新路径与当前路径相同")
	}

	if !rec.hasPublishedHistory() {
		if _, err = p.saveDraftLocked(pageID, rec.Version, Draft{Path: nPath, Lang: rec.Lang, DocJSON: rec.DocumentJSON, Plan: rec.Plan}); err != nil {
			p.mu.Unlock()
			return oldPath, err
		}
		p.mu.Unlock()
		logger.Scene("build").With("pageId", pageID).With("oldPath", oldPath).With("newPath", nPath).
			Info("纯草稿改 URL：仅迁移草稿路径，未构建未激活")
		return oldPath, nil
	}

	if _, err = p.saveDraftLocked(pageID, rec.Version, Draft{Path: nPath, Lang: rec.Lang, DocJSON: rec.DocumentJSON, Plan: rec.Plan}); err != nil {
		p.mu.Unlock()
		return oldPath, err
	}
	version := rec.Version
	in := BuildInput{
		PageID: pageID, Lang: rec.Lang, Path: nPath,
		DocJSON: append([]byte(nil), rec.DocumentJSON...),
		Plan:    draftPlan(rec.Plan),
		Usage:   &CompileUsage{}, Diagnostics: builder.NewDegradeCollector(),
	}
	p.mu.Unlock()

	a, cerr := p.compileArtifact(ctx, in)
	if cerr != nil {
		p.mu.Lock()
		if rec.Version == version {
			rec.FailedReason = cerr.Error()
			rec.Status = rec.currentStatus()
		}
		p.mu.Unlock()
		logger.Scene("build").With("pageId", pageID).Error(cerr, "URL 修改构建失败")
		return oldPath, cerr
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if rec.Version != version {
		return oldPath, ErrVersionConflict
	}
	if err = p.publishLockedWith(rec, a.Hash, pathLockHeld); err != nil {
		return oldPath, err
	}

	if withRedirect {
		ra, rerr := NewRedirectArtifact(nPath, 301)
		if rerr != nil {
			logger.Scene("build").With("pageId", pageID).With("oldPath", oldPath).Error(rerr, "旧 URL 301 产物构造失败（新 URL 已生效）")
			return oldPath, nil
		}
		rl, rerr := p.store.PutRedirect(ra)
		if rerr != nil {
			logger.Scene("build").With("pageId", pageID).With("oldPath", oldPath).Error(rerr, "旧 URL 301 产物落盘失败（新 URL 已生效）")
			return oldPath, nil
		}

		if aerr := p.oldPathWithPaths(nPath, oldPath, pathLockHeld, func() error {
			return p.activatePathInLock(oldPath, rl)
		}); aerr != nil {
			logger.Scene("build").With("pageId", pageID).With("oldPath", oldPath).Error(aerr, "旧 URL 301 激活失败（新 URL 已生效）")
			return oldPath, nil
		}
	} else {
		if derr := p.oldPathWithPaths(nPath, oldPath, pathLockHeld, func() error {
			return p.deactivatePathInLock(oldPath)
		}); derr != nil {
			logger.Scene("build").With("pageId", pageID).With("oldPath", oldPath).Error(derr, "旧 URL 取消激活失败（新 URL 已生效）")
			return oldPath, nil
		}
	}
	return oldPath, nil
}

func draftPlan(p *PublicationPlan) *PublicationPlan {
	if p == nil {
		return nil
	}
	cp := p.Normalize()
	return &cp
}

func freezeBuildLang(in BuildInput) BuildInput {
	if strings.TrimSpace(in.Lang) != "" {
		return in
	}
	if in.Plan == nil {
		return in
	}

	if inputs, ok := SiteLangInputsOfPlan(in.Plan); ok {
		in.Lang = inputs.DefaultLang
		return in
	}
	in.Lang = strings.TrimSpace(in.Plan.DefaultLang)
	return in
}

func (p *Publisher) compileArtifact(ctx context.Context, in BuildInput) (a *Artifact, err error) {

	in = freezeBuildLang(in)

	ctx = WithCompileUsage(ctx, in.Usage)
	html, err := p.compile(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("编译失败: %w", err)
	}
	m := &Manifest{
		ManifestSchemaVersion:     ManifestSchemaVersion,
		PageDocumentSchemaVersion: 1,
		CompilerVersion:           "internal-builder",
		SourceID:                  in.PageID,
		SourceType:                SourceTypePage,
		CanonicalPath:             in.Path,
		Lang:                      in.Lang,
		Dir:                       manifestDir(in.Lang),
		SourceHash:                SHA256(in.DocJSON),
		BuildInputHash:            SHA256(in.DocJSON),
	}

	if in.Usage != nil {
		m.SiteLangs = in.Usage.SiteLangs()

		if plan := in.Usage.PublicationPlan(); plan != nil {
			m.SiteDefaultLang = plan.DefaultLang
			m.References = plan.References.Clone()
		}

		if candidates, misses := in.Usage.ContentTranslationMisses(); candidates > 0 || misses > 0 {
			m.TranslationMisses = &ManifestTranslationMisses{
				Policy: TranslationPolicyFallback, Candidates: candidates, Misses: misses,
			}
		}
	}
	if p.deps != nil {
		m.Dependencies = p.deps(ctx, in)
	}

	m.Diagnostics = in.Diagnostics.Items()

	guardFiles, access, gerr := PageGuardEntries(in.DocJSON, in.Lang)
	if gerr != nil {
		return nil, fmt.Errorf("页面访问设置解析失败: %w", gerr)
	}
	m.Access = access
	a, err = NewArtifactWithEntries(html, m, guardFiles)
	if err != nil {
		return nil, err
	}
	if _, err = p.store.PutArtifact(a); err != nil {
		return nil, err
	}
	return a, nil
}

func (p *Publisher) RestoreArtifact(ctx context.Context, in BuildInput) (*Artifact, error) {
	if p.compile == nil {
		return nil, fmt.Errorf("编译函数未注入，无法重建产物")
	}
	return p.compileArtifact(ctx, in)
}

func (p *Publisher) ArtifactExists(hash string) error {
	return p.store.VerifyArtifact(ArtifactLocator(hash), hash)
}

func (p *Publisher) publishLocked(rec *PageRecord, stagedHash string) error {
	return p.publishLockedWith(rec, stagedHash, false)
}

func (p *Publisher) publishLockedWith(rec *PageRecord, stagedHash string, pathLockHeld bool) error {
	loc := ArtifactLocator(stagedHash)
	a, err := p.store.GetArtifact(loc)
	if err != nil {
		return err
	}
	if a.CanonicalPath != rec.Path {
		return fmt.Errorf("暂存产物路径 %s 与当前页面路径 %s 不一致，请重新构建", a.CanonicalPath, rec.Path)
	}

	var aerr error
	if pathLockHeld {
		aerr = p.activatePathInLock(rec.Path, loc)
	} else {
		aerr = p.activatePath(rec.Path, loc)
	}
	if err = aerr; err != nil {
		rec.Status = rec.currentStatus()
		return fmt.Errorf("激活失败（线上版本保持不变）: %w", err)
	}
	p.supersedeActiveLocked(rec)
	rec.ActiveHash = stagedHash
	rec.Status = StatePublished
	rec.markLatestHistory(stagedHash, StatePublished, rec.Path)
	return nil
}

const maxHistoryEntries = 50

func (rec *PageRecord) trimHistory() {
	if len(rec.Histories) <= maxHistoryEntries {
		return
	}
	overflow := len(rec.Histories) - maxHistoryEntries
	kept := make([]*HistoryEntry, 0, maxHistoryEntries)
	removed := 0
	for _, h := range rec.Histories {
		if removed < overflow && h.Status == StateSuperseded {
			removed++
			continue
		}
		kept = append(kept, h)
	}

	if len(kept) > maxHistoryEntries {
		kept = kept[len(kept)-maxHistoryEntries:]
	}
	rec.Histories = kept
}

func (p *Publisher) supersedeActiveLocked(rec *PageRecord) {
	for _, h := range rec.Histories {
		if h.Status == StatePublished {
			h.Status = StateSuperseded
		}
	}
}

func (rec *PageRecord) markLatestHistory(hash, status, path string) {
	var latest *HistoryEntry
	for _, h := range rec.Histories {
		if h.Hash == hash && (latest == nil || h.Order > latest.Order) {
			latest = h
		}
	}
	if latest != nil {
		latest.Status = status
		latest.Path = path
		return
	}
	rec.Histories = append(rec.Histories, &HistoryEntry{Hash: hash, Path: path, Status: status})
}

func (rec *PageRecord) findHistory(hash string) *HistoryEntry {
	for _, h := range rec.Histories {
		if h.Hash == hash {
			return h
		}
	}
	return nil
}

func (rec *PageRecord) hasPublishedHistory() bool {
	if rec.ActiveHash != "" {
		return true
	}
	for _, h := range rec.Histories {
		if h.Status == StatePublished {
			return true
		}
	}
	return false
}

func (rec *PageRecord) currentStatus() string {
	if rec.ActiveHash != "" {
		return StatePublished
	}
	if rec.StagedHash != "" {
		return StateReady
	}
	if rec.FailedReason != "" {
		return StateFailed
	}
	return StateDraft
}

func (p *Publisher) Status(pageID string) (*PageRecord, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	rec, ok := p.pages[pageID]
	if !ok {
		return nil, ErrPageNotFound
	}
	copyRec := *rec
	copyRec.DocumentJSON = append([]byte(nil), rec.DocumentJSON...)
	copyRec.Histories = append([]*HistoryEntry(nil), rec.Histories...)
	return &copyRec, nil
}

func (p *Publisher) LoadRecord(rec *PageRecord) {
	if rec == nil || rec.ID == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	snap := append([]byte(nil), rec.DocumentJSON...)
	histories := make([]*HistoryEntry, len(rec.Histories))
	for i, h := range rec.Histories {
		copied := *h
		histories[i] = &copied
	}
	restored := *rec
	restored.DocumentJSON = snap
	restored.Histories = histories
	p.pages[rec.ID] = &restored
}
