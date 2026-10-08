package artifactservice

// 背景：产物的内容寻址闭包（content_objects + page_artifact_objects）一直只有写入路径，
// 迁移 068 的注释里就记着「content_objects 的 GC 待落地」。产物行被回收
// （payload_state='deleted'）之后，它引用的共享内容对象往往已无人引用，却永远留在表里 ——
// 表只增不减。本文件补上清除侧。
//
// 做法是最朴素的标记清除：标记 =「是否仍被现存产物行引用」，清除无标记的对象。
// 安全默认与产物 GC 保持同一口径（DryRun 默认 true、保留期同一天数），
// 这样两个 GC 在报表里可以放在一起读。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/internal/module/artifact/dto"
	"go_wp/internal/module/artifact/enums"
	"go_wp/internal/module/artifact/model"
	"go_wp/pkg/database"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

// normalizeLang 归一化产物语言：空 → 全局默认语言（sys_config 的 i18n 组 default_lang，未初始化回退 zh-CN）。
//
// 必要性：lang 是 page_artifacts 唯一键 (page_id, version, lang) 的第三维，
// 落库为空会让唯一键退化成 (page_id, version)，同页多语言重新互相覆盖
// （docs/06-D-site-i18n.md §15.5 第 1 条）。调用方（page 装配层）通常已显式传语言，
// 本函数是契约层直调与历史调用方的兜底，保证任何路径都不会写入空 lang。
func normalizeLang(lang string) string {
	if l := strings.TrimSpace(lang); l != "" {
		return l
	}
	return i18n.GetDefaultLang()
}

// validateRecordReq 必填/格式校验（DTO binding 仅 HTTP 层生效，契约层直调必须自校验，
// 否则空 ArtifactHash/PageID/Version=0 直接落库抛 PG 原始错误）。
func validateRecordReq(req *artifactdto.RecordReq) error {
	if req == nil {
		return errors.New(artifactenums.ErrInvalidArtifact)
	}
	if strings.TrimSpace(req.ArtifactID) == "" || strings.TrimSpace(req.PageID) == "" {
		return errors.New(artifactenums.ErrInvalidArtifact)
	}
	if strings.TrimSpace(req.ArtifactHash) == "" {
		return errors.New(artifactenums.ErrInvalidArtifact)
	}
	if req.Version <= 0 {
		return errors.New(artifactenums.ErrInvalidArtifact)
	}
	return nil
}

