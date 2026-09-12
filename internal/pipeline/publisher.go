package pipeline

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"go_wp/internal/builder"
	"go_wp/internal/templates"
	"go_wp/pkg/logger"
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
	// Path 当前访问路径：单语言（off 方案）为草稿逻辑路径，多语言（方案 A'）为
	// 语言访问路径（默认语言无前缀 /about，非默认语言短码 /en/about，
	// 见 LangURLRule.Path）。激活与回滚校验都以它为准。
	Path string
	// Lang 本记录的构建语言（多语言 P2，docs/06-D §4.1 第 5 项）。
	//
	// 内存记录仍以页面 ID 为键（一次构建一个语言）；「按 (pageID, lang) 持有
	// 独立状态机」属后续阶段（docs/06-D §4.1 第 5 项标注为高风险改动）。
	// 为空表示未接入语言：Manifest 不写 lang，路径不带前缀（行为与改造前一致）。
	Lang string
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

// BuildInput 一次构建的完整输入（多语言 P2，docs/06-D §4.1 第 3 项）。
//
// 语言（Lang）与访问路径（Path）是同一层构建环境：同一 Page Document 在每个语言
// 下独立构建，产物路径按语言方案映射（默认语言无前缀、非默认语言短码）、
// Manifest 记录完整语言码，因此两者必须一起传入。
type BuildInput struct {
	// PageID 页面 ID：供装配层解析「文档自身不携带」的站点级资源（工程 ID → 导航菜单）。
	PageID string
	// Lang 本次构建的目标语言（空 = 未接入语言：Manifest 不写 lang、路径不带前缀）。
	Lang string
	// Path 已本地化的访问路径（见 LangURLRule.Path）。
	Path string
	// DocJSON 冻结的 Page Document 字节。
	DocJSON []byte
}

// CompileFn 冻结编译函数：构建输入 → 完整 HTML 文档字节。
// 发布期唯一编译入口；实现必须确定性（docs/03-pipeline.md §3.4）。
// ctx 为发起构建的请求上下文（构建链需查库解析块/集合时传播，支持超时取消）。
type CompileFn func(ctx context.Context, in BuildInput) (html []byte, err error)

// DefaultCompile 默认编译器：internal/builder 文档编译 + 完整文档组装。
// 不解析站点级资源（PageID 仅用于记录）；语言经 WithLanguage 影响组件固定文案。
func DefaultCompile(ctx context.Context, in BuildInput) ([]byte, error) {
	page, err := builder.ParsePage(in.DocJSON)
	if err != nil {
		return nil, err
	}
	// 组件模板 Set（Jet 渲染路径必需；embed 加载，不依赖进程工作目录）。
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		return nil, err
	}
	opts := append(ClientAssetOptions(),
		builder.WithContext(ctx), builder.WithComponentSet(set), builder.WithLanguage(in.Lang))
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

// Draft 一次草稿冻结输入（多语言 P2：语言与路径同层，均随草稿一起进入内核）。
type Draft struct {
	// Path 访问路径：单语言为逻辑路径，多语言为按方案映射后的语言访问路径。
	Path string
	// Lang 构建语言（空 = 未接入语言）。
	Lang string
	// DocJSON 草稿文档字节。
	DocJSON []byte
}

// Option Publisher 构造选项。
type Option func(*Publisher)

// WithCompile 注入替代编译器（测试/集成用）。
func WithCompile(fn CompileFn) Option {
	return func(p *Publisher) { p.compile = fn }
}

// DependencyProvider 构建期依赖提供者：按构建输入返回依赖条目，
// 内核把它们写入 Manifest.dependencies（docs/03-pipeline.md §8.2）。
//
// 装配层用它声明「产物字节依赖的外部资源」（如文案词条 revision），
// 内核保持通用——不 import 业务模块、不认识具体资源类型。
type DependencyProvider func(ctx context.Context, in BuildInput) []Dependency

// WithDependencies 注入构建期依赖提供者（未注入时 Manifest.dependencies 为空）。
func WithDependencies(fn DependencyProvider) Option {
	return func(p *Publisher) { p.deps = fn }
}

