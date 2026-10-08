package pageservice

// 一条判据：**产物一旦产出，它的站点语言输入就不再受配置改动影响**。
//
// 计划的生命周期只有两步：
//
//  1. 构建（或发布）时**冻结**：没有计划、或草稿已被改过（= 新的发布决策）时，
//     按当时的站点语言配置解析一份并落库；已有计划且草稿未变时**原样沿用**。
//  2. 此后一切重编译（发布前的确定性复构建、组件升级后的批量重建、灾难恢复重建）
//     都以冻结值为准 —— 不再回读 project_locales。
//
// 为什么把「草稿版本」当作重新冻结的判据：冻结要挡住的是「既有产物在重建后换了
// hreflang」这一类**无声**的漂移；而作者改了草稿再重新发布本来就产出新产物，
// 这时按当前配置重算才是期望行为。若把「配置变了」本身当判据，就等于没有冻结
//（配置一改，重建立刻跟着变，正是本条审计要消灭的现象）。

// 正向（链接是否可达）在 pipeline.AuditActiveLinks；这里补反向：磁盘上的产物目录有没有
// 人认领。两件事分开看是因为处置方式不同 —— 正向异常是「线上已经 404」，必须立刻处理；
// 孤儿只是占磁盘，交给 GC 按保留期回收即可。

// 三种回执形态（发布 / 改 URL / 回滚）在「访问面已切换、数据库没跟上」窗口里要补的
// 数据库步骤各不相同，但**主链与恢复必须共用同一段实现**：恢复例程若另写一遍
// 「补齐逻辑」，两边迟早分叉，而分叉的表现是「恢复后状态看着收敛了、但与正常
// 走一遍的结果不同」——例如漏迁移 reserved 路由、漏处置旧路径。
//
// 事务边界：DB 各步收在一个事务里（pages / page_publications / page_stagings 走
// 本模块的 *Tx 方法，page_routes 走 publication 的 *Tx 方法）。文件系统那一步
// （符号链接切换）在事务之外，用回执兜底 —— 这是规则允许的补偿形态：
// 跨系统、幂等、留痕、可重放。

// 问题：发布是「先切访问面（符号链接原子替换）→ 再写数据库活跃指针」。中间崩溃时
// 线上可能已经生效、也可能没有，而数据库里没有任何痕迹 —— 只能人工比对。
//
// 做法：切换前登记 pending 回执（publication_receipts，与路由回执同一张表），
// 成功后结案；启动时扫未结案的回执，按「符号链接实际指向哪个产物」判定：
//
//	指向本次要激活的产物 → 切换已生效、DB 没跟上 → 补完成（幂等）
//	指向别处或不存在     → 切换没发生 → 标已回滚（不碰文件与数据库）

// 问题：发布 / 改 URL / 回滚失败后会在 publication_receipts 留下 pending 回执，
// 而在这之前**只有进程启动时**跑一次恢复（RecoverPendingPublications）——
// 长时间不重启就一直 pending，线上与库长期不一致，而且没有任何可见性。
//
// 做法：判定与补齐逻辑一行都不在这个文件里（在 page_publish_recover.go 的 recoverOne），
// 这里只负责「谁在什么时候驱动它」。三个驱动源共用同一段重放实现：
//
//	启动首跑  RecoverPendingPublications —— 不限批，把上次进程留下的残留一次收干净
//	定时兜底  ConvergePendingReceipts   —— 时间驱动，分批领取（多实例安全）
//	写路径    NotifyPendingReceipt      —— 事务提交后的进程内快通道，正常路径毫秒级
//
// 为什么必须有定时兜底：启动恢复只在装配时跑一次，进程活得越久，pending 残留积得越多；
// 没有定时驱动，「线上已生效、数据库没跟上」会一直维持到下一次重启。

// 报告的问题（P1）：同步路径（RebuildStale 的前 20 页）遍历站点启用语言逐个构建，
// 并重新发布**此前已发布**的语言；溢出部分交给构建队列后，executor 只调 Build(ID) ——
// 没有语言、没有重新发布。于是「同一批第 21 个之后」可能停在默认语言的暂存态，
// 页面上线状态与前面 20 个不一致。
//
// 根因不是漏传了一个参数，而是**两条路径各有一份实现**。本文件把「一次单页重建」
// 的唯一实现收在这里：
//
//	planPageRebuild  冻结这次重建的上下文（语言集合 / 旧发布范围 / 输入版本 / 意图）；
//	rebuildPage      按计划逐语言构建，构建成功后只回写旧发布范围内的语言；
//	RunPageBuildJob  队列执行体的入口 —— 把任务行还原成计划，再走上面同一条编排。
//
// 同步路径（RebuildStale）与异步路径（构建队列 worker）因此只差「计划是怎么来的」，
// 不差「计划怎么执行」。

// 背景：访问面（/site）直接服务 active 目录的文件系统状态，产物文件被误删或磁盘
// 损坏后 DB 侧毫无察觉 —— page_artifacts 行还在、payload_state 仍是 available、
// pages.active_artifact_id 仍指着它，表现是「线上 404 但后台一切正常」。
// 本文件提供两个只读/只重建的能力：按元数据重建单个产物、巡检全部悬空链接。

// 三件事，边界写死在这里：
//  1. seoLangs：把站点的「语言 → 路径前缀」交给校验器（映射点仍是 pipeline.LangURLRule）；
//  2. inspectBuiltArtifact：构建完成后对**刚产出的字节**跑一次确定性校验，命中就记日志；
//  3. SEOPatrol：按激活清单逐份校验，产出可复核的巡检报告（URL / 规则 / 证据 / ArtifactHash）。
//
// 为什么三件事都**不改发布结果**：审计明确反对把内部信号当作发布成功或排名的依据
//（SEO-01 的验收那段），而 SEO 评分（internal/seo/scoring）尤其不得变成闸门。
// 合规校验比评分硬，但它判的仍是「产物内部是否自相矛盾」——确定性事实不足以决定
// 一份内容该不该上线（作者可能正要下线一个 noindex 页面）。因此本文件只产出证据：
// 构建期进日志、巡检进接口，人工据此决定怎么改。
//
// 与访问面抓取校验的分工（报告后半段，本轮不做）：本文件读的是**本机产物存储**与
// **本机路由表**，不发任何网络请求，因此结论也不依赖线上是否可达；「线上 301 是否
// 真的生效、sitemap 是否真的被收录」必须由发布后的抓取与站长平台数据回答。
//
// 与既有「产物 SEO 体检」的分工（internal/module/publication/service/seo_audit.go，
// 审计 SEO-019）—— 两者不是同一件事，刻意没有合并：
//   · 覆盖面不同：体检看「有没有」（缺 title / 缺 canonical / 图片缺 alt / 内链断了 /
//     title 重复），本文件看「自相矛盾」（两条 canonical、canonical 与产物路径不一致、
//     JSON-LD 解析不了或与标题不符、robots 取值不受控、noindex 却进了 sitemap、互指单向）；
//   · 锚点不同：体检的结论只有路径与文案，本文件的每条结论都带 ArtifactHash —— 报告
//     验收要求「可复核」，而同一路径的产物会随重建换掉，没有 hash 就回不到那份字节；
//   · 时机不同：体检读 active 目录（**只有已发布的**），本文件在构建期就能跑（还挂着
//     缓存的那份暂存产物也能查），因此发布前就能看见矛盾。
// 合并的代价是本次不允许改 publication 模块（文件域），且合并会把「按 active 目录直读」
// 与「按产物 hash 直读」两种取数方式混在一处；已在报告里列为后续可复核项。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/internal/builder"
	"go_wp/internal/module/artifact/contract"
	"go_wp/internal/module/artifact/enums"
	"go_wp/internal/module/page/contract"
	"go_wp/internal/module/page/dto"
	"go_wp/internal/module/page/model"
	"go_wp/internal/module/project/contract"
	"go_wp/internal/module/publication/contract"
	"go_wp/internal/pipeline"
	"go_wp/internal/seo"
	"go_wp/internal/seo/compliance"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

// 发布链路（docs/03-pipeline.md §6 / 0-A1 §2）：
//
//	Build   草稿确定性编译 → 不可变产物落盘 → 元数据入库 → 暂存指针回写；
//	Publish 校验暂存 → 原子激活符号链接 → active 指针与路由 active 化；
//	Rollback 内核按 hash 直接重激活历史产物（秒级，无重新编译）;
//	UpdateURL 新 URL 构建并激活，旧 URL 按 301 / 取消激活处理。
//
// 内核 pipeline.Publisher 的内存态可由数据库随时重建（LoadRecord），
// 因此进程重启不影响发布正确性；多实例队列化属后续 build 模块。
// page_artifacts.created_by 为 uuid 列：系统操作留空，
// artifact.Record 的 defaultCreator 会兜底为全零 UUID。
// 此前写入 "system" 字面量导致构建入库 500（uuid 解析失败），发布主链无法走通。
const systemCreator = ""

func (s *Service) Build(ctx context.Context, req *pagedto.BuildReq) (res *pagedto.PublishResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, ErrInvalidParam
	}
	page, err := s.getExistingPage(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if req.ExpectedVersion > 0 && req.ExpectedVersion != page.DraftVersion {
		return nil, ErrDraftVersionConflict
	}
	// 构建语言（多语言 P2）：请求显式指定优先，否则站点默认语言；
	// 实际访问路径由 sitePath 单点映射（开启前缀时 /{lang}/path）。
	lang := buildLang(req.Lang)
	// 页面级语言排除（迁移 491）：被排除的语言本页不产出 —— 在这里拦，而不是等到
	// 产物产出后再删（那样会白编译一份、并在访问面留下一瞬的字节）。
	// 批量发布把这个错误记成 skipped（正常业务状态），单语言入口把它原样报给调用方。
	if pageExcludesLang(page, lang) {
		return nil, ErrPageLangExcluded
	}
	// 发布计划（审计 I18N-01）：已冻结且草稿未变 → 原样沿用；否则按当前站点语言配置
	// 重新冻结，并在写暂存指针的同一事务里落库。路径映射用计划里的默认语言 ——
	// 与产物里的 x-default 同源。
	plan, persistPlan, err := s.publicationPlanFor(ctx, page, lang)
	if err != nil {
		return nil, err
	}
	path, err := s.sitePathOfWithPlan(ctx, lang, page, &plan)
	if err != nil {
		return nil, err
	}
	logger.Scene("build").With("pageId", page.ID).With("lang", lang).With("path", path).
		With("planHash", plan.Hash()).Info("开始构建")
	if err = s.syncKernel(path, lang, page.DraftDocument, page.ID, &plan); err != nil {
		return nil, err
	}
	hash, err := s.publisher.Build(ctx, page.ID, s.kernelVersion(page.ID))
	if err != nil {
		logger.Scene("build").With("pageId", page.ID).Error(err, "构建失败")
		return nil, mapPublishError(err)
	}

	artifactID, deps, err := s.ensureArtifactRow(ctx, page, hash, page.DraftDocument, lang)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	// 依赖记录（docs/03-pipeline.md §8.2：本次产物声明的依赖集合，供依赖源变更时按
	// (kind,key) 反查受影响页面）与暂存指针**同事务**。
	//
	// 此前依赖写失败只记日志，于是那一页不再被精确标 stale：内容改了、页面不重建，
	// 站点长期显示旧内容，而错误只在日志里。依赖记录是「精确失效」的依据，
	// 不是可丢的投影 —— 与暂存指针一起提交，任一步失败整体回滚。
	//
	// 产物行（ensureArtifactRow）不在此事务内：那是 artifact 模块的幂等归档
	// （内容寻址、按 (page_id, hash) 去重，失败不改变文件系统与页面状态），
	// page 侧不持有它的句柄 —— 跨模块事务需要 artifact 侧提供 …Tx 变体，见报告遗留项。
	if err = s.model.TransactionScoped(ctx, page.ProjectID, func(tx *gorm.DB) error {
		if derr := s.persistDependenciesTx(ctx, tx, page.ProjectID, page.ID, artifactID, deps); derr != nil {
			return derr
		}
		// 发布计划与暂存指针同事务（审计 I18N-01）：产物与它依据的语言输入必须同生共死。
		// 只写一半会留下「暂存指针指向按 A 份语言输入构建的产物、计划记的是 B 份」，
		// 后续重建按 B 复现不出那份产物，且没有任何报错。
		if persistPlan {
			if perr := s.model.UpdatePublicationPlanRecordTx(ctx, tx, page.ID, lang, plan, page.DraftVersion, now); perr != nil {
				return perr
			}
		}
		// 暂存指针按语言记录（多语言 P3）：Build(en-US) 不再覆盖 Build(zh-CN) 的暂存指针，
		// 「先构建两种语言、再逐个发布」由此可用；pages 的单值列仍是最近构建语言的镜像。
		return s.model.MarkStagedLangTx(ctx, tx, page.ProjectID, page.ID, lang, artifactID, hash, page.DraftVersion, now)
	}); err != nil {
		logger.Scene("build").With("pageId", page.ID).With("artifactID", artifactID).Error(err, "依赖记录/暂存指针写入失败")
		return nil, err
	}
	// 构建期 SEO 合规校验（审计 SEO-01）：对**刚产出的字节**做确定性事实校验，
	// 命中就记日志（URL / 规则 / 证据 / ArtifactHash），不改产物、不改发布结果 ——
	// 边界与理由见 page_seo_patrol.go 的文件头。
	//
	// sitemap 收录按**当前激活状态**如实回答：构建 ≠ 上线，拿不到「已激活」这个事实时
	// 不能替它假设（例如 noindex 页面只是被构建过、还没发布，就不该报「与 sitemap 冲突」）。
	s.inspectBuiltArtifact(ctx, page.ProjectID, hash, path, lang, s.pathListedInSitemap(path))
	logger.Scene("build").With("pageId", page.ID).With("hash", hash).Info("构建完成")
	return &pagedto.PublishResp{
		PageID: page.ID, Status: pipeline.StateReady,
		StagedHash: hash, DraftPath: page.DraftPath,
	}, nil
}

// Publish 激活暂存产物：二次构建校验一致性后原子切换活跃指针。
// nil/空 ID 属于请求不合法（ErrInvalidParam）；合法 ID 无页面才返回 ErrPageNotFound。
//
// 这是**对外发布入口**（后台 / 运维显式发布）：激活成功后还会刷新同页其余已发布语言的
// 互指（见 refreshPeerLocaleLinks，审计 I18N-01 续）。自动重建链路（RebuildStale 与队列
// worker 消费的 rebuildPage）走 publish(..., false)：那条链路自己会逐语言重建并重新发布，
// 各语言都会在同一轮里看到完整发布面，不需要、也不该再引入额外的发布动作。
func (s *Service) Publish(ctx context.Context, req *pagedto.PublishReq) (res *pagedto.PublishResp, err error) {
	return s.publish(ctx, req, true)
}

// PublishAllLanguages 一键发布全部启用语言（多语言开关开启时的发布口径）：
// 按站点启用语言清单逐语言「构建 + 激活」，一次请求把设置页配置的每种语言
// 各编译一份并上线 —— 「发布时设置了多少语言，就一次性编译多少」。
//
// 编排与 rebuildPage 同源（逐语言 Build → publish）而不是另起一条链：
// 单语言链路里的发布计划冻结、按语言取暂存、确定性校验在两条路径上必须只有一份实现。
// 互指不做循环内刷新（publish 传 false）：本循环自己会逐个重建并重新发布全部启用语言，
// 每一轮都能看到完整发布面 —— 与 rebuildPage 的既有注释同一理由。
//
// 失败口径与 rebuildPage 一致：单语言失败不阻断其余语言（继续下一个），
// 错误按语言逐条回传；语言清单不可读是整体失败（发布口径 Forbidden，审计 I18N-02）。
func (s *Service) PublishAllLanguages(ctx context.Context, req *pagedto.PublishReq) (res *pagedto.PublishAllResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, ErrInvalidParam
	}
	page, err := s.getExistingPage(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	// 语言清单是发布输入契约：读不到整体失败（不静默降级成单语言，
	// 那会让其余语言的线上产物停在旧字节而发布回执写着成功）。
	langs, err := s.publishLangsOf(ctx, page.ProjectID)
	if err != nil {
		return nil, err
	}
	res = &pagedto.PublishAllResp{
		PageID:  page.ID,
		Results: make([]pagedto.LangPublishResult, 0, len(langs)),
	}
	logger.Scene("publication").With("pageId", page.ID).
		With("langs", strings.Join(langs, ",")).Info("一键发布全部启用语言")
	for _, lang := range langs {
		if cerr := ctx.Err(); cerr != nil {
			res.Results = append(res.Results, pagedto.LangPublishResult{
				Lang: lang, Status: "failed", Error: cerr.Error(),
			})
			break
		}
		// 页面级语言排除：跳过而不是失败 —— 作者主动把这一页从该语言撤下来是正常决策，
		// 记 skipped 让回执如实反映「这一批发了哪几种、跳了哪几种」，不伪装成错误。
		if pageExcludesLang(page, lang) {
			res.Results = append(res.Results, pagedto.LangPublishResult{Lang: lang, Status: langPublishSkipped})
			res.Skipped++
			continue
		}
		if _, berr := s.Build(ctx, &pagedto.BuildReq{ID: req.ID, Lang: lang}); berr != nil {
			logger.Scene("publication").With("pageId", page.ID).With("lang", lang).
				Error(berr, "一键发布：语言构建失败")
			res.Results = append(res.Results, pagedto.LangPublishResult{
				Lang: lang, Status: "failed", Error: berr.Error(),
			})
			continue
		}
		pr, perr := s.publish(ctx, &pagedto.PublishReq{ID: req.ID, Lang: lang}, false)
		if perr != nil {
			logger.Scene("publication").With("pageId", page.ID).With("lang", lang).
				Error(perr, "一键发布：语言发布失败")
			res.Results = append(res.Results, pagedto.LangPublishResult{
				Lang: lang, Status: "failed", Error: perr.Error(),
			})
			continue
		}
		item := pagedto.LangPublishResult{Lang: lang, Status: "ok"}
		if pr != nil {
			item.ActiveHash = pr.ActiveHash
		}
		res.Results = append(res.Results, item)
		res.Published++
	}
	return res, nil
}