// Record 归档产物元数据与内容对象闭包（docs/03-pipeline.md §4）。
// 产物字节由 pipeline.ArtifactStore 在构建期落盘，本方法只做数据库投影：
// 元数据行 + manifest 内文件闭包写入 content_objects / page_artifact_objects，
// 三者同一事务提交；hash 重复时按产物不可变语义幂等返回现有记录。
func (s *Service) Record(ctx context.Context, req *artifactdto.RecordReq) (res *artifactdto.ArtifactResp, err error) {
	if err = validateRecordReq(req); err != nil {
		return nil, err
	}
	if existing, exists, existsErr := s.findByHash(ctx, req.PageID, req.ArtifactHash); existsErr != nil {
		return nil, existsErr
	} else if exists {
		// 内容寻址保证同 hash 同内容：重复归档直接返回既有记录。
		return toResp(existing), nil
	}

	var parsedManifest struct {
		Files map[string]string `json:"files"`
	}
	// manifest.files 是内容闭包来源：空闭包没有意义，统一拒绝
	// {"files":{}}、{"files":null} 与 {}（缺 files 键）三种形态
	// （此前仅 nil 被拒，空对象会静默建出无闭包记录，行为不一致）。
	if err = json.Unmarshal(req.Manifest, &parsedManifest); err != nil || len(parsedManifest.Files) == 0 {
		return nil, errors.New(artifactenums.ErrInvalidArtifact)
	}

	now := time.Now().UTC()
	// req.Manifest 是 pipeline.Manifest —— **输出清单**（canonicalPath / files /
	// dependencies / diagnostics），与产物目录里的 manifest.json 同一份字节，参与产物 hash。
	// 只写 manifest 一列（审计 DB-02，迁移 305）：这里此前把同一个 req.Manifest 也写进
	// build_input_manifest，两列同字节、而后者全仓零读取者；且「输入清单」这种产物并不存在
	// （pipeline.BuildInput 是内存结构，从不序列化落库；输入侧事实由 source_document 与
	// source_hash / build_input_hash 承载），所以两列同义 → 合并为 manifest 一个真源。
	entity := &artifactmodel.PageArtifactEntity{
		ID:                        req.ArtifactID,
		PageID:                    req.PageID,
		Version:                   req.Version,
		Lang:                      normalizeLang(req.Lang),
		SourceDocument:            req.SourceDocument,
		PageDocumentSchemaVersion: req.SchemaVersion,
		SourceHash:                req.SourceHash,
		BuildInputHash:            req.BuildInputHash,
		ArtifactProvider:          req.ArtifactProvider,
		ArtifactKey:               req.ArtifactKey,
		ArtifactHash:              req.ArtifactHash,
		CompilerVersion:           req.CompilerVersion,
		RegistryVersion:           req.RegistryVersion,
		Manifest:                  req.Manifest,
		PayloadState:              "available",
		Note:                      "",
		CreatedBy:                 defaultCreator(req.CreatedBy),
		CreatedAt:                 now,
	}

	lang := normalizeLang(req.Lang)
	err = s.model.Transaction(ctx, func(tx *gorm.DB) error {
		// 唯一冲突是不是「版本冲突」是业务判定，留在 service；SQL 在 model 的具名方法里。
		if cerr := s.model.CreateArtifactTx(ctx, tx, entity); cerr != nil {
			if database.IsUniqueViolation(cerr) {
				return errArtifactVersionConflict
			}
			return cerr
		}
		// 内容对象闭包：manifest.files 的每个文件哈希都是一条共享内容对象。
		//
		// 必须按哈希去重：同一份文件在页面里出现多次时 manifest.files 会带重复项，
		// 第二条 (artifact_id, content_hash) 直接撞主键，导致整个归档事务失败
		//（表现为「首次归档含同内容文件必失败」）。
		seenHashes := make(map[string]struct{}, len(parsedManifest.Files))
		for _, fileName := range sortedManifestFiles(parsedManifest.Files) {
			fileHash := strings.TrimSpace(parsedManifest.Files[fileName])
			if fileHash == "" {
				continue
			}
			if _, dup := seenHashes[fileHash]; dup {
				continue
			}
			seenHashes[fileHash] = struct{}{}
			if cerr := s.model.EnsureContentObjectTx(ctx, tx, fileHash, req.ArtifactProvider, artifactObjectKey(req.ArtifactKey, fileName), now); cerr != nil {
				return cerr
			}
			if cerr := s.model.CreateArtifactObjectTx(ctx, tx, &artifactmodel.PageArtifactObjectEntity{
				ArtifactID:  entity.ID,
				ContentHash: fileHash,
			}); cerr != nil {
				return cerr
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errArtifactVersionConflict) {
			if existing, gerr := s.model.GetByPageVersion(ctx, req.PageID, req.Version, lang); gerr == nil {
				if existing.ArtifactHash == req.ArtifactHash {
					return toResp(existing), nil
				}
				return nil, errors.New(artifactenums.ErrArtifactMismatch)
			}
		}
		return nil, mapPersistenceError(err)
	}
	return toResp(entity), nil
}

var errArtifactVersionConflict = errors.New("artifact version conflict")

// Detail 按 (pageId, hash) 查询产物元数据。
// nil/空参数属于请求不合法（ErrInvalidParam），与产物是否存在无关；
// 只有参数合法但查询无结果才返回 ErrArtifactNotFound。
func (s *Service) Detail(ctx context.Context, req *artifactdto.DetailReq) (res *artifactdto.ArtifactResp, err error) {
	if req == nil || strings.TrimSpace(req.PageID) == "" || strings.TrimSpace(req.Hash) == "" {
		return nil, errors.New(artifactenums.ErrInvalidParam)
	}
	entity, exists, err := s.findByHash(ctx, req.PageID, req.Hash)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.New(artifactenums.ErrArtifactNotFound)
	}
	return toResp(entity), nil
}

// DetailByID 按产物行 ID 查询产物元数据。
// nil/空 ID 属于请求不合法（ErrInvalidParam）；合法 ID 无记录才返回 ErrArtifactNotFound。
func (s *Service) DetailByID(ctx context.Context, req *artifactdto.DetailByIDReq) (res *artifactdto.ArtifactResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, errors.New(artifactenums.ErrInvalidParam)
	}
	entity, err := s.model.GetByID(ctx, req.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New(artifactenums.ErrArtifactNotFound)
	}
	if err != nil {
		return nil, err
	}
	return toResp(entity), nil
}

