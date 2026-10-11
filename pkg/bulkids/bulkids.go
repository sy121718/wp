// Package bulkids — 后台「批量操作」的 id 归一与边界校验（终局归属：纯字符串 + 两个整数）。
//
// 为什么需要它：批量动作（删除 / 状态变更 / 审批）全仓几十处，每处都从「id 列表」起步 ——
// 表单域（POST ids）或 JSON 请求体（[]string）。此前每处都是 `c.PostFormArray("ids")`
// 直接进循环：没有去重、没有数量上限。ids 的长度完全由调用方决定，一次提交上万个 id 会让
// 「逐条走单条路径」的循环膨胀成上万次数据库往返（每次还各自一个事务），既拖垮请求也压垮库。
//
// 上限为什么是「报错」而不是「截断」：静默截断比无界更危险 —— 用户勾了 500 条、只处理了
// 200 条，界面还回带「已处理 200 个」看着像成功，剩下 300 条无声消失。宁可整批拒绝并说清
// 原因，让人分批做。
//
// 超限错误是**带 sentinel 的类型**（ErrTooMany / IDsError），不是 fmt.Errorf 拼出来的字符串：
// 判据于是成为**类型事实**，而不是「读过这个文件的人知道它不会带库表信息」。调用方用
// errors.Is 判定、用 Count/Max 取值。
//
// 本包刻意不引 gin / i18n / logger / templates：它是终局纯工具（internal/shell 的终局是删除，
// 见 scripts/check-shell-migration.sh）。因此这里只有三样东西 —— 归一函数、常量、错误类型；
// 「从表单读 ids」（要 gin）与「按当前语言给受控文案」（要 i18n）都留在各自的调用层。
//
// 形态说明（pkg/CLAUDE.md 的 facade + provider 约定）：本包没有可替换的实现维度 ——
// 它就是一段纯函数与一个值类型，没有 provider/driver 可插拔，也没有需要 Init() 的配置，
// 因此不再套一层根包 facade（那层转发只会是 pkg 内部无谓的间接）。将来若出现第二种
// id 来源协议（例如分页游标式批量），再按 provider 维度拆。
package bulkids

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// MaxIDs 单次批量操作允许的最大 id 数量。
//
// 取值依据：列表页一屏 20 条（pageSize 默认 20），200 允许跨页累积十屏左右；
// 同时把「最坏情况下的逐条数据库往返次数」压在一个不伤库的量级。
const MaxIDs = 200

// ErrTooMany 是批量 id 超限的 sentinel —— 调用方用 errors.Is(err, ErrTooMany) 判定，
// 不要再去匹配文案（文案是给人看的，会随语言与措辞变化）。
var ErrTooMany = errors.New("bulkids: bulk ids exceeded")

// IDsError 批量 id 超限错误。
//
// 值域里只有两个整数：Max（上限）与 Count（去重后的本次条数）。**不塞任何内部细节** ——
// 没有表名、没有 SQLSTATE、没有驱动原文，所以它天然是「可以对外说明」的错误；
// 「这条错误是受控的」因此不再是一句注释，而是这个类型本身的事实。
type IDsError struct {
	Count int
	Max   int
}

// TooManyTemplate 超限提示的中文原文（两个 %s：上限、本次条数）。
//
// 刻意只保留**一份字面量**，供三处共用：① Error() 的取值；② 文案出口在词条缺失时的兜底；
// ③ 读侧的归一模板比对。各写一份就会出现「日志里一句、页面上另一句」的漂移。
const TooManyTemplate = "一次最多操作 %s 项，当前 %s 项，请分批进行"

// Error 实现 error；文本与受控提示同形。
//
// 它服务于日志与「旧式形状判定」（product / page / content / contenttemplate 四个模块仍按
// 「一次最多操作 N 项」这个前缀识别受控提示 —— 换掉前缀会让它们的超限路径静默退化成通用提示）。
// **新代码不要匹配这段文本**：要判定就用 errors.Is(err, ErrTooMany)，要展示就走调用层的受控出口。
func (e *IDsError) Error() string {
	return fmt.Sprintf(TooManyTemplate, strconv.Itoa(e.Max), strconv.Itoa(e.Count))
}

// Unwrap 让 errors.Is(err, ErrTooMany) 成立。
func (e *IDsError) Unwrap() error { return ErrTooMany }

// MsgTooMany 超限提示的 i18n key（词条见迁移 277，中英各一行）。
//
// key 仍沿用 `shell.err.*` 前缀：它已经被 277 seed、且被若干 feature 断言引用，
// 换 key 等于让中英词条与既有断言的语义基线一起漂移 —— 迁移期不动它，
// 等 internal/shell 删除时再随批量路径整体改名。
const MsgTooMany = "shell.err.bulkIdsTooMany"

// Normalize 归一化批量 id 列表：去空白 → 去空值 → 去重（保持首次出现顺序）→ 上限校验。
//
// 批量 id 现在有两个来源：表单（调用方读 PostFormArray 后传进来）与 JSON 请求体（[]string）。
// 规则必须**只有一份** —— 两个入口各复刻一遍，「比较上限用的是去重后的条数」这个细节会先分叉，
// 紧接着是超限提示里的两个数字分叉（调用层重算时拿不到去重后的 Count，这正是本包要消灭的
// 那类第二份真相）。
//
// 返回 error 表示**整批不可执行**（超过 MaxIDs），调用方应回带错误并中止，不要改成部分执行。
// 空列表返回 (nil, nil)，由调用方决定「未选择」的表现。
func Normalize(raw []string) ([]string, error) {
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
	if len(out) > MaxIDs {
		return nil, &IDsError{Count: len(out), Max: MaxIDs}
	}
	return out, nil
}