// publish 发布主链。refreshPeers 控制激活成功后是否刷新同页其余已发布语言的互指。
func (s *Service) publish(ctx context.Context, req *pagedto.PublishReq, refreshPeers bool) (res *pagedto.PublishResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, ErrInvalidParam
	}
	page, err := s.getExistingPage(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	lang := buildLang(req.Lang)
	// 页面级语言排除（迁移 491）：与 Build 同一判据，堵住「直接调发布、绕过构建」的入口。
	// 排除动作会下线该语言的产物，放它进来等于刚删掉的字节又被激活回访问面。
	if pageExcludesLang(page, lang) {
		return nil, ErrPageLangExcluded
	}
	// 发布计划（审计 I18N-01）：发布是**发布决策的落点**，这里的计划必须与暂存产物的
	// 构建输入一致。已冻结且草稿未变时原样沿用，于是随后的确定性复构建看到的语言输入
	// 与构建时逐字相同 —— 「第二次一致性构建看到的在线语言集合变了」这个成因被消除。
	plan, persistPlan, err := s.publicationPlanFor(ctx, page, lang)
	if err != nil {
		return nil, err
	}
	path, err := s.sitePathOfWithPlan(ctx, lang, page, &plan)
	if err != nil {
		return nil, err
	}
	logger.Scene("publication").With("pageId", page.ID).With("lang", lang).With("path", path).
		With("planHash", plan.Hash()).Info("开始发布")
	// 暂存产物按语言取（page_stagings 为真源）：Publish(en-US) 只看 en-US 的暂存，
	// 不会因为中途构建过其他语言而误报「无暂存产物」或发布错语言的产物。
	stagedArt, err := s.stagedArtifactOf(ctx, page, lang)
	if err != nil {
		return nil, err
	}
	if stagedArt.ArtifactKey == "" || stagedArt.PageID != page.ID {
		return nil, ErrNoStagedArtifact
	}
	// 活跃产物的依赖记录必须齐备（fan-out 反查的前提）：发布时按 Manifest 补写一次。
	s.persistDependenciesFromManifest(ctx, page.ProjectID, page.ID, stagedArt.ID, stagedArt.Manifest)

	// FS 激活前预检：目标路径被其他页面/展示实例占用时提前失败（H7），
	// 避免内核先把 FS 覆盖成本页产物、DB 路由写入才报错的状态分裂。
	if err = s.ensureRouteNotOccupied(ctx, page.ProjectID, path, page.ID); err != nil {
		logger.Scene("publication").With("pageId", page.ID).With("path", path).Warn("发布被拒绝：路径已被占用")
		return nil, err
	}

	// 确定性构建保证与暂存一致；用「当前草稿」（路径+文档）重建内核——
	// 若草稿在构建后又被 SaveDraft 修改（含改路径），重建 hash 必与暂存不同，
	// 走 ErrRebuildRequired 拒绝发布，避免发布旧内容后界面误报「已发布最新」。
	if err = s.syncKernel(path, lang, page.DraftDocument, page.ID, &plan); err != nil {
		return nil, err
	}
	version := s.kernelVersionOrOne(page.ID)
	built, buildErr := s.publisher.Build(ctx, page.ID, version)
	if buildErr != nil {
		logger.Scene("build").With("pageId", page.ID).Error(buildErr, "发布前复构建失败")
		return nil, mapPublishError(buildErr)
	}
	if built != stagedArt.ArtifactHash {
		// 暂存与复构建不一致。两种成因必须分开处理：
		//
		//  1. **草稿变了**（构建后又被 SaveDraft，含改 URL）：绝不能发布 —— 那正是
		//     「发布旧内容后界面误报已发布最新」。走 ErrRebuildRequired。
		//  2. **草稿没变、站点级外部状态变了**：语言切换器按访问面过滤（审计 I18N-021），
		//     其它语言恰好在这两次构建之间发布了，于是同一份草稿产出不同字节。
		//     这时报错会把「先构建多种语言、再逐个发布」这个自然操作挡死。
		//
		// 区分依据是**暂存行的草稿版本**，不是内核版本：syncKernel 刚刚把内核刷成了
		// 当前草稿，拿内核版本比永远相等，等于取消这道闸（草稿被改过也会照发）。
		// 暂存行记录的是「这份产物是从哪个草稿版本构建的」—— 它与当前草稿版本不一致
		// 就说明产物旧了，必须拒绝。
		staging, gerr := s.model.GetStaging(ctx, page.ID, lang)
		if gerr != nil || staging == nil || staging.DraftVersion != page.DraftVersion {
			return nil, ErrRebuildRequired
		}
		refreshed, ferr := s.publisher.Build(ctx, page.ID, version)
		if ferr != nil {
			return nil, mapPublishError(ferr)
		}
		if refreshed != built {
			// 重新构建仍与第一次不同：输入不稳定，属于真问题，不再往下掩盖。
			return nil, ErrRebuildRequired
		}
		logger.Scene("publication").With("pageId", page.ID).With("lang", lang).
			Warn("暂存产物落后于站点级状态（如其它语言刚发布），已按当前草稿重新构建")
		// 这里只调了内核的 Build，而 s.Build 的另一半责任（写产物行 + 依赖记录）要补上：
		// 少了它，激活用的 hash 在产物表里没有对应行 —— 符号链接指向一个「查不到出处」
		// 的产物，回滚与引用保护都会从这里出问题。
		artifactID, deps, aerr := s.ensureArtifactRow(ctx, page, refreshed, page.DraftDocument, lang)
		if aerr != nil {
			return nil, aerr
		}
		s.persistDependencies(ctx, page.ProjectID, page.ID, artifactID, deps)
		// 三个地方都要换成本次的 hash，否则「暂存指针 / 激活指针 / 符号链接」各自指向
		// 不同产物：stagedArt 用于落库与激活，built 供后续步骤读取。
		stagedArt.ID = artifactID
		stagedArt.ArtifactHash = refreshed
		built = refreshed
		// 这一份产物是发布路径上现构建的（没走 s.Build），因此单独校验一次。
		// 与 Build 路径同一实现：同一份字节必得同一结论（确定性校验不变量）。
		// 这里传 true 不是猜：紧接着的 publisher.Publish 就会把这个路径激活成线上路径，
		// 而 sitemap 由激活路径生成 —— 「即将进 sitemap」在发布路径上是确定事实。
		s.inspectBuiltArtifact(ctx, page.ProjectID, refreshed, path, lang, true)
	}
	// 发布前快照：本语言当前激活的产物（page_publications 为真源）。回执要如实记录
	// 「从哪个产物切到哪个产物」，所以必须在切换之前读。
	fromArtifactID := s.publishedArtifactIDOf(ctx, page, lang)

	// 记录发布前「本语言」的旧 active 路径快照（page_publications 为该语言真源）。
	//
	// 必须**在切换之前**读，并且要进回执：切换后 MarkPublishedLang 会把该语言的激活
	// 路径更新为本次路径，届时再读已是新值 —— 那时旧路径既无法取消激活，也无法写进
	// 回执交给启动恢复处置（「旧路径永不清理」的根因）。
	//
	// 多语言 P3：旧路径只取本语言那一行，因此 Publish(en-US) 不会取消
	// /zh-CN/about 的激活路由——「一页多语言同时在线」由此成立。
	oldPath, perr := s.publishedPathOf(ctx, page, lang)
	if perr != nil {
		return nil, perr
	}

	// 登记 pending 回执，必须在访问面切换之前（AR2-002 / TX-009）：切换是不可逆的
	// 副作用，登记放在之后，崩溃窗口里就查不到「这次发布发生过」。登记拿不到回执 id
	// 一律中止发布 —— 带着未知状态去切访问面，正是这条回执要消灭的分裂状态。
	receiptID, rerr := s.beginPublishReceipt(ctx, publishReceiptInput{
		Action:    pubcontract.ReceiptActionSwitchActive,
		ProjectID: page.ProjectID, PageID: page.ID, Path: path, Lang: lang,
		FromArtifactID: fromArtifactID, ToArtifactID: stagedArt.ID, OldPath: oldPath,
	})
	if rerr != nil {
		return nil, rerr
	}

	hash, err := s.publisher.Publish(page.ID)
	if err != nil {
		// 内核保证「激活失败线上保持不变」（pipeline.Publisher.Publish 同一分支），
		// 属于可判定的无副作用失败：显式结案为已回滚，不留给启动恢复一个假 pending。
		s.abortPublishReceipt(ctx, receiptID, "访问面切换失败")
		logger.Scene("publication").With("pageId", page.ID).Error(err, "发布失败")
		return nil, mapPublishError(err)
	}
	// 访问面已切换（符号链接原子替换）。此后任何失败都不能判定为「没生效」，
	// 一律保留 pending，交给启动恢复按链接的实际指向补齐或回滚。
	//
	// 数据库侧四步（活跃指针、旧路径取消占用、新路径路由激活）收在**一个事务**里：
	// 此前它们各自成事务，中途失败会留下「指针已是新产物、旧路由还 active」这类
	// 半截状态，只能靠启动恢复逐步对齐。
	now := time.Now().UTC()
	// 计划只在本次确实要重冻时才带（persistPlan）：正常路径下它已随 Build 落库，
	// 这里再写一次只会把 update_time 抖动一遍。
	activationPlan := (*pipeline.PublicationPlan)(nil)
	if persistPlan {
		activationPlan = &plan
	}
	if aerr := s.applyPublishActivation(ctx, publishActivationInput{
		Page: page, Lang: lang, Path: path,
		ArtifactID: stagedArt.ID, ArtifactHash: hash, OldPath: oldPath,
		Plan: activationPlan,
	}); aerr != nil {
		// FS 已原子激活（线上已生效），此处 DB 事务整体回滚属于部分成功：
		// 错误必须明确暴露，且重试可收敛（复构建 hash 与暂存一致 → 幂等再激活）。
		// 回执保持 pending：启动恢复看得到链接已指向本次产物，会补写这套状态。
		s.keepPublishReceiptPending(receiptID, "DB 激活状态写入失败")
		logger.Scene("publication").With("pageId", page.ID).With("hash", hash).
			Error(aerr, "发布 FS 已激活，但 DB 激活状态事务失败（线上已生效，重试可收敛）")
		return nil, fmt.Errorf("发布已生效但数据库状态同步失败: %w", aerr)
	}
	// 旧路径的访问面符号链接必须另行解除：/site 直接服务 active 目录的文件系统状态，
	// 只删 DB 路由行会让旧 URL 继续输出旧产物（同页双 active 占用），且此后没有任何
	// 入口能查到该清哪个链接 —— 与页面删除同一根因。这一步在事务之外（文件系统），
	// 失败保持回执 pending，由启动恢复重放（补链接删除是幂等的）。
	if s.routes != nil && oldPath != "" && oldPath != path {
		if derr := s.deactivatePaths([]string{oldPath}); derr != nil {
			s.keepPublishReceiptPending(receiptID, "解除旧路径访问面激活失败")
			logger.Scene("publication").With("pageId", page.ID).With("oldPath", oldPath).Error(derr, "发布前解除旧路径访问面激活失败")
			return nil, derr
		}
	}

	// 结案：访问面（符号链接）与数据库（page_publications + 指针 + 路由行）已经一致。
	// 结案本身失败不阻断发布 —— 此时两边都已就位，回执留在 pending 只会被收敛例程
	// 幂等收尾（Inspect 看到链接指向本次产物 → 补完成）。
	if cerr := s.completePublishReceipt(ctx, receiptID); cerr != nil {
		logger.Scene("publication").With("pageId", page.ID).With("receiptId", receiptID).
			Error(cerr, "发布回执结案失败（状态已一致，收敛例程会幂等收尾）")
		// 事务已提交、回执却没收口：推快通道让收敛立刻把它收掉（非阻塞，丢了有定时兜底）。
		s.NotifyPendingReceipt()
	}
	logger.Scene("publication").With("pageId", page.ID).With("hash", hash).Info("发布完成")
	// 站点级 SEO 产物与自定义 404 页：发布激活后刷新
	// sitemap.xml / robots.txt / feed.xml / 404.html。
	// 尽力而为——生成失败只记日志，不回滚已完成的发布（产物可由下次发布或手动接口重建）。
	// 语言集按**发布口径**取（审计 I18N-02）：sitemap 的 hreflang 分组按站点语言清单
	// 展开，按可见回退取列表会写出「只有默认语言一组」的 sitemap —— 线上站点文件被
	// 静默降级，而这次发布回执写的是成功。读不到就**跳过本次刷新**（保留上一版站点
	// 文件，它们至少是完整的），并留下一条 Error；发布本身已激活完成，不回滚。
	siteLangs, lerr := s.publishLangsOf(ctx, page.ProjectID)
	if lerr != nil {
		logger.Scene("publication").With("pageId", page.ID).
			Error(lerr, "站点语言清单不可读，跳过 sitemap/robots 刷新（保留上一版站点文件）")
	} else if rerr := s.routes.RefreshSiteFiles(ctx, page.ProjectID, siteBaseURL(), pipeline.ActiveRoot(),
		siteLangs, s.defaultLocaleOf(ctx, page.ProjectID),
		// 方案按**本工程**解析（publication 不认识 project 契约）：
		// 站点文件与这个工程的页面路径必须用同一份方案。
		string(pipeline.SiteLangURLModeOf(ctx, s.project, page.ProjectID)),
		s.notFoundHTMLOf(ctx, page.ProjectID)); rerr != nil {
		logger.Scene("publication").With("pageId", page.ID).Error(rerr, "sitemap/robots 刷新失败")
	}
	s.notifyIndexNow(ctx, page.ProjectID, path)
	// 互指刷新（审计 I18N-01 续）：本语言激活成功后，同页其余**已发布**语言的语言切换器
	// 与 hreflang 可能因此变成单向的（它们是在本语言上线之前构建的，那时看不到本语言）。
	// 放在最后一步：上面的站点文件刷新与 IndexNow 都已完成，刷新失败也不影响本次发布。
	if refreshPeers {
		s.refreshPeerLocaleLinks(ctx, page, plan, lang)
	}
	return &pagedto.PublishResp{
		PageID: page.ID, Status: pipeline.StatePublished, ActiveHash: hash,
		DraftPath: page.DraftPath, PublishedAt: now.Format(time.RFC3339),
	}, nil
}

// refreshPeerLocaleLinks 刷新同页其余已发布语言的互指（审计 I18N-01 续）。
//
// 缺陷现象（逐语言发布的常规流程）：
//
//	构建 zh / en → 发布 zh（en 还没上线，zh 产物不含指向 en 的互指）
//	             → 发布 en（zh 已上线，en 产物含指向 zh 的互指）
//	             → zh 那一份**没有任何人回头重建**，线上最终是单向互指。
//
// 这正是「发布顺序不改变同一计划的字节」与 page_seo_patrol 点名的「互指单向」。
//
// 三条边界，都是为了「刷新」不变成「一次隐式发布浪潮」：
//
//  1. **只在本页已发布语言的范围内**（≤ 站点语言数，且必须落在同一份冻结计划的语言集合里；
//     另设 maxCrossLinkRefreshLangs 上限，防病态配置把一次发布放大成 N 次重编译）
//     —— 冻结计划之外的语言不参与互指判定（它们不在本页的发布范围内）；
//  2. **hash 未变则一个字节都不写**：先用同一份冻结计划重编译一次，与当前激活 hash 相同
//     就直接返回（编译是纯 CPU，产物按内容寻址落盘要么命中已有文件、要么本就是本次要用的
//     那一份）；只有 hash 变了才走既有发布链（它会自己复算并激活，不再递归刷新）；
//  3. **失败只记日志**：刷新是本次发布的**后置副作用**，任何一步失败都不得把已经成功的
//     发布打回 —— 线上仍是「刚发布的那份」+「尚未收敛的其余语言」，两者都是可用状态。
//
// 为什么用「重编译比 hash」而不是「读产物 HTML 看互指」：判据必须与发布的确定性校验
// 同源（都是同一个编译输入产出同一份字节），读 HTML 解析互指是另一套实现，迟早漂移。
func (s *Service) refreshPeerLocaleLinks(ctx context.Context, page *pagemodel.PageEntity, plan pipeline.PublicationPlan, lang string) {
	if s == nil || page == nil || ctx.Err() != nil {
		return
	}
	// 单语言站点没有互指可言；冻结计划里的语言集合就是本页参与互指的全集。
	if len(plan.SiteLangs) <= 1 || len(plan.SiteLangs) > maxCrossLinkRefreshLangs {
		return
	}
	pubs, err := s.model.ListPublications(ctx, page.ID)
	if err != nil {
		logger.Scene("publication").With("pageId", page.ID).With("lang", lang).
			Error(err, "互指刷新跳过：读取本页发布状态失败")
		return
	}
	// 同一时刻只有一份发布事实：把本页已发布语言收成一个集合，刷新只在这个集合里做。
	published := make(map[string]string, len(pubs))
	for i := range pubs {
		if pubs[i].ActivePath != "" && planHasLang(plan, pubs[i].Lang) {
			published[pubs[i].Lang] = pubs[i].ArtifactHash
		}
	}
	if len(published) <= 1 {
		return
	}
	refreshed := make([]string, 0, len(published)-1)
	for _, peer := range plan.SiteLangs {
		if ctx.Err() != nil {
			break
		}
		if peer == lang {
			continue
		}
		activeHash, ok := published[peer]
		if !ok {
			// 该语言尚未上线：它自己的首次发布会看到完整发布面，不需要预先刷新
			//（而给未上线的语言刷互指，等于把用户送到一个还不存在的地址）。
			continue
		}
		// 逐个语言独立判定：每种语言的产物各自可能少了指向本次新上线语言的互指，
		// 而「谁需要刷新」由它自己的字节决定（hash 比较），不依赖其它语言的刷新结果。
		if s.refreshPeerLocaleLink(ctx, page, peer, activeHash) {
			refreshed = append(refreshed, peer)
		}
	}
	if len(refreshed) > 0 {
		// 影响面回执（只读）：这次发布把哪几份既有产物重建成「互相声明」只有这一刻知道。
		logger.Scene("publication").With("pageId", page.ID).With("lang", lang).
			With("refreshed", strings.Join(refreshed, ",")).
			Info("互指刷新完成：其余已发布语言已重建为与新发布面互指")
	}
}

// maxCrossLinkRefreshLangs 互指刷新的语言数上限（防病态配置把一次发布放大成 N 次重编译）。
const maxCrossLinkRefreshLangs = 32

// refreshPeerLocaleLink 刷新单个已发布语言的互指；返回是否重新激活过。
//
// 只在「按同一份冻结计划重编译得到的 hash 与当前激活产物不同」时才重新激活 ——
// 相等说明它的互指与切换器已经与新发布面一致，一个字节都不需要写（幂等）。
func (s *Service) refreshPeerLocaleLink(ctx context.Context, page *pagemodel.PageEntity, peerLang, activeHash string) bool {
	// 只读地取该语言**已冻结**的计划：缺失 / 草稿已变（新的发布决策）都跳过 ——
	// 那种情形下该语言的既有产物本来就该由下一次正常发布来更新，
	// 在这里替它做决定会把「发布计划随草稿重冻」的语义搅乱。
	peerPlan, ok := s.frozenPublicationPlan(ctx, page, peerLang)
	if !ok {
		return false
	}
	// 暂存行的草稿版本必须与当前草稿一致：不一致说明这一语言的产物落后于草稿，
	// 属于「需要重新构建 + 发布」，走正常发布入口（ErrRebuildRequired 由它报出）。
	staging, serr := s.model.GetStaging(ctx, page.ID, peerLang)
	if serr != nil || staging == nil || staging.DraftVersion != page.DraftVersion {
		return false
	}
	path, perr := s.sitePathOfWithPlan(ctx, peerLang, page, peerPlan)
	if perr != nil {
		logger.Scene("publication").With("pageId", page.ID).With("lang", peerLang).
			Error(perr, "互指刷新跳过：语言访问路径解析失败")
		return false
	}
	if kerr := s.syncKernel(path, peerLang, page.DraftDocument, page.ID, peerPlan); kerr != nil {
		logger.Scene("publication").With("pageId", page.ID).With("lang", peerLang).
			Error(kerr, "互指刷新跳过：内核记录同步失败")
		return false
	}
	candidate, berr := s.publisher.Build(ctx, page.ID, s.kernelVersion(page.ID))
	if berr != nil {
		logger.Scene("publication").With("pageId", page.ID).With("lang", peerLang).
			Error(berr, "互指刷新跳过：按冻结计划重编译失败")
		return false
	}
	if candidate == activeHash {
		// 互指与切换器已经与新发布面一致：既有的激活产物就是这份字节，什么都不做。
		return false
	}
	logger.Scene("publication").With("pageId", page.ID).With("lang", peerLang).
		With("from", activeHash).With("to", candidate).
		Info("互指刷新：其余语言已上线的这一份重新构建并激活")
	if _, perr = s.publish(ctx, &pagedto.PublishReq{ID: page.ID, Lang: peerLang}, false); perr != nil {
		logger.Scene("publication").With("pageId", page.ID).With("lang", peerLang).
			Error(perr, "互指刷新失败（保持原状，等待下一次发布或重建收敛）")
		return false
	}
	return true
}

