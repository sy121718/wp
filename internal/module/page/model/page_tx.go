package pagemodel

// page_tx.go — 跨聚合/跨模块事务的透传变体（AGENTS.md「写操作的事务与回滚」）。
//
// 定位：model 不自持事务边界，只提供「在调用方给的事务句柄上执行」的形态。
// service 决定边界（m.TransactionScoped），把 *gorm.DB 依次传给本文件的 *Tx 方法
// 与其它模块的 …Tx 方法（如 publication 的 ActivateTx / RenameReservedTx）。
//
// 两条硬约束：
//  1. 作用域：每写一张带 FORCE 策略的表之前都先 rls.ScopeTx(tx, projectID) ——
//     set_config(..., is_local => true) 在事务内可以反复改，所以「同一个事务里
//     先写 A 工程再写 B 工程」是允许的；但不设作用域在换非超级角色后是静默 0 行，
//     那正是这批 *Tx 方法要避免的失败形态。
//  2. 不接受「不传 tx 的变体」：本文件的方法一律只在外部 tx 上执行，
//     避免调用方以为自己在事务里、实际各写各的。
//
// 与既有非 Tx 方法的关系：非 Tx 方法（SaveDraftWithRevision / MarkPublishedLang …）
// 是「自足入口」（自带事务 + 作用域），保留给单步调用；两者共享同一段 SQL，
// 逻辑改动必须同时落两处（本文件的实现即从非 Tx 版本抽出，逐字一致）。

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/pkg/rls"
)

// TransactionScoped 开启事务并设置工程作用域，供 service 编排跨聚合/跨模块的写。
//
// service 侧不再直接碰 rls：作用域的设置与事务边界是同一件事（set_config 只在事务内
// 有效），放在这里可以避免「开了事务忘了设作用域」——那种写法在换非超级角色后
// 每条语句都匹配 0 行且不报错。
func (m *Model) TransactionScoped(ctx context.Context, projectID string, fn func(tx *gorm.DB) error) error {
	return m.Transaction(ctx, func(tx *gorm.DB) error {
		if err := rls.ScopeTx(tx, projectID); err != nil {
			return err
		}
		return fn(tx)
	})
}

// CreateWithRevisionTx 在外部事务内创建 Page 与初始 Revision。
func (m *Model) CreateWithRevisionTx(ctx context.Context, tx *gorm.DB, page *PageEntity, revision *RevisionEntity) error {
	if err := rls.ScopeTx(tx, page.ProjectID); err != nil {
		return err
	}
	if err := tx.WithContext(ctx).Create(page).Error; err != nil {
		return err
	}
	return tx.WithContext(ctx).Create(revision).Error
}

// SaveDraftWithRevisionTx 在外部事务内用乐观锁保存草稿与修订。
//
// 返回值语义与 SaveDraftWithRevision 一致：未命中当前版本返回 ErrDraftVersionConflict，
// 由外层回滚（此时调用方在同一事务里做的路径占用迁移也会一并撤销 —— 这正是
// 「先写 A 再补偿 B」做不到的事）。
func (m *Model) SaveDraftWithRevisionTx(
	ctx context.Context,
	tx *gorm.DB,
	projectID string,
	pageID string,
	expectedVersion int64,
	path string,
	document json.RawMessage,
	nextVersion int64,
	updatedAt time.Time,
	revision *RevisionEntity,
) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	result := tx.WithContext(ctx).Model(&PageEntity{}).
		Where("id = ? AND project_id = ? AND deleted_at IS NULL AND draft_version = ?", pageID, projectID, expectedVersion).
		Updates(draftSaveUpdates(path, document, nextVersion, updatedAt))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrDraftVersionConflict
	}
	return tx.WithContext(ctx).Create(revision).Error
}

