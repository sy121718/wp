package productservice

// product_ref_guard.go — 删除守卫的**跨工程**引用检查出口（审计 DB-03 §1.2 / §5.1 第 2 条，PROD-02）。
//
// 这个文件只做三件事：
//
//	① 取工程清单（跨工程扫描的枚举源）；
//	② 把一次扫描结论编成**可定位**的明细文案（哪张表 / 命中多少 / 涉及哪些工程与商品）；
//	③ 命中时的统一错误出口。
//
// 扫描本身在 model（productmodel.CrossProjectRef / ProductRefsByXxx），
// 它按工程逐个设置 RLS 作用域取并集 —— 为什么这样写、换非超级角色后是否仍然成立，
// 见 model/product_ref_scan.go 的文件头（结论：成立，与连接身份无关）。
//
// 口径（AGENTS.md「冲突与数据不一致一律打回给人」）：
//   - **禁止自动清理**：命中即拒绝删除，绝不替调用方解绑；
//   - **禁止静默放过**：拿不到工程清单 / 扫描出错一律向上返回，不用空清单当「没有引用」；
//   - **禁止自动改名 / 加后缀**：错误里列出可定位的数据，由人决定怎么处置。
//
// 为什么明细要卡字节预算：错误经 ?err= 回带到后台页，读侧 shell.FacingNotice 对超过
// shell.NoticeMaxBytes（512）的回执一律判为伪造落归口文案 —— 明细写太长反而会把
// 「可定位信息」整条丢掉（运营看到的从「命中哪个商品」退化成「系统内部错误」）。

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/i18n"
)

// refGuardRefSamples 明细里最多列出的商品样本数。
const refGuardRefSamples = 2

// refDetailBudget 明细部分的字节预算（**当前实现不再逐级收敛**，保留常量说明上限）。
//
// 整条回执 = 业务文案（最长的一条约 60 字节）+ "：" + 明细，必须留在
// shell.NoticeMaxBytes（512）以内。改成「词条骨架 + 纯数据样本」之后不再需要收敛：
// 样本形如 `商品id(SKU)@工程id`（每条约 30 字节），中文词条 ~90 字节 + 两条样本 ~60
// ≈ 150，英文 ≈ 210 —— 都远在上限以内。
const refDetailBudget = 380

// refDetailProjectIDs 明细里最多列出的工程 id 数（工程 id 只出现在商品样本的 `@` 之后，
// 清单本身不再单独列出，数量由词条里的 {projects} 参数给出）。
const refDetailProjectIDs = 2

// crossProjectRefScope 跨工程引用检查的工程枚举（全站工程 id）。
//
// 工程清单来自 project 契约的 List()：product model 不读别的模块的表，
// 这里拿到的 id 列表就是扫描的枚举源。
//
// 拿不到清单时**报错**而不是返回空：空清单会让扫描结论是「没有任何引用」——
// 那正是本次要消灭的静默放过形态。
func (s *Service) crossProjectRefScope(ctx context.Context) (projectIDs []string, err error) {
	if s.project == nil {
		return nil, errors.New("product: 跨工程引用检查需要工程清单，但 project 契约未注入")
	}
	list, err := s.project.List(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(list))
	for _, p := range list {
		if id := strings.TrimSpace(p.ID); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, errors.New("product: 跨工程引用检查拿不到任何工程（projects 为空）")
	}
	return ids, nil
}

// crossProjectRefs 跑一次跨工程引用扫描：工程清单在这里取，守卫只提供扫描函数。
func (s *Service) crossProjectRefs(ctx context.Context, scan func(projectIDs []string) (*productmodel.CrossProjectRef, error)) (*productmodel.CrossProjectRef, error) {
	scope, err := s.crossProjectRefScope(ctx)
	if err != nil {
		return nil, err
	}
	return scan(scope)
}

// crossProjectRefBlocked 命中引用时的统一出口（业务 key + 可定位明细）。
//
// 形态与 ErrRelatedInvalid 一致：key 与明细用全角冒号连接，读侧 productErrText 会把
// key 翻成当前语言、明细原样接在后面；错误识别走 productErrKey 的前缀匹配。
func crossProjectRefBlocked(key string, ref *productmodel.CrossProjectRef) error {
	if detail := refGuardDetail(ref); detail != "" {
		return fmt.Errorf("%s：%s", key, detail)
	}
	return errors.New(key)
}

// refGuardDetail 跨工程引用的可定位明细。
//
// 内容是「引用面（表.列）+ 命中商品数 + 涉及工程数 + 商品样本」，整句由词条
// （productenums.DetailRefGuardBlocked）承载、样本作为纯数据参数传入 ——
// 读侧（productErrText）取词并填 {columns} / {n} / {projects} / {refs}，
// 于是英文界面上这句也是英文。
func refGuardDetail(ref *productmodel.CrossProjectRef) string {
	if !ref.Referenced() {
		return ""
	}
	return i18n.ErrorDetail(productenums.DetailRefGuardBlocked,
		"columns", strings.Join(ref.RefColumns, " + "),
		"n", strconv.Itoa(ref.Total),
		"projects", strconv.Itoa(len(ref.ProjectIDs)),
		"refs", refGuardRefs(ref))
}

// refGuardRefs 命中的商品样本（**纯数据**，人才能定位到哪个工程去解绑）。
//
// 形态 `商品id(SKU)@工程id`：SKU 只在有值时带上（运营按编码找人），存量空串只报 id。
// 「工程」二字不在这里出现 —— 骨架文案（含样本格式的说明）在词条里。
func refGuardRefs(ref *productmodel.CrossProjectRef) string {
	if len(ref.Products) == 0 {
		return ""
	}
	listed := len(ref.Products)
	if listed > refGuardRefSamples {
		listed = refGuardRefSamples
	}
	parts := make([]string, 0, listed)
	for _, p := range ref.Products[:listed] {
		if sku := strings.TrimSpace(p.SKUCode); sku != "" {
			parts = append(parts, p.ProductID+"("+sku+")@"+p.ProjectID)
			continue
		}
		parts = append(parts, p.ProductID+"@"+p.ProjectID)
	}
	return strings.Join(parts, "、")
}
