package pageservice

import (
	"context"
	"fmt"
	"strings"
	"time"

	artifactcontract "go_wp/internal/module/artifact/contract"
	pagedto "go_wp/internal/module/page/dto"
	pagemodel "go_wp/internal/module/page/model"
	projectcontract "go_wp/internal/module/project/contract"
	pubcontract "go_wp/internal/module/publication/contract"

	"go_wp/internal/pipeline"
	"go_wp/pkg/logger"

	"gorm.io/gorm"
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