// MarkStaleForI18nTx 在外部事务内把该工程全部未删除页面标记为待重建，返回真正命中的页面 ID。
//
// 与 MarkStaleForI18n 共用同一段 SQL（markStaleForI18nIn），差别只有事务边界：
// service 的 MarkStaleForI18n 要把「pages 标记」与跨模块 peer（其它发布来源，如自动发布
// 实例）的标记放进**同一个事务** —— 同库跨模块的写必须同进同出（AGENTS.md「写操作的事务
// 与回滚」），否则 peer 失败时会留下「页面已标、实例未标」的半截状态，而那一侧没有任何
// 自动补的入口。作用域仍在这里设：set_config(..., is_local => true) 在事务内可重复设置，
// 外层设过也不冲突（见 pkg/rls.ScopeTx 的分工说明）。
func (m *Model) MarkStaleForI18nTx(ctx context.Context, tx *gorm.DB, projectID string, at time.Time) (ids []string, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if err = rls.ScopeTx(tx, projectID); err != nil {
		return nil, err
	}
	return m.markStaleForI18nIn(ctx, tx, projectID, at)
}

// DeletePublicationsByLangTx 在外部事务内删除某页面某语言的发布指针（幂等）。
//
// 与 DeletePublicationsByLang 同一段 SQL（只按 page_id + lang 删，绝不整页删 ——
// 其它语言的指针必须留着）。语义差别是与 publication 的 DeactivateTx 落在同一事务里：
// 「路径占用解除 + 发布指针删除」是一次退役动作的两半，各自提交会留下
// 「页面已退役但路径仍占用」或反过来。作用域在此显式设置：page_publications 带 FORCE 策略。
func (m *Model) DeletePublicationsByLangTx(ctx context.Context, tx *gorm.DB, projectID, pageID, lang string) (err error) {
	if strings.TrimSpace(projectID) == "" {
		return ErrProjectRequired
	}
	if err = rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return tx.WithContext(ctx).Model(&PublicationEntity{}).
		Where("page_id = ? AND lang = ?", pageID, lang).Delete(&PublicationEntity{}).Error
}

// DeleteStagingsByLangTx 在外部事务内删除某页面某语言的暂存指针（幂等，理由同上）。
func (m *Model) DeleteStagingsByLangTx(ctx context.Context, tx *gorm.DB, projectID, pageID, lang string) (err error) {
	if strings.TrimSpace(projectID) == "" {
		return ErrProjectRequired
	}
	if err = rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return tx.WithContext(ctx).Model(&StagingEntity{}).
		Where("page_id = ? AND lang = ?", pageID, lang).Delete(&StagingEntity{}).Error
}

// MoveDraftPathTx 在外部事务内同步草稿路径（逻辑路径，不含语言前缀）。
func (m *Model) MoveDraftPathTx(ctx context.Context, tx *gorm.DB, projectID, pageID, newPath string, at time.Time) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return tx.WithContext(ctx).Model(&PageEntity{}).
		Where("id = ? AND project_id = ? AND deleted_at IS NULL", pageID, projectID).
		Updates(mergeInputChange(map[string]any{"draft_path": newPath}, at)).Error
}

// MarkPublishedLangTx 在外部事务内记录某语言激活状态（page_publications upsert + pages 镜像）。
func (m *Model) MarkPublishedLangTx(ctx context.Context, tx *gorm.DB, projectID string, rec PublicationRecord) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	row := &PublicationEntity{
		PageID: rec.PageID, Lang: rec.Lang, ActivePath: rec.ActivePath,
		ArtifactHash: rec.ArtifactHash, PublishedAt: rec.PublishedAt, UpdatedAt: rec.PublishedAt,
		ReceiptID: rec.receiptIDColumn(),
	}
	if rec.ArtifactID != "" {
		id := rec.ArtifactID
		row.ArtifactID = &id
	}
	if cerr := tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "page_id"}, {Name: "lang"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"active_path", "artifact_id", "artifact_hash", "published_at", "update_time", "receipt_id",
		}),
	}).Create(row).Error; cerr != nil {
		return cerr
	}
	return tx.WithContext(ctx).Model(&PageEntity{}).Where("id = ? AND deleted_at IS NULL", rec.PageID).
		Updates(map[string]any{
			"active_artifact_id": rec.ArtifactID,
			"active_path":        rec.ActivePath,
			"published_at":       rec.PublishedAt,
			"stale":              false,
			"update_time":        rec.PublishedAt,
		}).Error
}

