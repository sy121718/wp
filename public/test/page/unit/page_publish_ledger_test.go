package unit

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/gorm"

	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pageservice "go_wp/internal/module/page/service"
	pubcontract "go_wp/internal/module/publication/contract"
	pubdto "go_wp/internal/module/publication/dto"

	"go_wp/internal/pipeline"
)

// TestRecoverPendingPublicationCompletesWhenSwitchHappened 崩在「已切访问面、未写数据库」之间时补齐状态（TX-009）。
//
// 这是 TX-009 的核心场景：符号链接已指向新产物（线上真的生效了），而数据库活跃指针
// 还停在旧值。没有回执时只能人工比对；有回执 + 链接实际指向就能判定并补齐。
func TestRecoverPendingPublicationCompletesWhenSwitchHappened(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	root := os.Getenv("GO_WP_ARTIFACT_ROOT")
	page, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/crash", DraftDocument: json.RawMessage(emptyRevDoc),
	})
	if err != nil {
		t.Fatalf("创建页面失败: %v", err)
	}
	hash := "hash-crash-1"
	writeArtifactDir(t, root, hash)
	artifactID := insertArtifactRow(t, db, page.ID, hash)
	// 访问面已切换：/crash 指向该产物（与生产一致，符号链接指向 artifacts 目录）。
	linkActivePath(t, root, "/crash", hash)
	insertPendingReceipt(t, db, page.ID, "/crash", "zh-CN", artifactID)

	recovered, rolledBack, rerr := recoverPending(t, svc, ctx)
	if rerr != nil {
		t.Fatalf("恢复失败: %v", rerr)
	}
	if recovered != 1 || rolledBack != 0 {
		t.Fatalf("应补完成 1 条、回滚 0 条，实际 %d / %d", recovered, rolledBack)
	}
	// 数据库活跃指针已补齐（「DB 没跟上」的那一半）。
	var activePath, activeHash string
	if err := db.Raw("SELECT active_path, artifact_hash FROM page_publications WHERE page_id = ? AND lang = ?", page.ID, "zh-CN").
		Row().Scan(&activePath, &activeHash); err != nil {
		t.Fatalf("读取激活记录失败: %v", err)
	}
	if activePath != "/crash" || activeHash != hash {
		t.Fatalf("激活记录应补齐为本次产物，实际 %q / %q", activePath, activeHash)
	}
	// 回执已结案，下次启动不会重复判定。
	if got := receiptState(t, db, "/crash"); got != "committed" {
		t.Fatalf("回执应结案为 committed，实际 %q", got)
	}
}

// TestRecoverPendingPublicationAbortsWhenSwitchNeverHappened 切换没发生时只结案、不改状态（TX-009）。
//
// 判定必须保守：证据不足时改数据库（或改文件）比什么都不做更危险 —— 这里断言
// 「不写激活记录、只把回执标成 rolled_back」。
func TestRecoverPendingPublicationAbortsWhenSwitchNeverHappened(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	root := os.Getenv("GO_WP_ARTIFACT_ROOT")
	page, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/nocrash", DraftDocument: json.RawMessage(emptyRevDoc),
	})
	if err != nil {
		t.Fatalf("创建页面失败: %v", err)
	}
	hash := "hash-crash-2"
	writeArtifactDir(t, root, hash)
	artifactID := insertArtifactRow(t, db, page.ID, hash)
	// 访问面没有链接（切换没发生），只有回执留在库里。
	insertPendingReceipt(t, db, page.ID, "/nocrash", "zh-CN", artifactID)

	recovered, rolledBack, rerr := recoverPending(t, svc, ctx)
	if rerr != nil {
		t.Fatalf("恢复失败: %v", rerr)
	}
	if recovered != 0 || rolledBack != 1 {
		t.Fatalf("应回滚 1 条、补完成 0 条，实际 %d / %d", recovered, rolledBack)
	}
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM page_publications WHERE page_id = ?", page.ID).Scan(&count).Error; err != nil {
		t.Fatalf("统计激活记录失败: %v", err)
	}
	if count != 0 {
		t.Fatalf("切换未发生时不写激活记录，实际 %d 条", count)
	}
	if got := receiptState(t, db, "/nocrash"); got != "rolled_back" {
		t.Fatalf("回执应标为 rolled_back，实际 %q", got)
	}
}

