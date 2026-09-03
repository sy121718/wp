package pipeline

import (
	"errors"
	"fmt"
	"sync"

	"go_wp/internal/builder"
	"go_wp/internal/templates"
)

// 状态常量（规范 0-A1 §2 发布生命周期，docs/03-pipeline.md §6）。
const (
	// StateDraft 草稿态：最新 Draft AST 已保存，无就绪产物。
	StateDraft = "draft"
	// StateBuilding 构建中：当前 AST 被冻结为只读快照。
	StateBuilding = "building"
	// StateReady 产物就绪：已构建并暂存（StagedHash 非空），等待激活。
	StateReady = "ready"
	// StateFailed 构建异常：线上版本保持不变。
	StateFailed = "failed"
	// StatePublished 线上发布：当前版本为活跃指针目标。
	StatePublished = "published"
	// StateSuperseded 历史版本：被新版本替换，可回滚（仅 HistoryEntry 层）。
	StateSuperseded = "superseded"
)

// 错误定义。
var (
	// ErrPageNotFound 页面不存在。
	ErrPageNotFound = errors.New("页面不存在")
	// ErrVersionConflict 草稿版本冲突（乐观锁）。
	ErrVersionConflict = errors.New("草稿版本冲突：写入基于旧版本")
	// ErrNoStagedArtifact 无就绪产物可发布。
	ErrNoStagedArtifact = errors.New("无就绪产物，请先构建")
	// ErrRollbackPathMismatch 回滚产物路径与当前 URL 不一致。
	ErrRollbackPathMismatch = errors.New("回滚产物 URL 与当前页面路径不一致，请先按 URL 修改流程重建")
)

// HistoryEntry 页面版本历史（产物级状态：published / superseded）。
type HistoryEntry struct {
	// Hash 产物内容哈希。
	Hash string
	// Path 构建时的规范化访问路径。
	Path string
	// Status published / superseded。
	Status string
	// Order 构建先后顺序（单调递增）。
	Order int
}

// PageRecord 页面运行时态（内存实现；生产由 Phase 0-A1 page 模块持久化）。
type PageRecord struct {
	// ID 页面唯一标识。
	ID string
	// Path 当前草稿路径（draft_path）。
	Path string
	// Version 草稿版本号（乐观锁，每次保存 +1）。
	Version int
	// Status 页面当前状态：draft / building / ready / failed / published。
	Status string
	// DocumentJSON 最近一次保存的草稿冻结快照（构建输入）。
	DocumentJSON []byte
	// StagedHash 已构建暂存、待激活的产物哈希。
	StagedHash string
	// ActiveHash 当前活跃产物哈希（线上指针目标）。
	ActiveHash string
	// FailedReason 最近一次构建失败原因（Status 为 failed 时非空）。
	FailedReason string
	// Histories 版本历史（按 Order 升序）。
	Histories []*HistoryEntry
}

// CompileFn 冻结编译函数：Page Document 字节 → 完整 HTML 文档字节。
// 发布期唯一编译入口；实现必须确定性（docs/03-pipeline.md §3.4）。
type CompileFn func(docJSON []byte) (html []byte, err error)

// DefaultCompile 默认编译器：internal/builder 文档编译 + 完整文档组装。
func DefaultCompile(docJSON []byte) ([]byte, error) {
	page, err := builder.ParsePage(docJSON)
	if err != nil {
		return nil, err
	}
	// 组件模板 Set（Jet 渲染路径必需；embed 加载，不依赖进程工作目录）。
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		return nil, err
	}
	compiled, err := builder.Compile(page, builder.WithComponentSet(set))
	if err != nil {
		return nil, err
	}
	return []byte(builder.RenderDocument(compiled)), nil
}

// Option Publisher 构造选项。
type Option func(*Publisher)

// WithCompile 注入替代编译器（测试/集成用）。
func WithCompile(fn CompileFn) Option {
	return func(p *Publisher) { p.compile = fn }
}

// Publisher 发布服务：页面生命周期状态机（0-A1 §2）+ 流水线编排。
//
// 并发保护：单进程内互斥；多实例部署由 Phase 0-A1 build 模块（数据库 + 队列）负责。
type Publisher struct {
	store   Store
	pub     PublicationStore
	compile CompileFn

	mu    sync.Mutex
	pages map[string]*PageRecord
	order int
}

