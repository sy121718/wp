package pageservice

// page_publish_artifact.go — 产物行的读写与指针读取（staged 查询、落行、active hash、错误映射）。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"go_wp/internal/builder"
	artifactcontract "go_wp/internal/module/artifact/contract"
	artifactenums "go_wp/internal/module/artifact/enums"
	pagemodel "go_wp/internal/module/page/model"

	"go_wp/internal/pipeline"
	"go_wp/pkg/logger"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// stagedArtifactOf 取该语言的暂存产物（page_stagings 为真源）。
//
// 兼容口径：迁移 063 之前只写 pages.staged_artifact_id，该镜像仅在「产物语言与
// 目标语言一致」时采用（多语言站点里镜像可能属于别的语言，绝不将错就错）。
func (s *Service) stagedArtifactOf(ctx context.Context, page *pagemodel.PageEntity, lang string) (*artifactcontract.ArtifactResp, error) {
	artifactID := ""
	st, err := s.model.GetStaging(ctx, page.ID, lang)
	switch {
	case err == nil && st != nil:
		artifactID = st.ArtifactID
	case err != nil && !errors.Is(err, gorm.ErrRecordNotFound):
		return nil, err
	}
	// 回退镜像：仅当镜像产物确实属于目标语言时可用。
	if artifactID == "" {
		if page.StagedArtifactID == nil || *page.StagedArtifactID == "" {
			return nil, ErrNoStagedArtifact
		}
		artifactID = *page.StagedArtifactID
	}
	art, derr := s.artifacts.DetailByID(ctx, &artifactcontract.DetailByIDReq{ID: artifactID})
	if derr != nil {
		// 仅真实「无暂存产物」（artifact 侧 ErrArtifactNotFound）归一为 409 业务冲突；
		// DB 故障等其他系统错误原样透传并记日志，避免被误判为「无暂存产物」误导前端。
		if strings.Contains(derr.Error(), artifactenums.ErrArtifactNotFound) {
			return nil, ErrNoStagedArtifact
		}
		logger.Scene("publication").With("pageId", page.ID).With("artifactID", artifactID).
			Error(derr, "查询暂存产物失败")
		return nil, derr
	}
	if art.Lang != "" && art.Lang != lang {
		// 该语言没有暂存产物（镜像属于其他语言）：按「无暂存产物」处理，不跨语言发布。
		return nil, ErrNoStagedArtifact
	}
	return art, nil
}

// ensureArtifactRow 返回该 hash 对应的产物元数据行 ID；不存在则归档新建。
// sourceDocument 必须与构建该产物的输入一致：Build 路径为当前草稿，
// UpdateURL 路径为活动产物冻结源文档（内核 restoreKernelForUpdate 的输入）。
// 若统一归档 page.DraftDocument，草稿较新时产物字节与归档 SourceDocument/
// SourceHash 不对应，日后按该产物回滚会编译出不同 hash（ErrRollbackPathMismatch）。
//
// lang 为本次构建语言：产物行唯一键是 (page_id, version, lang)，同页多语言各占一行；
// 预检查询按 hash（hash 覆盖 Manifest.lang，必同语言）即可，写入必须带 lang。
// 返回值第二项是本次产物声明的构建期依赖（Manifest.dependencies）——
// 无论产物行是新建还是已存在都返回，调用方据此写 page_dependencies。
func (s *Service) ensureArtifactRow(ctx context.Context, page *pagemodel.PageEntity, hash string, sourceDocument json.RawMessage, lang string) (string, []pipeline.Dependency, error) {
	existing, err := s.artifacts.Detail(ctx, &artifactcontract.DetailReq{PageID: page.ID, Hash: hash})
	loc := pipeline.ArtifactLocator(hash)
	art, err := s.store.GetArtifact(loc)
	if err != nil {
		return "", nil, err
	}
	if existing != nil && existing.ID != "" {
		return existing.ID, art.Manifest.Dependencies, nil
	}
	manifestJSON, err := json.Marshal(art.Manifest)
	if err != nil {
		return "", nil, err
	}
	recorded, err := s.artifacts.EnsureRecord(ctx, &artifactcontract.RecordReq{
		ArtifactID:       uuid.NewString(),
		PageID:           page.ID,
		Version:          page.DraftVersion,
		Lang:             lang,
		SourceDocument:   sourceDocument,
		SchemaVersion:    art.Manifest.PageDocumentSchemaVersion,
		SourceHash:       art.Manifest.SourceHash,
		BuildInputHash:   art.Manifest.BuildInputHash,
		ArtifactProvider: "local",
		ArtifactKey:      loc.Key,
		ArtifactHash:     hash,
		CompilerVersion:  art.Manifest.CompilerVersion,
		// 真实注册表版本（组件模板 + Props 结构 + 二进制 revision 的指纹），
		// 不是 Manifest 里的常量 —— 部署新组件后要靠它识别「哪些页面的产物是旧组件产的」，
		// 见 builder.RegistryVersion 与 Service.MarkStaleByRegistryVersion。
		RegistryVersion: builder.RegistryVersion(),
		Manifest:        manifestJSON,
		CreatedBy:       systemCreator,
	})
	if err != nil {
		return "", nil, err
	}
	return recorded.ID, art.Manifest.Dependencies, nil
}

func activeHashOf(rec *pipeline.PageRecord) string {
	if rec == nil {
		return ""
	}
	return rec.ActiveHash
}

func mapPublishError(err error) error {
	switch {
	case errors.Is(err, pipeline.ErrVersionConflict):
		return ErrDraftVersionConflict
	case errors.Is(err, pipeline.ErrNoStagedArtifact):
		return ErrNoStagedArtifact
	case errors.Is(err, pipeline.ErrRollbackPathMismatch):
		return ErrRebuildRequired
	case errors.Is(err, pipeline.ErrPageNotFound):
		return ErrPageNotFound
	default:
		return err
	}
}
