package feature

// url_intent_and_receipt_identity_test.go —— 改 URL 的输入意图线性化 + 成功 attempt 身份。
//
// 本批要证明的五件事（全部是行为断言，不查 SQL 字符串）：
//  1. 改 URL 在冻结段就持久化输入意图：url_path 变成新路径、input_version +1；
//  2. 编译/激活失败时意图不回滚，旧产物仍在线上（指针不动）——"输入已变、产物待追"；
//  3. 后到批次用旧代次提交必须被拒（并发交错的正确结论是冲突，不是覆盖），且不改任何列；
//  4. 成功激活的账本行带 receipt_id（= 该语言 switch_active 回执 id）且该回执已结案；
//  5. 改 URL 的旧路径处置：Path=新路径 / OldPath=旧路径 的回执被登记、执行、结案，
//     且收敛例程能重放它（失败不吞、留 pending 可重放）。
//
// 为什么必须在 feature 层：这些判据依赖真实 DDL（迁移 699 input_version、700 receipt_id）、
// 真实发布链（冻结/编译/提交三段）与真实回执表；model 单测只能钉形态。

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationmodel "go_wp/internal/module/presentation/model"
	pubcontract "go_wp/internal/module/publication/contract"
	pubmodel "go_wp/internal/module/publication/model"
	"go_wp/internal/pipeline"
	"go_wp/pkg/jsonx"
)

// readInstancePathVersion 读实例的路径与输入代次（权威事实，直接来自行）。
func readInstancePathVersion(t *testing.T, db *gorm.DB, id string) (path string, version int64) {
	t.Helper()
	var row struct {
		URLPath      string
		InputVersion int64
	}
	if err := db.Raw("SELECT url_path, input_version FROM presentation_instances WHERE id = ?", id).
		Scan(&row).Error; err != nil {
		t.Fatalf("读取实例路径/版本失败: %v", err)
	}
	return row.URLPath, row.InputVersion
}

// readActiveArtifact 读实例当前 active 指针（空串 = 无）。
func readActiveArtifact(t *testing.T, db *gorm.DB, id string) string {
	t.Helper()
	var out string
	if err := db.Raw("SELECT COALESCE(active_artifact_id::text,'') FROM presentation_instances WHERE id = ?", id).
		Scan(&out).Error; err != nil {
		t.Fatalf("读取 active 指针失败: %v", err)
	}
	return out
}

// TestUpdateURLPersistsInputIntentBeforeCompile 改 URL 的输入意图在冻结段落库：
// url_path 立即是新路径、input_version +1。
func TestUpdateURLPersistsInputIntentBeforeCompile(t *testing.T) {
	f := newMenuFenceFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, versionBefore := readInstancePathVersion(t, f.db, f.instance)

	if _, err := f.pres.UpdateURL(ctx, &presentationdto.UpdateURLReq{
		ProjectID: f.projectID, ID: f.instance, NewPath: "/menu-fence-moved",
	}); err != nil {
		t.Fatalf("改 URL 应成功: %v", err)
	}

	afterPath, afterVersion := readInstancePathVersion(t, f.db, f.instance)
	if afterPath != "/menu-fence-moved" {
		t.Fatalf("改 URL 后控制面 url_path 必须是新路径：%q", afterPath)
	}
	// 只推进一次：提交段的 RecordBatchURLTx 不推进版本，所以净值必须正好 +1。
	if afterVersion != versionBefore+1 {
		t.Fatalf("改 URL 是输入变化且只推进一次：before=%d after=%d", versionBefore, afterVersion)
	}
}

// TestUpdateURLInputIntentSurvivesCompileFailure 编译失败时意图不回滚：
// url_path 仍是新值、版本已推进、旧产物仍在线（指针不动）。
func TestUpdateURLInputIntentSurvivesCompileFailure(t *testing.T) {
	f := newMenuFenceFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	activeBefore := readActiveArtifact(t, f.db, f.instance)
	if activeBefore == "" {
		t.Fatalf("前置不成立：实例应已有一份在线产物")
	}
	_, versionBefore := readInstancePathVersion(t, f.db, f.instance)

	// 让编译必然失败：把绑定模板的文档改成语义非法（缺 blockId 的区块数组）。
	//
	// 不能写 `'{'` 这种非法 JSON：draft_document 是 jsonb 列，PG 在写入时就拒绝
	//（invalid input syntax for type json，22P02），失败点跑到夹具的 UPDATE 上，
	// 被测代码根本没被执行。要的是「合法 jsonb + 领域层判不出区块」，让失败落在编译段。
	// 破坏**编译实际读的那一列**：构建链走 ResolveTemplateByIDScoped → LatestVersion，
	// 读的是 content_template_versions.document，不是 content_templates.draft_document。
	// 改错列等于什么都没改，改 URL 自然成功（本用例曾因此假红）。
	const brokenDocument = `{"settings":{"structure":{"blocks":[{"type":"hero"}]}}}`
	if err := f.db.Exec(`UPDATE content_template_versions v SET document = ?::jsonb
		FROM content_templates t
		WHERE v.template_id = t.id AND t.entity_type = 'article'`, brokenDocument).Error; err != nil {
		t.Fatalf("破坏模板文档失败: %v", err)
	}

	_, err := f.pres.UpdateURL(ctx, &presentationdto.UpdateURLReq{
		ProjectID: f.projectID, ID: f.instance, NewPath: "/menu-fence-broken",
	})
	if err == nil {
		t.Fatalf("模板文档非法时改 URL 必须失败（编译失败）")
	}

	pathAfter, versionAfter := readInstancePathVersion(t, f.db, f.instance)
	if pathAfter != "/menu-fence-broken" {
		t.Fatalf("意图已持久化，路径不得回滚：%q", pathAfter)
	}
	if versionAfter != versionBefore+1 {
		t.Fatalf("意图持久化必须留下一次版本推进：before=%d after=%d", versionBefore, versionAfter)
	}
	if activeAfter := readActiveArtifact(t, f.db, f.instance); activeAfter != activeBefore {
		t.Fatalf("旧产物必须仍在线（指针不动）：before=%q after=%q", activeBefore, activeAfter)
	}
}