// notFoundHTMLOf 站点自定义 404 页内容（projects.settings.notFoundHtml，空 = 未配置）。
//
// 读不到工程时返回空串：404 页是可选能力，「取不到」与「没配」在访问面等价
// （都退回默认 404 行为），不该让它阻断发布 —— 与 enabledLangsOf / defaultLocaleOf
// 的降级口径一致。内容只做长度校验（保存时在后台入口），这里原样透传：
// 它是管理员配置的一份 HTML 文档，等同页面正文，发布链不做二次加工。
func (s *Service) notFoundHTMLOf(ctx context.Context, projectID string) string {
	if s.project == nil || strings.TrimSpace(projectID) == "" {
		return ""
	}
	project, err := s.project.Detail(ctx, &projectcontract.DetailReq{ID: projectID})
	if err != nil || project == nil {
		return ""
	}
	return projectcontract.ParseSiteSettings(project.Settings).NotFoundHTML
}

// Rollback 秒级回滚到历史产物：指针切换，不重新编译。
// nil 请求 / 空 ID / 空 TargetHash 属于请求不合法（ErrInvalidParam）；
// 合法 ID 无页面才返回 ErrPageNotFound，目标 hash 无产物返回 ErrRollbackTargetMiss。
func (s *Service) Rollback(ctx context.Context, req *pagedto.RollbackReq) (res *pagedto.PublishResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" || strings.TrimSpace(req.TargetHash) == "" {
		return nil, ErrInvalidParam
	}
	page, err := s.getExistingPage(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	logger.Scene("page").With("pageId", page.ID).With("targetHash", req.TargetHash).Info("开始回滚")
	targetArt, err := s.artifacts.Detail(ctx, &artifactcontract.DetailReq{PageID: page.ID, Hash: req.TargetHash})
	if err != nil {
		logger.Scene("page").With("pageId", page.ID).Error(err, "回滚目标产物缺失")
		return nil, ErrRollbackTargetMiss
	}
	// 回滚语言取目标产物冻结语言（产物 hash 覆盖 Manifest.lang，同 hash 必同语言）；
	// 目标产物未记录语言时回退请求语言 / 站点默认语言。
	// 按语言作用域回滚：只处置「该语言」的旧激活路由，其他语言保持在线。
	lang := buildLang(req.Lang)
	if strings.TrimSpace(targetArt.Lang) != "" {
		lang = targetArt.Lang
	}
	oldPath, perr := s.publishedPathOf(ctx, page, lang)
	if perr != nil {
		return nil, perr
	}
	if err = s.restoreKernelForHistory(page, targetArt); err != nil {
		return nil, err
	}
	// 登记 pending 回执（与 Publish / UpdateURL 同一套契约）：回滚同样先切访问面
	// （符号链接指向历史产物）、再写数据库；没有回执时中途失败会留下「线上是历史
	// 产物、DB 说是另一套」的状态，且启动恢复看不到它 —— 重启也捞不回来。
	fromArtifactID := s.publishedArtifactIDOf(ctx, page, lang)
	receiptID, rerr := s.beginPublishReceipt(ctx, publishReceiptInput{
		Action:    pubcontract.ReceiptActionRollback,
		ProjectID: page.ProjectID, PageID: page.ID, Path: targetArt.CanonicalPath, Lang: lang,
		FromArtifactID: fromArtifactID, ToArtifactID: targetArt.ID, OldPath: oldPath,
	})
	if rerr != nil {
		return nil, rerr
	}
	if err = s.publisher.Rollback(page.ID, req.TargetHash); err != nil {
		// 内核保证「激活失败线上保持不变」，属于可判定的无副作用失败：显式结案，不留给
		// 启动恢复一个假 pending。
		s.abortPublishReceipt(ctx, receiptID, "访问面切换失败")
		logger.Scene("page").With("pageId", page.ID).Error(err, "回滚失败")
		return nil, mapPublishError(err)
	}

	now := time.Now().UTC()
	// 数据库三步同事务（活跃指针 + 旧路径取消占用 + 目标路径路由激活）。
	if aerr := s.applyRollback(ctx, rollbackApplyInput{
		Page: page, Lang: lang, TargetPath: targetArt.CanonicalPath,
		TargetID: targetArt.ID, TargetHash: targetArt.ArtifactHash, OldPath: oldPath,
	}); aerr != nil {
		// FS 已切到历史产物（线上已生效）：保留 pending，交启动恢复补齐数据库状态。
		s.keepPublishReceiptPending(receiptID, "DB 激活状态写入失败")
		logger.Scene("page").With("pageId", page.ID).With("hash", req.TargetHash).
			Error(aerr, "回滚 FS 已激活，但 DB 激活状态事务失败（线上已生效，重试可收敛）")
		return nil, fmt.Errorf("回滚已生效但数据库状态同步失败: %w", aerr)
	}
	// 回滚到不同路径的历史产物时，旧路径的访问面链接必须另行解除（DB 行已在事务里
	// 取消占用）：/site 直接服务 active 目录的文件系统状态，残留链接会继续输出旧产物。
	if s.routes != nil && oldPath != "" && oldPath != targetArt.CanonicalPath {
		if derr := s.deactivatePaths([]string{oldPath}); derr != nil {
			s.keepPublishReceiptPending(receiptID, "解除旧路径访问面激活失败")
			logger.Scene("page").With("pageId", page.ID).With("oldPath", oldPath).Error(derr, "回滚前解除旧路径访问面激活失败")
			return nil, derr
		}
	}
	if cerr := s.completePublishReceipt(ctx, receiptID); cerr != nil {
		logger.Scene("page").With("pageId", page.ID).With("receiptId", receiptID).
			Error(cerr, "回滚回执结案失败（状态已一致，收敛例程会幂等收尾）")
		// 同上：事务已提交、回执未收口 —— 快通道让收敛立刻收掉。
		s.NotifyPendingReceipt()
	}
	st, _ := s.publisher.Status(page.ID)
	respStatus := pipeline.StatePublished
	if st != nil && st.Status != "" {
		respStatus = st.Status
	}
	logger.Scene("page").With("pageId", page.ID).With("targetHash", req.TargetHash).Info("回滚完成")
	return &pagedto.PublishResp{
		PageID: page.ID, Status: respStatus, ActiveHash: req.TargetHash,
		DraftPath: page.DraftPath, PublishedAt: now.Format(time.RFC3339),
	}, nil
}

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

// syncKernel 把页面当前草稿同步进内核记录（幂等；版本号以内核为准续增）。
// path 为实际访问路径（多语言下带 /{lang}/ 前缀），lang 为构建语言：
// 两者一起进入内核记录，决定 Manifest.lang 与激活路径。
// plan 为本次构建依据的**冻结发布计划**（审计 I18N-01，可空 = 未冻结）：
// 它同样属于「这次构建的站点环境」，必须随草稿一起进内核 —— 内核只认记录里的那一份，
// 编译期不会再回读工程服务。
func (s *Service) syncKernel(path, lang string, doc json.RawMessage, pageID string, plan *pipeline.PublicationPlan) error {
	draft := pipeline.Draft{Path: path, Lang: lang, DocJSON: doc, Plan: plan}
	st, err := s.publisher.Status(pageID)
	if errors.Is(err, pipeline.ErrPageNotFound) {
		_, err = s.publisher.SaveDraftInput(pageID, 0, draft)
		return err
	}
	if err != nil {
		return err
	}
	if st.Path != path || st.Lang != lang {
		// 内核记录路径/语言落后于数据库（如改 URL 中断恢复、语言切换）：整体重建。
		s.publisher.LoadRecord(&pipeline.PageRecord{ID: pageID})
		_, err = s.publisher.SaveDraftInput(pageID, 0, draft)
		return err
	}
	_, err = s.publisher.SaveDraftInput(pageID, st.Version, draft)
	return err
}

// restoreKernelForHistory 以目标产物为基线重建内核记录（回滚前置）。
//
// 计划的取法与恢复方向的语义一致：要回到的那份产物自带它的站点语言输入
// （Manifest.siteLangs / siteDefaultLang，审计 I18N-01）。取得到就带着走 ——
// 回滚本身不重编译，但内核记录会被后续的 UpdateURL / 重新构建复用，
// 那时若计划为空就会退回现场解析，等于把这条审计的失效重新引进回滚路径。
func (s *Service) restoreKernelForHistory(page *pagemodel.PageEntity, target *artifactcontract.ArtifactResp) error {
	doc := page.DraftDocumentFor(target.SourceDocument)
	rec := &pipeline.PageRecord{
		ID: page.ID, Path: target.CanonicalPath, Version: 1, Status: pipeline.StatePublished,
		DocumentJSON: doc, Plan: publicationPlanFromManifest(target.Manifest),
		Histories: []*pipeline.HistoryEntry{{
			Hash: target.ArtifactHash, Path: target.CanonicalPath,
			Status: pipeline.StateSuperseded, Order: 1,
		}},
	}
	s.publisher.LoadRecord(rec)
	return nil
}

// restoreKernelForUpdate 以当前发布路径重建内核记录并预激活现有产物（URL 变更前置）。
//
// plan 为本次改 URL 依据的冻结发布计划（审计 I18N-01，可空）：改 URL 会在新路径上
// **重新编译**一份产物，它的 hreflang / 语言切换器必须与既有产物同源，
// 否则同一页面在 /about 与 /new-about 上会声明两套互指。
func (s *Service) restoreKernelForUpdate(ctx context.Context, page *pagemodel.PageEntity, publishedPath string, plan *pipeline.PublicationPlan) error {
	doc := page.DraftDocument
	activeHash := ""
	histories := []*pipeline.HistoryEntry{}
	if page.ActiveArtifactID != nil && *page.ActiveArtifactID != "" {
		art, err := s.artifacts.DetailByID(ctx, &artifactcontract.DetailByIDReq{ID: *page.ActiveArtifactID})
		if err != nil {
			// 活动产物行缺失是数据不一致（产物行被删而指针未清）：显式失败而非
			// 降级为纯草稿——否则 histories/activeHash 留空，UpdateURL 误判纯草稿，
			// 只迁 draft_path 不构建不激活，线上旧 URL 继续出旧内容。
			return fmt.Errorf("页面活动产物缺失（artifact_id=%s），无法修改 URL: %w", *page.ActiveArtifactID, err)
		}
		activeHash = art.ArtifactHash
		histories = append(histories, &pipeline.HistoryEntry{
			Hash: art.ArtifactHash, Path: art.CanonicalPath,
			Status: pipeline.StatePublished, Order: 1,
		})
		doc = page.DraftDocumentFor(art.SourceDocument)
	}
	rec := &pipeline.PageRecord{
		ID: page.ID, Path: publishedPath, Version: 1, Status: pipeline.StatePublished,
		DocumentJSON: doc, ActiveHash: activeHash, Histories: histories,
		Plan: plan,
	}
	s.publisher.LoadRecord(rec)
	return nil
}

// publicationPlanFromManifest 从产物 Manifest 还原冻结的发布计划（审计 I18N-01）。
//
// 为什么从 Manifest 而不是 DB 计划表：这里是**按某一份具体产物**重建的场景
// （回滚、灾难恢复），要复现的是那份产物当时依据的输入，而不是「现在这个页面
// 依据哪份输入」。两者在「发布之后又改过配置、但还没重新冻结」的窗口里会分叉。
//
// 语言表为空（未接入语言的产物，或该字段引入之前的存量产物）时返回 nil：
// 不能拿一份空计划去覆盖现场解析。
func publicationPlanFromManifest(raw []byte) *pipeline.PublicationPlan {
	if len(raw) == 0 {
		return nil
	}
	var m pipeline.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	plan := pipeline.PublicationPlan{SiteLangs: m.SiteLangs, DefaultLang: m.SiteDefaultLang}
	if plan.Empty() {
		return nil
	}
	n := plan.Normalize()
	return &n
}

// kernelVersion 读取内核记录当前版本（不存在视为 1）。
func (s *Service) kernelVersion(pageID string) int {
	if st, err := s.publisher.Status(pageID); err == nil && st.Version > 0 {
		return st.Version
	}
	return 1
}

// pageHasPublishedState 判断页面是否曾上线（与 pipeline.PageRecord.hasPublishedHistory 口径一致）。
// UpdateURL 对纯草稿（从未发布）页面只迁移路径与保留路由，不构建不激活。
func pageHasPublishedState(rec *pipeline.PageRecord) bool {
	if rec == nil {
		return false
	}
	if rec.ActiveHash != "" {
		return true
	}
	for _, h := range rec.Histories {
		if h.Status == pipeline.StatePublished {
			return true
		}
	}
	return false
}

func (s *Service) kernelVersionOrOne(pageID string) int { return s.kernelVersion(pageID) }

// frozenPublicationPlan 读取页面某语言**仍然有效**的冻结计划。
//
// 返回 (nil, false) 的三种情况都必须回退现场解析并重新冻结：
//   - 从未冻结（首次构建 / 迁移前的存量页面）；
//   - 计划行损坏（JSON 不可解析 / 语言表为空）—— 与其拿一份半截输入去编译，
//     不如按当前配置重冻一份，并把原因写进日志；
//   - 草稿已变（作者做了新的发布决策）。
//
// 读取失败（数据库错误）同样返回 false：本次按现场配置解析，随后写库那一步若也
// 失败会让整个构建失败 —— 不会留下「产物按新配置、计划还是旧的」这种半截状态。
func (s *Service) frozenPublicationPlan(ctx context.Context, page *pagemodel.PageEntity, lang string) (*pipeline.PublicationPlan, bool) {
	if s.model == nil || page == nil || strings.TrimSpace(lang) == "" {
		return nil, false
	}
	row, err := s.model.GetPublicationPlan(ctx, page.ID, lang)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			logger.Scene("publication").With("pageId", page.ID).With("lang", lang).
				Error(err, "发布计划读取失败：本次按当前站点语言配置解析并重新冻结")
		}
		return nil, false
	}
	var plan pipeline.PublicationPlan
	if uerr := json.Unmarshal(row.Plan, &plan); uerr != nil {
		logger.Scene("publication").With("pageId", page.ID).With("lang", lang).
			Error(uerr, "发布计划解析失败：本次按当前站点语言配置解析并重新冻结")
		return nil, false
	}
	if plan.Empty() {
		return nil, false
	}
	if row.DraftVersion != page.DraftVersion {
		logger.Scene("publication").With("pageId", page.ID).With("lang", lang).
			With("planDraftVersion", row.DraftVersion).With("draftVersion", page.DraftVersion).
			Info("草稿已更新：按当前站点语言配置重新冻结发布计划")
		return nil, false
	}
	// 页面级语言排除（迁移 491）是**第二条**让既有计划失效的判据：计划里若还留着本页
	// 现在排除的语言，继续沿用就会把那批「不产出的语言」原样写回产物（Manifest.SiteLangs、
	// 批次口径的互指）—— 那是错的产物且看起来正常。排除集合变化属于「产出范围变了」，
	// 与草稿变更同级：重新冻结，而不是沿用。
	if planHasExcludedLang(plan, page.ExcludedLangs) {
		logger.Scene("publication").With("pageId", page.ID).With("lang", lang).
			With("excluded", strings.Join(page.ExcludedLangs, ",")).
			Info("本页排除了计划里包含的语言：按当前产出范围重新冻结发布计划")
		return nil, false
	}
	return &plan, true
}

// publicationPlanFor 解析本次构建 / 发布依据的发布计划（审计 I18N-01）。
//
// 第二个返回值是「是否需要落库」：沿用已冻结计划时为 false（不改库，避免无谓写入
// 与 update_time 抖动）；现场解析时一定为 true，由调用方在**写暂存指针的同一事务**
// 里落库 —— 产物与它依据的语言输入必须同生共死。
//
// 现场解析一律走发布口径（LangFallbackForbidden）：冻结动作本身就是发布决策，
// 语言表读不到时降级成「只有默认语言」会在库里留下一份**错的**冻结事实，
// 此后每次重建都忠实地复现它，且没有任何报错。
func (s *Service) publicationPlanFor(ctx context.Context, page *pagemodel.PageEntity, lang string) (pipeline.PublicationPlan, bool, error) {
	if frozen, ok := s.frozenPublicationPlan(ctx, page, lang); ok {
		// 沿用冻结值 —— 同时把「站点语言清单已经与这份冻结输入不一致」记成可见日志
		// （审计 I18N-01 续第 3 条）：不改变本次行为，只是不让人靠猜。
		s.warnPlanDrift(ctx, page, lang, *frozen)
		return *frozen, false, nil
	}
	inputs, err := pipeline.ResolveSiteLangInputs(ctx, s.project, page.ProjectID, pipeline.LangFallbackForbidden)
	if err != nil {
		return pipeline.PublicationPlan{}, false, err
	}
	// 页面级语言排除（迁移 491）在**冻结时**扣掉，而不是等发布循环里逐个跳过。
	//
	// 两条路径的差别不是风格，而是产物对不对：
	//   · 只在循环里跳过 → 计划（以及写进 Manifest 的 SiteLangs）仍声称「这次发布了该语言」，
	//     而产物根本不存在；批次口径下产物的互指直接按这份集合生成，于是出现
	//     「hreflang 指向一条本站永远不会有产物的路径」——产物自己说的话是错的。
	//   · 冻结时扣掉 → 产物只依赖「确实会产出的语言集合」，站点语言清单改了、排除集合
	//     改了都通过重新冻结体现，不存在半截冻结。
	//
	// 由此也定了「改排除后旧计划要不要重新冻结」：**要**（见 frozenPublicationPlan 的
	// 含排除语言即失效），否则旧计划里那份含被排除语言的集合会被后续重建忠实复现。
	inputs = dropExcludedLangs(inputs, page.ExcludedLangs)
	plan := pipeline.PlanOfSiteLangInputs(inputs)
	if plan.Empty() {
		return pipeline.PublicationPlan{}, false, pipeline.ErrLangTableUnavailable
	}
	// 重新冻结前先说清楚「为什么要按当前配置重算」：这是唯一会让既有产物的语言输入
	// 发生变化的入口（作者改了草稿 = 新的发布决策），日志里必须能看出这一点。
	logger.Scene("publication").With("pageId", page.ID).With("lang", lang).
		With("planHash", plan.Hash()).Info("按当前站点语言配置冻结发布计划")
	return plan, true, nil
}

// planHasLang 语言是否在计划的参与集合里（互指刷新据此把范围收在本页的发布范围内）。
func planHasLang(plan pipeline.PublicationPlan, lang string) bool {
	for _, l := range plan.SiteLangs {
		if l == lang {
			return true
		}
	}
	return false
}

// warnPlanDrift 冻结计划的语言集合与当前站点语言集合不一致时，记一条可见日志。
//
// 判据与「是否重冻」无关（重冻只由草稿变更触发，见 PagePublicationPlanEntity.DraftVersion）：
// 这里只是把「这份产物的互指不会包含新语言」这件事**变得可见** —— 否则运营加了语言、
// 页面却一直只有旧语言互指，全靠人猜（审计 I18N-01 续第 3 条）。
//
// 分等级：站点语言是**增加**时记 Info（正常演进，既有产物本就不该被改写）；
// 出现**移除/禁用**时记 Warn（那些语言的既有产物已经被 I18N-017 的退役流程下线，
// 而冻结计划仍留着它们，属于需要人工确认的漂移）。读取失败什么都不记 ——
// 诊断日志不该因为一次读库抖动就产出一条假的「配置不一致」。
func (s *Service) warnPlanDrift(ctx context.Context, page *pagemodel.PageEntity, lang string, plan pipeline.PublicationPlan) {
	if s == nil || s.project == nil || page == nil || plan.Empty() {
		return
	}
	current, err := s.project.EnabledLangs(ctx, page.ProjectID)
	if err != nil || len(current) == 0 {
		return
	}
	// 被本页排除的语言不在「应当产出」的集合里，必须一起扣掉：否则每次发布都会记一条
	// 「站点移除了这些语言」的假警告（真实成因是这一页主动排除），把告警噪音当信号用。
	current = dropExcludedLangs(pipeline.SiteLangInputs{SiteLangs: current}, page.ExcludedLangs).SiteLangs
	added, removed := langSetDiff(plan.SiteLangs, current)
	if added == nil && removed == nil {
		return
	}
	sc := logger.Scene("publication")
	ev := sc.With("pageId", page.ID).With("lang", lang).
		With("frozen", strings.Join(plan.SiteLangs, ",")).
		With("current", strings.Join(current, ","))
	// 只记「加进来的」（此时也在说明「为什么既有产物没有它们」）；移除的另记一条警告。
	if len(added) > 0 {
		ev.With("added", strings.Join(added, ",")).
			Info("站点语言清单新增了语言，但本页的冻结发布计划不含它：既有产物按冻结值发布，新语言需在草稿改动后的重新发布中生效")
	}
	if len(removed) > 0 {
		ev.With("removed", strings.Join(removed, ",")).
			Warn("站点语言清单已移除本页冻结计划里的语言：既有产物仍按冻结值声明互指，请确认该语言的路由已下线或安排重新发布")
	}
}

