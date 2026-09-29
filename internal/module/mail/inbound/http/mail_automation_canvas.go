// mail_automation_canvas.go — 自动化流程画布页（issue #38 P4）。
//
// 画布是**增强**，不是替代：表单页（/admin/mail/automation/edit）在没有 JS 时仍然完整可用。
// 所以画布页只做三件事：把图渲染成可拖的节点、画出连线、把位置存回去。
//
// 连线**不在画布上拖**，而是在侧栏用下拉改。这不是妥协，是刻意的：
//
//	· HTML5 drag and drop 在触屏上完全无效；pointer events 能做拖位置，但「从端口拉出一条线」
//	  在触屏上需要长按 + 命中判定，体验与误操作都难控；
//	· 下拉在鼠标 / 触屏 / 键盘下都能用，无障碍也天然达标；
//	· 连线的**真相在图数据里**，画布只是把它画出来 —— 改数据用下拉，看结构用画布。
package mailhttp

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"

	"github.com/gin-gonic/gin"

	mailenums "go_wp/internal/module/mail/enums"
	"go_wp/internal/web/shell"
)

// MailAutomationCanvas 流程画布页（?id=N）。
//
// 缺 id 前置判定见 MailCampaignPage 的说明（审计 P0）：改前 canvas 的「缺参」与「不存在」
// **合成同一句**「自动化流程不存在」—— 没带 id 的调用方会以为流程被删了，而不是自己漏了参数。
func (h *mailPageHandle) MailAutomationCanvas(c *gin.Context) {
	ctx := c.Request.Context()
	id, hasID := mailQueryID(c)
	if !hasID {
		c.Redirect(http.StatusFound, "/admin/mail/automation?err="+urlQueryEscape(mailAutomationIDRequiredText))
		return
	}
	item, err := h.mail.GetAutomation(ctx, id)
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/mail/automation?err="+urlQueryEscape(mailErrPageText(c, err)))
		return
	}
	nodesJSON, merr := json.Marshal(item.Nodes)
	if merr != nil {
		nodesJSON = []byte("[]")
	}
	meta := map[string]any{
		"automationId": item.ID,
		"name":         item.Name,
		"description":  item.Description,
		"triggerType":  item.TriggerType,
		"entry":        item.Entry,
		"status":       item.Status,
		"version":      item.Version,
		// 只有草稿 / 已暂停才允许调结构（启用中的流程改图会让在跑的实例走岔）。
		"structureEditable": item.Status != "active",
	}
	metaJSON, _ := json.Marshal(meta)
	// 发信节点的模板下拉（与表单页同一份数据）。
	templates, _ := h.mail.ListTemplates(ctx, "")
	c.HTML(http.StatusOK, "admin/mail/mail_automation_canvas.html", shell.Prepare(c, gin.H{
		"title":     mailLabel(shell.TranslateFor(c), mailenums.PageTitleAutomationCanvas),
		"A":         item,
		"NodesJSON": shell.JsonSafe(string(nodesJSON)),
		"MetaJSON":  shell.JsonSafe(string(metaJSON)),
		"Templates": templates,
		"jsVer":     automationJsVer(),
		"Err":       mailPageErr(c),
		// 按需内联动效关键帧（本次只用到入场一个）。不声明就是零字节 ——
		// 后台不常驻加载 63 条营销动效，谁用谁声明（见 core.KeyframeCSS）。
		"KeyframesCSS": core.KeyframeCSS([]string{"sky-fade-up"}),
	}))
}

// automationJsVer 自动化编辑器脚本的缓存版本（模块目录下 .js 的最新 mtime）。
//
// 与 workbenchJsVer 同一手法：开发期改 JS 不必手动升版本号，
// 任一模块改动都会让入口 URL 的 ?v= 变化，浏览器不会再执行旧模块。
func automationJsVer() string {
	root := filepath.Join("internal", "templates", "static", "js", "automation")
	var latest int64
	_ = filepath.Walk(root, func(_ string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil // 目录缺失不阻断渲染，版本退化为 0
		}
		if fi.IsDir() || !strings.HasSuffix(fi.Name(), ".js") {
			return nil
		}
		if m := fi.ModTime().Unix(); m > latest {
			latest = m
		}
		return nil
	})
	if latest == 0 {
		return "0"
	}
	return strconv.FormatInt(latest, 10)
}