// TestUpdateURLStaleVersionSubmissionIsRejected 并发交错的判据：
// 带旧版本代次的提交在行锁内被拒，且不改任何列；重新冻结后能成功。
func TestUpdateURLStaleVersionSubmissionIsRejected(t *testing.T) {
	f := newMenuFenceFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, staleVersion := readInstancePathVersion(t, f.db, f.instance)

	if _, err := f.pres.UpdateURL(ctx, &presentationdto.UpdateURLReq{
		ProjectID: f.projectID, ID: f.instance, NewPath: "/menu-fence-first",
	}); err != nil {
		t.Fatalf("第一次改 URL 应成功: %v", err)
	}
	_, afterFirst := readInstancePathVersion(t, f.db, f.instance)
	if afterFirst != staleVersion+1 {
		t.Fatalf("第一次改 URL 必须推进一代：stale=%d now=%d", staleVersion, afterFirst)
	}

	inst, gerr := f.model.GetInstance(ctx, f.projectID, f.instance)
	if gerr != nil {
		t.Fatalf("读取实例失败: %v", gerr)
	}
	art := readActiveArtifact(t, f.db, f.instance)
	inst.ActiveArtifactID = &art
	inst.StagedArtifactID = &art
	inst.URLPath = "/menu-fence-second"
	err := f.model.Transaction(ctx, func(tx *gorm.DB) error {
		return f.model.WriteInstanceActivationTx(tx, f.projectID, inst, staleVersion)
	})
	if err == nil {
		t.Fatalf("旧代次提交必须被拒，实际成功")
	}
	if !errors.Is(err, presentationmodel.ErrInputVersionMoved) && !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Logf("激活被拒的形态（应与 WriteInstanceActivationTx 的 0 行错误同源）: %v", err)
	}
	pathAfter, versionAfter := readInstancePathVersion(t, f.db, f.instance)
	if pathAfter != "/menu-fence-first" || versionAfter != afterFirst {
		t.Fatalf("被拒的提交不得改路径/版本：path=%q version=%d（期望 /menu-fence-first, %d）",
			pathAfter, versionAfter, afterFirst)
	}

	// 拒绝的是旧代次，不是永远拒绝：按最新输入再改一次必须成功。
	if _, err := f.pres.UpdateURL(ctx, &presentationdto.UpdateURLReq{
		ProjectID: f.projectID, ID: f.instance, NewPath: "/menu-fence-third",
	}); err != nil {
		t.Fatalf("按最新输入重新改 URL 应成功: %v", err)
	}
	if pathNow, _ := readInstancePathVersion(t, f.db, f.instance); pathNow != "/menu-fence-third" {
		t.Fatalf("最新一次改 URL 应生效：%q", pathNow)
	}
}