// langSetDiff 以**集合**语义比较两份语言表（顺序无关：默认语言在前是实现细节，
// is_default 换人不应被误报成「语言集合变了」）。
// 返回 (仅出现在 current 的、仅出现在 frozen 的)；两者都为空时返回 (nil, nil)。
func langSetDiff(frozen, current []string) (added, removed []string) {
	frozenSet := make(map[string]bool, len(frozen))
	for _, l := range frozen {
		frozenSet[strings.TrimSpace(l)] = true
	}
	currentSet := make(map[string]bool, len(current))
	for _, l := range current {
		currentSet[strings.TrimSpace(l)] = true
	}
	for _, l := range current {
		if l = strings.TrimSpace(l); l != "" && !frozenSet[l] {
			added = append(added, l)
		}
	}
	for _, l := range frozen {
		if l = strings.TrimSpace(l); l != "" && !currentSet[l] {
			removed = append(removed, l)
		}
	}
	if len(added) == 0 && len(removed) == 0 {
		return nil, nil
	}
	return added, removed
}

// sitePathOf 计算页面实际访问路径（语言 URL 方案单点映射），失败归一为 ErrInvalidPath。
//
// 默认语言现场解析。构建 / 发布口径请走 sitePathOfWithPlan —— 默认语言一旦在
// project_locales 里被改（is_default 换人），现场解析会把默认语言算到另一个语言上，
// 于是同一条草稿路径映射到另一个访问路径（默认语言无前缀、非默认语言带前缀）。
func (s *Service) sitePathOf(ctx context.Context, lang string, page *pagemodel.PageEntity) (string, error) {
	return s.sitePathOfWithPlan(ctx, lang, page, nil)
}

// sitePathOfWithPlan 用**冻结计划**里的默认语言计算访问路径（审计 I18N-01）。
//
// plan 为 nil 时等价 sitePathOf（现场解析）—— 两条路径都只经 sitePath 单点映射，
// 禁止在任何一侧手拼 "/" + lang + path。
func (s *Service) sitePathOfWithPlan(ctx context.Context, lang string, page *pagemodel.PageEntity, plan *pipeline.PublicationPlan) (string, error) {
	rule := s.langURLRuleOf(ctx, page.ProjectID)
	if plan != nil && strings.TrimSpace(plan.DefaultLang) != "" {
		rule = pipeline.LangURLRuleForProjectWithDefault(ctx, s.project, page.ProjectID, plan.DefaultLang)
	}
	path, err := sitePath(rule, lang, page.DraftPath)
	if err != nil {
		return "", ErrInvalidPath
	}
	return path, nil
}