// NewPublisher 构造发布服务。
func NewPublisher(store Store, pub PublicationStore, opts ...Option) *Publisher {
	p := &Publisher{
		store:   store,
		pub:     pub,
		compile: DefaultCompile,
		pages:   map[string]*PageRecord{},
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// SetCompile 运行时替换编译函数（装配感知编译注入，方案 C：页眉/页脚块内联）。
func (p *Publisher) SetCompile(fn CompileFn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if fn != nil {
		p.compile = fn
	}
}

// SaveDraft 保存草稿（0-A1 §4.1：仅更新草稿字段并追加历史版本快照，返回新版本号）。
// expectedVersion 为乐观锁：页面不存在时为 0（创建），否则必须等于当前版本。
func (p *Publisher) SaveDraft(pageID string, expectedVersion int, path string, docJSON []byte) (version int, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.saveDraftLocked(pageID, expectedVersion, path, docJSON)
}

func (p *Publisher) saveDraftLocked(pageID string, expectedVersion int, path string, docJSON []byte) (version int, err error) {
	if pageID == "" {
		return 0, errors.New("页面 ID 不能为空")
	}
	nPath, err := NormalizeURL(path)
	if err != nil {
		return 0, err
	}
	rec, ok := p.pages[pageID]
	if !ok {
		if expectedVersion != 0 {
			return 0, ErrVersionConflict
		}
		rec = &PageRecord{ID: pageID, Path: nPath, Version: 1, Status: StateDraft}
		p.pages[pageID] = rec
	} else {
		if expectedVersion != rec.Version {
			return 0, ErrVersionConflict
		}
		rec.Version++
		rec.Path = nPath
	}

	// 冻结快照：拷贝字节，构建期使用，防止后续写入影响产物。
	snap := make([]byte, len(docJSON))
	copy(snap, docJSON)
	rec.DocumentJSON = snap

	// 保存草稿不改 staged/active（docs/03-pipeline.md §6.1）。
	rec.Status = rec.currentStatus()
	return rec.Version, nil
}

// Build 构建：基于指定草稿版本冻结快照 → 确定性编译 → 不可变 Artifact 落盘 → 暂存。
// expectedVersion 必须等于当前草稿版本（§6.2 防数据撕裂）。
func (p *Publisher) Build(pageID string, expectedVersion int) (hash string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	rec, ok := p.pages[pageID]
	if !ok {
		return "", ErrPageNotFound
	}
	return p.buildLocked(rec, expectedVersion)
}

// Publish 激活暂存产物（0-A1 §2.3）：校验后原子切换活跃指针，旧活跃版本转 Superseded。
func (p *Publisher) Publish(pageID string) (hash string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	rec, ok := p.pages[pageID]
	if !ok {
		return "", ErrPageNotFound
	}
	if rec.StagedHash == "" {
		return "", ErrNoStagedArtifact
	}
	loc := Locator{Provider: "local", Key: "artifacts/" + rec.StagedHash}

	// 校验产物与路径一致性（§6.3）：不可变产物 + 内容哈希 + canonicalPath。
	a, err := p.store.GetArtifact(loc)
	if err != nil {
		return "", err
	}
	if a.CanonicalPath != rec.Path {
		return "", fmt.Errorf("暂存产物路径 %s 与当前页面路径 %s 不一致，请重新构建", a.CanonicalPath, rec.Path)
	}

	if err = p.pub.Activate(rec.Path, loc); err != nil {
		// 激活失败线上保持不变（保持 ready）。
		rec.Status = rec.currentStatus()
		return "", fmt.Errorf("激活失败（线上版本保持不变）: %w", err)
	}

	p.supersedeActiveLocked(rec)
	rec.ActiveHash = rec.StagedHash
	rec.Status = StatePublished
	rec.markLatestHistory(rec.StagedHash, StatePublished, rec.Path)
	return rec.ActiveHash, nil
}

// Rollback 秒级回滚（0-A1 §2.4 / docs/03-pipeline.md §6.4）：
// 任意历史产物可重新激活，无需重新编译。
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

	loc := Locator{Provider: "local", Key: "artifacts/" + targetHash}
	if err = p.store.VerifyArtifact(loc, targetHash); err != nil {
		return err
	}
	if err = p.pub.Activate(rec.Path, loc); err != nil {
		return fmt.Errorf("回滚激活失败（线上版本保持不变）: %w", err)
	}

	p.supersedeActiveLocked(rec)
	rec.ActiveHash = targetHash
	// 不修改 StagedHash：回滚不影响暂存指针（docs/03-pipeline.md §6.4）。
	rec.Status = StatePublished
	rec.markLatestHistory(targetHash, StatePublished, rec.Path)
	return nil
}

// UpdateURL 修改访问路径（docs/03-pipeline.md §6.5）：
// 先基于新 URL 构建并原子激活新 URL，再按显式策略处理旧 URL（301 / 取消激活）。
// withRedirect 为 true 时旧 URL 注册 301 永久重定向（规范 0-A2 §1.1）。
func (p *Publisher) UpdateURL(pageID string, newPath string, withRedirect bool) (oldPath string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	rec, ok := p.pages[pageID]
	if !ok {
		return "", ErrPageNotFound
	}
	oldPath = rec.Path

	nPath, err := NormalizeURL(newPath)
	if err != nil {
		return oldPath, err
	}
	if nPath == oldPath {
		return oldPath, errors.New("新路径与当前路径相同")
	}

	// 1. 将新 URL 写入草稿路径；2. 基于新 URL 构建；3. 原子激活新 URL。
	if _, err = p.saveDraftLocked(pageID, rec.Version, nPath, rec.DocumentJSON); err != nil {
		return oldPath, err
	}
	// rec 引用不变（map 值是指针），Version 已 +1。
	hash, err := p.buildLocked(rec, rec.Version)
	if err != nil {
		return oldPath, err
	}
	if err = p.publishLocked(rec, hash); err != nil {
		return oldPath, err
	}

	// 4. 处理旧 URL（新 URL 已生效后执行；失败不撤销新 URL）。
	// 重定向目标必须是新 URL（0-A2 §1.1：旧 URL -> 新 URL 的 301）。
	if withRedirect {
		ra, rerr := NewRedirectArtifact(nPath, 301)
		if rerr != nil {
			return oldPath, rerr
		}
		rl, rerr := p.store.PutRedirect(ra)
		if rerr != nil {
			return oldPath, rerr
		}
		if aerr := p.pub.Activate(oldPath, rl); aerr != nil {
			return oldPath, fmt.Errorf("新 URL 已生效，但旧 URL 301 激活失败: %w", aerr)
		}
	} else {
		if derr := p.pub.Deactivate(oldPath); derr != nil {
			return oldPath, fmt.Errorf("新 URL 已生效，但旧 URL 取消激活失败: %w", derr)
		}
	}
	return oldPath, nil
}

// buildLocked 在锁内执行构建（UpdateURL 复用）。
func (p *Publisher) buildLocked(rec *PageRecord, expectedVersion int) (hash string, err error) {
	if expectedVersion != rec.Version {
		return "", ErrVersionConflict
	}
	rec.Status = StateBuilding

	html, err := p.compile(rec.DocumentJSON)
	if err != nil {
		rec.FailedReason = err.Error()
		rec.Status = rec.currentStatus()
		return "", fmt.Errorf("编译失败: %w", err)
	}
	m := &Manifest{
		ManifestSchemaVersion:     ManifestSchemaVersion,
		PageDocumentSchemaVersion: 1,
		CompilerVersion:           "internal-builder",
		SourceID:                  rec.ID,
		SourceType:                SourceTypePage,
		CanonicalPath:             rec.Path,
		SourceHash:                SHA256(rec.DocumentJSON),
		BuildInputHash:            SHA256(rec.DocumentJSON),
	}
	a, err := NewArtifact(html, m)
	if err != nil {
		rec.FailedReason = err.Error()
		rec.Status = rec.currentStatus()
		return "", err
	}
	if _, err = p.store.PutArtifact(a); err != nil {
		rec.FailedReason = err.Error()
		rec.Status = rec.currentStatus()
		return "", err
	}
	rec.StagedHash = a.Hash
	rec.FailedReason = ""
	rec.Status = rec.currentStatus()
	p.order++
	rec.Histories = append(rec.Histories, &HistoryEntry{
		Hash: a.Hash, Path: rec.Path, Status: StateReady, Order: p.order,
	})
	return a.Hash, nil
}

// publishLocked 在锁内执行发布（UpdateURL 复用）。
func (p *Publisher) publishLocked(rec *PageRecord, stagedHash string) error {
	loc := Locator{Provider: "local", Key: "artifacts/" + stagedHash}
	a, err := p.store.GetArtifact(loc)
	if err != nil {
		return err
	}
	if a.CanonicalPath != rec.Path {
		return fmt.Errorf("暂存产物路径 %s 与当前页面路径 %s 不一致，请重新构建", a.CanonicalPath, rec.Path)
	}
	if err = p.pub.Activate(rec.Path, loc); err != nil {
		rec.Status = rec.currentStatus()
		return fmt.Errorf("激活失败（线上版本保持不变）: %w", err)
	}
	p.supersedeActiveLocked(rec)
	rec.ActiveHash = stagedHash
	rec.Status = StatePublished
	rec.markLatestHistory(stagedHash, StatePublished, rec.Path)
	return nil
}

// supersedeActiveLocked 把已发布的旧版本标记为 Superseded（激活前调用，
// 以 hash 匹配保证同内容重复构建时旧条目同样失效）。
func (p *Publisher) supersedeActiveLocked(rec *PageRecord) {
	for _, h := range rec.Histories {
		if h.Status == StatePublished {
			h.Status = StateSuperseded
		}
	}
}

// markLatestHistory 写入/更新历史条目状态，按 Order 命中最新一次构建
// （同 hash 重复构建时只标记最后一条，避免旧条目被错误提升）。
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

// findHistory 查找页面历史条目。
func (rec *PageRecord) findHistory(hash string) *HistoryEntry {
	for _, h := range rec.Histories {
		if h.Hash == hash {
			return h
		}
	}
	return nil
}

// currentStatus 派生页面当前状态（published > ready > failed > draft）。
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

// Status 查询页面当前状态（测试与诊断用）。
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

// LoadRecord 恢复进程内页面记录（控制面重启或回滚目标注入时使用）。
//
// 持久化产物在 ArtifactStore 中永不消失，因此内核内存态可随时由
// 控制面用数据库指针重建；本方法整体替换同 ID 记录。
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