// ListStalePageIDs 在调用方给定的「当前产物」集合内挑出 registry_version 与 current
// 不同的产物，返回它们所属的页面 ID。
//
// 集合由调用方（page 模块）从自己的语言账本（page_publications / page_stagings）里选出 ——
// 「哪一行算当前产物」是 page 的领域事实，artifact 只回答「这一行是哪个版本产出的」。
// 详见 model 侧同名方法的注释（旧实现扫全部 available 行会把未 GC 的历史产物算进来）。
//
// current 为空表示调用方无法确定当前版本（例如二进制无 VCS 信息）：此时返回空列表，
// 宁可不标记也不误标记全站（避免每次启动都触发全量重建）。
// artifactIDs 为空（该站没有任何 active/staged 产物）同样返回空列表。
func (s *Service) ListStalePageIDs(ctx context.Context, current string, artifactIDs []string) (ids []string, err error) {
	if strings.TrimSpace(current) == "" || len(artifactIDs) == 0 {
		return nil, nil
	}
	return s.model.ListStalePageIDs(ctx, current, artifactIDs)
}

func (s *Service) findByHash(ctx context.Context, pageID, hash string) (e *artifactmodel.PageArtifactEntity, exists bool, err error) {
	e, err = s.model.GetByHash(ctx, pageID, hash)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return e, true, nil
}