// UpdateURL 修改访问路径：新 URL 构建激活后，旧 URL 注册 301 或取消激活。
// nil/空 ID 属于请求不合法（ErrInvalidParam）；合法 ID 无页面才返回 ErrPageNotFound。
// FS 激活发生在 publisher.UpdateURL 内部（先于 DB 路由写入），因此新路径
// 占用检查必须在此之前完成，避免「FS 先覆盖、DB 后报错」的状态分裂（H2/H7）。
//
// 已发布页面按 Publish 的同一套协议：**切换之前**登记 pending 回执（action
// update_url，记下旧路径与该路径的处置方式），切换之后把 DB 各步收进一个事务
// （applyUpdateURL，与启动恢复共用实现）；中途失败保持 pending，由
// RecoverPendingPublications 的 update_url 分支补齐 —— 此前完全没有回执，
// 任一 DB 步失败都会留下「线上是新 URL、DB 还是旧路径」，且重启也捞不回来。
//
// 纯草稿页面（从未发布）不需要回执：内核只迁移草稿路径，访问面没有发生任何切换。
func (s *Service) UpdateURL(ctx context.Context, req *pagedto.UpdateURLReq) (res *pagedto.PublishResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, ErrInvalidParam
	}
	// 逻辑新路径（DB 语义，写入 pages.draft_path）与内核实际路径（带语言前缀）分离：
	// 多语言下二者不同，混淆会导致下次构建重复加前缀（/zh-CN/zh-CN/about）。
	newPath, err := normalizePagePath(req.NewPath)
	if err != nil {
		return nil, err
	}
	page, err := s.getExistingPage(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	// 站点语言集合在**任何内核调用之前**解析（审计 I18N-02 收尾）。
	//
	// 为什么必须在切访问面之前：改 URL 要按启用语言逐语言迁移保留路由，而路径解析
	// 只需一次读语言表。把这次读留到「内核已把 FS 切到新路径之后、事务之内」，
	// 读失败时只剩两种坏选择：带着「只有默认语言」的清单迁移（其余语言的 reserved
	// 行停在旧路径，事务照常提交 —— 三方分裂），或者让已经生效的切换回退（FS 上没有
	// 回退这条路）。放在前面之后，读不到就在**访问面还没动**时失败，什么都不用拆。
	//
	// 口径用发布硬口径（publishLangsOf）：这是会写站点访问路径的动作，
	// 「不知道站点有哪几种语言」不能降级成「只动默认语言」。
	routeLangs, lerr := s.publishLangsOf(ctx, page.ProjectID)
	if lerr != nil {
		logger.Scene("page").With("pageId", page.ID).
			Error(lerr, "改 URL 中止：站点语言清单不可读，不带着不完整的语言集合去切访问面")
		return nil, lerr
	}
	lang := buildLang(req.Lang)
	rule := s.langURLRuleOf(ctx, page.ProjectID)
	kernelNewPath, err := sitePath(rule, lang, newPath)
	if err != nil {
		return nil, ErrInvalidPath
	}
	oldPath := page.DraftPathValue()
	oldRoutePath, err := sitePath(rule, lang, oldPath)
	if err != nil {
		return nil, ErrInvalidPath
	}
	// FS 激活前预检：新路径已被其他页面/展示实例占用（active/redirect/
	// reserved 任一 kind）时提前失败，绝不触发内核的 FS 覆盖。
	if err = s.ensureRouteNotOccupied(ctx, page.ProjectID, kernelNewPath, page.ID); err != nil {
		logger.Scene("page").With("pageId", page.ID).With("newPath", kernelNewPath).Warn("改 URL 被拒绝：新路径已被占用")
		return nil, err
	}
	logger.Scene("page").With("pageId", page.ID).With("lang", lang).With("newPath", kernelNewPath).Info("开始修改 URL")
	// 本语言当前线上路径（page_publications 为该语言真源）；该语言尚未发布时
	// 回退本语言的草稿路由路径（纯草稿分支不会用到它做重定向/取消激活）。
	publishedPath, perr := s.publishedPathOf(ctx, page, lang)
	if perr != nil {
		return nil, perr
	}
	if publishedPath == "" {
		publishedPath = oldRoutePath
	}

	// 发布计划（审计 I18N-01）：改 URL 会在新路径上**重新编译**一份产物，
	// 它必须与既有产物声明同一套互指；计划已冻结且草稿未变时原样沿用。
	plan, persistPlan, err := s.publicationPlanFor(ctx, page, lang)
	if err != nil {
		return nil, err
	}
	// 内核以旧发布路径为基线执行 UpdateURL（内部完成构建+激活+旧路径处置）。
	if err = s.restoreKernelForUpdate(ctx, page, publishedPath, &plan); err != nil {
		return nil, err
	}
	stBefore, _ := s.publisher.Status(page.ID)

	// 纯草稿（从未发布）页面：内核 UpdateURL 只迁移草稿路径（未构建未激活），
	// 访问面没有任何切换 —— 因此不需要回执，两处 DB 写收在一个事务里即可。
	// 只迁 draft_path 与 reserved 路由，不归档产物、不激活路由、不建重定向
	//（线上从未存在，无旧路径可处置）——审计 Medium：UpdateURL 纯草稿。
	if !pageHasPublishedState(stBefore) {
		if _, err = s.publisher.UpdateURL(ctx, page.ID, kernelNewPath, req.WithRedirect); err != nil {
			logger.Scene("page").With("pageId", page.ID).Error(err, "URL 修改失败")
			return nil, mapPublishError(err)
		}
		now := time.Now().UTC()
		if err = s.model.TransactionScoped(ctx, page.ProjectID, func(tx *gorm.DB) error {
			if merr := s.model.MoveDraftPathTx(ctx, tx, page.ProjectID, page.ID, newPath, now); merr != nil {
				return merr
			}
			return s.renameReservedAllLangsTx(ctx, tx, renameReservedInput{
				ProjectID: page.ProjectID, PageID: page.ID,
				OldLogical: oldPath, NewLogical: newPath, TargetLang: lang, Langs: routeLangs,
			})
		}); err != nil {
			logger.Scene("page").With("pageId", page.ID).Error(err, "纯草稿路径迁移失败")
			return nil, err
		}
		st, _ := s.publisher.Status(page.ID)
		status := pipeline.StateDraft
		if st != nil && st.Status != "" {
			status = st.Status
		}
		logger.Scene("page").With("pageId", page.ID).With("oldPath", publishedPath).With("newPath", newPath).
			Info("纯草稿 URL 修改完成（仅迁移路径与保留路由）")
		return &pagedto.PublishResp{
			PageID: page.ID, Status: status, DraftPath: newPath,
			PublishedAt: now.Format(time.RFC3339),
		}, nil
	}

	// 已发布页面：登记 pending 回执，**必须在访问面切换之前**。
	//
	// 改 URL 的产物是切换时按新路径现编译的，登记时还没有对应的产物行，
	// 因此回执的 ToArtifactID 留空（见 publication 侧 DTO 注释），恢复改用
	// 「新路径上的产物 canonicalPath 是否等于新路径」判定；FromArtifactID 记下
	// 切换前的活跃产物，恢复归档新产物行时用它拿冻结源文档。
	receiptID, rerr := s.beginPublishReceipt(ctx, publishReceiptInput{
		Action:    pubcontract.ReceiptActionUpdateURL,
		ProjectID: page.ProjectID, PageID: page.ID,
		Path: kernelNewPath, Lang: lang,
		FromArtifactID: s.publishedArtifactIDOf(ctx, page, lang),
		OldPath:        publishedPath,
		Redirect:       req.WithRedirect,
	})
	if rerr != nil {
		return nil, rerr
	}
	if _, err = s.publisher.UpdateURL(ctx, page.ID, kernelNewPath, req.WithRedirect); err != nil {
		// 内核保证「构建失败不激活、激活失败线上不变」（pipeline.Publisher.UpdateURL
		// 的各错误分支都在 publishLocked 之前）：属于可判定的无副作用失败，显式结案，
		// 不留给启动恢复一个假 pending。
		s.abortPublishReceipt(ctx, receiptID, "访问面切换失败")
		logger.Scene("page").With("pageId", page.ID).Error(err, "URL 修改失败")
		return nil, mapPublishError(err)
	}
	st, _ := s.publisher.Status(page.ID)

	// 新产物归档换取 page_artifacts 行 ID：路由 artifact_id 是 uuid 列，
	// 必须写产物行主键而非内容 hash（生产 DDL 下写 hash 必然 22P02 失败）。
	// 归档源文档用内核构建输入（st.DocumentJSON，即活动产物冻结源文档），
	// 与 restoreKernelForUpdate 的编译输入一致（H4）。
	artifactRowID, deps, err := s.ensureArtifactRow(ctx, page, activeHashOf(st), st.DocumentJSON, lang)
	if err != nil {
		// FS 已切到新路径：保留 pending，恢复流程会用同一份源文档重做归档。
		s.keepPublishReceiptPending(receiptID, "产物归档失败")
		logger.Scene("page").With("pageId", page.ID).With("hash", activeHashOf(st)).Error(err, "URL 修改后产物归档失败")
		return nil, err
	}

	// 数据库各步收在**一个事务**里（applyUpdateURL 与启动恢复共用同一段实现）：
	// draft_path 迁移、该语言激活路径迁移、各语言 reserved 路由改名、新路径路由激活、
	// 旧路径处置（301 或取消激活）、依赖记录同步。任一步失败整体回滚 + 回执保持
	// pending，由启动恢复按链接的实际指向补齐。
	activationPlan := (*pipeline.PublicationPlan)(nil)
	if persistPlan {
		activationPlan = &plan
	}
	if err = s.applyUpdateURL(ctx, updateURLApplyInput{
		Page: page, ArtifactRowID: artifactRowID, Lang: lang,
		KernelNewPath: kernelNewPath, NewLogicalPath: newPath,
		OldLogicalPath: oldPath, OldKernelPath: publishedPath,
		WithRedirect: req.WithRedirect, Deps: deps, RouteLangs: routeLangs,
		Plan: activationPlan,
	}); err != nil {
		s.keepPublishReceiptPending(receiptID, "DB 状态写入失败")
		logger.Scene("page").With("pageId", page.ID).With("newPath", kernelNewPath).
			Error(err, "URL 修改 FS 已切换，但 DB 事务失败（线上已生效，恢复会补齐）")
		return nil, fmt.Errorf("URL 修改已生效但数据库状态同步失败: %w", err)
	}
	// 旧路径的访问面处置（跨系统动作，事务之外）：非重定向时解除链接，重定向时重放
	// 同一份 301 产物 —— 内核那一步失败只记日志，不在这里重放就会留下「DB 说
	// redirect、FS 还指着旧页面」的错位。两步都幂等；失败保持 pending，恢复会重放。
	if derr := s.settleOldPath(kernelNewPath, publishedPath, req.WithRedirect); derr != nil {
		s.keepPublishReceiptPending(receiptID, "旧路径访问面处置失败")
		logger.Scene("page").With("pageId", page.ID).With("oldPath", publishedPath).Error(derr, "旧 URL 访问面处置失败")
		return nil, derr
	}
	if cerr := s.completePublishReceipt(ctx, receiptID); cerr != nil {
		logger.Scene("page").With("pageId", page.ID).With("receiptId", receiptID).
			Error(cerr, "改 URL 回执结案失败（状态已一致，收敛例程会幂等收尾）")
		// 同上：事务已提交、回执未收口 —— 快通道让收敛立刻收掉。
		s.NotifyPendingReceipt()
	}
	status := pipeline.StatePublished
	if st != nil && st.Status != "" {
		status = st.Status
	}
	logger.Scene("page").With("pageId", page.ID).With("oldPath", publishedPath).With("newPath", newPath).Info("URL 修改完成")
	return &pagedto.PublishResp{
		PageID: page.ID, Status: status, ActiveHash: activeHashOf(st),
		OldPath: publishedPath, DraftPath: newPath,
		PublishedAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// publishedPathOf 取该语言当前线上激活路径（page_publications 为真源）。
//
// 兼容口径：关闭站点语言前缀（全局配置 site_lang_url_mode=off）的单语言站点，
// pages.active_path 单值即该语言的路径，历史行（迁移 062 回填前）也按此读；
// 开启前缀时不猜测——没有该语言的激活记录就返回空（本语言从未发布），
// 绝不拿别的语言的路径去 Deactivate（这正是 Publish(en-US) 取消 /zh-CN/about 的根因）。
func (s *Service) publishedPathOf(ctx context.Context, page *pagemodel.PageEntity, lang string) (string, error) {
	pub, err := s.model.GetPublication(ctx, page.ID, lang)
	if err == nil && pub != nil {
		return pub.ActivePath, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}
	if !i18n.SiteLangURLsSeparated(pipeline.SiteLangURLModeOf(ctx, s.project, page.ProjectID)) {
		return page.ActivePathValue(), nil
	}
	return "", nil
}

// ---- 内核记录重建辅助 ----

// ensureRouteNotOccupied 校验目标路径未被其他页面/展示实例占用
// （page_routes 中 active/redirect/reserved 任一 kind；page_id 为空即展示
// 实例占用）。本页面自己的占用行不算冲突。
// 该检查必须在触发 FS 激活的 publisher 调用之前执行（H7 前置防线），
// 并发抢占窗口由 publication Activate 事务内的归属校验兜底。
// 经 publication contract 查询（page_routes 单一所有归 publication）。
func (s *Service) ensureRouteNotOccupied(ctx context.Context, projectID, path, selfPageID string) error {
	if s.routes == nil {
		return nil
	}
	occupied, err := s.routes.IsPathOccupied(ctx, &pubcontract.IsOccupiedReq{
		ProjectID: projectID, Path: path, ExcludePageID: selfPageID,
	})
	if err != nil {
		return err
	}
	if occupied {
		return ErrPathOccupied
	}
	return nil
}

// siteBaseURL 站点公开根地址（sitemap/robots 用）。
// 通过环境变量 WP_SITE_BASE_URL 配置；未配置时返回空串，生成器会省略绝对 URL 前缀。
func siteBaseURL() string {
	return strings.TrimSpace(os.Getenv("WP_SITE_BASE_URL"))
}

// SetExternalArtifactOwners 注入「其它模块认领的产物 hash」提供者（装配期调用一次）。
//
// 传 nil 表示没有其它产物来源：此时只按本模块认领的产物（经 artifact 契约取）判定归属，
// 别的模块的产物会被报成孤儿 —— 所以装配层应当把 presentation 的清单接进来。
func (s *Service) SetExternalArtifactOwners(provider func(ctx context.Context) ([]string, error)) {
	s.externalArtifactOwners = provider
}

// auditOrphanArtifacts 反向对账：磁盘有、无人认领的产物目录（只报告，不删除）。
func (s *Service) auditOrphanArtifacts(ctx context.Context) (orphans []pagedto.OrphanArtifact, checked int, err error) {
	if s == nil || s.store == nil {
		return nil, 0, nil
	}
	owners := []pipeline.OwnerProvider{
		func(ctx context.Context) ([]string, error) {
			return s.pageArtifactHashes(ctx)
		},
	}
	if s.externalArtifactOwners != nil {
		owners = append(owners, s.externalArtifactOwners)
	}
	found, n, aerr := pipeline.AuditOrphanArtifacts(ctx, s.store.Root, owners...)
	if aerr != nil {
		return nil, n, aerr
	}
	orphans = make([]pagedto.OrphanArtifact, 0, len(found))
	var bytes int64
	for _, o := range found {
		orphans = append(orphans, pagedto.OrphanArtifact{Hash: o.Hash, Path: o.Path, Bytes: o.Bytes, Files: o.Files})
		bytes += o.Bytes
	}
	if len(orphans) > 0 {
		// 只记日志不告警：孤儿是可回收的磁盘占用，不是线上故障。
		logger.Scene("page").With("count", len(orphans)).With("bytes", bytes).
			Info("产物对账发现无人认领的目录（交由产物 GC 按保留期回收）")
	}
	return orphans, n, nil
}

// notifyIndexNow 发布成功后异步 ping IndexNow（SEO-022）；失败不影响发布。
func (s *Service) notifyIndexNow(ctx context.Context, projectID string, paths ...string) {
	if s.project == nil || strings.TrimSpace(projectID) == "" || len(paths) == 0 {
		return
	}
	project, err := s.project.Detail(ctx, &projectcontract.DetailReq{ID: projectID})
	if err != nil || project == nil {
		return
	}
	settings := projectcontract.ParseSiteSettings(project.Settings)
	seo.NotifyIndexNowAsync(siteBaseURL(), settings.IndexNowKey, paths)
}

// publishActivationInput 发布（switch_active）的 DB 步骤入参。
type publishActivationInput struct {
	Page         *pagemodel.PageEntity
	Lang         string
	Path         string
	ArtifactID   string
	ArtifactHash string
	// OldPath 该语言发布前的线上路径（发布改路径后要取消它的激活）。
	OldPath string
	// Plan 本次要**冻结落库**的发布计划（审计 I18N-01，可空 = 本次无需写计划）。
	//
	// 可空是必须的：启动恢复走的是同一段实现，而恢复时计划早已随构建落库 ——
	// 带着计划重放会让「崩溃恢复」看起来像一次新的发布决策。
	Plan *pipeline.PublicationPlan
}

// applyPublishActivation 在一个事务里落定发布的数据库状态（主链与恢复共用）。
//
// 幂等：upsert 活跃指针、按归属重复激活路由、按路径重复取消激活都不会产生新状态；
// 因此恢复重放它不会把已收敛的状态改坏。
func (s *Service) applyPublishActivation(ctx context.Context, in publishActivationInput) error {
	now := time.Now().UTC()
	return s.model.TransactionScoped(ctx, in.Page.ProjectID, func(tx *gorm.DB) error {
		if merr := s.model.MarkPublishedLangTx(ctx, tx, in.Page.ProjectID, pagemodel.PublicationRecord{
			PageID: in.Page.ID, Lang: in.Lang, ActivePath: in.Path,
			ArtifactID: in.ArtifactID, ArtifactHash: in.ArtifactHash, PublishedAt: now,
		}); merr != nil {
			return merr
		}
		// 发布计划与激活状态同事务（审计 I18N-01）：两者共同构成「这次发布的事实」，
		// 分开提交会留下「指针已切、计划还是旧的」，而后续重建据此复现不出线上那份产物。
		if in.Plan != nil {
			if perr := s.model.UpdatePublicationPlanRecordTx(ctx, tx, in.Page.ID, in.Lang, *in.Plan, in.Page.DraftVersion, now); perr != nil {
				return perr
			}
		}
		// 故障注入点（测试用；生产恒为 nil）：命中「访问面已切换、数据库尚未落定」窗口。
		// 放在第一条写之后，要证明的是「半截写随事务一起回滚」，而不是「还没开始写」。
		if ferr := s.publishWindowFaultHit(); ferr != nil {
			return ferr
		}
		if s.routes == nil {
			return nil
		}
		if in.OldPath != "" && in.OldPath != in.Path {
			if derr := s.routes.DeactivateTx(ctx, tx, &pubcontract.DeactivateReq{
				ProjectID: in.Page.ProjectID, Path: in.OldPath,
			}); derr != nil {
				return derr
			}
		}
		_, aerr := s.routes.ActivateTx(ctx, tx, &pubcontract.ActivateReq{
			ProjectID: in.Page.ProjectID, Path: in.Path, PageID: in.Page.ID, ArtifactID: in.ArtifactID,
		})
		return aerr
	})
}

// updateURLApplyInput 改 URL 的 DB 步骤入参（主链与恢复共用）。
type updateURLApplyInput struct {
	Page           *pagemodel.PageEntity
	ArtifactRowID  string
	Lang           string
	KernelNewPath  string
	NewLogicalPath string
	// OldLogicalPath 改 URL 前的**草稿**逻辑路径（reserved 路由按它迁移）。
	OldLogicalPath string
	// OldKernelPath 改 URL 前的**线上**路径（301 或取消激活按它处置）。
	OldKernelPath string
	WithRedirect  bool
	// RouteLangs 站点语言集合（发布口径，默认语言在前），由调用方在**事务之前**解析：
	// 保留路由要逐语言迁移，而把它留到事务内再读一次语言表，读失败就会只迁移默认语言
	// （审计 I18N-02 收尾）。主链在切访问面之前解析，恢复路径在补写之前解析。
	RouteLangs []string
	// Deps 本次产物声明的构建期依赖（Manifest.dependencies）。URL 变更不改变依赖集合，
	// 但产物行可能是本次新建的，依赖投影必须同步 —— 否则该产物的精确失效查询会漏掉它。
	Deps []pipeline.Dependency
	// Plan 本次要**冻结落库**的发布计划（审计 I18N-01，可空 = 本次无需写计划）。
	// 可空的理由与 publishActivationInput.Plan 相同：恢复路径重放同一段实现，
	// 而计划早已随构建落库，不该被恢复重写成「一次新的发布决策」。
	Plan *pipeline.PublicationPlan
}

// applyUpdateURL 在一个事务里落定改 URL 的全部数据库状态（主链与恢复共用）。
//
// 五步同事务：draft_path 迁移 → 该语言激活路径迁移 → 各语言 reserved/本语言 active
// 路由改名 → 新路径路由激活 → 旧路径处置（301 或取消激活）。
// 顺序与语义和改动前逐条一致，区别只在于「中途失败整体回滚」——
// 不再出现「FS 已是新 URL、pages.draft_path 与 page_routes 还在旧路径」。
func (s *Service) applyUpdateURL(ctx context.Context, in updateURLApplyInput) error {
	now := time.Now().UTC()
	return s.model.TransactionScoped(ctx, in.Page.ProjectID, func(tx *gorm.DB) error {
		if merr := s.model.MoveDraftPathTx(ctx, tx, in.Page.ProjectID, in.Page.ID, in.NewLogicalPath, now); merr != nil {
			return merr
		}
		if ferr := s.publishWindowFaultHit(); ferr != nil {
			return ferr
		}
		if merr := s.model.MovePublicationPathTx(ctx, tx, in.Page.ProjectID, in.Page.ID, in.Lang, in.KernelNewPath, now); merr != nil {
			return merr
		}
		// 依赖投影与路由同事务：它是「依赖源变更时精确标 stale」的依据，
		// 失败只记日志会让该页长期显示旧内容（Build 侧已按同一口径收口）。
		// deps 为空视为「本次没有要写的条目」而不是「清空」：URL 变更不改变依赖集合，
		// 空集合只可能来自 Manifest 缺失，不该顺手把既有投影删掉。
		if len(in.Deps) > 0 {
			if derr := s.persistDependenciesTx(ctx, tx, in.Page.ProjectID, in.Page.ID, in.ArtifactRowID, in.Deps); derr != nil {
				return derr
			}
		}
		// 发布计划与路径迁移同事务（审计 I18N-01）：新路径上的产物是按这份计划编译的，
		// 两者分开提交会留下「路径已迁、计划还是旧的」——后续重建复现不出线上那份字节。
		if in.Plan != nil {
			if perr := s.model.UpdatePublicationPlanRecordTx(ctx, tx, in.Page.ID, in.Lang, *in.Plan, in.Page.DraftVersion, now); perr != nil {
				return perr
			}
		}
		if s.routes == nil {
			return nil
		}
		if rerr := s.renameReservedAllLangsTx(ctx, tx, renameReservedInput{
			ProjectID: in.Page.ProjectID, PageID: in.Page.ID,
			OldLogical: in.OldLogicalPath, NewLogical: in.NewLogicalPath,
			TargetLang: in.Lang, Langs: in.RouteLangs,
		}); rerr != nil {
			return rerr
		}
		if _, aerr := s.routes.ActivateTx(ctx, tx, &pubcontract.ActivateReq{
			ProjectID: in.Page.ProjectID, Path: in.KernelNewPath,
			PageID: in.Page.ID, ArtifactID: in.ArtifactRowID,
		}); aerr != nil {
			return aerr
		}
		if in.OldKernelPath == "" || in.OldKernelPath == in.KernelNewPath {
			return nil
		}
		if in.WithRedirect {
			// 旧路径 → 新路径登记为 redirect 行（**只登记 DB 占用**，不落盘重定向产物：
			// 301 产物由内核 publisher.UpdateURL 落盘并激活到旧路径；恢复路径下
			// settleOldPath 会用同一份产物幂等地重放）。
			//
			// 历史上这里曾自己 NewRedirectArtifact(publishedPath, 301) 再 PutRedirect 一次 ——
			// 该函数第一个参数是 targetPath，传旧路径自身等于生成一条 A→A 的自环 301。
			// 它落在内容寻址 store 里、从不被激活，所以线上行为一直是对的（生效的是内核那份），
			// 代价是每工程每改一次 URL 就多一份永不使用的垃圾产物。已删除，勿再引入。
			_, rerr := s.routes.RedirectTx(ctx, tx, &pubcontract.RedirectReq{
				ProjectID: in.Page.ProjectID, OldPath: in.OldKernelPath, PageID: in.Page.ID,
			})
			return rerr
		}
		return s.routes.DeactivateTx(ctx, tx, &pubcontract.DeactivateReq{
			ProjectID: in.Page.ProjectID, Path: in.OldKernelPath,
		})
	})
}

// rollbackApplyInput 回滚的 DB 步骤入参（主链与恢复共用）。
type rollbackApplyInput struct {
	Page       *pagemodel.PageEntity
	Lang       string
	TargetPath string
	TargetID   string
	TargetHash string
	// OldPath 回滚前的线上路径（与目标路径不同才需要取消它的激活）。
	OldPath string
}

// applyRollback 在一个事务里落定回滚的数据库状态（主链与恢复共用）。
func (s *Service) applyRollback(ctx context.Context, in rollbackApplyInput) error {
	now := time.Now().UTC()
	return s.model.TransactionScoped(ctx, in.Page.ProjectID, func(tx *gorm.DB) error {
		if merr := s.model.MarkPublishedLangTx(ctx, tx, in.Page.ProjectID, pagemodel.PublicationRecord{
			PageID: in.Page.ID, Lang: in.Lang, ActivePath: in.TargetPath,
			ArtifactID: in.TargetID, ArtifactHash: in.TargetHash, PublishedAt: now,
		}); merr != nil {
			return merr
		}
		if ferr := s.publishWindowFaultHit(); ferr != nil {
			return ferr
		}
		if s.routes == nil {
			return nil
		}
		if in.OldPath != "" && in.OldPath != in.TargetPath {
			if derr := s.routes.DeactivateTx(ctx, tx, &pubcontract.DeactivateReq{
				ProjectID: in.Page.ProjectID, Path: in.OldPath,
			}); derr != nil {
				return derr
			}
		}
		_, aerr := s.routes.ActivateTx(ctx, tx, &pubcontract.ActivateReq{
			ProjectID: in.Page.ProjectID, Path: in.TargetPath, PageID: in.Page.ID, ArtifactID: in.TargetID,
		})
		return aerr
	})
}

// settleOldPath 处置旧路径的**访问面**状态（跨系统补偿：文件系统，事务边界之外）。
//
// 幂等 + 可重放：非重定向直接删链接（未激活时 Deactivate 返回 nil）；重定向重放
// 同一份 301 产物并重新激活（内容寻址，重复 Put 是 no-op）。失败时把错误交回调用方，
// 让回执保持 pending —— 下次启动重放同一段补齐，而 DB 那几步是幂等的。
func (s *Service) settleOldPath(newPath, oldPath string, withRedirect bool) error {
	if strings.TrimSpace(oldPath) == "" || oldPath == newPath {
		return nil
	}
	if !withRedirect {
		return s.deactivatePaths([]string{oldPath})
	}
	if s.store == nil || s.publication == nil {
		return nil
	}
	ra, rerr := pipeline.NewRedirectArtifact(newPath, 301)
	if rerr != nil {
		return rerr
	}
	loc, perr := s.store.PutRedirect(ra)
	if perr != nil {
		return perr
	}
	if aerr := s.publication.Activate(oldPath, loc); aerr != nil {
		return aerr
	}
	logger.Scene("page").With("oldPath", oldPath).With("newPath", newPath).
		Info("旧路径的 301 已重放（改 URL 恢复）")
	return nil
}

// activeArtifactHashAt 读路径当前激活产物的 hash（未激活 / 重定向 / 读不到时为空串）。
func (s *Service) activeArtifactHashAt(path string) string {
	if s == nil || s.publication == nil {
		return ""
	}
	state, err := s.publication.Inspect(path)
	if err != nil || state == nil || state.Locator == nil {
		return ""
	}
	return strings.TrimPrefix(state.Locator.Key, "artifacts/")
}

// updateURLArtifactAt 读新路径上的激活产物并校验它确实是「按该路径编译」的产物。
//
// 改 URL 的产物 canonicalPath 就是新路径（canonicalPath 进 Manifest 并参与 hash），
// 所以「新路径上的链接指向一个 canonicalPath == 新路径的产物」足以证明切换发生过；
// 链接不存在、指向别处、是 301、或产物读不出来一律判定为未生效（保守：拿不准就
// 不做数据库写入，只把回执结案）。
func (s *Service) updateURLArtifactAt(path string) (*pipeline.Artifact, string, bool) {
	if s == nil || s.publication == nil || s.store == nil {
		return nil, "", false
	}
	state, err := s.publication.Inspect(path)
	if err != nil || state == nil || state.Kind != pipeline.PublicationPage || state.Locator == nil {
		return nil, "", false
	}
	art, aerr := s.store.GetArtifact(*state.Locator)
	if aerr != nil || art == nil || art.CanonicalPath != path {
		return nil, "", false
	}
	hash := strings.TrimPrefix(state.Locator.Key, "artifacts/")
	if hash == "" {
		hash = art.Hash
	}
	return art, hash, true
}

// ensureUpdateURLArtifactRow 取（必要时归档）改 URL 新产物的元数据行 id。
//
// 崩溃点可能落在「FS 已切到新路径」之后、归档之前，此时产物行还不存在。
// 源文档必须取**活动产物的冻结源文档**：改 URL 以它为构建基线（restoreKernelForUpdate），
// 用本页当前草稿归档会让日后按该产物回滚编译出不同 hash（ErrRollbackPathMismatch）。
func (s *Service) ensureUpdateURLArtifactRow(ctx context.Context, page *pagemodel.PageEntity, hash, fromArtifactID, lang string) (string, []pipeline.Dependency, error) {
	if strings.TrimSpace(hash) == "" {
		return "", nil, errors.New("改 URL 恢复：产物 hash 为空")
	}
	if existing, derr := s.artifacts.Detail(ctx, &artifactcontract.DetailReq{PageID: page.ID, Hash: hash}); derr == nil && existing != nil && existing.ID != "" {
		return existing.ID, nil, nil
	}
	source := json.RawMessage(nil)
	if id := strings.TrimSpace(fromArtifactID); id != "" {
		if old, oerr := s.artifacts.DetailByID(ctx, &artifactcontract.DetailByIDReq{ID: id}); oerr == nil && old != nil {
			source = old.SourceDocument
		}
	}
	if len(source) == 0 {
		source = page.DraftDocument
		logger.Scene("page").With("pageId", page.ID).With("hash", hash).
			Warn("改 URL 恢复：取不到活动产物的冻结源文档，回退当前草稿归档（该产物行的 source_document 可能与产物字节不符）")
	}
	rowID, deps, aerr := s.ensureArtifactRow(ctx, page, hash, source, lang)
	if aerr != nil {
		return "", nil, aerr
	}
	// 依赖集合交给调用方，与路由改名等步骤落进同一个事务（崩溃点若落在归档之前，
	// 依赖行同样缺失；写失败的后果是「内容改了该页不被精确标 stale」）。
	return rowID, deps, nil
}

// recoverSwitchActiveReceipt 补齐「发布：FS 已切、DB 没跟上」的回执。
//
// 判定证据：路径上的符号链接确实指向本次要激活的产物（hash 相等）。
// 证据不足一律走回滚分支（只结案、不动数据库）——判定错会写出错误的活跃指针。
func (s *Service) recoverSwitchActiveReceipt(ctx context.Context, item pubcontract.PendingReceiptResp) (bool, error) {
	actualHash := s.activeArtifactHashAt(item.Path)
	expectedHash, herr := s.pageArtifactHashByID(ctx, item.ToArtifactID)
	if herr != nil {
		s.abortPublishReceipt(ctx, item.ID, "读取回执产物失败")
		return false, nil
	}
	if actualHash == "" || expectedHash == "" || actualHash != expectedHash {
		// 切换没发生（或指向的还是旧产物）：数据库保持原样即可，只结案。
		logger.Scene("publication").With("receiptId", item.ID).With("path", item.Path).
			With("actual", actualHash).With("expected", expectedHash).
			Warn("未结案的发布回执判定为「未生效」，标记回滚")
		s.abortPublishReceipt(ctx, item.ID, "访问面未指向本次产物")
		return false, nil
	}
	page, perr := s.locatePageInProjects(ctx, item.SourceID)
	if perr != nil {
		s.abortPublishReceipt(ctx, item.ID, "页面不存在")
		return false, nil
	}
	if err := s.applyPublishActivation(ctx, publishActivationInput{
		Page: page, Lang: buildLang(item.Lang), Path: item.Path,
		ArtifactID: item.ToArtifactID, ArtifactHash: expectedHash, OldPath: item.OldPath,
	}); err != nil {
		return false, err
	}
	if cerr := s.completePublishReceipt(ctx, item.ID); cerr != nil {
		return false, cerr
	}
	if serr := s.settleOldPath(item.Path, item.OldPath, false); serr != nil {
		return false, serr
	}
	logger.Scene("publication").With("pageId", page.ID).With("path", item.Path).
		Info("发布在崩溃前已生效，已补齐数据库状态")
	return true, nil
}

// recoverUpdateURLReceipt 补齐「改 URL：FS 已切到新路径、DB 没跟上」的回执。
func (s *Service) recoverUpdateURLReceipt(ctx context.Context, item pubcontract.PendingReceiptResp) (bool, error) {
	_, hash, ok := s.updateURLArtifactAt(item.Path)
	if !ok {
		logger.Scene("publication").With("receiptId", item.ID).With("path", item.Path).
			Warn("未结案的改 URL 回执判定为「未生效」，标记回滚")
		s.abortPublishReceipt(ctx, item.ID, "访问面未指向本次改 URL 的新产物")
		return false, nil
	}
	page, perr := s.locatePageInProjects(ctx, item.SourceID)
	if perr != nil {
		s.abortPublishReceipt(ctx, item.ID, "页面不存在")
		return false, nil
	}
	lang := buildLang(item.Lang)
	rowID, deps, aerr := s.ensureUpdateURLArtifactRow(ctx, page, hash, item.FromArtifactID, lang)
	if aerr != nil {
		return false, aerr
	}
	rule := s.langURLRuleOf(ctx, page.ProjectID)
	// 语言集合在补写事务**之前**解析（审计 I18N-02 收尾）：与主链同一判据 —— 读不到就
	// 不做「只迁默认语言」的打折迁移，直接返回错误让回执保持 pending，等下一次收敛重放。
	// 此时访问面早已切换（崩溃点就在切换之后），所以宁可原地不动也不能迁一半：
	// 迁一半留下的是「新路径已激活、其余语言的保留路由还在旧路径」，没有任何入口能发现。
	routeLangs, lerr := s.publishLangsOf(ctx, page.ProjectID)
	if lerr != nil {
		return false, lerr
	}
	if err := s.applyUpdateURL(ctx, updateURLApplyInput{
		Page: page, ArtifactRowID: rowID, Lang: lang,
		KernelNewPath:  item.Path,
		NewLogicalPath: rule.Strip(lang, item.Path),
		// 事务没提交时 draft_path 仍是旧逻辑路径；若事务其实已提交（只是回执未结案），
		// 这里取到的是新路径 —— 那时的改名是一个 no-op，重放安全。
		OldLogicalPath: page.DraftPath,
		OldKernelPath:  item.OldPath,
		WithRedirect:   item.Redirect,
		Deps:           deps,
		RouteLangs:     routeLangs,
	}); err != nil {
		return false, err
	}
	if cerr := s.completePublishReceipt(ctx, item.ID); cerr != nil {
		return false, cerr
	}
	if serr := s.settleOldPath(item.Path, item.OldPath, item.Redirect); serr != nil {
		return false, serr
	}
	logger.Scene("page").With("pageId", page.ID).With("path", item.Path).
		Info("改 URL 在崩溃前已生效，已补齐数据库状态")
	return true, nil
}

// recoverRollbackReceipt 补齐「回滚：FS 已切回历史产物、DB 没跟上」的回执。
func (s *Service) recoverRollbackReceipt(ctx context.Context, item pubcontract.PendingReceiptResp) (bool, error) {
	actualHash := s.activeArtifactHashAt(item.Path)
	expectedHash, herr := s.pageArtifactHashByID(ctx, item.ToArtifactID)
	if herr != nil {
		s.abortPublishReceipt(ctx, item.ID, "读取回执产物失败")
		return false, nil
	}
	if actualHash == "" || expectedHash == "" || actualHash != expectedHash {
		logger.Scene("publication").With("receiptId", item.ID).With("path", item.Path).
			With("actual", actualHash).With("expected", expectedHash).
			Warn("未结案的回滚回执判定为「未生效」，标记回滚")
		s.abortPublishReceipt(ctx, item.ID, "访问面未指向回滚目标产物")
		return false, nil
	}
	page, perr := s.locatePageInProjects(ctx, item.SourceID)
	if perr != nil {
		s.abortPublishReceipt(ctx, item.ID, "页面不存在")
		return false, nil
	}
	if err := s.applyRollback(ctx, rollbackApplyInput{
		Page: page, Lang: buildLang(item.Lang), TargetPath: item.Path,
		TargetID: item.ToArtifactID, TargetHash: expectedHash, OldPath: item.OldPath,
	}); err != nil {
		return false, err
	}
	if cerr := s.completePublishReceipt(ctx, item.ID); cerr != nil {
		return false, cerr
	}
	if serr := s.settleOldPath(item.Path, item.OldPath, false); serr != nil {
		return false, serr
	}
	logger.Scene("page").With("pageId", page.ID).With("path", item.Path).
		Info("回滚在崩溃前已生效，已补齐数据库状态")
	return true, nil
}

// ErrPublishLedgerUnavailable 发布回执登记失败（无法判定状态，因此不切换访问面）。
var ErrPublishLedgerUnavailable = errors.New("发布回执登记失败，未切换访问面")

// publishReceiptInput 登记 pending 回执的入参。
//
// 用结构体而不是 8 个位置参数：三种动作（发布 / 改 URL / 回滚）各有几个专属字段
// （改 URL 的 OldPath 与 Redirect、回滚的 FromArtifactID），位置参数下一个调用点
// 传错顺序编译器不会报错，而错的是「恢复按哪个路径补哪一步」。
type publishReceiptInput struct {
	// Action 见 pubcontract.ReceiptAction*：决定恢复流程分派到哪个补齐例程。
	Action         string
	ProjectID      string
	PageID         string
	Path           string
	Lang           string
	FromArtifactID string
	// ToArtifactID 本次要激活的产物行 id；Action 为 update_url 时必须留空
	// （产物按新路径现编译，登记时还不存在对应行，见 publication 侧字段注释）。
	ToArtifactID string
	// OldPath 切换前该语言的线上路径（改 URL / 回滚用它处置旧路径）。
	OldPath string
	// Redirect 旧路径是否登记为 301（仅 update_url 使用）。
	Redirect bool
}

// beginPublishReceipt 登记 pending 回执，返回回执 id 与失败原因（AR2-002 / TX-009）。
//
// 必须在访问面切换**之前**调用：切换是不可逆的副作用，登记放在之后，崩溃窗口里就
// 查不到「这次发布发生过」。拿不到 id 一律按硬失败返回 —— 带着未知状态去切访问面，
// 正是这条回执要消灭的分裂状态，调用方据此中止发布（不静默继续）。
//
// 唯一不算失败的是路由契约未装配（s.routes == nil）：此时发布链本身也不写路由行，
// 属于「这台实例没有回执设施」而不是「登记失败」，返回空 id + nil 让发布按原样继续。
func (s *Service) beginPublishReceipt(ctx context.Context, in publishReceiptInput) (string, error) {
	if s == nil || s.routes == nil {
		return "", nil
	}
	action := strings.TrimSpace(in.Action)
	if action == "" {
		action = pubcontract.ReceiptActionSwitchActive
	}
	id, err := s.routes.BeginPublishReceipt(ctx, &pubcontract.BeginPublishReceiptReq{
		ProjectID: in.ProjectID, Path: in.Path, PageID: in.PageID,
		FromArtifactID: in.FromArtifactID, ToArtifactID: in.ToArtifactID, Lang: in.Lang,
		Action: action, OldPath: in.OldPath, Redirect: in.Redirect,
	})
	if err == nil && strings.TrimSpace(id) == "" {
		err = errors.New("发布回执登记未返回 id")
	}
	if err != nil {
		logger.Scene("publication").With("pageId", in.PageID).With("path", in.Path).With("action", action).
			Error(err, "发布回执登记失败（不切换访问面）")
		return "", ErrPublishLedgerUnavailable
	}
	return id, nil
}

// publishedArtifactIDOf 取该语言当前激活产物 id（page_publications 为真源）。
//
// 回执要如实记录「切换前指着哪个产物」：from/to 两侧合起来才是这次发布是从哪个版本
// 切到哪个版本，只记 to 会让回滚与审计失去「从哪来」的依据。口径与 publishedPathOf
// 一致 —— 没有该语言的激活记录（本语言从未发布）返回空串，绝不拿别的语言的产物顶替。
func (s *Service) publishedArtifactIDOf(ctx context.Context, page *pagemodel.PageEntity, lang string) string {
	if s == nil || page == nil {
		return ""
	}
	pub, err := s.model.GetPublication(ctx, page.ID, lang)
	if err != nil || pub == nil || pub.ArtifactID == nil {
		return ""
	}
	return *pub.ArtifactID
}

// publishWindowFaultHit 触发「访问面已切换、数据库尚未写入」窗口的故障注入点
// （生产恒为 nil，只多一次判空；见 Service.publishWindowFault 字段注释）。
func (s *Service) publishWindowFaultHit() error {
	if s == nil || s.publishWindowFault == nil {
		return nil
	}
	return s.publishWindowFault()
}

// keepPublishReceiptPending 收敛「无法判定」的失败：访问面可能已经切换，此刻把回执
// 标成 rolled_back 会让恢复流程以为这次发布从未生效 —— 错误判定比不判定更糟。
// 因此只记日志、保留 pending，交给收敛例程按符号链接的实际指向补齐或回滚。
//
// 同时推一次进程内快通道：调用点都在**事务已经落定之后**（DB 事务失败的回滚已发生、
// 或文件系统那一步已失败），此刻正是「库里有 pending、线上状态未知」——
// 让收敛立刻跑一遍，而不是等下一个定时间隔（正常情况下毫秒级收敛，见
// page_publish_converge.go）。信号非阻塞，且收敛本身幂等。
func (s *Service) keepPublishReceiptPending(receiptID, reason string) {
	if s == nil || strings.TrimSpace(receiptID) == "" {
		return
	}
	logger.Scene("publication").With("receiptId", receiptID).
		Warn("发布中断在「已切换访问面、数据库未跟上」窗口，回执保持 pending 交收敛例程判定：" + reason)
	s.NotifyPendingReceipt()
}

// completePublishReceipt 结案（访问面与数据库已一致）。
func (s *Service) completePublishReceipt(ctx context.Context, receiptID string) error {
	if s == nil || s.routes == nil || receiptID == "" {
		return nil
	}
	return s.routes.CompletePublishReceipt(ctx, receiptID)
}

// abortPublishReceipt 结案为已回滚（切换没发生或无副作用失败）。
func (s *Service) abortPublishReceipt(ctx context.Context, receiptID, reason string) {
	if s == nil || s.routes == nil || receiptID == "" {
		return
	}
	if err := s.routes.AbortPublishReceipt(ctx, receiptID); err != nil {
		logger.Scene("publication").With("receiptId", receiptID).
			Error(err, "发布回执结案失败（"+reason+"）")
	}
}

// recoverableReceiptActions 本模块认得的访问面切换回执动作名（与 publication 的登记端
// 同一份词汇表）。
//
// 为什么必须显式在册：publication_receipts 是**共用表** —— 只处理手工页面的回执
// （自动发布实例的活跃指针在别的表上，用同一套恢复逻辑会写错地方），
// 而路由变更回执（Activate 的 activate / Redirect 的 redirect）也落在这里、
// 由各自事务内的结案与补偿处理：拿发布形态的判据去「补齐」一条路由回执，
// 会把路由变更纠正成发布状态。
//
// 三种动作「已切换访问面、数据库没跟上」时要补的数据库步骤不同，因此还必须按动作分派。
// 这份清单同时是**领取条件**（ClaimPendingReceipts 的 action IN）与**判定条件**
// （recoverableReceiptAction）——两处各写一套筛选条件，迟早出现「领得到却判不出来」的空转。
var recoverableReceiptActions = []string{
	pubcontract.ReceiptActionSwitchActive,
	pubcontract.ReceiptActionUpdateURL,
	pubcontract.ReceiptActionRollback,
}

// recoverableReceiptAction 判断某个动作是否在本模块的收敛范围内（不在册一律跳过，
// 而不是当成发布形态硬套）。
func recoverableReceiptAction(action string) bool {
	for _, known := range recoverableReceiptActions {
		if known == action {
			return true
		}
	}
	return false
}

// recoverOne 判定单条回执。返回 true 表示已补完成，false 表示已标回滚。
func (s *Service) recoverOne(ctx context.Context, item pubcontract.PendingReceiptResp) (bool, error) {
	// 新形态先分派：改 URL（路径迁移 + 旧路径处置）与回滚（活跃指针 + 旧路径下线）
	// 要补的步骤与发布不同，各自实现见 page_publish_recover.go。
	switch item.Action {
	case pubcontract.ReceiptActionUpdateURL:
		return s.recoverUpdateURLReceipt(ctx, item)
	case pubcontract.ReceiptActionRollback:
		return s.recoverRollbackReceipt(ctx, item)
	}
	return s.recoverSwitchActiveReceipt(ctx, item)
}

const (
	// pendingReceiptConvergeBatch 单批领取上限（条）。
	//
	// 批次存在的意义与保留期清理同一条：不制造长事务，也不让一次收敛把启动链拖住。
	// 一条重放要读文件系统符号链接 + 若干次 DB 往返，20 条把单批耗时压在秒级以内。
	pendingReceiptConvergeBatch = 20
	// pendingReceiptConvergeMaxBatches 单次收敛的批次上限（20 × 50 = 1000 条/次）。
	//
	// 正常待办是个位数，这个上限只用来兜住「积压了成千上万条」的异常场面：
	// 超出的部分交给下一轮，而不是让一个 goroutine 长时间占着数据库连接跑下去。
	pendingReceiptConvergeMaxBatches = 50
	// pendingReceiptStartupMaxBatches 启动首跑的批次上限；<= 0 表示不限批。
	//
	// 启动首跑沿用改动前的语义（一次把上次进程的残留全部收干净），由调用方给的超时封顶。
	pendingReceiptStartupMaxBatches = 0
	// pendingReceiptConvergeInterval 定时兜底的间隔。
	//
	// 取值理由：正常路径由写路径的快通道以毫秒级驱动，定时器只兜底两种场面 ——
	// 快通道信号丢了（容量 1 的通道在收敛进行中只能留住一次信号），
	// 以及残留是**别的实例**留下的（本进程收不到它的写信号）。
	// 一分钟足以把不一致窗口压在一分钟内；再短只增加空转次数，再长则兜底失去意义。
	// 空转成本本身被迁移 267 的部分索引压到「一次零行的索引扫描」，与表的历史规模无关。
	pendingReceiptConvergeInterval = time.Minute
	// pendingReceiptConvergeTimeout 定时 / 快通道单次收敛的超时。
	pendingReceiptConvergeTimeout = 2 * time.Minute
	// pendingReceiptStartupTimeout 启动首跑的超时（与改动前的启动恢复一致，5 分钟）。
	pendingReceiptStartupTimeout = 5 * time.Minute
)

// pendingReceiptConvergeResult 一次收敛的结果（供日志与返回值使用）。
type pendingReceiptConvergeResult struct {
	claimed    int // 本轮领取到的回执条数
	completed  int // 补齐数据库状态后结案
	rolledBack int // 判定为未生效、结案为已回滚
	failed     int // 仍失败，保留 pending
	batches    int // 实际领取的批次数
}

// converged 已离开 pending 的条数（补完成 + 标回滚）。
//
// 「仍失败」不算收敛：它还在 pending，下一轮继续重放。把失败的 pending 直接标死，
// 等于拿「看起来收敛了」换掉「状态未知」，比不收敛更糟。
func (r pendingReceiptConvergeResult) converged() int { return r.completed + r.rolledBack }

// ConvergePendingReceipts 收敛未结案的发布回执（定时器与写路径快通道的入口）。
//
// 与启动恢复共用同一段重放实现（convergePendingReceipts → recoverOne）：
// 两种入口各写一套判定迟早分叉，而分叉的表现是「定时收敛说已收敛、重启恢复又改一遍，
// 两边结果不同」。
//
// 幂等：重放的每一步都是 upsert / 内容寻址 / 按归属重复激活，结案又只改 pending 行，
// 因此重复收敛（多实例、快通道与定时器同时触发）不会写出新状态。
//
// 返回的 error 汇总「领取失败」与「单条仍失败」；已收敛的条数照常返回 ——
// 部分失败不该把整轮的结果丢掉（调用方据此记日志，失败的行保留 pending 等下轮）。
func (s *Service) ConvergePendingReceipts(ctx context.Context) (converged int, err error) {
	res, claimErr, itemErrs := s.convergePendingReceipts(ctx, pendingReceiptConvergeMaxBatches)
	return res.converged(), errors.Join(append([]error{claimErr}, itemErrs...)...)
}

// RecoverPendingPublications 启动 / 运维全量恢复：把未结案的发布回执一次收干净。
//
// 与定时收敛共用同一段实现（convergePendingReceipts），区别只在批次上限：
// 这里不限批（由调用方给的超时封顶），定时 / 快通道按批（不制造长事务）。
// 返回值保持原有形状（补完成 / 标回滚），供既有调用方与用例断言。
//
// 只把「领取失败」回传给调用方；单条重放失败按「保留 pending、下一轮再试」处理，
// 不让一条收不了的旧回执把整个启动恢复判成失败。
func (s *Service) RecoverPendingPublications(ctx context.Context) (recovered, rolledBack int, err error) {
	res, claimErr, _ := s.convergePendingReceipts(ctx, pendingReceiptStartupMaxBatches)
	return res.completed, res.rolledBack, claimErr
}

// convergePendingReceipts 收敛主循环：分批领取 → 逐条重放既有恢复例程。
//
// maxBatches <= 0 表示不限批（启动首跑；由 ctx 超时封顶）。
// 三个返回值：结果统计、领取错误（数据库层，整轮中止）、单条重放错误（保留 pending）。
func (s *Service) convergePendingReceipts(ctx context.Context, maxBatches int) (res pendingReceiptConvergeResult, claimErr error, itemErrs []error) {
	// 与 RecoverPendingPublications 改动前的守卫一致：没有契约、没有访问面存储时不做任何事
	// （这台实例没有回执设施，不是「没有待办」）。
	if s == nil || s.routes == nil || s.publication == nil {
		return res, nil, nil
	}
	start := time.Now()
	for batch := 0; maxBatches <= 0 || batch < maxBatches; batch++ {
		if cerr := ctx.Err(); cerr != nil {
			itemErrs = append(itemErrs, cerr)
			break
		}
		items, cerr := s.routes.ClaimPendingReceipts(ctx, &pubcontract.ReceiptsQueryReq{
			SourceType: pubcontract.ReceiptSourcePage,
			Actions:    recoverableReceiptActions,
			Limit:      pendingReceiptConvergeBatch,
		})
		if cerr != nil {
			claimErr = cerr
			logger.Scene("publication").Error(cerr, "领取未结案发布回执失败（本轮收敛中止）")
			break
		}
		if len(items) == 0 {
			break
		}
		res.claimed += len(items)
		res.batches++
		handled, failed := 0, 0
		for _, item := range items {
			// 领取条件已经筛过一遍，这里是第二道（防止领取口径将来被放宽后静默扩大重放范围：
			// 拿发布形态的判据去「补齐」一条路由回执，会把路由变更纠正成发布状态）。
			if item.SourceType != pubcontract.ReceiptSourcePage || !recoverableReceiptAction(item.Action) {
				continue
			}
			handled++
			done, rerr := s.recoverOne(ctx, item)
			if rerr != nil {
				failed++
				logger.Scene("publication").With("receiptId", item.ID).With("action", item.Action).
					With("path", item.Path).Error(rerr, "发布回执收敛失败（保留 pending，下一轮重试）")
				itemErrs = append(itemErrs, fmt.Errorf("回执 %s(%s): %w", item.ID, item.Action, rerr))
				continue
			}
			if done {
				res.completed++
			} else {
				res.rolledBack++
			}
		}
		res.failed += failed
		// 本批出现仍失败的行就收手：那些行还在 pending（且多半正卡在最旧的一批上），
		// 继续领下一批只会把它们再重放一遍 —— 交给下一轮，别在这里空转。
		if failed > 0 {
			break
		}
		// 本批没有一行属于本收敛例程（领取口径与判定口径不一致）：再领还是同一批，收手。
		if handled == 0 {
			break
		}
	}
	s.lastConvergeAt.Store(time.Now().Unix())
	s.logPendingReceiptConverge(res, time.Since(start))
	return res, claimErr, itemErrs
}

// logPendingReceiptConverge 记一条结构化收敛日志（收敛条数 / 失败条数 / 耗时）。
//
// 空转（一条都没领到）走 Debug：定时器每分钟一次，用 Info 记「什么都没发生」只会把
// 真正有用的那几条淹掉。有失败一律 Warn —— 失败的 pending 是「线上与库还不一致」，
// 必须在日志里看得见。
func (s *Service) logPendingReceiptConverge(res pendingReceiptConvergeResult, cost time.Duration) {
	entry := logger.Scene("publication").
		With("claimed", res.claimed).
		With("converged", res.converged()).
		With("completed", res.completed).
		With("rolledBack", res.rolledBack).
		With("failed", res.failed).
		With("batches", res.batches).
		With("costMs", cost.Milliseconds())
	switch {
	case res.failed > 0:
		entry.Warn("发布回执收敛完成（有仍未成功的行，保留 pending 等待下一轮）")
	case res.claimed > 0:
		entry.Info("发布回执收敛完成")
	default:
		entry.Debug("发布回执收敛空转（没有未结案的回执）")
	}
}

// NotifyPendingReceipt 写路径的进程内快通道信号（事务提交后调用）。
//
// 容量 1、非阻塞发送：并发写入合并成一次收敛（多推几次不携带额外信息），
// 通道满时直接丢弃 —— 定时器兜底，丢信号只会让收敛晚一个间隔，不会让回执永久 pending。
// 未启动调度（用例、或未装配页面路由的进程）时这个信号没有接收方，同样无害。
func (s *Service) NotifyPendingReceipt() {
	if s == nil || s.convergeWake == nil {
		return
	}
	select {
	case s.convergeWake <- struct{}{}:
	default:
	}
}

// PendingReceiptStatus 只读观测：当前未收敛的 pending 回执数 + 本进程最近一次收敛时刻。
//
// 口径与本收敛例程的领取条件完全一致（page + 三种访问面切换动作）：数的是
// 「本实例会去收、且还没收掉的行」，健康检查据此判断「收敛是不是跟不上了」。
// 路由变更回执（activate / redirect）由各自的事务内结案处理、不归这里，
// presentation 的回执由 presentation 自己的恢复例程负责，也都不计入。
func (s *Service) PendingReceiptStatus(ctx context.Context) (pending int64, lastConvergeAt time.Time, err error) {
	if s == nil || s.routes == nil {
		return 0, time.Time{}, nil
	}
	pending, err = s.routes.CountPendingReceipts(ctx, &pubcontract.ReceiptsQueryReq{
		SourceType: pubcontract.ReceiptSourcePage,
		Actions:    recoverableReceiptActions,
	})
	if err != nil {
		return 0, time.Time{}, err
	}
	if unix := s.lastConvergeAt.Load(); unix > 0 {
		lastConvergeAt = time.Unix(unix, 0).UTC()
	}
	return pending, lastConvergeAt, nil
}

// PendingReceiptBacklog 只读观测：待收敛积压的完整形状 —— 条数 + 最老一条已等待多久
// + 本进程最近一次收敛时刻。
//
// 与 PendingReceiptStatus 的分工：那个只给「条数 + 收敛时刻」（三元组形状由既有调用方钉住），
// 而条数看不出积压是不是在增长 —— 收掉 3 条又来 3 条，数字纹丝不动。
// **最老一条的年龄**才是「收敛跟不跟得上」的判据：它只会在真的收不动时持续变大。
//
// 口径与收敛例程严格一致（page + 三种访问面切换动作），数出来的就是「本实例会去收、
// 且还没收掉的行」；路由变更回执与 presentation 的回执不归这里（各自的处理方负责）。
//
// 两个查询不共用事务：观测允许读到中间态（收敛正在跑时条数可能少一条、年龄刚被清零），
// 这不影响「积压是否在增长」的判断。
func (s *Service) PendingReceiptBacklog(ctx context.Context) (pending int64, oldestAge time.Duration, lastConvergeAt time.Time, err error) {
	if s == nil || s.routes == nil {
		return 0, 0, time.Time{}, nil
	}
	pending, err = s.routes.CountPendingReceipts(ctx, &pubcontract.ReceiptsQueryReq{
		SourceType: pubcontract.ReceiptSourcePage,
		Actions:    recoverableReceiptActions,
	})
	if err != nil {
		return 0, 0, time.Time{}, err
	}
	if unix := s.lastConvergeAt.Load(); unix > 0 {
		lastConvergeAt = time.Unix(unix, 0).UTC()
	}
	if pending <= 0 {
		return 0, 0, lastConvergeAt, nil
	}
	return pending, s.oldestPendingReceiptAge(ctx), lastConvergeAt, nil
}

// oldestPendingReceiptAge 本模块口径里最老一条 pending 回执已等待的时长；读不到时返回 0。
//
// 走 ListPendingReceipts（按 create_time ASC 的**只读**列举）：契约里唯一一条不加锁的列举能力。
// 不能用 ClaimPendingReceipts —— 它用 FOR UPDATE SKIP LOCKED **认领**行，健康检查 / 列表页
// 走那条路等于让观测动作把待收敛的行锁住再放回去，直接干扰收敛（多实例下还会把行从
// 别的实例的批次里抢走）。
//
// 代价：只在条数不为 0 时调用，且这一次列举会把全部 pending 行拉回来。正常积压是个位数；
// 真积压成千上万条时，多这一趟读取相比「积压完全不可见」仍是划算的。
// 时间戳解析失败按「读不到」处理 —— 年龄是附加信息，不该连条数一起丢掉。
func (s *Service) oldestPendingReceiptAge(ctx context.Context) time.Duration {
	items, err := s.routes.ListPendingReceipts(ctx)
	if err != nil {
		logger.Scene("publication").With("err", err).
			Warn("读取最老未结案回执失败（积压条数仍可用，年龄暂缺）")
		return 0
	}
	for _, item := range items {
		// 列表按 create_time ASC：第一条命中本模块口径的就是最老的一条。
		if item.SourceType != pubcontract.ReceiptSourcePage || !recoverableReceiptAction(item.Action) {
			continue
		}
		ts, perr := time.Parse(time.RFC3339, item.CreatedAt)
		if perr != nil || ts.IsZero() {
			return 0
		}
		if age := time.Since(ts); age > 0 {
			return age
		}
		return 0
	}
	return 0
}

// StartPendingReceiptConvergenceScheduler 启动发布回执收敛调度（进程内 goroutine + ticker）。
//
// 形状与 page_retention / order_expire / build_worker 的既有调度一致：先跑一次，再等间隔。
// 两个信号源由 select 合并：
//   - ticker：兜底（快通道信号丢失，或残留是别的实例留下的）；
//   - convergeWake：写路径在事务提交后推的进程内快通道，正常路径毫秒级收敛。
//
// 启动首跑直接做全量恢复（不限批 + 5 分钟预算）—— 它取代了原先装配处的裸启动恢复。
// 不能留着那个入口：同一段实现若有两个「启动时跑一次」的驱动源，启动瞬间会有两个
// goroutine 并发重放同一批回执（重放虽幂等，仍会白白多跑一遍、多占一次连接）。
func StartPendingReceiptConvergenceScheduler(svc *Service) {
	if utils.IsTestProcess() {
		return // 测试进程不启动：调度首跑会动真实库与存储，测试的行为必须由用例自己触发（见 utils.IsTestProcess）。
	}
	startPendingReceiptConvergenceScheduler(svc, pendingReceiptConvergeInterval)
}

// StartPendingReceiptConvergenceSchedulerWithInterval 同上，但可注入间隔。
//
// 给用例用：注入一个远大于用例时长的间隔，就能把「收敛确实由快通道驱动、
// 而不是定时器顺手做掉的」证成（见 public/test/page/unit/page_receipt_converge_test.go）。
func StartPendingReceiptConvergenceSchedulerWithInterval(svc *Service, interval time.Duration) {
	startPendingReceiptConvergenceScheduler(svc, interval)
}

func startPendingReceiptConvergenceScheduler(svc *Service, interval time.Duration) {
	if svc == nil {
		return
	}
	if interval <= 0 {
		interval = pendingReceiptConvergeInterval
	}
	go func() {
		// 启动首跑：全量收一次上次进程留下的残留。
		startupCtx, cancelStartup := context.WithTimeout(context.Background(), pendingReceiptStartupTimeout)
		_, _, _ = svc.RecoverPendingPublications(startupCtx)
		cancelStartup()

		converge := func() {
			ctx, cancel := context.WithTimeout(context.Background(), pendingReceiptConvergeTimeout)
			defer cancel()
			_, _ = svc.ConvergePendingReceipts(ctx)
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				converge()
			case <-svc.convergeWake:
				converge()
			}
		}
	}()
}

// maxAutoRebuildPages 单次依赖失效触发的自动重建上限。
//
// 为什么需要上限：自动重建发生在内容写入的请求内（PIPE-2 构建队列尚未落地），
// 无界重建会让一次内容保存耗时随站点规模线性增长。超限的页面交给构建队列
// （enqueueOverflowBuildJobs），由它的编排按同一套语义重建 —— 不是丢弃。
const maxAutoRebuildPages = 20

// pageRebuildPlan 一次单页重建的冻结上下文（编排的唯一输入）。
//
// 「冻结」的含义：三样东西在**进入编排之前**一次性确定，编排内部不再重新推导 ——
// 推导两次就可能拿到两份（两次读之间站点语言表 / 发布台账变了），
// 而这次的构建与回写必须落在同一份事实上。
type pageRebuildPlan struct {
	// Page 已定位（含工程作用域）的页面记录。
	Page *pagemodel.PageEntity
	// Langs 本次要构建的语言集合（默认语言在前）。
	Langs []string
	// PublishLangs 旧发布范围：构建成功后允许回写线上的语言（Langs 的子集）。
	// 为空表示「这次不做任何回写」——手工构建与从未发布过的页面都走这条。
	PublishLangs []string
	// DraftVersion / InputHash 计划生成时冻结的输入版本。
	DraftVersion int64
	InputHash    string
	// Intent 构建意图（pagecontract.BuildIntent*）。
	Intent string
}

// publishes 该语言是否在旧发布范围内（构建成功后要重新发布）。
func (p *pageRebuildPlan) publishes(lang string) bool {
	if p == nil {
		return false
	}
	for _, l := range p.PublishLangs {
		if l == lang {
			return true
		}
	}
	return false
}

// normalizeRebuildIntent 归一化构建意图：空 = 依赖重建（与 build_jobs.intent 的默认值同口径）。
//
// 白名单外的值也归到依赖重建：这个值只影响「构建完要不要回写线上」，而回写又只发生在
// 旧发布范围内（此前已发布的语言）。未知意图被当成依赖重建最多是「按已发布语言刷新一次」，
// 当成手工构建却会让本该上线的语言停在旧字节 —— 默认值必须站在更安全的一侧。
func normalizeRebuildIntent(raw string) string {
	if strings.TrimSpace(raw) == pagecontract.BuildIntentManual {
		return pagecontract.BuildIntentManual
	}
	return pagecontract.BuildIntentDependency
}

// planPageRebuild 组装单页重建计划。
//
// langs 为空时按**发布口径**解析站点启用语言（读不到即返回错误，不降级为默认语言一种）；
// 非空时用它（队列任务的语言在入队时就冻结了，这里不再读一次）。
//
// 旧发布范围只在 dependency 意图下解析：manual 是「人工点一次构建」，它不该顺手把页面
// 重新上线 —— 回写线上是发布动作，必须由人显式发起。
func (s *Service) planPageRebuild(ctx context.Context, page *pagemodel.PageEntity, langs []string, intent string) (*pageRebuildPlan, error) {
	if page == nil {
		return nil, ErrPageNotFound
	}
	plan := &pageRebuildPlan{
		Page: page, Intent: normalizeRebuildIntent(intent),
		DraftVersion: page.DraftVersion, InputHash: hash(page.DraftDocument),
	}
	if len(langs) == 0 {
		resolved, err := s.publishLangsOf(ctx, page.ProjectID)
		if err != nil {
			return nil, err
		}
		langs = resolved
	}
	plan.Langs = langs
	if plan.Intent == pagecontract.BuildIntentManual {
		return plan, nil
	}
	// 旧发布范围：此前已发布的语言（page_publications 为真源）。
	//
	// 一次性解析而不是在构建循环里逐语言读：循环里读到的集合可能中途变化，
	// 而本次构建与回写要落在同一份事实上。读不到即整体失败（调用方保持 stale）——
	// 「读不到」与「没发布过」在访问面上的差别是「站点停更」与「不上线」，
	// 拿不准就宁可不动，也不能用猜出来的范围去回写线上。
	for _, lang := range langs {
		path, err := s.publishedPathOf(ctx, page, lang)
		if err != nil {
			return nil, fmt.Errorf("读取语言 %s 的发布状态失败: %w", lang, err)
		}
		if path != "" {
			plan.PublishLangs = append(plan.PublishLangs, lang)
		}
	}
	return plan, nil
}

// RebuildPage 按计划执行单页重建（同步路径与队列 worker 共用的**唯一**实现）。
//
// 逐语言：构建；构建成功后，若该语言在旧发布范围内则发布（重新发布此前已发布的语言，
// 从未发布过的语言只留在暂存态 —— 「CMS 变更自动发布」的落地口径）。
//
// 单语言失败不阻断其余语言（继续下一个，错误累积后一并返回）：
//   - 同步路径的调用方是内容写入的后置副作用，它按「尽力而为」处理并把错误记进日志，
//     页面保持 stale 等待收敛；
//   - 队列 worker 的调用方是执行器，它按错误把任务标 failed（失败准确反映到任务上）。
func (s *Service) rebuildPage(ctx context.Context, plan *pageRebuildPlan) (rebuilt, published int, err error) {
	if plan == nil || plan.Page == nil {
		return 0, 0, ErrInvalidParam
	}
	manual := plan.Intent == pagecontract.BuildIntentManual
	var errs []error
	for _, lang := range plan.Langs {
		if cerr := ctx.Err(); cerr != nil {
			errs = append(errs, cerr)
			break
		}
		if _, berr := s.Build(ctx, &pagedto.BuildReq{ID: plan.Page.ID, Lang: lang}); berr != nil {
			errs = append(errs, fmt.Errorf("语言 %s 构建失败: %w", lang, berr))
			continue
		}
		rebuilt++
		if manual || !plan.publishes(lang) {
			continue
		}
		// 走 publish(..., false)：**不做互指刷新**（审计 I18N-01 续）。本循环自己会逐个
		// 重建并重新发布全部相关语言，每一轮都能看到完整发布面；在这里再触发一次刷新，
		// 等于让「自动重建」多出一层不受调用方控制的发布动作。
		if _, perr := s.publish(ctx, &pagedto.PublishReq{ID: plan.Page.ID, Lang: lang}, false); perr != nil {
			errs = append(errs, fmt.Errorf("语言 %s 重新发布失败: %w", lang, perr))
			continue
		}
		published++
	}
	return rebuilt, published, errors.Join(errs...)
}

// RunPageBuildJob 执行一条构建队列任务（执行器执行体，实现 pagecontract.PageService）。
//
// 任务行的三样上下文（lang / intent / 输入版本）在这里还原成计划：
//   - lang 非空：本条任务只负责该语言（依赖重建在入队时按语言拆成了多行，见迁移 307）；
//   - lang 为空：只可能是 ARCH-04 之前入队的存量行，按**站点启用语言集合**处理 ——
//     与同步路径逐条一致，而不是把空 lang 当成「默认语言一种」（那正是本条审计要消灭的
//     「停在默认语言暂存态」）；
//   - intent=manual：只构建，不回写线上。
//
// 输入版本（DraftVersion / BuildInputHash）在这里**不作为跳过条件**：依赖重建的语义是
// 「这一页的产物可能过期了」，重建永远以当前草稿为准（确定性构建使同输入产出同字节）；
// 若冻结版本落后于当前草稿，记一条日志说明本次实际构建的是更新后的草稿 ——
// 这是如实记录，不改变结果。用版本差异跳过会让「草稿变了但没再扇出」的页面永远停在旧字节。
func (s *Service) RunPageBuildJob(ctx context.Context, req *pagedto.PageBuildJobReq) error {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return ErrInvalidParam
	}
	page, err := s.getExistingPage(ctx, req.ID)
	if err != nil {
		// 页面不存在 / 已删除：任务失败（原因写进 error_message），不静默算成功。
		return err
	}
	intent := normalizeRebuildIntent(req.Intent)
	var langs []string
	if lang := strings.TrimSpace(req.Lang); lang != "" {
		langs = []string{lang}
	} else if intent == pagecontract.BuildIntentManual {
		// 手工构建没有语言上下文时按 BuildReq 的既有口径：站点默认语言一种。
		langs = []string{buildLang("")}
	}
	// intent=dependency 且 lang 为空（存量任务）时 langs 留空，交给计划按站点语言集合解析。
	plan, perr := s.planPageRebuild(ctx, page, langs, intent)
	if perr != nil {
		s.markRebuildFailure(ctx, page, pageRebuildStagePlan)
		return perr
	}
	if req.DraftVersion > 0 && req.DraftVersion != page.DraftVersion {
		logger.Scene("build").With("pageId", page.ID).With("lang", req.Lang).
			With("frozenDraftVersion", req.DraftVersion).With("currentDraftVersion", page.DraftVersion).
			Info("构建任务冻结的草稿版本已落后于当前草稿，本次按当前草稿重建（依赖重建以最新草稿为准）")
	}
	rebuilt, published, rerr := s.rebuildPage(ctx, plan)
	if rerr != nil {
		logger.Scene("build").With("pageId", page.ID).With("lang", req.Lang).With("intent", plan.Intent).
			With("rebuilt", rebuilt).With("published", published).Error(rerr, "构建任务执行失败（页面保持 stale）")
		s.markRebuildFailure(ctx, page, pageRebuildStageBuild)
		return rerr
	}
	// 成功**不**清失败痕迹：队列任务按语言拆行（迁移 307），一条任务成功不等于整页恢复 ——
	// 若另一语言的同类任务还在失败，清了痕迹会让界面上那个失败消失，而页面其实仍然落后。
	// 清空只由「整页语义」的重建负责（RebuildStale 的逐页循环）。这是刻意的偏差方向：
	// 宁可让痕迹多留一会儿（界面会带上失败时刻，读的人自己看得旧），也不谎报恢复。
	logger.Scene("build").With("pageId", page.ID).With("lang", req.Lang).With("intent", plan.Intent).
		With("rebuilt", rebuilt).With("published", published).Info("构建任务执行完成")
	return nil
}