// MarkStagedLangTx 在外部事务内回写某语言的暂存产物指针（page_stagings upsert + pages 镜像）。
//
// **不写 stale=false**（裁定 §4.3）：构建不等于发布。此前这里顺手把 stale 清掉，
// 于是「构建成功、激活失败」会留下一个「产物已过期但页面看起来干净」的状态，
// 而触发那次构建的输入变更再也不会有第二次机会把它重建成新产物。
// stale 只由**成功激活**（MarkPublishedLangTx）清。
func (m *Model) MarkStagedLangTx(ctx context.Context, tx *gorm.DB, projectID, pageID, lang, artifactID, artifactHash string, draftVersion int64, at time.Time) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	row := &StagingEntity{
		PageID: pageID, Lang: lang, ArtifactID: artifactID,
		ArtifactHash: artifactHash, DraftVersion: draftVersion, UpdatedAt: at,
	}
	if cerr := tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "page_id"}, {Name: "lang"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"artifact_id", "artifact_hash", "draft_version", "update_time",
		}),
	}).Create(row).Error; cerr != nil {
		return cerr
	}
	return tx.WithContext(ctx).Model(&PageEntity{}).Where("id = ? AND deleted_at IS NULL", pageID).
		Updates(map[string]any{"staged_artifact_id": artifactID, "update_time": at}).Error
}

// ReplaceDependenciesTx 在外部事务内全量替换某产物的依赖记录（delete + insert）。
func (m *Model) ReplaceDependenciesTx(ctx context.Context, tx *gorm.DB, projectID, pageID, artifactID string, rows []DependencyEntity) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	if err := m.requirePageOwned(ctx, tx, projectID, pageID); err != nil {
		return err
	}
	if derr := tx.WithContext(ctx).Where("artifact_id = ?", artifactID).Delete(&DependencyEntity{}).Error; derr != nil {
		return derr
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.WithContext(ctx).CreateInBatches(rows, 200).Error
}

// SoftDeleteTx 在外部事务内软删页面（含同聚合的 page_publications / page_stagings 清理）。
func (m *Model) SoftDeleteTx(ctx context.Context, tx *gorm.DB, projectID, pageID string, at time.Time) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return m.softDeleteTx(ctx, tx, projectID, pageID, at)
}

// MarkStaleByDependencyTx 在外部事务内按依赖源 (kind,key) 精确标记指定工程的受影响页面。
func (m *Model) MarkStaleByDependencyTx(ctx context.Context, tx *gorm.DB, projectID, kind, key string, at time.Time) (ids []string, err error) {
	if kind == "" || key == "" {
		return nil, nil
	}
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return nil, err
	}
	return markStaleByDependencyIn(ctx, tx, projectID, kind, key, at)
}

// ============ 激活判定（发布线性化点） ============
//
// 为什么判定必须拆成「锁读 + 写」两个方法而不是一个带 fs 回调的方法：
// model 只碰本模块的表（AGENTS.md 分层判据）。把文件系统切换塞进 model，
// 事务边界与 IO 就一起跨进了数据访问层 —— 之后任何人在 model 里加一句 SQL 都会
// 落在「已 rename、还没提交」的窗口里，而那个窗口的正确性由 service 的编排顺序决定。
// 因此：service 开事务 → LockPageForActivationTx 锁读 → publisher 切 FS → WritePageActivationTx 写库 → 提交。
// 行锁由第一条语句持有到事务结束，跨过中间那次 FS 调用仍然有效（同一 tx、同一连接）。

// depKindMenu 菜单依赖的 kind（与 pipeline.DepKindMenu 同值）。
//
// 为什么不直接 import pipeline 的常量：model 不 import 发布内核（AGENTS.md 分层判据，
// 且 check-service-db-boundary / 模块边界门禁要求 model 只碰本模块表）。字面量在这里
// 出现一次，值由 pipeline 侧的常量钉住（两侧不一致时契约用例会红，见 page_tx_inputversion_test.go）。
const depKindMenu = "menu"