// EnsureRecord 幂等归档：同 (pageID, hash) 直接返回；同 (pageID, version, lang)
// 重构建（编译器升级导致 hash 变化）时替换该行产物指针与对象闭包；
// 否则新建。确定性构建模型下「同版本同语言」产物的唯一正确语义。
//
// 语言维度（多语言 P3）：替换范围严格限定在同一语言内 —— 同页不同语言各占一行，
// 构建 en-US 绝不触碰 zh-CN 的行，路由 artifact_id 因此始终指向正确语言的产物。
func (s *Service) EnsureRecord(ctx context.Context, req *artifactdto.RecordReq) (res *artifactdto.ArtifactResp, err error) {
	if err = validateRecordReq(req); err != nil {
		return nil, err
	}
	lang := normalizeLang(req.Lang)
	if e, exists, err := s.findByHash(ctx, req.PageID, req.ArtifactHash); err != nil {
		return nil, err
	} else if exists {
		return toResp(e), nil
	}
	// 同版本同语言已有记录：替换产物内容（UNIQUE(page_id, version, lang) 允许恰好一行）。
	if e, err := s.model.GetByPageVersion(ctx, req.PageID, req.Version, lang); err == nil {
		var parsedManifest struct {
			Files map[string]string `json:"files"`
		}
		// 与 Record 同一校验语义：空 files（{} / null / 缺键）一律拒绝。
		if err = json.Unmarshal(req.Manifest, &parsedManifest); err != nil || len(parsedManifest.Files) == 0 {
			return nil, errors.New(artifactenums.ErrInvalidArtifact)
		}
		now := time.Now().UTC()
		// 同 Record：只写 manifest 一个真源（理由见 Record 里的注释与迁移 305）。
		newEntity := &artifactmodel.PageArtifactEntity{
			ID:                        e.ID,
			PageID:                    req.PageID,
			Version:                   req.Version,
			Lang:                      lang,
			SourceDocument:            req.SourceDocument,
			PageDocumentSchemaVersion: req.SchemaVersion,
			SourceHash:                req.SourceHash,
			BuildInputHash:            req.BuildInputHash,
			ArtifactProvider:          req.ArtifactProvider,
			ArtifactKey:               req.ArtifactKey,
			ArtifactHash:              req.ArtifactHash,
			CompilerVersion:           req.CompilerVersion,
			RegistryVersion:           req.RegistryVersion,
			Manifest:                  req.Manifest,
			PayloadState:              e.PayloadState,
			Note:                      e.Note,
			CreatedBy:                 e.CreatedBy,
			CreatedAt:                 e.CreatedAt,
		}
		objects := make([]artifactmodel.PageArtifactObjectEntity, 0, len(parsedManifest.Files))
		contentObjects := make([]artifactmodel.ContentObjectEntity, 0, len(parsedManifest.Files))
		seen := make(map[string]struct{}, len(parsedManifest.Files))
		// 闭包记按内容哈希去重（多个文件名共享同一内容时只归档一条），
		// 但 object_key 必须精确到**文件**（见 artifactObjectKey）：
		// 文件名按字典序遍历，保证「首个引用该 hash 的文件」是确定的。
		for _, fileName := range sortedManifestFiles(parsedManifest.Files) {
			fileHash := strings.TrimSpace(parsedManifest.Files[fileName])
			if fileHash == "" {
				continue
			}
			if _, dup := seen[fileHash]; dup {
				continue
			}
			seen[fileHash] = struct{}{}
			objects = append(objects, artifactmodel.PageArtifactObjectEntity{
				ArtifactID: e.ID, ContentHash: fileHash,
			})
			contentObjects = append(contentObjects, artifactmodel.ContentObjectEntity{
				ContentHash: fileHash,
				Provider:    req.ArtifactProvider,
				ObjectKey:   artifactObjectKey(req.ArtifactKey, fileName),
				ByteSize:    0,
				CreatedAt:   now,
			})
		}
		if err = s.model.ReplaceArtifactContent(ctx, e.ID, newEntity, objects, contentObjects); err != nil {
			return nil, mapPersistenceError(err)
		}
		return toResp(newEntity), nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	// 全新记录。
	return s.Record(ctx, req)
}

// sortedManifestFiles 按文件名（manifest.files 的 key）字典序返回文件名列表。
//
// map 的遍历顺序在 Go 里是随机的，而内容对象的 object_key 取「首个引用该 hash 的文件名」
// （同一份内容出现在多个文件名下时），因此必须有序遍历才是确定性行为。
func sortedManifestFiles(files map[string]string) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// artifactObjectKey 内容对象在存储里的位置 = 产物目录 + 文件名。
//
// **必须精确到文件**：content_objects 有 UNIQUE (provider, object_key)，而已发布的产物
// 是「一个目录装多个文件」（artifacts/<hash>/index.html、…/manifest.json）。
// 若沿用产物级 key（artifacts/<hash>）登记每条内容对象，第二条就撞唯一约束；
// 配上 ON CONFLICT DO NOTHING 会静默吞掉它，紧接着闭包表的外键找不到 content_hash
// 而整单回滚 —— 报出来的是外键违例，看起来像「库坏了」而不是「key 选错了」。
func artifactObjectKey(artifactKey, fileName string) string {
	base := strings.TrimSuffix(strings.TrimSpace(artifactKey), "/")
	name := strings.TrimPrefix(strings.TrimSpace(fileName), "/")
	if name == "" {
		return base
	}
	return base + "/" + name
}

// 共享内容对象（content_objects）的幂等写入已下移到 model：见
// artifactmodel.EnsureContentObjectTx —— ON CONFLICT (content_hash) DO NOTHING 与
// first-writer-wins 的完整论证跟 SQL 放在一起，避免同一份判定分居两处。

func defaultCreator(createdBy string) string {
	if strings.TrimSpace(createdBy) == "" {
		return "00000000-0000-0000-0000-000000000000"
	}
	return createdBy
}

func mapPersistenceError(err error) error {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return errors.New(artifactenums.ErrArtifactMismatch)
	}
	return err
}

func toResp(e *artifactmodel.PageArtifactEntity) *artifactdto.ArtifactResp {
	var meta struct {
		CanonicalPath string `json:"canonicalPath"`
	}
	if err := json.Unmarshal(e.Manifest, &meta); err != nil {
		logger.Scene("artifact").With("err", err).Warn("产物 manifest 解析失败")
	}
	return &artifactdto.ArtifactResp{
		ID:               e.ID,
		PageID:           e.PageID,
		Version:          e.Version,
		Lang:             e.Lang,
		SourceDocument:   e.SourceDocument,
		SourceHash:       e.SourceHash,
		BuildInputHash:   e.BuildInputHash,
		ArtifactProvider: e.ArtifactProvider,
		ArtifactKey:      e.ArtifactKey,
		ArtifactHash:     e.ArtifactHash,
		CanonicalPath:    meta.CanonicalPath,
		CompilerVersion:  e.CompilerVersion,
		RegistryVersion:  e.RegistryVersion,
		Manifest:         e.Manifest,
		PayloadState:     e.PayloadState,
		CreatedBy:        e.CreatedBy,
		CreatedAt:        utils.NewJSONTime(e.CreatedAt),
	}
}