// SetBuildQueue 注入构建队列端口（装配期调用）。
//
// 未注入时 enqueueOverflowBuildJobs 会退回「记告警、保持 stale」的既有行为 ——
// 不静默丢弃，也不假装已经排上了。
func (s *Service) SetBuildQueue(q pagecontract.BuildQueueEnqueuer) {
	if s == nil {
		return
	}
	s.buildQueue = q
}

// enqueueOverflowBuildJobs 把超出单次同步重建上限的页面交给构建队列。
//
// 逐页逐语言入队（审计 ARCH-04）：语言集合在**入队时冻结** —— 每条任务负责一种语言，
// 消费侧因此不再需要（也不应该）重新解析站点语言表。队列的待办去重键含 lang
// （迁移 307），所以同一页面的两种语言是两条待办，不会互相去重。
//
// 输入版本：draft_version 与草稿文档摘要一起进任务行，作为去重键与审计依据
// （不再用空 hash 充当「这是依赖重建」的隐式操作码 —— 意图由 intent 列显式表达）。
//
// 单个页面入队失败只记日志：这是一条尽力而为的旁路（同步那部分已经重建完了），
// 抛错会让调用方误以为整批失败。
func (s *Service) enqueueOverflowBuildJobs(ctx context.Context, ids []string) {
	if len(ids) == 0 {
		return
	}
	if s.buildQueue == nil {
		logger.Scene("dependency").With("affected", len(ids)).
			Warn("自动重建超出单次上限且构建队列未接入，剩余页面保持 stale 等待后续触发")
		return
	}
	queued := 0
	for _, id := range ids {
		// 同 RebuildStale：入队前也要按工程作用域读一次页面（漏作用域时整批任务静默不再入队）。
		page, err := s.locatePageInProjects(ctx, id)
		if err != nil {
			continue
		}
		langs, lerr := s.publishLangsOf(ctx, page.ProjectID)
		if lerr != nil {
			logger.Scene("dependency").With("page_id", id).
				Error(lerr, "超限重建入队跳过：站点语言清单不可读，入队默认语言一种会让其余语言永久停在旧字节")
			continue
		}
		inputHash := hash(page.DraftDocument)
		for _, lang := range langs {
			if qerr := s.buildQueue.EnqueuePageBuild(ctx, id, page.ProjectID, lang,
				pagecontract.BuildIntentDependency, page.DraftVersion, inputHash); qerr != nil {
				logger.Scene("dependency").With("page_id", id).With("lang", lang).
					Error(qerr, "超限重建任务入队失败")
				continue
			}
			queued++
		}
	}
	logger.Scene("dependency").With("queued", queued).With("affected", len(ids)).
		Info("超限的自动重建已交给构建队列")
}