// recoverPending 调用回执恢复入口（运维能力不进服务契约，走类型断言）。
func recoverPending(t *testing.T, svc pagecontract.PageService, ctx context.Context) (int, int, error) {
	t.Helper()
	recoverer, ok := svc.(interface {
		RecoverPendingPublications(context.Context) (int, int, error)
	})
	if !ok {
		t.Fatal("页面服务未提供发布回执恢复入口")
	}
	return recoverer.RecoverPendingPublications(ctx)
}

// insertArtifactRow 直插一条 page_artifacts 行（返回 id）：回执恢复要按 id 反查 hash。
func insertArtifactRow(t *testing.T, db *gorm.DB, pageID, hash string) string {
	t.Helper()
	var id string
	// 一条完整 SQL：gorm 的 Raw 只把首参当语句，其余参数按占位符取值。
	statement := "INSERT INTO page_artifacts (" +
		"id, page_id, version, source_document, page_document_schema_version, source_hash, " +
		"build_input_manifest, build_input_hash, artifact_provider, artifact_key, artifact_hash, " +
		"compiler_version, registry_version, manifest, payload_state, note, created_by, create_time, lang" +
		") VALUES (gen_random_uuid(), ?, 1, '{}'::jsonb, 1, 'src', '{}'::jsonb, 'bih', 'local', 'key-x', ?, " +
		"'v1', 'r1', '{}'::jsonb, 'available', '', gen_random_uuid(), now(), 'zh-CN') RETURNING id"
	if err := db.Raw(statement, pageID, hash).Scan(&id).Error; err != nil {
		t.Fatalf("插入产物行失败: %v", err)
	}
	return id
}

// linkActivePath 在访问面根目录下建一个指向产物的符号链接（模拟「切换已发生」）。
func linkActivePath(t *testing.T, root, urlPath, hash string) {
	t.Helper()
	link := filepath.Join(root, "public", "active", strings.TrimPrefix(urlPath, "/"))
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatalf("创建访问面目录失败: %v", err)
	}
	// 必须与生产同形：相对链接（形如 ../../../artifacts/<hash>）。Inspect 靠剥掉
	// "../" 前缀还原产物 key —— 用绝对路径建的链接在真实系统里根本不会被产生，
	// 用它测出来的结论也不作数。
	target, err := filepath.Rel(filepath.Dir(link), filepath.Join(root, "artifacts", hash))
	if err != nil {
		t.Fatalf("计算相对链接目标失败: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("创建符号链接失败: %v", err)
	}
}

// insertPendingReceipt 直插一条 pending 的访问面切换回执（模拟「崩在切换前后」留下的痕迹）。
func insertPendingReceipt(t *testing.T, db *gorm.DB, pageID, path, lang, artifactID string) {
	t.Helper()
	data := "{\"lang\":\"" + lang + "\"}"
	// DB-019/020 第一批：receipts.id 已是 bigint identity，不再手填 uuid；时间列改名 create_time。
	if err := db.Exec("INSERT INTO publication_receipts (source_type, source_id, action, path, to_artifact_id, receipt_state, receipt_data, create_time) VALUES ('page', ?, 'switch_active', ?, ?, 'pending', ?::jsonb, now())",
		pageID, path, artifactID, data).Error; err != nil {
		t.Fatalf("插入回执失败: %v", err)
	}
}

// ---- AR2-002：发布主链必须自己登记回执（修复「回执只实现了恢复端」） ----
//
// 修复前 beginPublishReceipt 零调用者、complete/abort 只被 RecoverPendingPublications
// 自己调用：发布主链无论成功失败都不在 publication_receipts 里留痕，于是启动恢复
// 永远扫不到「正在进行的发布」，只能在人工比对链接与数据库之后才发现状态分裂。
// 下面三条用例把这条接线钉住：登记+结案、窗口故障后收敛、登记失败即中止。