// ListGCCandidates 列出可回收候选（见 contract 说明）。
func (s *Service) ListGCCandidates(ctx context.Context, before time.Time, excludeIDs []string) (list []artifactdto.GCCandidateResp, err error) {
	rows, err := s.model.ListGCCandidates(ctx, before, excludeIDs)
	if err != nil {
		return nil, err
	}
	list = make([]artifactdto.GCCandidateResp, 0, len(rows))
	for _, r := range rows {
		list = append(list, artifactdto.GCCandidateResp{
			ID: r.ID, PageID: r.PageID, Version: r.Version, Lang: r.Lang,
			ArtifactHash: r.ArtifactHash, ArtifactKey: r.ArtifactKey, CreatedAt: utils.NewJSONTime(r.CreatedAt),
		})
	}
	return list, nil
}

// CountOtherAvailableByHash 见 contract 说明。
func (s *Service) CountOtherAvailableByHash(ctx context.Context, hash, excludeID string) (n int64, err error) {
	return s.model.CountOtherAvailableByHash(ctx, hash, excludeID)
}

// MarkPayloadState 见 contract 说明。
func (s *Service) MarkPayloadState(ctx context.Context, ids []string, state string) (n int64, err error) {
	return s.model.MarkPayloadState(ctx, ids, state, time.Now().UTC())
}

const (
	// defaultContentObjectRetentionDays 内容对象默认保留窗口，与产物 GC 同口径：
	// 窗口内的对象一律不回收 —— 刚发布的内容对象可能只是还没被后续归档复用。
	defaultContentObjectRetentionDays = 30
	// contentObjectGCBatch 单批处理上限：分批的意义是不制造长事务与长锁等待。
	contentObjectGCBatch = 2000
)

// ExternalContentRefs 由其他模块注入：指出入参 hash 中哪些仍被它引用。
//
// 存在的理由是「引用来源不止一处」：page_artifact_objects 是当前唯一的闭包投影，
// 但 content_objects 是共享表，其它模块的产物将来同样会引用它。孤儿判定必须能问遍
// 所有来源，而不是把「我不知道的引用」当成「没有引用」。
//
// 未注入 = 确认没有任何外部引用来源。当前即此状态：presentation 侧原本的孪生表
// presentation_artifact_objects 零写入方，已按 CQ-015 删除（迁移 207）—— 那张表
// 「有 DDL、无消费方」，留着只会让人读成「presentation 的闭包已经在跑」。
// 等该侧归档真正落地时，按 page 侧的 peer 形态重建并在这里注入，判定无需再改。
// 注入后查询失败一律放弃本轮回收。
type ExternalContentRefs func(ctx context.Context, hashes []string) (referenced []string, err error)

// SetExternalContentRefs 注入外部引用来源；装配期调用一次。
func (s *Service) SetExternalContentRefs(fn ExternalContentRefs) {
	if s == nil {
		return
	}
	s.externalRefs = fn
}

// 单条候选的处理结论（与产物 GC 的 Action 取值同风格）。
const (
	objectActionWouldDelete   = "would_delete"
	objectActionDeleted       = "deleted"
	objectActionKeptExternal  = "kept_external"
	objectActionKeptReclaimed = "kept_reclaimed"
	objectActionDeleteFailed  = "delete_failed"
)