// RebuildArtifact 用 DB 冻结的 source_document 重新编译并落盘，恢复丢失的产物文件。
//
// 语义（灾难恢复，不是重新发布）：
//   - 只重建文件；不激活 URL、不改 DB 指针、不建重定向；
//   - 重建后**必须**校验 hash。产物 hash = SHA256(manifestJSON + "\n" + indexHTML)，
//     HTML 取决于「源文档 + 组件注册表 + 编译期依赖内容（CMS/主题/导航）」。
//     只有这些输入全部未变才得到同一个 hash；任一变化（典型是组件更新）会产出
//     另一个版本，此时返回不一致详情而**不冒充**旧产物，交由调用方决定走正常发布。
func (s *Service) RebuildArtifact(ctx context.Context, req *pagedto.RebuildArtifactReq) (res *pagedto.RebuildArtifactResp, err error) {
	if req == nil || strings.TrimSpace(req.ArtifactID) == "" {
		return nil, ErrInvalidParam
	}
	art, err := s.artifacts.DetailByID(ctx, &artifactcontract.DetailByIDReq{ID: req.ArtifactID})
	if err != nil {
		return nil, err
	}
	if len(art.SourceDocument) == 0 {
		return nil, fmt.Errorf("产物 %s 缺少冻结源文档，无法重建", art.ID)
	}
	res = &pagedto.RebuildArtifactResp{
		ArtifactID:   art.ID,
		Lang:         art.Lang,
		Path:         art.CanonicalPath,
		ExpectedHash: art.ArtifactHash,
	}

	// 幂等短路：文件已在且校验通过，不重复编译。
	if verr := s.publisher.ArtifactExists(art.ArtifactHash); verr == nil {
		res.Restored, res.HashMatched, res.AlreadyThere = true, true, true
		res.ActualHash = art.ArtifactHash
		return res, nil
	}

	// 站点语言输入取**产物自己记录的那一份**（Manifest.siteLangs / siteDefaultLang，
	// 审计 I18N-01）：这里要原样复现这一份产物，凡是按当前配置重算的输入都会让它
	// 复现不出原字节 —— 而 hreflang 正是其中最隐蔽的一项（路径没变、链接变了）。
	// 取不到（该字段引入之前的存量产物）时为 nil，退回现场解析的既有行为。
	rebuilt, rerr := s.publisher.RestoreArtifact(ctx, pipeline.BuildInput{
		PageID:  art.PageID,
		Lang:    art.Lang,
		Path:    art.CanonicalPath,
		DocJSON: art.SourceDocument,
		Plan:    publicationPlanFromManifest(art.Manifest),
	})
	if rerr != nil {
		res.Reason = "重新编译失败: " + rerr.Error()
		return res, fmt.Errorf("重建产物失败: %w", rerr)
	}
	res.ActualHash = rebuilt.Hash
	if rebuilt.Hash != art.ArtifactHash {
		res.Reason = fmt.Sprintf(
			"重建产物 hash 与元数据不一致（期望 %s，实际 %s）：构建输入已变化 —— "+
				"典型原因是组件注册表更新，或编译期依赖内容（CMS/主题/导航）变动。"+
				"该产物无法原样恢复；如需上线新版本请走正常发布流程（build + publish）。",
			art.ArtifactHash, rebuilt.Hash)
		logger.Scene("page").With("artifactId", art.ID).
			With("expected", art.ArtifactHash).With("actual", rebuilt.Hash).
			Warn("产物重建 hash 不一致（构建输入已变化）")
		return res, nil
	}
	res.Restored, res.HashMatched = true, true
	logger.Scene("page").With("artifactId", art.ID).With("hash", rebuilt.Hash).
		Info("产物文件重建成功（hash 一致）")
	return res, nil
}