// setPublishWindowFault 注入「访问面已切换、数据库尚未写入」窗口的故障（nil = 清除，
// 等价于进程重启后的干净状态）。运维/测试能力不进服务契约，走类型断言。
func setPublishWindowFault(t *testing.T, svc pagecontract.PageService, fn func() error) {
	t.Helper()
	injector, ok := svc.(interface{ SetPublishWindowFault(func() error) })
	if !ok {
		t.Fatal("页面服务未提供发布窗口故障注入口")
	}
	injector.SetPublishWindowFault(fn)
}

// receiptRouterFailure 只让回执登记失败，其余能力透传给真实契约
// （嵌入真实实现 + 覆写一个方法）：用来验证「登记拿不到 id = 硬失败，不切访问面」。
// 嵌入的必须是真实实现而不是 nil 接口 —— Create 阶段就会调 ReservePath，
// 空嵌入会让它 panic 在一个与用例无关的地方。
type receiptRouterFailure struct {
	pubcontract.PublicationService
	id  string
	err error
}

func (f receiptRouterFailure) BeginPublishReceipt(context.Context, *pubdto.BeginPublishReceiptReq) (string, error) {
	return f.id, f.err
}

// TestPublishMainlineRecordsCommittedReceipt 正常发布必须留下已结案的回执，且 from/to 如实（AR2-002）。
func TestPublishMainlineRecordsCommittedReceipt(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	page := createPage(t, svc, projectID, "/receipt-main", headingDocument)
	buildAndPublish(t, svc, page.ID)

	state, fromID, toID := receiptOf(t, db, "/receipt-main")
	if state != "committed" {
		t.Fatalf("正常发布后回执应结案为 committed，实际 %q", state)
	}
	if fromID != "" {
		t.Fatalf("首次发布的 from 应为空（本语言从未发布），实际 %q", fromID)
	}
	active := activeArtifactID(t, db, page.ID)
	if toID == "" || toID != active {
		t.Fatalf("回执记录的产物应与激活记录一致，回执 %q / 激活 %q", toID, active)
	}

	// 第二次发布：from 必须接上一次的 to —— 两端合起来才是「从哪个版本切到哪个版本」，
	// 只记 to 会让回滚与审计失去「从哪来」的依据。
	detail, derr := svc.Detail(ctx, &pagedto.DetailReq{ProjectID: projectID, ID: page.ID})
	if derr != nil {
		t.Fatalf("查询页面失败: %v", derr)
	}
	if _, serr := svc.SaveDraft(ctx, &pagedto.SaveDraftReq{
		ID: page.ID, ExpectedVersion: detail.DraftVersion, DraftPath: "/receipt-main",
		DraftDocument: json.RawMessage(strings.Replace(headingDocument, "你好", "第二版", 1)),
	}); serr != nil {
		t.Fatalf("保存草稿失败: %v", serr)
	}
	buildAndPublish(t, svc, page.ID)

	state2, fromID2, toID2 := receiptOf(t, db, "/receipt-main")
	if state2 != "committed" {
		t.Fatalf("再次发布后回执应结案为 committed，实际 %q", state2)
	}
	if fromID2 != toID {
		t.Fatalf("第二次发布的 from 应为上一次的 to（%q），实际 %q", toID, fromID2)
	}
	if toID2 == "" || toID2 == fromID2 {
		t.Fatalf("第二次发布应切到新产物，from/to 实际 %q / %q", fromID2, toID2)
	}
	if got := receiptCount(t, db, "/receipt-main"); got != 2 {
		t.Fatalf("每次发布登记一条回执，两次发布应为 2 条，实际 %d", got)
	}
}