// GarbageCollectContentObjects 回收不再被任何产物行引用的共享内容对象。
//
// 顺序：先数（Orphans = 窗口外的孤儿总数）→ 取一批候选 → 问外部引用 → dryRun 只报告，
// 真删走「语句内复查」的 DELETE（见 model.DeleteOrphanContentObjects）。
//
// 与产物 GC 的两点差异：
//  1. 内容对象本身不在磁盘上（它们是指向产物目录内文件的 Locator 投影），删除只动 DB；
//     产物目录的删除仍由产物 GC 负责，本函数不碰文件系统，因此两个 GC 可以任意顺序，
//     但调用方应「先产物、后内容对象」—— 那样同一轮里刚变成孤儿的对象能立刻被收掉。
//  2. 失败口径是「整轮放弃」而不是「逐条跳过」：问不到外部引用时一个都不删。
//     误删共享对象的代价高于多留一轮垃圾。
func (s *Service) GarbageCollectContentObjects(ctx context.Context, req *artifactdto.ContentObjectGCReq) (res *artifactdto.ContentObjectGCResp, err error) {
	retention, dryRun, limit := defaultContentObjectRetentionDays, true, contentObjectGCBatch
	if req != nil {
		if req.RetentionDays > 0 {
			retention = req.RetentionDays
		}
		if req.DryRun != nil {
			dryRun = *req.DryRun
		}
		if req.Limit > 0 {
			limit = req.Limit
		}
	}
	before := time.Now().UTC().AddDate(0, 0, -retention)
	res = &artifactdto.ContentObjectGCResp{
		RetentionDays: retention, DryRun: dryRun, Items: []artifactdto.OrphanContentObjectResp{},
	}

	orphans, err := s.model.CountOrphanContentObjects(ctx, before)
	if err != nil {
		return nil, err
	}
	res.Orphans = orphans
	if orphans == 0 {
		return res, nil
	}
	rows, err := s.model.ListOrphanContentObjects(ctx, before, limit)
	if err != nil {
		return nil, err
	}
	res.Scanned = len(rows)
	if len(rows) == 0 {
		return res, nil
	}

	hashes := make([]string, 0, len(rows))
	for _, row := range rows {
		hashes = append(hashes, row.ContentHash)
	}
	keptExternal, eerr := s.externalKept(ctx, hashes)
	if eerr != nil {
		return nil, eerr
	}

	if dryRun {
		for _, row := range rows {
			item := orphanItem(row)
			if keptExternal[row.ContentHash] {
				item.Action, item.Reason = objectActionKeptExternal, "外部模块仍引用该内容对象"
				res.SkippedExtern++
			} else {
				item.Action = objectActionWouldDelete
			}
			res.Items = append(res.Items, item)
		}
		logger.Scene("artifact").With("orphans", res.Orphans).With("scanned", res.Scanned).
			With("skippedExternal", res.SkippedExtern).Info("孤儿内容对象预演完成（未删除）")
		return res, nil
	}

	deletable := make([]string, 0, len(rows))
	for _, row := range rows {
		if !keptExternal[row.ContentHash] {
			deletable = append(deletable, row.ContentHash)
		}
	}
	deletedSet, derr := s.deleteOrphans(ctx, deletable, before)
	if derr != nil {
		for _, row := range rows {
			item := orphanItem(row)
			if keptExternal[row.ContentHash] {
				item.Action, item.Reason = objectActionKeptExternal, "外部模块仍引用该内容对象"
				res.SkippedExtern++
			} else {
				item.Action, item.Reason = objectActionDeleteFailed, derr.Error()
				res.Failed++
			}
			res.Items = append(res.Items, item)
		}
		res.FailedRate = failedRate(res.Failed, res.Scanned)
		logger.Scene("artifact").With("failed", res.Failed).With("failedRate", res.FailedRate).
			Error(derr, "孤儿内容对象回收失败")
		return res, nil
	}

	// 删后复查：没进 RETURNING 的候选有两种成因 —— 「被并发归档重新引用」（正常赛跑）与
	// 「删除语句没生效」（异常：行还在、且仍无引用）。只看差集分不出来，两者都落进去；
	// 而默认按前者解释，异常就永远静默（外键挡删那次正是这样只增不减的）。
	stillOrphan, rerr := s.stillOrphanAfterDelete(ctx, deletable, deletedSet, before)
	if rerr != nil {
		// 复查失败只影响归因精度：本轮删除已经执行完毕，不改变结果，也不能反过来报失败。
		logger.Scene("artifact").With("undecided", len(deletable)-len(deletedSet)).
			Error(rerr, "删除后复查失败：本轮无法区分并发认领与删除未生效")
	}

	for _, row := range rows {
		item := orphanItem(row)
		switch {
		case keptExternal[row.ContentHash]:
			item.Action, item.Reason = objectActionKeptExternal, "外部模块仍引用该内容对象"
			res.SkippedExtern++
		case deletedSet[row.ContentHash]:
			item.Action = objectActionDeleted
			res.Deleted++
		case stillOrphan[row.ContentHash]:
			// 删除没生效：行仍在，且复查确认它仍无任何引用。这不是赛跑，是异常。
			item.Action, item.Reason = objectActionDeleteFailed, "删除未生效：复查确认该行仍存在且仍无引用"
			res.Failed++
		default:
			// 候选与删除之间被并发归档重新引用：这是正常赛跑结果（语句内复查拦住了误删），
			// 不计失败也不计跳过 —— 下一轮它若仍是孤儿自会再被选中。
			item.Action, item.Reason = objectActionKeptReclaimed, "删除前已被新产物引用，本轮保留"
		}
		res.Items = append(res.Items, item)
	}
	res.FailedRate = failedRate(res.Failed, res.Scanned)
	if res.Failed > 0 {
		// 失败率告警：失败口径是「记统计 + 打日志」，调用方拿不到 error，
		// 所以日志必须是 Error 级并带比率 —— 正常一轮 Failed 恒为 0，非零必是异常。
		logger.Scene("artifact").With("orphans", res.Orphans).With("scanned", res.Scanned).
			With("deleted", res.Deleted).With("skippedExternal", res.SkippedExtern).
			With("failed", res.Failed).With("failedRate", res.FailedRate).
			Error(fmt.Errorf("%s: %d/%d", artifactenums.ErrContentObjectDeleteNotApplied, res.Failed, res.Scanned),
				"内容对象回收存在失败（失败率告警）")
	} else if res.Deleted > 0 {
		logger.Scene("artifact").With("orphans", res.Orphans).With("scanned", res.Scanned).
			With("deleted", res.Deleted).With("skippedExternal", res.SkippedExtern).
			Info("孤儿内容对象回收完成")
	}
	return res, nil
}

