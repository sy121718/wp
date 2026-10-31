package feature

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

func readActiveArtifact(t *testing.T, db *gorm.DB, id string) string {
	t.Helper()
	var out string
	if err := db.Raw("SELECT COALESCE(active_artifact_id::text,'') FROM presentation_instances WHERE id = ?", id).
		Scan(&out).Error; err != nil {
		t.Fatalf("读取 active 指针失败: %v", err)
	}
	return out
}

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

	if afterVersion != versionBefore+1 {
		t.Fatalf("改 URL 是输入变化且只推进一次：before=%d after=%d", versionBefore, afterVersion)
	}
}

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

	if _, err := f.pres.UpdateURL(ctx, &presentationdto.UpdateURLReq{
		ProjectID: f.projectID, ID: f.instance, NewPath: "/menu-fence-third",
	}); err != nil {
		t.Fatalf("按最新输入重新改 URL 应成功: %v", err)
	}
	if pathNow, _ := readInstancePathVersion(t, f.db, f.instance); pathNow != "/menu-fence-third" {
		t.Fatalf("最新一次改 URL 应生效：%q", pathNow)
	}
}

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

	if !strings.Contains(row.Links, ":") {
		t.Fatalf("回执必须持久化关联的成功回执（语言 → switch_active 回执 id），实际 %q", row.Links)
	}
	if row.ReceiptState != pubmodel.ReceiptCommitted {
		t.Fatalf("新路径已生效时旧路径处置应已执行并结案，实际 %q", row.ReceiptState)
	}
}

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

func TestOldPathReceiptNotExecutedWhenActivationFailed(t *testing.T) {
	f := newMenuFenceFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	if _, err := f.pres.UpdateURL(ctx, &presentationdto.UpdateURLReq{
		ProjectID: f.projectID, ID: f.instance, NewPath: "/menu-fence-guard",
	}); err != nil {
		t.Fatalf("改 URL 应成功: %v", err)
	}

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

func TestRepeatedActivationSameURLIsDistinguishedByReceiptID(t *testing.T) {
	f := newMenuFenceFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

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

	if err := f.db.Exec(`UPDATE publication_receipts SET receipt_state = ?, completed_at = NULL
		WHERE source_type = ? AND action = ? AND source_id = ?`,
		pubmodel.ReceiptPending, pubcontract.ReceiptSourcePresentation,
		pubcontract.ReceiptActionUpdateURL, f.instance).Error; err != nil {
		t.Fatalf("重置回执失败: %v", err)
	}

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

	store := &pipeline.LocalPublicationStore{ActiveRoot: pipeline.ActiveRoot()}
	if err := store.Activate(row.OldPath, pipeline.Locator{Provider: "local", Key: "artifacts/" + otherArtifact}); err != nil {
		t.Skipf("无法构造外来链接现场（跳过）：%v", err)
	}

	if _, cerr := f.pres.ConvergePendingReceipts(ctx); cerr != nil {

		t.Logf("收敛返回可见错误（可重试形态）: %v", cerr)
	}
	if st, ierr := store.Inspect(row.OldPath); ierr != nil {
		t.Fatalf("复核旧路径状态失败: %v", ierr)
	} else if st.Kind == pipeline.PublicationNone {
		t.Fatalf("旧路径的链接属于别的产物，旧路径处置不得删除它（实际已被下线）")
	}
}

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

	if err := f.db.Exec(`UPDATE publication_receipts
		SET receipt_data = jsonb_set(receipt_data, '{successfulReceipts}',
			jsonb_build_object('zh-CN', '999999999'::text), true),
		    receipt_state = ?, completed_at = NULL
		WHERE source_type = ? AND source_id = ? AND action = ?`,
		pubmodel.ReceiptPending, pubcontract.ReceiptSourcePresentation,
		f.instance, pubcontract.ReceiptActionUpdateURL).Error; err != nil {
		t.Fatalf("改写关联事实失败: %v", err)
	}

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
