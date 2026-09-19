package artifactservice

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"encoding/json"
	artifactdto "go_wp/internal/module/artifact/dto"
	artifactenums "go_wp/internal/module/artifact/enums"
	artifactmodel "go_wp/internal/module/artifact/model"
	"go_wp/pkg/database"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
	"gorm.io/gorm"
)

// normalizeLang 归一化产物语言：空 → 站点默认语言（i18n.default_lang，未初始化回退 zh-CN）。
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
	entity := &artifactmodel.PageArtifactEntity{
		ID:                        req.ArtifactID,
		PageID:                    req.PageID,
		Version:                   req.Version,
		Lang:                      normalizeLang(req.Lang),
		SourceDocument:            req.SourceDocument,
		PageDocumentSchemaVersion: req.SchemaVersion,
		SourceHash:                req.SourceHash,
		BuildInputManifest:        req.Manifest,
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

// ListPageIDsByOtherRegistryVersion 返回「存在 registry_version 与 current 不同的
// 可用产物」的页面 ID。
//
// current 为空表示调用方无法确定当前版本（例如二进制无 VCS 信息）：此时返回空列表，
// 宁可不标记也不误标记全站（避免每次启动都触发全量重建）。
func (s *Service) ListPageIDsByOtherRegistryVersion(ctx context.Context, current string) (ids []string, err error) {
	if strings.TrimSpace(current) == "" {
		return nil, nil
	}
	return s.model.ListPageIDsByOtherRegistryVersion(ctx, current)
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
		newEntity := &artifactmodel.PageArtifactEntity{
			ID:                        e.ID,
			PageID:                    req.PageID,
			Version:                   req.Version,
			Lang:                      lang,
			SourceDocument:            req.SourceDocument,
			PageDocumentSchemaVersion: req.SchemaVersion,
			SourceHash:                req.SourceHash,
			BuildInputManifest:        req.Manifest,
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