// TestPublishedLedgerCarriesReceiptID 成功激活的账本行必须带 switch_active 回执 id，
// 且那条回执已结案 —— 这是"成功 attempt 身份"落库的唯一证据。
func TestPublishedLedgerCarriesReceiptID(t *testing.T) {
	f := newMenuFenceFixture(t)
	if f == nil {
		return
	}

	var rows []struct {
		Lang      string
		ReceiptID *string
	}
	if err := f.db.Raw(`SELECT lang, receipt_id::text AS receipt_id FROM presentation_publications
		WHERE presentation_id = ?`, f.instance).Scan(&rows).Error; err != nil {
		t.Fatalf("读取语言账本失败: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("前置不成立：创建实例应至少激活默认语言")
	}
	for _, row := range rows {
		if row.ReceiptID == nil || *row.ReceiptID == "" {
			t.Fatalf("语言 %s 的账本缺成功 attempt 身份（receipt_id）", row.Lang)
		}
		var state string
		if err := f.db.Raw(`SELECT receipt_state FROM publication_receipts
			WHERE id = ?::bigint AND source_type = ?`,
			*row.ReceiptID, pubcontract.ReceiptSourcePresentation).
			Scan(&state).Error; err != nil {
			t.Fatalf("读取回执 %s 失败: %v", *row.ReceiptID, err)
		}
		if state != pubmodel.ReceiptCommitted {
			t.Fatalf("语言 %s 的账本引用的回执应已结案(committed)，实际 %q", row.Lang, state)
		}
	}
}

// TestUpdateURLRegistersOldPathReceiptAndExecutes 旧路径处置持久化并执行：
// 回执 Path=新路径、OldPath=旧路径，激活成功后执行并结案（不是只 Warn）。
func TestUpdateURLRegistersOldPathReceiptAndExecutes(t *testing.T) {
	f := newMenuFenceFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	var before []struct {
		Lang       string
		ActivePath string
	}
	if err := f.db.Raw(`SELECT lang, active_path FROM presentation_publications
		WHERE presentation_id = ? ORDER BY lang`, f.instance).Scan(&before).Error; err != nil {
		t.Fatalf("读取改前账本失败: %v", err)
	}
	if len(before) == 0 {
		t.Fatalf("前置不成立：实例应至少有一份语言账本")
	}

	if _, err := f.pres.UpdateURL(ctx, &presentationdto.UpdateURLReq{
		ProjectID: f.projectID, ID: f.instance, NewPath: "/menu-fence-redirect", WithRedirect: true,
	}); err != nil {
		t.Fatalf("改 URL 应成功: %v", err)
	}

	var rows []struct {
		Path         string
		OldPath      string
		ReceiptState string
		OldPaths     string
		Links        string
	}
	if err := f.db.Raw(`SELECT path,
			COALESCE(receipt_data->>'oldPath','') AS old_path,
			COALESCE(receipt_data->>'oldPaths','') AS old_paths,
			COALESCE(receipt_data->>'successfulReceipts','') AS links,
			receipt_state
		FROM publication_receipts
		WHERE source_type = ? AND action = ? AND source_id = ?
		ORDER BY id`, pubcontract.ReceiptSourcePresentation,
		pubcontract.ReceiptActionUpdateURL, f.instance).Scan(&rows).Error; err != nil {
		t.Fatalf("读取 update_url 回执失败: %v", err)
	}
	// 一次改址 = **一条**回执（page 侧既有形态），而不是每条语言一条。
	if len(rows) != 1 {
		t.Fatalf("一次改址应只登记一条 update_url 回执，实际 %d 条", len(rows))
	}
	row := rows[0]
	oldPaths := make(map[string]bool, len(before))
	for _, b := range before {
		oldPaths[b.ActivePath] = true
	}
	if !oldPaths[row.OldPath] {
		t.Fatalf("回执 OldPath=%q 必须是一条真实的旧访问路径（%v）", row.OldPath, oldPaths)
	}
	if row.Path == "" || row.Path == row.OldPath {
		t.Fatalf("回执 Path 必须是新路径且不同于旧路径：path=%q old=%q", row.Path, row.OldPath)
	}
	// 关联事实必须真实落库：恢复侧靠它判定「本次激活成功了没有」，不看时间也不看 URL。
	if !strings.Contains(row.Links, ":") {
		t.Fatalf("回执必须持久化关联的成功回执（语言 → switch_active 回执 id），实际 %q", row.Links)
	}
	if row.ReceiptState != pubmodel.ReceiptCommitted {
		t.Fatalf("新路径已生效时旧路径处置应已执行并结案，实际 %q", row.ReceiptState)
	}
}

// TestUpdateURLOldPathReceiptIsReplayable 回执 pending 时收敛例程真的重放并结案。
func TestUpdateURLOldPathReceiptIsReplayable(t *testing.T) {
	f := newMenuFenceFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	if _, err := f.pres.UpdateURL(ctx, &presentationdto.UpdateURLReq{
		ProjectID: f.projectID, ID: f.instance, NewPath: "/menu-fence-replay",
	}); err != nil {
		t.Fatalf("改 URL 应成功: %v", err)
	}
	// 现场：把该实例的 update_url 回执重置回 pending（模拟"执行前崩溃"）。
	if err := f.db.Exec(`UPDATE publication_receipts SET receipt_state = ?, completed_at = NULL
		WHERE source_type = ? AND action = ? AND source_id = ?`,
		pubmodel.ReceiptPending, pubcontract.ReceiptSourcePresentation,
		pubcontract.ReceiptActionUpdateURL, f.instance).Error; err != nil {
		t.Fatalf("重置回执为 pending 失败: %v", err)
	}

	converged, cerr := f.pres.ConvergePendingReceipts(ctx)
	if cerr != nil {
		t.Fatalf("收敛旧路径处置回执失败: %v", cerr)
	}
	if converged == 0 {
		t.Fatalf("pending 的 update_url 回执必须被收敛（重放或明确结案），实际 0 条")
	}
	var pending int
	if err := f.db.Raw(`SELECT COUNT(*) FROM publication_receipts
		WHERE source_type = ? AND action = ? AND source_id = ? AND receipt_state = ?`,
		pubcontract.ReceiptSourcePresentation, pubcontract.ReceiptActionUpdateURL,
		f.instance, pubmodel.ReceiptPending).Scan(&pending).Error; err != nil {
		t.Fatalf("统计 pending 失败: %v", err)
	}
	if pending != 0 {
		t.Fatalf("收敛后不应留下 pending 的旧路径处置回执：%d", pending)
	}
}

// TestMarkInstanceMenuStalePortIsWired Service 必须真的实现 PresentationStalePort
// （装配层用类型断言，缺方法会 panic），且未知种类报错而不是静默全站标记。
func TestMarkInstanceMenuStalePortIsWired(t *testing.T) {
	f := newMenuFenceFixture(t)
	if f == nil {
		return
	}
	port, ok := interface{}(f.pres).(presentationcontract.PresentationStalePort)
	if !ok {
		t.Fatalf("presentation.Service 未实现 PresentationStalePort（装配期会 panic）")
	}
	var n int64
	err := f.model.Transaction(context.Background(), func(tx *gorm.DB) error {
		var merr error
		n, merr = port.MarkInstanceMenuStaleTx(context.Background(), tx, f.projectID, "not_a_menu_kind", time.Now().UTC())
		return merr
	})
	if err == nil {
		t.Fatalf("未知菜单种类必须报错（实际命中了 %d 行）", n)
	}
}

// TestOldPathReceiptNotExecutedWhenActivationFailed 编译失败后收敛**不得**处置旧路径：
// 现场与"激活成功但处置未执行"逐字相同（url_path 已是新值、实例在线旧产物、
// update_url 回执 pending），唯一区别是关联的 switch_active 回执未 committed。
//
// 这一条针对的正是"实例存在 + 逻辑路径相等"那个错误判据：它在失败现场同样为真，
// 会把仍在服务的旧路径 301 走或下线。正确判据只认持久化的成功身份。
func TestOldPathReceiptNotExecutedWhenActivationFailed(t *testing.T) {
	f := newMenuFenceFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	// ① 先造一次真实的改 URL（回执正常结案、旧路径已被处置）。
	if _, err := f.pres.UpdateURL(ctx, &presentationdto.UpdateURLReq{
		ProjectID: f.projectID, ID: f.instance, NewPath: "/menu-fence-guard",
	}); err != nil {
		t.Fatalf("改 URL 应成功: %v", err)
	}

	// ② 把该实例的**全部**回执打回 pending，并让关联的 switch_active 变成 rolled_back：
	//    这正是"冻结段已持久新 URL、编译/激活失败"在库里的样子。
	if err := f.db.Exec(`UPDATE publication_receipts SET receipt_state = ?, completed_at = NULL
		WHERE source_type = ? AND source_id = ? AND action = ?`,
		pubmodel.ReceiptRolledBack, pubcontract.ReceiptSourcePresentation,
		f.instance, pubcontract.ReceiptActionSwitchActive).Error; err != nil {
		t.Fatalf("改写 switch_active 状态失败: %v", err)
	}
	if err := f.db.Exec(`UPDATE publication_receipts SET receipt_state = ?, completed_at = NULL
		WHERE source_type = ? AND source_id = ? AND action = ?`,
		pubmodel.ReceiptPending, pubcontract.ReceiptSourcePresentation,
		f.instance, pubcontract.ReceiptActionUpdateURL).Error; err != nil {
		t.Fatalf("改写 update_url 状态失败: %v", err)
	}

	// ③ 收敛：判据应为"激活未成功 → 不处置旧路径"，且必须把这条回执结案（不留永久 pending）。
	if _, cerr := f.pres.ConvergePendingReceipts(ctx); cerr != nil {
		t.Fatalf("收敛失败: %v", cerr)
	}
	var state string
	if err := f.db.Raw(`SELECT receipt_state FROM publication_receipts
		WHERE source_type = ? AND action = ? AND source_id = ?`,
		pubcontract.ReceiptSourcePresentation, pubcontract.ReceiptActionUpdateURL, f.instance).
		Scan(&state).Error; err != nil {
		t.Fatalf("读取 update_url 回执状态失败: %v", err)
	}
	if state == pubmodel.ReceiptPending {
		t.Fatalf("激活未成功时旧路径处置回执应被结案（不执行处置），实际仍 pending")
	}
	if state == pubmodel.ReceiptCommitted {
		t.Fatalf("激活未成功时不得把旧路径处置回执标成已执行(committed)，实际 %q", state)
	}
}

// TestRepeatedActivationSameURLIsDistinguishedByReceiptID 同一路径连续两次 attempt：
// URL 完全相等，只有回执 id 能说明"哪一次成功了"。
//
// 形态：第一次 attempt 的回执被回滚，第二次成功 —— 库里的 URL、artifact 都可以相同，
// 因此任何形如"路径相等 / 时间更晚"的判据都会给出任意结论。
func TestRepeatedActivationSameURLIsDistinguishedByReceiptID(t *testing.T) {
	f := newMenuFenceFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	// 再发布一次到**同一路径**：真实链路上这是一次新的 attempt（新回执、新账本身份）。
	if _, err := f.pres.Rebuild(ctx, &presentationdto.RebuildReq{
		ProjectID: f.projectID, EntityID: f.entity,
	}); err != nil {
		t.Fatalf("同路径再发布应成功: %v", err)
	}

	var rows []struct {
		Lang      string
		ReceiptID *string
	}
	if err := f.db.Raw(`SELECT lang, receipt_id::text AS receipt_id FROM presentation_publications
		WHERE presentation_id = ? ORDER BY lang`, f.instance).Scan(&rows).Error; err != nil {
		t.Fatalf("读取账本失败: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("前置不成立：应至少有一份语言账本")
	}
	for _, row := range rows {
		if row.ReceiptID == nil || *row.ReceiptID == "" {
			t.Fatalf("语言 %s 的账本必须记录成功 attempt 身份（不能只靠 artifact 判归属）", row.Lang)
		}
		// 身份必须指向一条**已结案**的 switch_active 回执：否则"成功身份"是假的。
		var state string
		if err := f.db.Raw(`SELECT receipt_state FROM publication_receipts WHERE id = ?::bigint`,
			*row.ReceiptID).Scan(&state).Error; err != nil {
			t.Fatalf("读取回执 %s 失败: %v", *row.ReceiptID, err)
		}
		if state != pubmodel.ReceiptCommitted {
			t.Fatalf("账本引用的回执应已结案，实际 %q（lang=%s id=%s）", state, row.Lang, *row.ReceiptID)
		}
	}
}

// TestOldPathDisposalHoldsEntityLockAcrossSideEffects 判定与副作用必须在同一个实体锁内：
// 用"锁内另开连接去读实例行"的方式证明——若判定与处置之间存在解锁窗口，
// 那条读会拿到锁（说明锁已释放）；本批实现必须让它阻塞到事务结束。
//
// 这里用真实的另一个连接发 SELECT ... FOR UPDATE NOWAIT：拿到锁 = 旧实现（锁已释放），
// 报"锁被占用"= 现行实现（判定与副作用同锁）。不依赖时序、不 sleep 等竞态。
func TestOldPathDisposalHoldsEntityLockAcrossSideEffects(t *testing.T) {
	f := newMenuFenceFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	if _, err := f.pres.UpdateURL(ctx, &presentationdto.UpdateURLReq{
		ProjectID: f.projectID, ID: f.instance, NewPath: "/menu-fence-lock",
	}); err != nil {
		t.Fatalf("改 URL 应成功: %v", err)
	}
	// 把 update_url 回执打回 pending，并保持关联 switch_active committed（真实成功现场）。
	if err := f.db.Exec(`UPDATE publication_receipts SET receipt_state = ?, completed_at = NULL
		WHERE source_type = ? AND action = ? AND source_id = ?`,
		pubmodel.ReceiptPending, pubcontract.ReceiptSourcePresentation,
		pubcontract.ReceiptActionUpdateURL, f.instance).Error; err != nil {
		t.Fatalf("重置回执失败: %v", err)
	}

	// 在收敛进行中，另一个连接尝试 NOWAIT 锁实例行：必须失败（锁被收敛事务持有）。
	var lockBlocked bool
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			var id string
			err := f.db.Raw("SELECT id::text FROM presentation_instances WHERE id = ? FOR UPDATE NOWAIT", f.instance).
				Scan(&id).Error
			if err != nil {
				lockBlocked = true
				return
			}
		}
	}()
	if _, cerr := f.pres.ConvergePendingReceipts(ctx); cerr != nil {
		t.Fatalf("收敛失败: %v", cerr)
	}
	close(stop)
	<-done
	if !lockBlocked {
		t.Fatalf("收敛期间实例行必须被持锁（否则判定与旧路径副作用之间存在解锁窗口）")
	}
}