// AuditPublication 巡检激活面，返回所有悬空/异常链接。
func (s *Service) AuditPublication(ctx context.Context) (res *pagedto.PublicationAuditResp, err error) {
	issues, checked, aerr := s.publication.AuditActiveLinks()
	if aerr != nil {
		return nil, aerr
	}
	res = &pagedto.PublicationAuditResp{Checked: checked, Healthy: len(issues) == 0}
	res.Issues = make([]pagedto.PublicationIssue, 0, len(issues))
	for _, it := range issues {
		res.Issues = append(res.Issues, pagedto.PublicationIssue{URLPath: it.URLPath, Link: it.Link, Reason: it.Reason})
	}
	// 反向对账（IDX-015）：磁盘上的产物目录是否都有人认领。
	// 失败不影响正向结论 —— 正向异常是「线上已经 404」，必须先给出来。
	orphans, orphanChecked, oerr := s.auditOrphanArtifacts(ctx)
	if oerr != nil {
		logger.Scene("page").Error(oerr, "产物反向对账失败")
	} else {
		res.Orphans = orphans
		res.OrphanChecked = orphanChecked
	}
	if len(issues) > 0 {
		logger.Scene("page").With("count", len(issues)).Warn("激活面巡检发现异常链接")
	}
	return res, nil
}

// defaultArtifactRetentionDays 默认保留窗口：30 天内的产物一律不回收（回滚窗口）。
const defaultArtifactRetentionDays = 30

// GarbageCollectArtifacts 回收超出保留窗口且不再被任何指针引用的产物文件。
//
// 保护集合（任一命中即绝不回收）：
//   - pages.active_artifact_id / pages.staged_artifact_id
//   - page_publications.artifact_id（每语言激活真源）、page_stagings.artifact_id
//   - page_routes.artifact_id（访问面实际指向）
//
// 内容寻址去重：同一 hash 的物理文件可能被多条元数据行引用 —— 只有当没有任何其他
// available 行引用该 hash 时才删文件，否则只把本行标为 gc_pending（表示「想回收但
// 被共享占用」）。
//
// 可回滚性：删除的是物理文件，page_artifacts.source_document 始终保留 ——
// 需要时可经 POST /api/page/artifact/rebuild 重建（hash 一致则完美恢复）。
func (s *Service) GarbageCollectArtifacts(ctx context.Context, req *pagedto.GCArtifactsReq) (res *pagedto.GCArtifactsResp, err error) {
	retention, dryRun := defaultArtifactRetentionDays, true
	if req != nil {
		if req.RetentionDays > 0 {
			retention = req.RetentionDays
		}
		if req.DryRun != nil {
			dryRun = *req.DryRun
		}
	}
	before := time.Now().UTC().AddDate(0, 0, -retention)
	res = &pagedto.GCArtifactsResp{RetentionDays: retention, DryRun: dryRun}

	// 共享内容对象（content_objects）的孤儿回收（IDX-016）挂在 defer 上，而不是写在函数末尾：
	// 产物 GC 有多处提前返回（保护集合为空、候选查询失败），写在末尾会被那些路径整段跳过 ——
	// 而内容对象回收自有一套完整的引用判定（闭包投影 + 产物负载状态），不依赖产物的保护集合，
	// 没有理由跟着一起不跑。defer 覆盖全部退出路径，且仍发生在产物删除之后。
	defer func() {
		if res == nil {
			return
		}
		objRes, oerr := s.collectOrphanContentObjects(ctx, retention, dryRun)
		if oerr != nil {
			// 失败不影响产物侧结论：文件已经删了，回收内容对象失败只是表里多留一轮垃圾。
			logger.Scene("artifact").Error(oerr, "回收孤儿内容对象失败")
			return
		}
		if objRes == nil {
			return
		}
		res.OrphanObjects = objRes.Orphans
		res.ObjectsDeleted = objRes.Deleted
		res.ObjectsSkippedExternal = objRes.SkippedExtern
		res.ObjectsFailed = objRes.Failed
	}()

	protected, err := s.model.ListProtectedArtifactIDs(ctx)
	if err != nil {
		return nil, err
	}
	if s.routes != nil {
		refs, rerr := s.routes.ListReferencedArtifactIDs(ctx)
		if rerr != nil {
			return nil, rerr
		}
		protected = append(protected, refs...)
	}
	if len(protected) == 0 {
		// 保护集合为空：查询异常或系统尚未发布任何内容 —— 宁可不回收也不误删。
		return res, nil
	}

	cands, err := s.artifacts.ListGCCandidates(ctx, before, protected)
	if err != nil {
		return nil, err
	}
	res.Scanned = len(cands)
	for _, c := range cands {
		item := pagedto.GCRecoveredArtifact{ID: c.ID, ArtifactHash: c.ArtifactHash, Lang: c.Lang}
		others, cerr := s.artifacts.CountOtherAvailableByHash(ctx, c.ArtifactHash, c.ID)
		if cerr != nil {
			item.Action, item.Reason = "skipped", "同 hash 引用检查失败: "+cerr.Error()
			res.Failed++
			res.Items = append(res.Items, item)
			continue
		}
		if others > 0 {
			item.Action = "kept_shared"
			item.Reason = fmt.Sprintf("同 hash 仍被 %d 条产物行引用，文件保留", others)
			res.SkippedShared++
			if !dryRun {
				_, _ = s.artifacts.MarkPayloadState(ctx, []string{c.ID}, artifactcontract.PayloadStateGCPending)
			}
			res.Items = append(res.Items, item)
			continue
		}
		if dryRun {
			item.Action = "would_delete"
			res.Items = append(res.Items, item)
			continue
		}
		if derr := s.store.DeleteArtifact(pipeline.ArtifactLocator(c.ArtifactHash), c.ArtifactHash); derr != nil {
			item.Action, item.Reason = "delete_failed", derr.Error()
			res.Failed++
			logger.Scene("artifact").With("id", c.ID).With("hash", c.ArtifactHash).Error(derr, "产物文件删除失败")
			res.Items = append(res.Items, item)
			continue
		}
		if _, merr := s.artifacts.MarkPayloadState(ctx, []string{c.ID}, artifactcontract.PayloadStateDeleted); merr != nil {
			item.Action, item.Reason = "state_failed", "文件已删但状态未更新: "+merr.Error()
			res.Failed++
		} else {
			item.Action = "deleted"
			res.Deleted++
		}
		res.Items = append(res.Items, item)
	}
	logger.Scene("artifact").With("scanned", res.Scanned).With("deleted", res.Deleted).
		With("skippedShared", res.SkippedShared).With("dryRun", dryRun).Info("产物回收完成")
	return res, nil
}

// collectOrphanContentObjects 调用 artifact 模块做内容对象孤儿回收。
//
// 保留窗口与 dryRun 都与产物 GC 同值：两个 GC 的语义必须一致，否则会出现
// 「产物 dryRun 预演、内容对象真删」这种把风险藏起来的组合。
func (s *Service) collectOrphanContentObjects(ctx context.Context, retention int, dryRun bool) (res *artifactcontract.ContentObjectGCResp, err error) {
	if s.artifacts == nil {
		return nil, nil
	}
	return s.artifacts.GarbageCollectContentObjects(ctx, &artifactcontract.ContentObjectGCReq{
		RetentionDays: retention, DryRun: &dryRun,
	})
}

// seoLangProbePath 反推语言前缀用的探针路径。
//
// 为什么要探针而不是直接拼 "/" + 语言码：前缀由 pipeline.LangURLRule.Path 单点决定
// （默认语言可能不带前缀、语言码可能映射成短码、首页还会变成 /index），
// 自己拼一份等于把那条规则抄第二遍 —— 抄错的表现是「校验说路径不符，但线上是对的」。
const seoLangProbePath = "/__seo_lang_probe__"

// seoLangs 站点启用语言的 URL 形状（不足两种语言时返回 nil：单语言站点没有前缀概念）。
func (s *Service) seoLangs(ctx context.Context, projectID string) []compliance.LangRule {
	if s == nil || s.project == nil || strings.TrimSpace(projectID) == "" {
		return nil
	}
	langs := pipeline.EnabledLangs(ctx, s.project, projectID)
	if len(langs) < 2 {
		return nil
	}
	return langRulesOf(pipeline.LangURLRuleForProject(ctx, s.project, projectID), langs)
}

// langRulesOf 由语言 URL 规则推导校验器的语言表（纯函数：只依赖规则与语言清单）。
//
// 抽成纯函数是为了可测：语言前缀的推导是这次整改里最容易写错的一处
// （默认语言可能不带前缀、语言码可能映射成短码），而它错了会直接产出错误的
// 「语种 / 路径不一致」结论 —— 那种假警比不校验更糟。
func langRulesOf(rule pipeline.LangURLRule, langs []string) []compliance.LangRule {
	out := make([]compliance.LangRule, 0, len(langs))
	for _, lang := range langs {
		p, err := rule.Path(lang, seoLangProbePath)
		if err != nil {
			// 语言或路径非法：构建期本身就会失败，这里不替它编一个前缀
			//（编错的前缀会让「语种 / 路径一致」这条规则给出错误的结论）。
			continue
		}
		out = append(out, compliance.LangRule{
			Code:   lang,
			Prefix: strings.TrimSuffix(p, seoLangProbePath),
		})
	}
	return out
}

// inspectArtifactSEO 读取产物字节并做确定性校验（只读：不写库、不改产物、不发网络请求）。
//
// sitemapListed 由调用方回答：sitemap 由**已激活路径**生成，因此「这份产物会不会进
// sitemap」= 「它当前是否已激活」（构建期用 pathListedInSitemap 如实判定），
// 而发布路径上它是确定事实（下一步就激活），巡检激活面时恒为 true。
func (s *Service) inspectArtifactSEO(hash, url, lang string, sitemapListed bool, langs []compliance.LangRule) (*compliance.Report, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("产物存储未就绪")
	}
	if strings.TrimSpace(hash) == "" {
		return nil, fmt.Errorf("产物 hash 为空")
	}
	art, err := s.store.GetArtifact(pipeline.ArtifactLocator(hash))
	if err != nil {
		return nil, err
	}
	return compliance.Inspect(compliance.Artifact{
		URL: url, Lang: lang, ArtifactHash: hash,
		HTML: art.Entries["index.html"], Langs: langs, SitemapListed: sitemapListed,
		SiteBaseURL: seo.SiteBaseURL(),
	}), nil
}

// inspectBuiltArtifact 构建 / 发布路径上的构建期校验：命中就记日志，绝不阻断。
//
// sitemapListed 由调用方按**事实**回答（见 pathListedInSitemap）：sitemap 由激活路径
// 生成，所以「这份产物会不会进 sitemap」= 「这个路径此刻是否已激活」。构建期不猜
// 「发布之后大概会进」—— 猜出来的结论就是假警，而假警会让整份报告失去意义。
//
// 失败一律降级：读不到产物字节时只记一条告警 —— 校验是观测手段，
// 不能因为它自己出问题而让一次本来好的构建失败。
//
// 代价说明：这里会再读一次产物（manifest.json + index.html），而 ensureArtifactRow
// 归档时已经读过一次。多这一次是有意的 —— 校验的证据必须是**落盘之后**的字节
// （内存里那份还没经过 NewArtifact/EncodeManifest 的确定性编码），而构建本身是低频
// 重操作，多一次小文件读入不值得为它把归档的返回值改胖。
func (s *Service) inspectBuiltArtifact(ctx context.Context, projectID, hash, url, lang string, sitemapListed bool) {
	langs := s.seoLangs(ctx, projectID)
	rep, err := s.inspectArtifactSEO(hash, url, lang, sitemapListed, langs)
	if err != nil {
		logger.Scene("build").With("hash", hash).With("url", url).Warn("构建期 SEO 合规校验跳过：产物字节不可读")
		return
	}
	logSEOReport("build", rep)
}

// pathListedInSitemap 判断某个访问路径此刻是否已在站点 sitemap 里。
//
// 判定来源是**访问面本身**（激活目录的符号链接），与 sitemap 的生成口径同源
// （publication.RefreshSiteFiles 由已激活路径写 sitemap）—— 查数据库的路由表会得出
// 「已登记 = 已收录」，而登记与激活是两件事。这与语言切换器的发布态判定是同一实现。
func (s *Service) pathListedInSitemap(path string) bool {
	if s == nil || s.publication == nil || strings.TrimSpace(path) == "" {
		return false
	}
	state, err := s.publication.Inspect(path)
	return err == nil && state != nil && state.Kind != "none"
}

// logSEOReport 把结论逐条写进结构化日志（URL / 规则 / 证据 / ArtifactHash 四要素齐全）。
func logSEOReport(scene string, rep *compliance.Report) {
	if rep == nil {
		return
	}
	for _, f := range rep.Findings {
		entry := logger.Scene(scene).
			With("url", f.URL).With("lang", f.Lang).With("rule", f.Rule).
			With("artifactHash", f.ArtifactHash).With("evidence", f.Evidence)
		if f.Level == compliance.LevelError {
			entry.Warn("构建期 SEO 合规校验：产物自相矛盾")
			continue
		}
		entry.Info("构建期 SEO 合规校验：需人工确认")
	}
}

// SEOPatrol 站点 SEO 合规巡检：按激活清单逐份校验，产出可复核的报告。
//
// 数据来源全部在本机：page_routes 的 active 行（谁在线）+ 本地产物存储（在线的是什么字节）。
// 不发网络请求，因此「线上可抓取性」不在本方法的结论范围内 —— 那是报告后半段（抓取校验
// 与站长平台数据）的事。
//
// 未被纳入的路由（缺产物行、自动发布实例的产物归别的模块）在报告的 Unchecked 里如实列出：
// 巡检报告的价值取决于「它漏了什么」同样可见。
func (s *Service) SEOPatrol(ctx context.Context, req *pagedto.SEOPatrolReq) (res *pagedto.SEOPatrolResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, ErrInvalidParam
	}
	projectID := strings.TrimSpace(req.ProjectID)
	langs := s.seoLangs(ctx, projectID)
	routes, err := s.model.ListActiveRoutesByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}

	arts := make([]compliance.Artifact, 0, len(routes))
	res = &pagedto.SEOPatrolResp{
		ProjectID: projectID,
		SampledAt: utils.NewJSONTime(time.Now().UTC()),
	}
	unchecked := func(url, reason string) {
		res.Unchecked = append(res.Unchecked, pagedto.SEOUncheckedRoute{URL: url, Reason: reason})
	}
	for _, rt := range routes {
		url := seo.CanonicalPublicPath(rt.Path)
		if rt.PresentationID != nil && strings.TrimSpace(*rt.PresentationID) != "" {
			// 自动发布实例的产物记在 presentation_artifacts：另一张表、另一个模块的属地，
			// page 模块按表隔离不能读它（跨模块直查才是更严重的问题）。
			unchecked(url, "归属自动发布实例：其产物元数据不属于 page 模块（本轮未纳入）")
			continue
		}
		if rt.ArtifactID == nil || strings.TrimSpace(*rt.ArtifactID) == "" {
			unchecked(url, "激活路由没有指向产物")
			continue
		}
		art, derr := s.artifacts.DetailByID(ctx, &artifactcontract.DetailByIDReq{ID: *rt.ArtifactID})
		if derr != nil || art == nil || art.ArtifactHash == "" {
			unchecked(url, "产物元数据读取失败（产物行不存在或不可读）")
			continue
		}
		loaded, lerr := s.store.GetArtifact(pipeline.ArtifactLocator(art.ArtifactHash))
		if lerr != nil {
			unchecked(url, "产物文件缺失或不可读："+art.ArtifactHash)
			continue
		}
		arts = append(arts, compliance.Artifact{
			URL: url, Lang: art.Lang, ArtifactHash: art.ArtifactHash,
			HTML: loaded.Entries["index.html"], Langs: langs,
			SiteBaseURL:   seo.SiteBaseURL(),
			SitemapListed: true, // 激活清单即 sitemap 的条目来源
		})
	}

	site := compliance.InspectSite(arts)
	res.ChecksPerArtifact = site.Checks
	res.Checked = site.Checked
	res.Healthy = site.Healthy()
	res.Errors = site.ErrorCount()
	res.Warnings = site.WarnCount()
	res.Artifacts = make([]pagedto.SEOArtifactItem, 0, len(site.Reports))
	for _, r := range site.Reports {
		item := pagedto.SEOArtifactItem{
			URL: r.URL, Lang: r.Lang, ArtifactHash: r.ArtifactHash, Checks: r.Checks,
			Index: r.Index.Index, Follow: r.Index.Follow,
			RobotsExplicit: r.Index.Explicit, Robots: r.Index.Raw,
		}
		for _, f := range r.Findings {
			item.Findings = append(item.Findings, toSEOFindingItem(f))
		}
		res.Artifacts = append(res.Artifacts, item)
	}
	for _, f := range site.Findings {
		res.CrossFindings = append(res.CrossFindings, toSEOFindingItem(f))
	}
	if res.Errors > 0 {
		logger.Scene("page").With("projectId", projectID).
			With("checked", res.Checked).With("errors", res.Errors).With("warnings", res.Warnings).
			Warn("SEO 合规巡检发现产物自相矛盾")
	}
	return res, nil
}

// toSEOFindingItem 结论 → DTO（字段一一对应，四要素不丢）。
func toSEOFindingItem(f compliance.Finding) pagedto.SEOFindingItem {
	return pagedto.SEOFindingItem{
		URL: f.URL, Lang: f.Lang, ArtifactHash: f.ArtifactHash,
		Rule: f.Rule, Level: string(f.Level), Evidence: f.Evidence, Expect: f.Expect,
	}
}
