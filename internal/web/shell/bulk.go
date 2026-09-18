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

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
)

// MaxBulkIDs 单次批量操作允许的最大 id 数量。
//
// 取值依据：列表页一屏 20 条（pageSize 默认 20），200 允许跨页累积十屏左右；
// 同时把「最坏情况下的逐条数据库往返次数」压在一个不伤库的量级。
const MaxBulkIDs = 200

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
		return nil, fmt.Errorf("一次最多操作 %d 项，当前 %d 项，请分批进行", MaxBulkIDs, len(out))
	}
	return out, nil
}