// TestMultiLangOldPathDisposalReplaysEveryPath 多语言改址：回执里持久化的**全部**
// 旧路径都要被处置，不能只做 OldPath 那一条。
//
// 造法：先手工把账本与回执的 oldPaths 置成两条（模拟多语言站），再走真实收敛，
// 断言两条旧路径都不再是 active 路由占用的 active 行（即被 redirect/下线过）。
func TestMultiLangOldPathDisposalReplaysEveryPath(t *testing.T) {
	f := newMenuFenceFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	if _, err := f.pres.UpdateURL(ctx, &presentationdto.UpdateURLReq{
		ProjectID: f.projectID, ID: f.instance, NewPath: "/menu-fence-multi",
	}); err != nil {
		t.Fatalf("改 URL 应成功: %v", err)
	}
	// 追加一条额外旧路径（模拟第二个语言的历史访问路径），并让回执回到 pending。
	extra := "/menu-fence-lang2"
	if err := f.db.Exec(`UPDATE publication_receipts
		SET receipt_data = jsonb_set(receipt_data, '{oldPaths}',
			(COALESCE(receipt_data->'oldPaths', '[]'::jsonb) || to_jsonb(?::text)), true),
		    receipt_state = ?, completed_at = NULL
		WHERE source_type = ? AND action = ? AND source_id = ?`,
		extra, pubmodel.ReceiptPending, pubcontract.ReceiptSourcePresentation,
		pubcontract.ReceiptActionUpdateURL, f.instance).Error; err != nil {
		t.Fatalf("改写回执 oldPaths 失败: %v", err)
	}

	if _, cerr := f.pres.ConvergePendingReceipts(ctx); cerr != nil {
		t.Fatalf("收敛失败: %v", cerr)
	}
	var state string
	if err := f.db.Raw(`SELECT receipt_state FROM publication_receipts
		WHERE source_type = ? AND action = ? AND source_id = ?`,
		pubcontract.ReceiptSourcePresentation, pubcontract.ReceiptActionUpdateURL, f.instance).
		Scan(&state).Error; err != nil {
		t.Fatalf("读取回执状态失败: %v", err)
	}
	if state != pubmodel.ReceiptCommitted {
		t.Fatalf("多语言旧路径全部处置成功后才可结案，实际 %q", state)
	}
	// 两条旧路径都必须留下路由痕迹（redirect 或已无 active 行）。
	var oldPathCount int
	if err := f.db.Raw(`SELECT COUNT(*) FROM page_routes
		WHERE project_id = ? AND presentation_id = ? AND path IN ('menu-fence-multi', ?) AND route_kind = 'active'`,
		f.projectID, f.instance, extra).Scan(&oldPathCount).Error; err != nil {
		t.Fatalf("统计旧路径占用失败: %v", err)
	}
	if oldPathCount != 0 {
		t.Fatalf("旧路径不应再是 active 占用（应已 redirect 或释放），实际 %d 条", oldPathCount)
	}
}

