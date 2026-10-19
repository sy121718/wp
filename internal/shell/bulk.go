package shell

// bulk.go — 批量 id 的**壳层残留**（归一与判据都在 pkg/bulkids）。
//
// task-55 已删掉四个纯别名（MaxBulkIDs / ErrBulkIDsTooMany / BulkIDsError / MsgBulkIDsTooMany）：
// 调用方全部直引 go_wp/pkg/bulkids。同值映射与类型别名曾保证旧代码的 errors.Is / errors.As
// 继续成立，但没人用之后它就是「第二份真相」—— 每次迁移都得盯住两份永远同值，收益却是零。
//
// 这一层只剩一件**只有壳层能做**的事：
//   · BulkIDs(c)：唯一还需要 gin 的部分（读表单域 ids），读出来立刻交给 pkg 的纯函数。
//
// BulkIDsFacingText(c, err) 已于 task-151 迁 internal/adminui/bulkids.go：它的终局归属是
// **页面出口**（要 i18n、logger 与归口文案 adminui.PageInternalText），不是 pkg ——
// 18 处调用点同批改名，本包不再导出任何「文案出口」。
//
// BulkIDsNoticeTemplate 已随读侧回执协议整族下线（task-67）：它是给页面 ?err= 读侧做
// 「数字归一后比对」用的模板，而页面写通道改成整页提示与 JSON 写响应后，读侧不再从 query
// 取受控文案，这个导出没有消费方（连它的唯一使用者也一起删掉了）。
//
// NormalizeBulkIDs 这个导出更早已删除：JSON 端点直接 import pkg/bulkids，全仓无生产调用方。
// 新增代码**不要**走这一层：JSON 端点直接 import go_wp/pkg/bulkids。
//
// 超限错误是**带 sentinel 的类型**（bulkids.ErrTooMany / *bulkids.IDsError）：判据是类型事实，
// 不是「读过这个文件的人知道它不会带库表信息」—— 调用方用 errors.Is 判定、用 Count/Max 取值，
// 不必重算文案（重算就是第二份真相，而且拿不到去重后的 Count）。

import (
	"github.com/gin-gonic/gin"

	bulkids "go_wp/pkg/bulkids"
)

// BulkIDs 读取批量操作的表单 id 列表（表单域 ids）—— 唯一留在壳层的原因是需要 gin。
//
// 归一化规则一行都不在这里：交给 pkg/bulkids 的同一个函数，表单与 JSON 两个入口共用一份判据。
// 这属于**过渡转发**：旧页面写路径的调用方
// 遍布十几个模块，等这些页面 POST 逐批下线、调用方归零后，本函数随之删除。
func BulkIDs(c *gin.Context) ([]string, error) {
	return bulkids.Normalize(c.PostFormArray("ids"))
}
