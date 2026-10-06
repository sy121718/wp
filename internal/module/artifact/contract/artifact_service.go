// Package artifactcontract 定义 artifact 模块对外能力。
package artifactcontract

import (
	"context"
	"time"

	artifactdto "go_wp/internal/module/artifact/dto"
)

// 请求/响应 DTO 重导出：跨模块调用方只依赖 contract，不直接 import artifact/dto。
type (
	RecordReq     = artifactdto.RecordReq
	DetailReq     = artifactdto.DetailReq
	DetailByIDReq = artifactdto.DetailByIDReq
	ArtifactResp  = artifactdto.ArtifactResp

	GCCandidateResp = artifactdto.GCCandidateResp

	PageArtifactMissRow = artifactdto.PageArtifactMissRow

	ContentObjectGCReq  = artifactdto.ContentObjectGCReq
	ContentObjectGCResp = artifactdto.ContentObjectGCResp
)

// 产物负载状态（跨模块传值用；与 page_artifacts 的 CHECK 约束逐字对应）。
const (
	PayloadStateAvailable = "available"
	PayloadStateGCPending = "gc_pending"
	PayloadStateDeleted   = "deleted"
)

// ArtifactService 不可变构建产物归档能力。
type ArtifactService interface {
	// Record 把已落盘的 pipeline Artifact 元数据与内容对象闭包写入数据库。
	// req.Lang 为空时按全局默认语言归档（sys_config 的 i18n 组 default_lang）。
	Record(ctx context.Context, req *artifactdto.RecordReq) (res *artifactdto.ArtifactResp, err error)
	// EnsureRecord 幂等归档：同 hash 返回现记录；同 (pageId, version, lang) 重构建
	// 时替换该行产物指针（编译器升级场景）；否则新建。
	// 语言维度：替换只在同一语言内发生，同页不同语言各占一行、互不覆盖。
	EnsureRecord(ctx context.Context, req *artifactdto.RecordReq) (res *artifactdto.ArtifactResp, err error)
	// Detail 按 (pageId, hash) 查询产物元数据（hash 覆盖 Manifest.lang，无需语言参数）。
	Detail(ctx context.Context, req *artifactdto.DetailReq) (res *artifactdto.ArtifactResp, err error)
	// DetailByID 按产物行 ID 查询产物元数据。
	DetailByID(ctx context.Context, req *artifactdto.DetailByIDReq) (res *artifactdto.ArtifactResp, err error)
	// ListStalePageIDs 在**调用方给定的当前产物集合**内挑出 registry_version 与
	// current 不同的产物，返回它们所属的页面 ID（去重、字典序）。
	// 用途：部署新组件后的全站待重建识别 —— 组件编译进二进制，没有运行时事件能
	// 提示「已有产物由旧组件产出」，只能靠产物元数据里的版本号比对。
	//
	// 为什么集合由调用方给：「当前产物」是来源模块的语言账本事实
	// （page_publications / page_stagings 里 active/staged 指向的行），artifact
	// 只知道每行的版本指纹。旧签名（本方法此前不接受集合）扫全部 available 行，
	// 会把未 GC 的历史回滚产物也算成当前产物，导致重建后每次重启反复误标。
	ListStalePageIDs(ctx context.Context, current string, artifactIDs []string) (ids []string, err error)
	// ListGCCandidates 列出可回收候选：payload_state=available、早于 before、
	// 且不在 excludeIDs（保护集合）内。
	// excludeIDs 为空表示调用方无法确定保护集合 —— 此时返回空列表（宁可不回收也不误删）。
	ListGCCandidates(ctx context.Context, before time.Time, excludeIDs []string) (list []artifactdto.GCCandidateResp, err error)
	// CountOtherAvailableByHash 统计同 hash 的其他 available 行数。
	// 产物是内容寻址的（artifacts/<hash>/），多条元数据行可能指向同一份文件 ——
	// 只有返回 0 时删除物理文件才安全。
	CountOtherAvailableByHash(ctx context.Context, hash, excludeID string) (n int64, err error)
	// MarkPayloadState 批量更新负载状态（gc_pending / deleted），返回受影响行数。
	MarkPayloadState(ctx context.Context, ids []string, state string) (n int64, err error)
	// GarbageCollectContentObjects 回收不再被任何现存产物行引用的共享内容对象
	// （content_objects 的标记清除 GC，审计 IDX-016）。
	// DryRun 默认 true，真删必须显式传 false；外部引用来源未接时按「没有外部引用」处理，
	// 接上后查询失败即整轮放弃（宁可不回收也不误删）。
	GarbageCollectContentObjects(ctx context.Context, req *artifactdto.ContentObjectGCReq) (res *artifactdto.ContentObjectGCResp, err error)
}

// PageArtifactReader 页面产物元数据的只读视图。
//
// `page_artifacts` 是**本模块的表**（Entity 在 artifact/model），但它的读者主要在 page
// 模块：发布回执恢复要按 id 反查 hash、孤儿对账要属主 hash 清单、依赖表归属校验要先知道
// 产物挂在哪张页面。让另一个模块拿着裸表名去查，等于把这张表的列名变成跨模块接口 ——
// 改一列不会有编译错误，只会在那边静默读到空值。所以这些读全部收在这里。
//
// 它不是 ArtifactService 的一部分，单独成接口：`ArtifactService` 有二十多个测试替身，
// 往里加方法会全部编译失败，而它们里没有一个碰页面产物元数据。
type PageArtifactReader interface {
	// ListPageArtifactHashes 列出全部认领中的产物 hash（IDX-015 反向对账的属主清单）。
	ListPageArtifactHashes(ctx context.Context) (hashes []string, err error)
	// PageArtifactHashByID 按产物行 id 取 hash（发布回执只记 id，判定要拿 hash）。
	// 该 id 不存在时返回空串、不报错。
	PageArtifactHashByID(ctx context.Context, id string) (hash string, err error)
	// PageArtifactPageID 取产物行挂在哪张页面上。
	// 本表没有 project_id 列，归属由调用方拿 page_id 回自己的 pages 表判断。
	// 该 id 不存在时返回空串、不报错。
	PageArtifactPageID(ctx context.Context, id string) (pageID string, err error)
	// TranslationMisses 取给定页面「每个 (page_id, lang) 最新产物」的 manifest 缺译计数，
	// 只含 misses > 0；pageIDs 为空返回空。
	//
	// 依据是**产物自己的 Manifest**（构建期写入）而不是实时重算：缺失是「这一份已产出的
	// 字节里有多少取词没命中」，实时算会得出与线上字节不一致的第二份真相。
	TranslationMisses(ctx context.Context, pageIDs []string) (rows []artifactdto.PageArtifactMissRow, err error)
}