// TestNewerReceiptIdentityBlocksOlderAttemptDisposal 同一产物、同一路径的两次 attempt：
// 后来的 attempt 成功之后，先前那条 attempt 的旧路径处置**必须被拒**。
//
// 造法：
//  1. 真实改 URL 一次 → 得到一条已结案的 update_url 回执 A（旧路径处置已完成）；
//  2. 新建一条回执 B，刻意声明与 A 完全相同的 Path/OldPath/ToArtifactID/successfulReceipts
//     —— 即"同产物、同路径的另一次 attempt"，但它的 fromReceiptId 是 **0**（确认没有更早的
//     成功身份）；
//  3. 把账本成功身份改成 A 的 id（模拟 A 才是真正落账的那一次）。
//
// 此时 B 的身份事实是"我以为我没接替任何人"，而账本上是 A —— 一个**新的**成功身份。
// 任何只看 path / artifact 的判据在此给出"可以执行"，正确判据必须判 superseded 并结案不执行。
func TestNewerReceiptIdentityBlocksOlderAttemptDisposal(t *testing.T) {
	f := newMenuFenceFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	if _, err := f.pres.UpdateURL(ctx, &presentationdto.UpdateURLReq{
		ProjectID: f.projectID, ID: f.instance, NewPath: "/menu-fence-identity",
	}); err != nil {
		t.Fatalf("改 URL 应成功: %v", err)
	}
	// A：本次改址的真实回执（已结案）。
	var aRow struct {
		ID           string
		Path         string
		OldPath      string
		Links        string
		ToArtifactID string
	}
	if err := f.db.Raw(`SELECT id::text AS id, path,
			COALESCE(receipt_data->>'oldPath','') AS old_path,
			COALESCE(receipt_data->>'successfulReceipts','{}') AS links,
			COALESCE(to_artifact_id::text,'') AS to_artifact_id
		FROM publication_receipts
		WHERE source_type = ? AND action = ? AND source_id = ? AND receipt_state = ?
		ORDER BY id DESC LIMIT 1`, pubcontract.ReceiptSourcePresentation,
		pubcontract.ReceiptActionUpdateURL, f.instance, pubmodel.ReceiptCommitted).
		Scan(&aRow).Error; err != nil {
		t.Fatalf("读取真实改址回执失败: %v", err)
	}
	if aRow.ID == "" || aRow.Links == "" {
		t.Fatalf("前置不成立：应有一条带关联事实的已结案 update_url 回执")
	}
	// 账本上真实的成功身份是**激活回执 id**（不是 update_url 回执自己的 id）：
	// 口径见 MarkPublishedLangTx 的 ReceiptID 参数与 receiptIdentityVerdictTx 的注释。
	var aLinks map[string]string
	if uerr := jsonx.Unmarshal([]byte(aRow.Links), &aLinks); uerr != nil {
		t.Fatalf("解析 A 的关联事实失败: %v", uerr)
	}
	var ledgerLang, ledgerReceipt string
	for lang, rid := range aLinks {
		ledgerLang, ledgerReceipt = lang, rid
		break
	}
	if ledgerReceipt == "" {
		t.Fatalf("前置不成立：A 的关联事实应为非空的语言→回执映射")
	}
	if err := f.db.Exec(`UPDATE presentation_publications SET receipt_id = ?::bigint
		WHERE presentation_id = ?`, ledgerReceipt, f.instance).Error; err != nil {
		t.Fatalf("写账本身份失败: %v", err)
	}
	// B：同形路径的另一条 attempt，但它的关联事实是**另一批更晚的激活回执**。
	// 只有这样才能表达"账本上是一个新的成功身份"：如果 B 复用 A 的关联回执，
	// 那它就是同一次激活的重放（账本认它就是它自己）。
	var newerID string
	if err := f.db.Raw(`INSERT INTO publication_receipts
			(source_type, source_id, action, path, to_artifact_id, receipt_state, receipt_data, create_time)
		VALUES (?, ?, ?, ?, NULLIF(?,'')::uuid, ?, '{}'::jsonb, now())
		RETURNING id::text`,
		pubcontract.ReceiptSourcePresentation, f.instance, pubcontract.ReceiptActionSwitchActive,
		aRow.Path, aRow.ToArtifactID, pubmodel.ReceiptCommitted).Scan(&newerID).Error; err != nil {
		t.Fatalf("登记更晚的激活回执失败: %v", err)
	}
	bLinksJSON, merr := jsonx.Marshal(map[string]string{ledgerLang: newerID})
	if merr != nil {
		t.Fatalf("序列化 B 的关联事实失败: %v", merr)
	}
	var bID string
	if err := f.db.Raw(`INSERT INTO publication_receipts
			(source_type, source_id, action, path, to_artifact_id, receipt_state, receipt_data, create_time)
		VALUES (?, ?, ?, ?, NULLIF(?,'')::uuid, ?, jsonb_build_object(
			'oldPath', ?::text, 'oldPaths', jsonb_build_array(?::text), 'redirect', false,
			'successfulReceipts', ?::jsonb, 'fromReceiptId', '0'), now())
		RETURNING id::text`,
		pubcontract.ReceiptSourcePresentation, f.instance, pubcontract.ReceiptActionUpdateURL,
		aRow.Path, aRow.ToArtifactID, pubmodel.ReceiptPending, aRow.OldPath, aRow.OldPath,
		string(bLinksJSON)).Scan(&bID).Error; err != nil {
		t.Fatalf("登记同形 attempt 回执失败: %v", err)
	}

	if _, cerr := f.pres.ConvergePendingReceipts(ctx); cerr != nil {
		t.Fatalf("收敛失败: %v", cerr)
	}
	var state string
	if err := f.db.Raw(`SELECT receipt_state FROM publication_receipts WHERE id = ?::bigint`, bID).
		Scan(&state).Error; err != nil {
		t.Fatalf("读取 B 的状态失败: %v", err)
	}
	if state == pubmodel.ReceiptPending {
		t.Fatalf("账本已是另一个成功身份时，旧 attempt 必须被结案（不能永久 pending 重放）")
	}
	if state == pubmodel.ReceiptCommitted {
		t.Fatalf("被后来的成功身份取代的 attempt 不得执行旧路径处置，实际 committed")
	}
}

