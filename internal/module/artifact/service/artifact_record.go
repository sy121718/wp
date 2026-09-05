package artifactservice

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	artifactdto "go_wp/internal/module/artifact/dto"
	artifactenums "go_wp/internal/module/artifact/enums"
	artifactmodel "go_wp/internal/module/artifact/model"
	"go_wp/pkg/logger"

	"gorm.io/gorm"
)

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

	err = s.model.Transaction(ctx, func(tx *gorm.DB) error {
		if err := tx.Create(entity).Error; err != nil {
			return err
		}
		// 内容对象闭包：manifest.files 的每个文件哈希都是一条共享内容对象。
		for _, fileHash := range parsedManifest.Files {
			if strings.TrimSpace(fileHash) == "" {
				continue
			}
			if err := ensureContentObject(tx, fileHash, req.ArtifactProvider, req.ArtifactKey, now); err != nil {
				return err
			}
			if err := tx.Create(&artifactmodel.PageArtifactObjectEntity{
				ArtifactID:  entity.ID,
				ContentHash: fileHash,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, mapPersistenceError(err)
	}
	return toResp(entity), nil
}

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

// EnsureRecord 幂等归档：同 (pageID, hash) 直接返回；同 (pageID, version)
// 重构建（编译器升级导致 hash 变化）时替换该行产物指针与对象闭包；
// 否则新建。确定性构建模型下同版本产物的唯一正确语义。
func (s *Service) EnsureRecord(ctx context.Context, req *artifactdto.RecordReq) (res *artifactdto.ArtifactResp, err error) {
	if err = validateRecordReq(req); err != nil {
		return nil, err
	}
	if e, exists, err := s.findByHash(ctx, req.PageID, req.ArtifactHash); err != nil {
		return nil, err
	} else if exists {
		return toResp(e), nil
	}
	// 同版本已有记录：替换产物内容（UNIQUE(page_id, version) 允许恰好一行）。
	if e, err := s.model.GetByPageVersion(ctx, req.PageID, req.Version); err == nil {
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
		// 遍历 VALUE（内容哈希）而非 map KEY（文件名）：内容寻址语义。
		for _, fileHash := range parsedManifest.Files {
			if strings.TrimSpace(fileHash) == "" {
				continue
			}
			objects = append(objects, artifactmodel.PageArtifactObjectEntity{
				ArtifactID: e.ID, ContentHash: fileHash,
			})
			if err = ensureContentObject(s.model.DB(ctx), fileHash, req.ArtifactProvider, req.ArtifactKey, now); err != nil {
				return nil, err
			}
		}
		if err = s.model.ReplaceArtifactContent(ctx, e.ID, newEntity, objects); err != nil {
			return nil, mapPersistenceError(err)
		}
		return toResp(newEntity), nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	// 全新记录。
	return s.Record(ctx, req)
}

// ensureContentObject 幂等写入共享内容对象（content_objects，content_hash 为主键）。
//
// 语义：内容寻址下同一 content_hash 代表同一内容字节，其物理位置应当唯一，
// 因此采用 first-writer-wins —— 首个引用该 hash 的 (provider, object_key)
// 即该对象的规范 Locator，后续引用（即使 provider/object_key 不同）只共享
// 对象行，不更新、不覆盖。这是有意的设计权衡而非缺陷：
//   - content_hash 主键约束保证一行一对象，位置唯一，不做多存储冗余登记；
//   - 确定性构建不变量（同输入同产物）保证正常路径下同内容同位置，
//     不同 provider 引用同一 hash 是跨存储冗余信号，首个写入即权威；
//   - 若改为 last-writer-wins，同一对象位置会随引用顺序漂移，
//     破坏内容寻址的不可变语义，且并发写入存在竞态。
//
// 产物行（page_artifacts.artifact_provider/artifact_key）各自记录其自身位置，
// 不受本函数的 first-writer-wins 影响。
func ensureContentObject(tx *gorm.DB, contentHash, provider, objectKey string, now time.Time) error {
	var count int64
	if err := tx.Model(&artifactmodel.ContentObjectEntity{}).
		Where("content_hash = ?", contentHash).
		Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	return tx.Create(&artifactmodel.ContentObjectEntity{
		ContentHash: contentHash,
		Provider:    provider,
		ObjectKey:   objectKey,
		ByteSize:    0,
		CreatedAt:   now,
	}).Error
}

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
		CreatedAt:        e.CreatedAt,
	}
}