// depKindNavigation 按**菜单项**引用的依赖 kind（与 pipeline.DepKindNavigation 同值）。
// 与 depKindMenu 同样是字面量镜像：model 不 import 发布内核，两侧一致性由键的拼法
// （navigationDependencyKey）与 pipeline.NavigationKey 逐字对齐来保证。
const depKindNavigation = "navigation"

// menuDependencyKey 菜单依赖键，形如 "menu:{projectID}:{kind}"。
//
// 与 pipeline.MenuKey 逐字一致（构建期登记用的是那一个），两侧不一致时的表现是
// 「标 stale 恒 0 行且不报错」—— 因此重合由真实依赖键夹具守着
// （page_input_version_model_test.go 用 pipeline.MenuKey 产出的键插依赖行）。
// 本函数是本模块内拼这个键的唯一一处。
func menuDependencyKey(projectID, kind string) string {
	return "menu:" + projectID + ":" + kind
}

// navigationDependencyKey **按菜单项**引用的依赖键，形如 "navigation:{itemID}"。
//
// 与 pipeline.NavigationKey 逐字一致（构建期按项引用时登记的是那一个）。按项键没有工程
// 分量：菜单项 id 是 uuid，全局唯一；键的拼法同样只在本文件出现一次。
func navigationDependencyKey(navigationID string) string {
	return "navigation:" + navigationID
}

// PageActivationSnapshot 激活判定的只读事实（在调用方事务内锁定读到的那一份）。
type PageActivationSnapshot struct {
	PageID string
	// InputVersion 锁内读到的当前输入代次；与编译时捕获的值不等即说明输入已变。
	InputVersion int64
	// ActiveArtifactID pages 上的单值镜像（最近发布语言），只作诊断与向前完成的旁证。
	ActiveArtifactID string
}

// LockPageForActivationTx 在调用方事务内锁住页面行并读出激活判定所需的事实。
//
// 这是本域**唯一的发布判定入口**：行锁由本句持有到事务结束，调用方必须在**同一个 tx**
// 上完成 FS 切换与指针写入，否则锁定无效（行锁的生命周期是事务，不是函数）。
// 纯读：不发 UPDATE、不写任何表。
//
// 为什么用 FOR UPDATE 而不是 advisory lock：这里现成就有可锁的行（pages），
// 行锁自带可见性与作用域，且与 RLS（FORCE 策略）天然兼容；advisory 没有谓词，
// 还要自己保证键的唯一性与规范形态。
// LockPageEntityAdvisoryTx 在**调用方事务**内取 "page:<页面 id>" 的事务级顾问锁
// （pg_advisory_xact_lock，提交/回滚自动释放）。
//
// 用途唯一：页面行已被软删（deleted_at 非空）之后，LockPageForActivationTx 就
// 永远返回 ErrRecordNotFound —— 而「撤掉它遗留的访问面链接」仍必须与「正在进行的那次
// 删除」互斥。删除路径在同一个事务里也取这把锁，两侧时间线因此不会交错到对方的中间态。
//
// **它不是行锁的替代品**，两者的职责不可互换：
//   - 正常 publish / Update 只取 pages 行锁（LockPageForActivationTx），不取本锁；
//   - 因此只取顾问锁的路径与它们**并不互斥**。凡是页面行仍在的场景，必须先取顾问锁、
//     再取行锁（锁序固定：顾问锁 → 行锁 → 路径锁），只靠顾问锁等于没锁。
//
// 为什么不是进程内锁：软删之后不能退化到进程锁 —— 多实例部署时锁不到同一处，
// 而这里的副作用（撤链）不可回退。键取 "page:<id>"，与 presentation/product 的
// 键空间不重叠（各自带类型前缀，且都经 hashtext 归一）。
func (m *Model) LockPageEntityAdvisoryTx(ctx context.Context, tx *gorm.DB, pageID string) error {
	if tx == nil {
		return errors.New("page: 实体顾问锁必须在调用方事务内取")
	}
	key := strings.TrimSpace(pageID)
	if key == "" {
		return ErrProjectRequired
	}
	// pg_advisory_xact_lock 是无表、无 Entity 可映射的数据库函数调用，GORM 没有对应
	// 表达能力（与 internal/module/product/model/product_outbox_model.go、
	// internal/module/presentation/model/presentation_model.go 同一先例，已登记在
	// internal/architecture/raw_sql_boundary_test.go 的 rawSQLAllowedList）。
	return tx.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(hashtext(?))",
		"page:"+key).Error
}