// TestOwnReceiptIdentityIsIdempotentlyReplayed 账本成功身份就是这条回执自己时，
// 重放必须幂等成功（再执行一次旧路径处置无害），不能因为"已有身份"被判成 superseded。
func TestOwnReceiptIdentityIsIdempotentlyReplayed(t *testing.T) {
	f := newMenuFenceFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	if _, err := f.pres.UpdateURL(ctx, &presentationdto.UpdateURLReq{
		ProjectID: f.projectID, ID: f.instance, NewPath: "/menu-fence-own",
	}); err != nil {
		t.Fatalf("改 URL 应成功: %v", err)
	}
	// 账本上的成功身份保持**真实形状**（语言 → 激活回执 id，由 MarkPublishedLangTx 在
	// 切换时写入），只把 update_url 回执打回 pending：这就是"我已落过账、只是回执没结案"
	// 的幂等重放现场，重放必须补处置并结案，不能因"账本已有身份"判成被取代。
	if err := f.db.Exec(`UPDATE publication_receipts SET receipt_state = ?, completed_at = NULL
		WHERE source_type = ? AND action = ? AND source_id = ?`,
		pubmodel.ReceiptPending, pubcontract.ReceiptSourcePresentation,
		pubcontract.ReceiptActionUpdateURL, f.instance).Error; err != nil {
		t.Fatalf("重置回执失败: %v", err)
	}

	if _, cerr := f.pres.ConvergePendingReceipts(ctx); cerr != nil {
		t.Fatalf("收敛失败: %v", cerr)
	}
	var state string
	if err := f.db.Raw(`SELECT receipt_state FROM publication_receipts
		WHERE source_type = ? AND action = ? AND source_id = ?`,
		pubcontract.ReceiptSourcePresentation, pubcontract.ReceiptActionUpdateURL, f.instance).
		Scan(&state).Error; err != nil {
		t.Fatalf("读取状态失败: %v", err)
	}
	if state != pubmodel.ReceiptCommitted {
		t.Fatalf("账本身份就是本回执时重放应幂等结案，实际 %q", state)
	}
}

