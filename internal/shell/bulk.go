package shell

// bulk.go — 后台列表页「批量操作」的公共入口：批量 id 的读取与边界校验。
//
// 为什么要有这个包内单点：批量动作（删除 / 状态变更 / 审批）全仓有 31 处，
// 此前每处都是 `c.PostFormArray("ids")` 直接进循环 —— 没有去重、没有数量上限。
// ids 来自表单，长度完全由调用方决定：一次提交上万个 id 会让「逐条走单条路径」
// 的循环膨胀成上万次数据库往返（每次还各自一个事务），既拖垮请求也压垮库。
//
// 上限为什么是「报错」而不是「截断」：静默截断比无界更危险 —— 用户勾了 500 条、
// 只处理了 200 条，界面还回带「已处理 200 个」看着像成功，剩下 300 条无声消失。
// 宁可整批拒绝并说清原因，让人分批做。
//
// 超限错误是**带 sentinel 的类型**（ErrBulkIDsTooMany / BulkIDsError），不是 fmt.Errorf 拼出来的
// 字符串：判据于是成为**类型事实**，而不是「读过这个文件的人知道它不会带库表信息」。
// 调用方用 errors.Is 判定、用 Count/Max 取值，对外文案只经 BulkIDsFacingText 一个出口 ——
// 这样既不必为「已知受控」写门禁豁免，也不必在模块里用 MaxBulkIDs 重算一遍文案
//（重算就是第二份真相，而且拿不到去重后的 Count）。

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

// MaxBulkIDs 单次批量操作允许的最大 id 数量。
//
// 取值依据：列表页一屏 20 条（pageSize 默认 20），200 允许跨页累积十屏左右；
// 同时把「最坏情况下的逐条数据库往返次数」压在一个不伤库的量级。
const MaxBulkIDs = 200

// ErrBulkIDsTooMany 是批量 id 超限的 sentinel —— 调用方用 errors.Is(err, ErrBulkIDsTooMany) 判定，
// 不要再去匹配文案（文案是给人看的，会随语言与措辞变化）。
var ErrBulkIDsTooMany = errors.New("shell: bulk ids exceeded")

// BulkIDsError 批量 id 超限错误。
//
// 值域里只有两个整数：Max（上限）与 Count（去重后的本次条数）。**不塞任何内部细节** ——
// 没有表名、没有 SQLSTATE、没有驱动原文，所以它天然是「可以对外说明」的错误；
// 「这条错误是受控的」因此不再是一句注释，而是这个类型本身的事实。
type BulkIDsError struct {
	Count int
	Max   int
}

// bulkIDsTooManyTemplate 超限提示的中文原文（两个 %s：上限、本次条数）。
//
// 刻意只保留**一份字面量**，供两处共用：① BulkIDsFacingText 在词条缺失时的兜底模板；
// ② BulkIDsError.Error() 的取值。各写一份就会出现「日志里一句、页面上另一句」的漂移，
// 而消除这类第二份真相正是本批的目的。
const bulkIDsTooManyTemplate = "一次最多操作 %s 项，当前 %s 项，请分批进行"

// Error 实现 error；文本与受控提示同形。
//
// 它服务于日志与「旧式形状判定」（product / page / content / contenttemplate 四个模块仍按
// 「一次最多操作 N 项」这个前缀识别受控提示 —— 换掉前缀会让它们的超限路径静默退化成通用提示）。
// **新代码不要匹配这段文本**：要判定就用 errors.Is(err, ErrBulkIDsTooMany)，
// 要展示就用 BulkIDsFacingText。
func (e *BulkIDsError) Error() string {
	return fmt.Sprintf(bulkIDsTooManyTemplate, strconv.Itoa(e.Max), strconv.Itoa(e.Count))
}

// Unwrap 让 errors.Is(err, ErrBulkIDsTooMany) 成立。
func (e *BulkIDsError) Unwrap() error { return ErrBulkIDsTooMany }