// TestPublishCrashBetweenActivateAndDBConvergesOnRecovery 崩在「已切访问面、未写数据库」窗口后，恢复必须收敛（AR2-002）。
//
// 这是 AR2-002 的核心场景：修复前这个窗口里库里没有任何记录，重启不会补齐也不会判定；
// 现在发布链在切换之前就登记了 pending，恢复按符号链接的实际指向补齐数据库（committed）
// 或结案为未生效（rolled_back）—— 不许停在 pending。
func TestPublishCrashBetweenActivateAndDBConvergesOnRecovery(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()

	// 陪跑页面：恢复必须只按回执记录的作用域动，不许把别的页面状态一起改了。
	other := createPage(t, svc, projectID, "/crash-other", headingDocument)
	otherHash := buildAndPublish(t, svc, other.ID)

	page := createPage(t, svc, projectID, "/crash-window", headingDocument)
	built, berr := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID})
	if berr != nil {
		t.Fatalf("构建失败: %v", berr)
	}

	// 故障注入：恰好命中「访问面已切换、数据库尚未写入」窗口（真实崩溃会连进程一起终止）。
	injected := errors.New("故障注入：进程终止于切换访问面之后、写数据库之前")
	setPublishWindowFault(t, svc, func() error { return injected })

	if _, perr := svc.Publish(ctx, &pagedto.PublishReq{ID: page.ID}); perr == nil {
		t.Fatal("窗口故障注入后 Publish 应报错，实际返回成功")
	}

	// 窗口现场：访问面已生效、数据库没跟上、回执留在 pending（而不是被误判为 rolled_back）。
	target, lerr := os.Readlink(filepath.Join(pipeline.ActiveRoot(), "crash-window"))
	if lerr != nil {
		t.Fatalf("访问面符号链接应已建立（切换已发生）: %v", lerr)
	}
	if !strings.Contains(target, built.StagedHash) {
		t.Fatalf("符号链接应指向本次产物 %s，实际 %s", built.StagedHash, target)
	}
	if n := publicationCount(t, db, page.ID); n != 0 {
		t.Fatalf("崩溃窗口里数据库活跃指针不应写入，实际 %d 条", n)
	}
	if got := receiptState(t, db, "/crash-window"); got != "pending" {
		t.Fatalf("窗口回执应保持 pending（交启动恢复判定），实际 %q", got)
	}

	// 进程重启：清掉注入点，跑启动恢复。
	setPublishWindowFault(t, svc, nil)
	recovered, rolledBack, rerr := recoverPending(t, svc, ctx)
	if rerr != nil {
		t.Fatalf("恢复失败: %v", rerr)
	}
	if recovered != 1 || rolledBack != 0 {
		t.Fatalf("应补完成 1 条、回滚 0 条，实际 %d / %d", recovered, rolledBack)
	}
	if got := receiptState(t, db, "/crash-window"); got != "committed" {
		t.Fatalf("恢复后回执应收敛为 committed（不许停在 pending），实际 %q", got)
	}
	if got := activeHashOf(t, db, page.ID); got != built.StagedHash {
		t.Fatalf("恢复应补齐激活记录到本次产物 %s，实际 %q", built.StagedHash, got)
	}

	// 幂等：重复恢复不改结果，也不误改陪跑页面。
	againRecovered, againRolledBack, aerr := recoverPending(t, svc, ctx)
	if aerr != nil {
		t.Fatalf("重复恢复失败: %v", aerr)
	}
	if againRecovered != 0 || againRolledBack != 0 {
		t.Fatalf("重复恢复不应再判定任何回执，实际 %d / %d", againRecovered, againRolledBack)
	}
	if got := receiptState(t, db, "/crash-window"); got != "committed" {
		t.Fatalf("重复恢复不得改变已结案回执，实际 %q", got)
	}
	if got := receiptState(t, db, "/crash-other"); got != "committed" {
		t.Fatalf("陪跑页面回执不得被改动，实际 %q", got)
	}
	if got := activeHashOf(t, db, other.ID); got != otherHash {
		t.Fatalf("陪跑页面激活记录不得被改动，实际 %q（期望 %s）", got, otherHash)
	}
}