// stillOrphanAfterDelete 复查「候选里没被删掉的」哪些仍然是孤儿。
//
// 返回空集且 err 为 nil，表示每个没删掉的候选都已被重新引用 —— 这才是正常赛跑。
func (s *Service) stillOrphanAfterDelete(ctx context.Context, deletable []string, deleted map[string]bool, before time.Time) (set map[string]bool, err error) {
	set = map[string]bool{}
	if len(deleted) >= len(deletable) {
		return set, nil
	}
	remaining := make([]string, 0, len(deletable)-len(deleted))
	for _, h := range deletable {
		if !deleted[h] {
			remaining = append(remaining, h)
		}
	}
	if len(remaining) == 0 {
		return set, nil
	}
	still, serr := s.model.ListStillOrphanHashes(ctx, remaining, before)
	if serr != nil {
		return set, serr
	}
	for _, h := range still {
		set[h] = true
	}
	return set, nil
}

// failedRate 失败率（Scanned 为 0 时返回 0，避免除零）。
func failedRate(failed, scanned int) float64 {
	if scanned <= 0 {
		return 0
	}
	return float64(failed) / float64(scanned)
}

// externalKept 询问外部引用来源，返回「必须保留」的 hash 集合。
// 未注入守卫 = 没有外部来源，返回空集合；注入后失败即向上抛（调用方整轮放弃）。
func (s *Service) externalKept(ctx context.Context, hashes []string) (kept map[string]bool, err error) {
	kept = map[string]bool{}
	if s.externalRefs == nil || len(hashes) == 0 {
		return kept, nil
	}
	referenced, err := s.externalRefs(ctx, hashes)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", artifactenums.ErrContentRefQueryFailed, err)
	}
	for _, h := range referenced {
		kept[h] = true
	}
	return kept, nil
}

// deleteOrphans 执行删除并把结果转成集合；空入参直接返回空集合（不发语句）。
func (s *Service) deleteOrphans(ctx context.Context, hashes []string, before time.Time) (deleted map[string]bool, err error) {
	deleted = map[string]bool{}
	if len(hashes) == 0 {
		return deleted, nil
	}
	rows, err := s.model.DeleteOrphanContentObjects(ctx, hashes, before)
	if err != nil {
		return nil, err
	}
	for _, h := range rows {
		deleted[h] = true
	}
	return deleted, nil
}

// orphanItem 把一条候选转成响应条目（Action 由调用方按结论填写）。
func orphanItem(row artifactmodel.ContentObjectEntity) artifactdto.OrphanContentObjectResp {
	return artifactdto.OrphanContentObjectResp{
		ContentHash: row.ContentHash, Provider: row.Provider, ObjectKey: row.ObjectKey,
		ByteSize: row.ByteSize, CreatedAt: utils.NewJSONTime(row.CreatedAt),
	}
}