// TestOldPathDisposalRefusesForeignFSTarget 旧路径的访问面链接已被**别的产物**占据时，
// 下线必须拒绝（保留 pending），不得替别人下架线上页面。
//
// DB 侧条件写只能证明"路由占用归我"，不能证明"这条符号链接是我激活的"：跨实体没有
// 共享路径锁，因此处置前必须在 FS 侧复核目标。本测试直接制造"链接指向别的产物"的现场。
func TestOldPathDisposalRefusesForeignFSTarget(t *testing.T) {
	f := newMenuFenceFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	if _, err := f.pres.UpdateURL(ctx, &presentationdto.UpdateURLReq{
		ProjectID: f.projectID, ID: f.instance, NewPath: "/menu-fence-foreign",
	}); err != nil {
		t.Fatalf("改 URL 应成功: %v", err)
	}
	var row struct {
		OldPath string
	}
	if err := f.db.Raw(`SELECT COALESCE(receipt_data->>'oldPath','') AS old_path
		FROM publication_receipts
		WHERE source_type = ? AND action = ? AND source_id = ?
		ORDER BY id DESC LIMIT 1`, pubcontract.ReceiptSourcePresentation,
		pubcontract.ReceiptActionUpdateURL, f.instance).Scan(&row).Error; err != nil {
		t.Fatalf("读取回执失败: %v", err)
	}
	if row.OldPath == "" {
		t.Fatalf("前置不成立：回执应带旧路径")
	}
	// 现场：旧路径上放一个指向**别的**产物的链接（等价于另一实体接管了该路径），
	// 同时把回执打回 pending，让收敛必须重新处置这条旧路径。
	if err := f.db.Exec(`UPDATE publication_receipts SET receipt_state = ?, completed_at = NULL
		WHERE source_type = ? AND action = ? AND source_id = ?`,
		pubmodel.ReceiptPending, pubcontract.ReceiptSourcePresentation,
		pubcontract.ReceiptActionUpdateURL, f.instance).Error; err != nil {
		t.Fatalf("重置回执失败: %v", err)
	}
	var otherArtifact string
	if err := f.db.Raw(`SELECT id::text FROM presentation_artifacts WHERE presentation_id <> ?
		LIMIT 1`, f.instance).Scan(&otherArtifact).Error; err != nil || otherArtifact == "" {
		t.Skip("环境里没有第二个实体可用的产物（跳过外来链接场景）")
	}
	// 用真实 store 激活旧路径到别人的产物：这正是"另一个实体接管了这条路径"的现场。
	store := &pipeline.LocalPublicationStore{ActiveRoot: pipeline.ActiveRoot()}
	if err := store.Activate(row.OldPath, pipeline.Locator{Provider: "local", Key: "artifacts/" + otherArtifact}); err != nil {
		t.Skipf("无法构造外来链接现场（跳过）：%v", err)
	}

	if _, cerr := f.pres.ConvergePendingReceipts(ctx); cerr != nil {
		// 收敛本身上报错误是可接受的（可重试），此处只断言"没有被删掉 / 没有结案成已执行"。
		t.Logf("收敛返回可见错误（可重试形态）: %v", cerr)
	}
	if st, ierr := store.Inspect(row.OldPath); ierr != nil {
		t.Fatalf("复核旧路径状态失败: %v", ierr)
	} else if st.Kind == pipeline.PublicationNone {
		t.Fatalf("旧路径的链接属于别的产物，旧路径处置不得删除它（实际已被下线）")
	}
}

