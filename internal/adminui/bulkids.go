package adminui

// bulkids.go — 批量 id 超限错误的**受控文案出口**（自 internal/shell 迁入的终局归属）。
//
// 为什么在这里而不是各模块自己判：超限文案带两个只有错误类型知道的数字（上限 Max 与
// **去重后**的本次条数 Count）。调用方各自用 bulkids.MaxIDs 重算一遍就丢了 Count，
// 而按当前语言拼前缀再拿 err.Error() 比对又是第二份真相 —— 上游改措辞或换语言即静默失配。
// 判据统一成**类型事实**（*bulkids.IDsError），渲染与判定同源，两种问题一起消失。
//
// 三档行为（与迁入前逐字一致）：
//   · 命中 sentinel（*bulkids.IDsError）→ 按当前语言渲染「一次最多操作 N 项，当前 M 项，请分批进行」，
//     两个数字取自类型字段，**与 err.Error() 的文本无关**（哪怕是 fmt.Errorf("……%w", err) 包装过的，
//     包装进来的上下文也不会出现在文案里）；
//   · 未命中 → 回落 PageInternalText（错误原文只进日志）；
//   · 「未命中也接住」是刻意的：调用方几乎总是把批量读取的错误直接递进来，但判据必须是类型
//     而不是「谁构造的」—— 换成别的错误时应当退回归口文案，而不是把不认识的原文送出去。

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	bulkids "go_wp/pkg/bulkids"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

// bulkIDsTooManyTemplate 超限提示的中文原文（两个 %s：上限、本次条数），
// 与 Error() 的取值共用同一份字面量。
const bulkIDsTooManyTemplate = bulkids.TooManyTemplate

// BulkIDsFacingText 批量 id 读取失败的受控文案出口。
func BulkIDsFacingText(c *gin.Context, err error) string {
	var tooMany *bulkids.IDsError
	if errors.As(err, &tooMany) {
		logger.Scene("adminui").
			With("path", requestURIOf(c)).
			With("user_id", builtin.CurrentUserID(c)).
			With("count", tooMany.Count).
			With("max", tooMany.Max).
			Warn("批量操作被拒：提交的 id 数量超过上限")
		tpl := translateFor(c)(bulkids.MsgTooMany, bulkIDsTooManyTemplate)
		// 词条里若混入 %d 等协议外占位符，Sprintf 会输出 %!d(...) 之类的垃圾 —— 此时用中文原文兜底。
		if !i18n.HasStringPlaceholdersOnly(tpl) {
			tpl = bulkIDsTooManyTemplate
		}
		return fmt.Sprintf(tpl, strconv.Itoa(tooMany.Max), strconv.Itoa(tooMany.Count))
	}
	if err != nil {
		logger.Scene("adminui").
			With("path", requestURIOf(c)).
			Error(err, "批量 id 读取失败：未命中受控错误，只对外给归口文案")
	}
	return PageInternalText(c)
}