// LockProjectEntityAdvisoryTx 在**调用方事务**内取 "project:<工程 id>" 的事务级顾问锁。
//
// 用途唯一：项目级下线（重定向删除）没有实体行可锁 —— redirect 行的 page_id /
// presentation_id 都是 NULL。它与同路径上的重定向重建并不互斥（后者走的是共享
// 路径锁 Activate/Redirect），因此这里只负责把"两次项目级下线"串起来；真正的归属
// 边界在路径锁内的身份复验。
//
// 键空间独立（"project:" 前缀），与 page / presentation / product 的键不重叠。
func (m *Model) LockProjectEntityAdvisoryTx(ctx context.Context, tx *gorm.DB, projectID string) error {
	if tx == nil {
		return errors.New("page: 工程顾问锁必须在调用方事务内取")
	}
	key := strings.TrimSpace(projectID)
	if key == "" {
		return ErrProjectRequired
	}
	return tx.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(hashtext(?))",
		"project:"+key).Error
}

func (m *Model) LockPageForActivationTx(ctx context.Context, tx *gorm.DB, projectID, pageID string) (snap PageActivationSnapshot, err error) {
	if strings.TrimSpace(projectID) == "" {
		return snap, ErrProjectRequired
	}
	if err = rls.ScopeTx(tx, projectID); err != nil {
		return snap, err
	}
	var row PageEntity
	terr := tx.WithContext(ctx).Model(&PageEntity{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND project_id = ? AND deleted_at IS NULL", pageID, projectID).
		First(&row).Error
	if terr != nil {
		return snap, terr
	}
	snap.PageID = row.ID
	snap.InputVersion = row.InputVersion
	if row.ActiveArtifactID != nil {
		snap.ActiveArtifactID = *row.ActiveArtifactID
	}
	return snap, nil
}

// WritePageActivationTx 在调用方事务内写本语言的激活状态与页面镜像。
//
// 前提：同一事务已先调用 LockPageForActivationTx 并比对通过；本方法**不重复判定**，
// 只写 page_publications（upsert）+ pages（active_* / published_at / stale=false）。
// expectInputVersion 是编译时捕获的代次，写进 WHERE 谓词 —— 即使将来有人在别处
// 提前释放了锁，这条语句也不会把过期输入的产物钉成线上状态。
//
// 返回 written=false 表示谓词未命中（输入已过期 / 页面已删 / 工程不符）：
// 调用方必须回滚并视为激活失败，绝不当成成功。
//
// input_version **不被本语句修改**：激活改变的是可见指针，不是编译输入。
func (m *Model) WritePageActivationTx(ctx context.Context, tx *gorm.DB, projectID string, rec PublicationRecord, expectInputVersion int64) (written bool, err error) {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return false, err
	}
	if err := m.upsertPublicationTx(ctx, tx, rec); err != nil {
		return false, err
	}
	res := tx.WithContext(ctx).Model(&PageEntity{}).
		Where("id = ? AND project_id = ? AND deleted_at IS NULL AND input_version = ?",
			rec.PageID, projectID, expectInputVersion).
		Updates(map[string]any{
			"active_artifact_id": rec.ArtifactID,
			"active_path":        rec.ActivePath,
			"published_at":       rec.PublishedAt,
			"stale":              false,
			"update_time":        rec.PublishedAt,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// WritePageForwardCompletionTx 收敛「向前完成」专用：把 DB 补成访问面上已经生效的那个产物。
//
// 与 WritePageActivationTx 的差别只有一条谓词：额外要求「本语言账本此刻仍是**空**或仍是
// 回执里记的 from 产物」—— 也就是「DB 落后，但没有被别的成功激活替代」。
// 为什么必须多这一条：收敛读符号链接在事务外，判定与写入之间可能有别的事务已经成功激活；
// 只按版本相等去写会把那个更新的成功结果覆盖成自己的陈旧产物（superseded 判定要的正是这里）。
//
// rec.ArtifactID 为本次要落的产物；fromArtifactID 是回执记录的切换前产物（可空 = 原本没有记录）。
func (m *Model) WritePageForwardCompletionTx(ctx context.Context, tx *gorm.DB, projectID string, rec PublicationRecord, expectInputVersion int64, fromArtifactID string) (written bool, err error) {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return false, err
	}
	// 先按「账本仍是从前那个」做条件 upsert：命中就说明没有人替代，可以向前写。
	cur, gerr := m.publicationForUpdateTx(ctx, tx, rec.PageID, rec.Lang)
	if gerr != nil {
		return false, gerr
	}
	curID := ""
	if cur != nil && cur.ArtifactID != nil {
		curID = *cur.ArtifactID
	}
	if curID != "" && curID != strings.TrimSpace(fromArtifactID) {
		// 账本已被别的成功者写走：本次不再前进（调用方据此结案 superseded）。
		return false, nil
	}
	if err := m.upsertPublicationTx(ctx, tx, rec); err != nil {
		return false, err
	}
	res := tx.WithContext(ctx).Model(&PageEntity{}).
		Where("id = ? AND project_id = ? AND deleted_at IS NULL AND input_version = ?",
			rec.PageID, projectID, expectInputVersion).
		Updates(map[string]any{
			"active_artifact_id": rec.ArtifactID,
			"active_path":        rec.ActivePath,
			"published_at":       rec.PublishedAt,
			"stale":              false,
			"update_time":        rec.PublishedAt,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// upsertPublicationTx 写某语言激活账本（page_publications upsert），不含 pages 镜像。
func (m *Model) upsertPublicationTx(ctx context.Context, tx *gorm.DB, rec PublicationRecord) error {
	row := &PublicationEntity{
		PageID: rec.PageID, Lang: rec.Lang, ActivePath: rec.ActivePath,
		ArtifactHash: rec.ArtifactHash, PublishedAt: rec.PublishedAt, UpdatedAt: rec.PublishedAt,
		ReceiptID: rec.receiptIDColumn(),
	}
	if rec.ArtifactID != "" {
		id := rec.ArtifactID
		row.ArtifactID = &id
	}
	// receipt_id 也在 upsert 的更新列里：同一个 (page, lang) 被另一次成功激活覆盖时，
	// 账本上的成功身份必须是**新的那一条** —— 留着旧回执 id 会让收敛把新结果
	// 认成旧回执兑现，从而对真正的旧回执做出错误的前向完成。
	return tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "page_id"}, {Name: "lang"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"active_path", "artifact_id", "artifact_hash", "published_at", "update_time", "receipt_id",
		}),
	}).Create(row).Error
}

// publicationForUpdateTx 锁读某语言的激活账本行；不存在返回 (nil, nil)。
//
// SKIP LOCKED 不适用：这里是判定段，等锁是正确行为（等着看前一个事务的结论）。
func (m *Model) publicationForUpdateTx(ctx context.Context, tx *gorm.DB, pageID, lang string) (*PublicationEntity, error) {
	var row PublicationEntity
	err := tx.WithContext(ctx).Model(&PublicationEntity{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("page_id = ? AND lang = ?", pageID, lang).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// PublicationArtifactIDTx 在调用方事务内读某语言当前激活产物 id（无记录返回空串）。
func (m *Model) PublicationArtifactIDTx(ctx context.Context, tx *gorm.DB, pageID, lang string) (string, error) {
	var rows []struct {
		ArtifactID *string `gorm:"column:artifact_id"`
	}
	if err := tx.WithContext(ctx).Model(&PublicationEntity{}).
		Select("artifact_id").Where("page_id = ? AND lang = ?", pageID, lang).
		Limit(1).Find(&rows).Error; err != nil {
		return "", err
	}
	if len(rows) == 0 || rows[0].ArtifactID == nil {
		return "", nil
	}
	return *rows[0].ArtifactID, nil
}

// PublicationReceiptIDTx 在调用方事务内读某语言账本上的**成功身份**（回执 id）。
//
// 0 = 该语言尚无账本行、或行是历史行（本迁移之前成功 / 无回执设施的链路）。
// 收敛要靠它区分「同版本、同产物的两次成功激活」：只看 (version, artifact) 时
// 二者完全一样，而只有其中一次是「我这条回执兑现的」。
//
// 与 PublicationArtifactIDTx 分开而不是合并成一个结构体：两处调用点各取所需，
// 合并只会让「只关心产物 id」的地方多带一个它并不理解的字段。
func (m *Model) PublicationReceiptIDTx(ctx context.Context, tx *gorm.DB, pageID, lang string) (int64, error) {
	var rows []struct {
		ReceiptID *int64 `gorm:"column:receipt_id"`
	}
	if err := tx.WithContext(ctx).Model(&PublicationEntity{}).
		Select("receipt_id").Where("page_id = ? AND lang = ?", pageID, lang).
		Limit(1).Find(&rows).Error; err != nil {
		return 0, err
	}
	if len(rows) == 0 || rows[0].ReceiptID == nil {
		return 0, nil
	}
	return *rows[0].ReceiptID, nil
}

// PublicationActivePathsTx 在调用方事务内读该页**全部语言**的激活路径集合。
//
// 用途：主提交之后的旧路径处置要在锁内复核「账本现在指向哪里」。只看某一个语言不够 ——
// 改 URL 与多语言发布都会写多行账本，而处置动作是按路径做的：任何一行仍指着将要被
// 清理的旧路径，都说明它还没有被本次尝试之外的人写走。
//
// 空串行（active_path 为空）直接跳过：它们不构成「谁在服务这个 URL」的证据。
func (m *Model) PublicationActivePathsTx(ctx context.Context, tx *gorm.DB, pageID string) ([]string, error) {
	var rows []struct {
		ActivePath string `gorm:"column:active_path"`
	}
	if err := tx.WithContext(ctx).Model(&PublicationEntity{}).
		Select("active_path").Where("page_id = ?", pageID).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		if strings.TrimSpace(r.ActivePath) == "" {
			continue
		}
		out = append(out, r.ActivePath)
	}
	return out, nil
}

// PublicationReceiptIDTxAny 是 PublicationReceiptIDTx 的**自带事务**版本（捕获段用）。
//
// 捕获段（登记 pending 回执之前）手上没有事务：那个事务还没开始，回执也还没登记。
// 分两个入口而不是让 Tx 版本接受 nil 句柄：nil 句柄要么 panic，要么被悄悄降级成
// 「另开一个连接」，正是 Tx 变体要消灭的形态。
func (m *Model) PublicationReceiptIDTxAny(ctx context.Context, projectID, pageID, lang string) (int64, error) {
	if strings.TrimSpace(projectID) == "" {
		return 0, ErrProjectRequired
	}
	var id int64
	err := rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		got, gerr := m.PublicationReceiptIDTx(ctx, tx, pageID, lang)
		if gerr != nil {
			return gerr
		}
		id = got
		return nil
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}

// MarkMenuStaleTx 在调用方事务内把「引用某菜单的页面」标记为待重建并推进输入代次。
//
// 用途：导航（页眉 / 页脚 × 桌面 / 移动）的译文变更要在**写译文同一个事务**里
// 把受影响页面标 stale —— 否则会出现「译文已提交、页面没标」的半截状态，
// 而那一侧没有任何自动补的入口（菜单 CRUD 的 go runRebuild 是异步、失败只记日志）。
//
// kind 是菜单位置（header / header_mobile / footer / footer_mobile）。**参数是位置，不是键**：
// 构建期在产物 manifest 里登记的是 pipeline.MenuKey(projectID, kind) 产出的完整键
// （"menu:{projectID}:{kind}"），page_dependencies.dependency_key 存的也是那个完整键。
// 所以这里的匹配必须先把完整键拼出来再比对 —— 直接拿裸 kind 当 key 做精确等式，
// 一行都命中不了，且**不报错**（表现是「译文保存成功、页面永远不重建」）。
// 键的拼法收敛在本函数内（唯一一处），与 pipeline.MenuKey 逐字一致；
// 两侧一旦漂移就是同一个静默失败，因此改动任何一侧都要同步这里与那边。
// 返回真正命中的页面数（调用方只用来记日志与断言，不做业务判断）。
//
// 与 MarkStaleByDependencyTx 的差别：那条按任意 (kind,key) 标，这条是导航模块的冻结端口，
// 语义固定在「菜单依赖」，签名由 navigation 侧的 contract 钉住 —— 两个模块之间只认这一条，
// 免得日后有人拿一个「kind/key 都随便传」的入口当通用标记用。
func (m *Model) MarkMenuStaleTx(ctx context.Context, tx *gorm.DB, projectID, kind string, at time.Time) (n int64, err error) {
	if strings.TrimSpace(projectID) == "" {
		return 0, ErrProjectRequired
	}
	if strings.TrimSpace(kind) == "" {
		return 0, nil
	}
	if err = rls.ScopeTx(tx, projectID); err != nil {
		return 0, err
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	// depKindMenu / menuDependencyKey 的字面量与 pipeline.DepKindMenu（"menu"）与
	// pipeline.MenuKey 逐字一致：page model 不 import pipeline（分层：model 只碰本模块表与契约），
	// 键的拼法因此在本文件里镜像一份，并由 PageMenuStaleTx 的真实依赖夹具守着重合。
	ids, err := markStaleByDependencyIn(ctx, tx, projectID, depKindMenu, menuDependencyKey(projectID, kind), at)
	if err != nil {
		return 0, err
	}
	return int64(len(ids)), nil
}

// MarkNavigationStaleTx 在调用方事务内把「**按菜单项**引用了该菜单」的页面标记为待重建。
//
// 为什么与 MarkMenuStaleTx 并列而不是复用它：按位置键（menu:{project}:{kind}）与按项键
// （navigation:{itemID}）是两条不同的依赖键，谁也不能替谁命中 —— 页眉只放某一支时产物里
// 登记的是项键，位置键一行都命中不到。菜单项的增删改排序必须两条键都打，否则那一类页面
// 会停在旧链接上且没有任何报错。
//
// 与 MarkStaleByDependencyTx 的差别同上：这条的语义被 navigation 侧的契约钉死在「菜单项」，
// 不接受任意 (kind,key)。
func (m *Model) MarkNavigationStaleTx(ctx context.Context, tx *gorm.DB, projectID, navigationID string) (n int64, err error) {
	navigationID = strings.TrimSpace(navigationID)
	if navigationID == "" {
		return 0, nil
	}
	if strings.TrimSpace(projectID) == "" {
		return 0, ErrProjectRequired
	}
	if err = rls.ScopeTx(tx, projectID); err != nil {
		return 0, err
	}
	// kind 必须是 "navigation" 而不是 "menu"：构建期按项引用登记的是
	// pipeline.NavigationKey 的 DepKey{Kind: "navigation", Key: "navigation:<id>"}，
	// 用错 kind 会让精确等式一行都命中不到且**不报错**。
	ids, err := markStaleByDependencyIn(ctx, tx, projectID, depKindNavigation, navigationDependencyKey(navigationID), time.Now().UTC())
	if err != nil {
		return 0, err
	}
	return int64(len(ids)), nil
}