// Publisher 发布服务：页面生命周期状态机（0-A1 §2）+ 流水线编排。
//
// 并发保护：单进程内互斥；多实例部署由 Phase 0-A1 build 模块（数据库 + 队列）负责。
type Publisher struct {
	store   Store
	pub     PublicationStore
	compile CompileFn
	// deps 构建期依赖提供者（可选）：为 Manifest.dependencies 供数。
	deps DependencyProvider

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
	return p.SaveDraftInput(pageID, expectedVersion, Draft{Path: path, DocJSON: docJSON})
}

// SaveDraftInput 保存草稿（多语言 P2：显式携带语言，见 Draft）。
// 语言为空时等价旧签名 SaveDraft（不写 Manifest.lang、路径不带前缀）。
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
//
// 并发设计（M2 修复）：编译与落盘是慢操作（Jet 渲染 + IO），原实现持有全局
// mutex 执行，单页慢构建会阻塞所有页面的状态机。现改为「锁内取快照 → 锁外
// 编译落盘 → 锁内二次校验版本并提交」：编译期间其他页面操作不受阻塞，提交时
// 校验版本未变（并发 SaveDraft 推进版本则本次构建作废）。
func (p *Publisher) Build(ctx context.Context, pageID string, expectedVersion int) (hash string, err error) {
	// 锁内：校验版本 + 取冻结快照。
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
	in := BuildInput{PageID: pageID, Lang: rec.Lang, Path: rec.Path, DocJSON: docSnapshot}
	p.mu.Unlock()

	// 锁外：确定性编译 + 产物落盘。
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

	// 锁内：二次校验版本 + 提交暂存。
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
	loc := ArtifactLocator(rec.StagedHash)

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
		logger.Scene("build").With("pageId", rec.ID).Error(err, "激活失败（线上保持不变）")
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

	loc := ArtifactLocator(targetHash)
	if err = p.store.VerifyArtifact(loc, targetHash); err != nil {
		return err
	}
	if err = p.pub.Activate(rec.Path, loc); err != nil {
		logger.Scene("build").With("pageId", rec.ID).Error(err, "回滚失败")
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
func (p *Publisher) UpdateURL(ctx context.Context, pageID string, newPath string, withRedirect bool) (oldPath string, err error) {
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

	// 纯草稿（从未发布，含已构建未发布）页面：只迁移草稿路径（内核 Path 字段），
	// 不构建、不激活、不写 active_path；旧路径从未在访问面激活，也无需
	// 301 / 取消激活处理。DB 侧迁移（draft_path 与 reserved 路由）由调用方
	// 负责（page service MoveDraftPath / RenameReserved）。
	if !rec.hasPublishedHistory() {
		if _, err = p.saveDraftLocked(pageID, rec.Version, Draft{Path: nPath, Lang: rec.Lang, DocJSON: rec.DocumentJSON}); err != nil {
			p.mu.Unlock()
			return oldPath, err
		}
		p.mu.Unlock()
		logger.Scene("build").With("pageId", pageID).With("oldPath", oldPath).With("newPath", nPath).
			Info("纯草稿改 URL：仅迁移草稿路径，未构建未激活")
		return oldPath, nil
	}

	// 1. 锁内：新 URL 写入草稿路径（Version +1）。
	if _, err = p.saveDraftLocked(pageID, rec.Version, Draft{Path: nPath, Lang: rec.Lang, DocJSON: rec.DocumentJSON}); err != nil {
		p.mu.Unlock()
		return oldPath, err
	}
	version := rec.Version
	in := BuildInput{PageID: pageID, Lang: rec.Lang, Path: nPath, DocJSON: append([]byte(nil), rec.DocumentJSON...)}
	p.mu.Unlock()

	// 2. 锁外：基于新 URL 构建（慢操作不阻塞其他页面）。
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

	// 3. 锁内：二次校验版本 + 原子激活新 URL。
	p.mu.Lock()
	defer p.mu.Unlock()
	if rec.Version != version {
		return oldPath, ErrVersionConflict
	}
	if err = p.publishLocked(rec, a.Hash); err != nil {
		return oldPath, err
	}

	// 4. 处理旧 URL（新 URL 已生效后执行；失败不撤销新 URL）。
	// 重定向目标必须是新 URL（0-A2 §1.1：旧 URL -> 新 URL 的 301）。
	//
	// 新 URL 已上线是不可逆事实：旧路径处置失败只记日志、返回成功，让调用方
	// 继续 DB 同步（draft_path / 路由）。否则内核与 FS 已在新路径、DB 仍停在
	// 旧路径，且 syncKernel 的 LoadRecord 重建无法修复路由脱节，造成三方分裂。
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
		if aerr := p.pub.Activate(oldPath, rl); aerr != nil {
			logger.Scene("build").With("pageId", pageID).With("oldPath", oldPath).Error(aerr, "旧 URL 301 激活失败（新 URL 已生效）")
			return oldPath, nil
		}
	} else {
		if derr := p.pub.Deactivate(oldPath); derr != nil {
			logger.Scene("build").With("pageId", pageID).With("oldPath", oldPath).Error(derr, "旧 URL 取消激活失败（新 URL 已生效）")
			return oldPath, nil
		}
	}
	return oldPath, nil
}

// compileArtifact 锁外编译并落盘（纯函数，不碰 Publisher 锁）。
// in 为锁内取出的冻结快照（页面 ID / 语言 / 路径 / 文档字节），编译期间不访问
// rec 可变字段，因此可在锁外并行执行——慢操作不再阻塞其他页面的状态机（M2）。
func (p *Publisher) compileArtifact(ctx context.Context, in BuildInput) (a *Artifact, err error) {
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
		SourceHash:                SHA256(in.DocJSON),
		BuildInputHash:            SHA256(in.DocJSON),
	}
	if p.deps != nil {
		m.Dependencies = p.deps(ctx, in)
	}
	a, err = NewArtifact(html, m)
	if err != nil {
		return nil, err
	}
	if _, err = p.store.PutArtifact(a); err != nil {
		return nil, err
	}
	return a, nil
}