// BulkIDs 读取批量操作的表单 id 列表（表单域 ids）。
//
// 处理顺序：去空白 → 去空值 → 去重（保持首次出现顺序）→ 上限校验。
// 去重是刻意的：ids 由调用方构造，同一个 id 重复提交会让「已删除 N 个」里的 N
// 虚高（同一条被算两次，第二次还多半失败并计入跳过数），而正常勾选不会产生重复。
//
// 返回 error 表示**整批不可执行**（超过 MaxBulkIDs），调用方应回带错误并中止，
// 不要改成部分执行。空列表返回 (nil, nil)，由调用方决定「未选择」的表现。
func BulkIDs(c *gin.Context) ([]string, error) {
	raw := c.PostFormArray("ids")
	if len(raw) == 0 {
		return nil, nil
	}
	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	if len(out) > MaxBulkIDs {
		return nil, &BulkIDsError{Count: len(out), Max: MaxBulkIDs}
	}
	return out, nil
}

// MsgBulkIDsTooMany 超限提示的 i18n key（词条见迁移 277，中英各一行）。
//
// 放在壳层与 MsgInternalError 同理：批量 id 的读取与上限都由 shell 拥有，
// 这条文案的**唯一知情者**只有 shell（去重后的条数在这里才知道）——
// 让各模块自己拼一遍，就会各自成为第二份真相。errors.go 顶部已写明
// 「i18n key 是字符串协议、壳包是通用文案的唯一实现方」这个先例。
const MsgBulkIDsTooMany = "shell.err.bulkIdsTooMany"

// BulkIDsFacingText 批量 id 读取失败的**受控文案出口**。
//
//	· 命中 sentinel（*BulkIDsError）→ 按当前语言返回「一次最多操作 N 项，当前 M 项，请分批进行」，
//	  两个数字取自类型字段，**与 err.Error() 的文本无关**（哪怕是 fmt.Errorf("……%w", err)
//	  包装过的错误，包装进来的上下文也不会出现在文案里）；
//	· 未命中 → 回落 MsgInternalError（错误原文只进日志）。
//
// 「未命中也接住」是刻意的：调用方几乎总是把 shell.BulkIDs 的错误直接递进来，但判据必须是
// 类型而不是「谁构造的」—— 换成别的错误时应当退回归口文案，而不是把不认识的原文送出去。
//
// 为什么只有这一个出口：admin / navigation / inventory 三处此前直传 err.Error() 靠门禁豁免放行，
// mail / order / user 三处则各自用 shell.MaxBulkIDs 重组了同一句话（且丢掉了 Count）。
// 现在对外文案与判定同源，三种问题一起消失。
func BulkIDsFacingText(c *gin.Context, err error) string {
	var tooMany *BulkIDsError
	if errors.As(err, &tooMany) {
		logger.Scene("shell").
			With("path", requestURI(c)).
			With("user_id", CurrentUserID(c)).
			With("count", tooMany.Count).
			With("max", tooMany.Max).
			Warn("批量操作被拒：提交的 id 数量超过上限")
		tpl := TranslateFor(c)(MsgBulkIDsTooMany, bulkIDsTooManyTemplate)
		// 词条里若混入 %d 等协议外占位符，Sprintf 会输出 %!d(...) 之类的垃圾 —— 此时用中文原文兜底。
		if !i18n.HasStringPlaceholdersOnly(tpl) {
			tpl = bulkIDsTooManyTemplate
		}
		return fmt.Sprintf(tpl, strconv.Itoa(tooMany.Max), strconv.Itoa(tooMany.Count))
	}
	if err != nil {
		logger.Scene("shell").
			With("path", requestURI(c)).
			Error(err, "批量 id 读取失败：未命中受控错误，只对外给归口文案")
	}
	return PageInternalText(c)
}

// BulkIDsNoticeTemplate 批量上限提示的**归一模板**（当前语言）。
//
// 给页面**读侧**的受控判定用：?err= 带回来的那句「一次最多操作 200 项，当前 5 项，请分批进行」
// 必须能与本模板比对（数字归一后相等），否则运营看到的是「系统内部错误」，
// 而写侧刚刚才把这条可操作提示放进 URL。
//
// 与 BulkIDsFacingText 取**同一条词条与同一个兜底常量**：这里若另抄一份字面量，
// 词条改动就会让读侧静默失配（提示在写侧可见、到了页面上变成归口文案）。
func BulkIDsNoticeTemplate(c *gin.Context) string {
	return NoticeTemplate(TranslateFor(c)(MsgBulkIDsTooMany, bulkIDsTooManyTemplate))
}
