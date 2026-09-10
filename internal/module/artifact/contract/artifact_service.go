// Package artifactcontract 定义 artifact 模块对外能力。
package artifactcontract

import (
	"context"

	artifactdto "go_wp/internal/module/artifact/dto"
)

// 请求/响应 DTO 重导出：跨模块调用方只依赖 contract，不直接 import artifact/dto。
type (
	RecordReq     = artifactdto.RecordReq
	DetailReq     = artifactdto.DetailReq
	DetailByIDReq = artifactdto.DetailByIDReq
	ArtifactResp  = artifactdto.ArtifactResp
)

// ArtifactService 不可变构建产物归档能力。
type ArtifactService interface {
	// Record 把已落盘的 pipeline Artifact 元数据与内容对象闭包写入数据库。
	// req.Lang 为空时按站点默认语言归档（i18n.default_lang）。
	Record(ctx context.Context, req *artifactdto.RecordReq) (res *artifactdto.ArtifactResp, err error)
	// EnsureRecord 幂等归档：同 hash 返回现记录；同 (pageId, version, lang) 重构建
	// 时替换该行产物指针（编译器升级场景）；否则新建。
	// 语言维度：替换只在同一语言内发生，同页不同语言各占一行、互不覆盖。
	EnsureRecord(ctx context.Context, req *artifactdto.RecordReq) (res *artifactdto.ArtifactResp, err error)
	// Detail 按 (pageId, hash) 查询产物元数据（hash 覆盖 Manifest.lang，无需语言参数）。
	Detail(ctx context.Context, req *artifactdto.DetailReq) (res *artifactdto.ArtifactResp, err error)
	// DetailByID 按产物行 ID 查询产物元数据。
	DetailByID(ctx context.Context, req *artifactdto.DetailByIDReq) (res *artifactdto.ArtifactResp, err error)
	// ListPageIDsByOtherRegistryVersion 返回「存在 registry_version 与 current 不同的
	// 可用产物」的页面 ID（去重、字典序）。
	// 用途：部署新组件后的全站待重建识别 —— 组件编译进二进制，没有运行时事件能
	// 提示「已有产物由旧组件产出」，只能靠产物元数据里的版本号比对。
	ListPageIDsByOtherRegistryVersion(ctx context.Context, current string) (ids []string, err error)
}