// RestoreArtifact 按给定构建输入重新编译并落盘，返回完整产物（含 Hash）。
//
// 用途：灾难恢复 —— 产物文件丢失后，用 DB 里冻结的 source_document 重新编译。
// 语义边界：
//   - 不修改内存内核状态、不激活 URL、不写数据库；纯「把 artifacts/<hash>/ 写出来」；
//   - 调用方**必须**校验返回的 Hash 是否等于元数据里记录的值。
//
// 为什么必须校验：产物 hash = SHA256(manifestJSON + "\n" + indexHTML)，而 HTML 的
// 内容取决于「源文档 + 组件注册表 + 编译期依赖内容（CMS/主题/导航）」。只有这些
// 输入全部未变时 hash 才相等；任一变化（典型是组件更新）都会产出**另一个版本**，
// 此时绝不能拿它冒充旧产物。
func (p *Publisher) RestoreArtifact(ctx context.Context, in BuildInput) (*Artifact, error) {
	if p.compile == nil {
		return nil, fmt.Errorf("编译函数未注入，无法重建产物")
	}
	return p.compileArtifact(ctx, in)
}

// ArtifactExists 检查产物文件是否存在且内容与期望 hash 一致（不存在返回错误）。
func (p *Publisher) ArtifactExists(hash string) error {
	return p.store.VerifyArtifact(ArtifactLocator(hash), hash)
}

// publishLocked 在锁内执行发布（UpdateURL 复用）。
func (p *Publisher) publishLocked(rec *PageRecord, stagedHash string) error {
	loc := ArtifactLocator(stagedHash)
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

// maxHistoryEntries 单页历史条目上限：防止长期运行进程内 Histories 无界增长，
// 同时约束 supersedeActiveLocked/markLatestHistory/findHistory 的全量线性扫描开销。
const maxHistoryEntries = 50

// trimHistory 淘汰超限历史条目：优先从头部删除最老的 Superseded（保留
// published/ready 与最近条目），Superseded 不足时强制截断到最近 N 条。
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
	// Superseded 不足时强制保留最近 N 条（回滚依赖最新历史）。
	if len(kept) > maxHistoryEntries {
		kept = kept[len(kept)-maxHistoryEntries:]
	}
	rec.Histories = kept
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

// hasPublishedHistory 判断页面是否曾上线：活跃产物指针非空，或历史条目
// 中出现过 published 状态（回滚/改 URL 重建的内核记录依赖后者）。
// 注意不能用 Status 字段判断：restoreKernelForUpdate 重建的纯草稿记录
// 会把 Status 硬编码为 published，而 ActiveHash/Histories 才是真实依据。
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