// TestPendingLinkedReceiptKeepsOldPathIntentRetryable 关联回执仍是 pending 时，
// 旧路径处置回执必须**保留 pending**（可重试），不能被判死成 rolled_back。
//
// 这是"证据稍后才落库"的现场：关联回执的结案发生在写路径快通道里，收敛可能先看到它。
func TestPendingLinkedReceiptKeepsOldPathIntentRetryable(t *testing.T) {
	f := newMenuFenceFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	if _, err := f.pres.UpdateURL(ctx, &presentationdto.UpdateURLReq{
		ProjectID: f.projectID, ID: f.instance, NewPath: "/menu-fence-pending",
	}); err != nil {
		t.Fatalf("改 URL 应成功: %v", err)
	}
	// 「证据未到」的现场：update_url 回执的关联成功回执**还没落库**（id 指向一行不存在），
	// 同时把回执本身打回 pending。
	//
	// 为什么不用"把 switch_active 重置成 pending"来造：收敛会先领到那条 switch_active 并把它
	// 重放补齐（补切换是它的正常职责），等轮到 update_url 时证据已经到齐 —— 现场在收敛过程中
	// 自己消失了，用例就测不到"证据未到"这条分支（实测：committed）。
	if err := f.db.Exec(`UPDATE publication_receipts
		SET receipt_data = jsonb_set(receipt_data, '{successfulReceipts}',
			jsonb_build_object('zh-CN', '999999999'::text), true),
		    receipt_state = ?, completed_at = NULL
		WHERE source_type = ? AND source_id = ? AND action = ?`,
		pubmodel.ReceiptPending, pubcontract.ReceiptSourcePresentation,
		f.instance, pubcontract.ReceiptActionUpdateURL).Error; err != nil {
		t.Fatalf("改写关联事实失败: %v", err)
	}

	// 证据未到是**可重试**形态：收敛如实上报这一条（不能被当成"已处理"），
	// 回执本身必须留在 pending 等下一轮。所以这里只容忍错误，不把它当失败。
	if _, cerr := f.pres.ConvergePendingReceipts(ctx); cerr != nil {
		t.Logf("收敛返回可见错误（可重试形态）: %v", cerr)
	}
	var state string
	if err := f.db.Raw(`SELECT receipt_state FROM publication_receipts
		WHERE source_type = ? AND action = ? AND source_id = ?`,
		pubcontract.ReceiptSourcePresentation, pubcontract.ReceiptActionUpdateURL, f.instance).
		Scan(&state).Error; err != nil {
		t.Fatalf("读取回执状态失败: %v", err)
	}
	if state != pubmodel.ReceiptPending {
		t.Fatalf("关联回执未定案时旧路径处置必须保留 pending 可重试，实际 %q", state)
	}
}