// TestPublishAbortsWhenReceiptRegistrationFails 登记拿不到回执 id = 硬失败，不切访问面（AR2-002 第 1 条）。
func TestPublishAbortsWhenReceiptRegistrationFails(t *testing.T) {
	cases := []struct {
		name string
		id   string
		err  error
	}{
		{"登记报错", "", errors.New("回执登记失败（故障注入）")},
		{"登记返回空 id", "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, svc, _, projectID := newPageServiceWithRouter(t, func(real pubcontract.PublicationService) pubcontract.PublicationService {
				return receiptRouterFailure{PublicationService: real, id: tc.id, err: tc.err}
			})
			ctx := context.Background()
			page := createPage(t, svc, projectID, "/receipt-fail", headingDocument)
			if _, berr := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID}); berr != nil {
				t.Fatalf("构建失败: %v", berr)
			}
			if _, perr := svc.Publish(ctx, &pagedto.PublishReq{ID: page.ID}); !errors.Is(perr, pageservice.ErrPublishLedgerUnavailable) {
				t.Fatalf("登记失败应中止发布并返回 ErrPublishLedgerUnavailable，实际 %v", perr)
			}
			// 访问面没被切换：带着「这次发布没登记」的状态去切访问面，正是回执要消灭的分裂。
			if _, lerr := os.Lstat(filepath.Join(pipeline.ActiveRoot(), "receipt-fail")); !os.IsNotExist(lerr) {
				t.Fatalf("登记失败时不得切换访问面，链接状态: %v", lerr)
			}
		})
	}
}

// receiptOf 读该路径最新一条回执的 (状态, from 产物, to 产物)。
func receiptOf(t *testing.T, db *gorm.DB, path string) (state, fromID, toID string) {
	t.Helper()
	var row struct {
		State  string  `gorm:"column:state"`
		FromID *string `gorm:"column:from_id"`
		ToID   *string `gorm:"column:to_id"`
	}
	// 必须按 action 筛：publication_receipts 是共用表，Activate 每次也会按同一 path
	// 登记一条路由回执（其 from 为空），不筛就会把路由回执当成发布回执读出来。
	statement := "SELECT receipt_state AS state, from_artifact_id::text AS from_id, to_artifact_id::text AS to_id " +
		"FROM publication_receipts WHERE path = ? AND action = 'switch_active' ORDER BY id DESC LIMIT 1"
	if err := db.Raw(statement, path).Scan(&row).Error; err != nil {
		t.Fatalf("读取回执失败: %v", err)
	}
	if row.FromID != nil {
		fromID = *row.FromID
	}
	if row.ToID != nil {
		toID = *row.ToID
	}
	return row.State, fromID, toID
}

// receiptCount 统计某路径的回执条数（每次发布登记一条）。
func receiptCount(t *testing.T, db *gorm.DB, path string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM publication_receipts WHERE path = ? AND action = 'switch_active'", path).Scan(&n).Error; err != nil {
		t.Fatalf("统计回执失败: %v", err)
	}
	return n
}

// activeArtifactID 读激活记录里的产物 id。
func activeArtifactID(t *testing.T, db *gorm.DB, pageID string) string {
	t.Helper()
	return activeColumnOf(t, db, pageID, "artifact_id")
}

// activeHashOf 读激活记录里的产物 hash。
func activeHashOf(t *testing.T, db *gorm.DB, pageID string) string {
	t.Helper()
	return activeColumnOf(t, db, pageID, "artifact_hash")
}

func activeColumnOf(t *testing.T, db *gorm.DB, pageID, column string) string {
	t.Helper()
	var row struct {
		Value *string `gorm:"column:value"`
	}
	statement := "SELECT " + column + "::text AS value FROM page_publications WHERE page_id = ?"
	if err := db.Raw(statement, pageID).Scan(&row).Error; err != nil {
		t.Fatalf("读取激活记录失败: %v", err)
	}
	if row.Value == nil {
		return ""
	}
	return *row.Value
}

// publicationCount 统计激活记录条数（崩溃窗口里应为 0）。
func publicationCount(t *testing.T, db *gorm.DB, pageID string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM page_publications WHERE page_id = ?", pageID).Scan(&n).Error; err != nil {
		t.Fatalf("统计激活记录失败: %v", err)
	}
	return n
}

// receiptState 读回执状态。
func receiptState(t *testing.T, db *gorm.DB, path string) string {
	t.Helper()
	var state string
	if err := db.Raw("SELECT receipt_state FROM publication_receipts WHERE path = ? AND action = 'switch_active'", path).Scan(&state).Error; err != nil {
		t.Fatalf("读取回执状态失败: %v", err)
	}
	return state
}
