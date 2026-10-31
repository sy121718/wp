// build_menu_stale.go — menu_stale 的可靠入队（迁移 698）。
//
// 这一条入口的存在理由是「菜单变化必须与触发它的业务写入同事务落库」：
// 写入成功而任务丢了 → 导航改了、产物永远停在旧字节；任务在而写入回滚 → worker
// 重建一份不存在的变化。两种半成品都只能靠同事务消除，所以本方法**只接受**调用方
// 已 scope 好的事务句柄，自己不开事务、不提交、不回滚、不设作用域。
package buildservice

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	builddto "go_wp/internal/module/build/dto"
	buildenums "go_wp/internal/module/build/enums"
	buildmodel "go_wp/internal/module/build/model"
)

// menuKinds 菜单位置白名单（与迁移 698 的 ck_build_job_seq_kind、迁移 285 的
// navigations_kind_check 逐字一致）。
//
// 为什么四个值都要收：桌面 / 移动 × 页眉 / 页脚是四个**独立的数据位置**，
// 构建期依赖登记（ctx.UseMenu(kind)）会产生四值键；只认两值会让移动端菜单的改动
// 静默不入队。
var menuKinds = map[string]struct{}{
	"header":        {},
	"header_mobile": {},
	"footer":        {},
	"footer_mobile": {},
}

// EnqueueMenuStaleTx 在调用方事务里为 (工程, 菜单位置) 分配新版本号并入队一条 menu_stale 任务。
//
// 顺序不可交换：
//  1. 校验句柄是真实事务 —— 没有它就谈不上「同事务」，后面两步都没有意义；
//  2. 校验 kind 与工程 id —— 先于取号：非法输入不该消耗版本号（虽然回滚能撤销，
//     但「参数错却先写了一张序列表」会让失败路径多一次无谓的写入）；
//  3. 物化并复验工程 id —— 空串或非 UUID 一律在这里拒绝，绝不让它进 uuid 列；
//  4. 在 tx 内取号（原子 UPSERT 递增）；
//  5. 以该版本号为分量构造 build_input_hash 并插入任务行。
//
// build_input_hash 由 contentKey + 版本号组成：内容键保证「不同位置互不相干」，
// 版本号保证「同位置的每一次真实变化都是新工作」—— A→B→A 的第三次改动拿到的是
// 第三个号，不会被第一次遗留的 pending / running 任务吞掉。
//
// lang 不写 kind：kind 已经在 source_id 与内容键里，lang 留给构建语言；这里没有
// 语言维度的信息，写空串（= 未冻结语言），绝不用它夹带别的语义。
func (s *Service) EnqueueMenuStaleTx(ctx context.Context, tx *gorm.DB, projectID, kind string) (inputVersion int64, err error) {
	if s == nil || s.m == nil {
		return 0, buildenums.Err(buildenums.ErrInvalidParam)
	}
	kind = strings.TrimSpace(kind)
	if _, ok := menuKinds[kind]; !ok {
		return 0, buildenums.Err(buildenums.ErrInvalidMenuKind)
	}
	sourceID, ok := builddto.MenuStaleSourceID(projectID, kind)
	if !ok {
		return 0, buildenums.Err(buildenums.ErrInvalidParam)
	}
	// 工程 id 已经过 MenuStaleSourceID 的 uuid.Parse，这里取规范形式写 ProjectID 列：
	// 大小写变体进 uuid 列会变成两个工程，而 source_id 用的一定是规范形式。
	//
	// 取出来之后必须**重新验证**（canonicalProjectID）：它接下来会作为字面量参数进
	// build_job_seq 的 uuid 列，而 PG 对 uuid 列不做「空串转 NULL」的宽容处理 —— 逐字
	// 传空串会得到 22P02 invalid input syntax for type uuid: ""，把「参数非法」伪装成
	// 一条数据库内部错误冒到调用方（菜单 CRUD 会因此报「创建导航项失败: ERROR: ...」）。
	canonicalProject, cerr := canonicalProjectID(sourceID)
	if cerr != nil {
		return 0, cerr
	}

	// 取号与插行都在同一个 tx 上：要么两者都提交，要么随调用方一起回滚。
	version, err := s.m.NextMenuStaleVersionTx(tx, canonicalProject, kind)
	if err != nil {
		return 0, err
	}

	e := &buildmodel.Entity{
		SourceType: buildmodel.SourceTypeMenuStale,
		SourceID:   sourceID,
		ProjectID:  &canonicalProject,
		// 空串 = 未冻结语言：menu_stale 的任务按工程内全部启用语言重建，语言在消费侧决定。
		Lang:           "",
		Intent:         buildmodel.IntentDependency,
		DraftVersion:   0,
		BuildInputHash: menuStaleInputHash(canonicalProject, kind, version),
		InputVersion:   &version,
		// 去重语境与 source_id 是同一个串（「工程:位置」）：uq_build_jobs_context_version
		// 按 (context_key, input_version) 建唯一索引，而 source_id 已带位置、按
		// (source_type, source_id) 管 running 互斥。两列都落同一个值不是冗余 —— 前者回答
		// 「同一位置的同一版本只能排一次」，后者回答「同一位置同一时刻只能跑一条」。
		// 迁移 698 的 build_jobs_menu_stale_shape_check 把这条等式钉在库里。
		ContextKey: &sourceID,
		Status:     buildmodel.StatusPending,
		CreateTime: s.now(),
	}
	if cerr := s.m.CreateTx(tx, e); cerr != nil {
		return 0, cerr
	}
	return version, nil
}

// canonicalProjectID 从 menu_stale 的 source_id（「工程:位置」）取回可写入 uuid 列的工程 id。
//
// 两道判据缺一不可：
//   - 拆出来的前缀非空；
//   - 它本身能过 uuid.Parse —— 这个值最终作为字面量绑定到 build_job_seq.project_id
//     （uuid NOT NULL）。只判非空会让 "abc" 这种形状走到 PG 才失败。
//
// 不做「空串归一成 NULL」的兜底：该列在 build_job_seq 上是 NOT NULL，归一只把参数错误
// 推迟成 NOT NULL 违例（23502）。非法输入就该在进入 SQL 之前被拒绝。
func canonicalProjectID(sourceID string) (string, error) {
	projectID, _, ok := builddto.SplitMenuStaleSourceID(sourceID)
	if !ok {
		return "", buildenums.Err(buildenums.ErrInvalidParam)
	}
	if _, err := uuid.Parse(projectID); err != nil {
		return "", buildenums.Err(buildenums.ErrInvalidParam)
	}
	return projectID, nil
}

// menuStaleInputHash menu_stale 的构建输入标识：「内容键#版本号」。
//
// 不用 hash 函数：这里的输入就是一个整数版本号与一个封闭的内容键，
// 直接拼出来比 sha256 更可读（后台列表、排障日志里直接看得懂），
// 而且不会被误当成「内容摘要」去和页面侧的 build_input_hash 比较。
// projectID 必须是规范形式（小写带连字符），否则同一工程会得到两个输入键。
func menuStaleInputHash(projectID, kind string, version int64) string {
	return fmt.Sprintf("menu_stale:%s:%s#%d", projectID, kind, version)
}
