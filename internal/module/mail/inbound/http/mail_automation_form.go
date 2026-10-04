// mail_automation_form.go — 自动化流程编辑页的「步骤表单 ⇄ 图定义」双向换算。
//
// 用户概念只有「触发方式 + 按顺序的步骤」（issue #38 P3 重做）：标识 / 下一步 / yes / no
// 四个输入框从界面消失 —— 用户原话是「新建自动化不知道是个什么东西完全没法用」。
// 换算规则固定在这里，别处不许再有一份：
//
//   - **顺序即执行顺序**：第 i 步的 next 就是第 i+1 步的 key；
//   - **末步接结束节点**：末步不是「结束」时自动补一个（key 优先 end），分支臂选
//     「结束」也指向它；
//   - **入口由服务端补**：入口（trigger）节点的 key 来自隐藏域 entry（新建 n1、编辑带回
//     原值），next 指向第一步。用户只需在基本信息里选触发方式，不需要知道「入口节点」是什么；
//   - **只有条件分支要用户指定跳转**：界面上的「第 N 步 / 结束」在提交时换算回节点 key。
//
// 反向（既有流程 → 步骤行）只在**表单能表达的子集**内成立：主链必须从 trigger 入口出发、
// 每个节点都落在主链上、分支只向后跳、条件只有一条。超出子集（环 / 非 trigger 入口 /
// 有节点走不到 / 多条件分支）**不猜也不丢节点** —— 返回 ok=false，由 handler 走只读兜底，
// 用户看到的是原图 + 一个去画布的入口。
package mailhttp

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	"go_wp/pkg/logger"
)

// automationEntryKeyDefault 新建流程时入口节点的 key（与旧版隐藏域 entry 的默认值一致）。
const automationEntryKeyDefault = "n1"

// automationEndKeyBase 自动补的结束节点 key 基名（被占用时加后缀，见 freeKey）。
const automationEndKeyBase = "end"

// automationStep 表单里的一步 —— 界面概念，一次提交对应一行。
type automationStep struct {
	// Index 步号（1 起）：界面上的「第 N 步」，也是错误文案的定位（不是节点身份）。
	Index int
	// Key 既有步骤的原 key（隐藏域 node_key_N）。新建的步骤留空，由后端按步号生成。
	//
	// 保留 key 是为了既有实例：运行中的实例用 key 记录「走到哪了」，重新保存时换掉 key
	// 会让它们全部找不到当前节点。
	Key string
	// Type 步骤类型（node_type_N）：delay / email / branch / tag / end。
	Type string
	// Param 主参数：模板 key（email）/ 标签串（tag）/ 条件取值（branch）。
	Param string
	// Tag 条件为「带着某个标签」时附带的标签名（param_tag_N）。
	Tag string
	// Unit / Value 等待步骤的单位与时长（param_unit_N / param_unit_value_N）。
	Unit  string
	Value string
	// Yes / No 条件分支两条出边的界面取值：""=下一步、"end"=结束、其余=步号。
	Yes string
	No  string
}

// readAutomationSteps 读表单里的步骤行。
//
// 按固定上界扫 1..maxAutomationNodes：表单只渲染真实存在的步骤，但行号是界面的定位，
// 不能靠「读到空行就停」来猜（隐藏行会错位）。
func readAutomationSteps(c *gin.Context) []automationStep {
	steps := make([]automationStep, 0, maxAutomationNodes)
	for i := 1; i <= maxAutomationNodes; i++ {
		idx := strconv.Itoa(i)
		steps = append(steps, automationStep{
			Index: i,
			Key:   strings.TrimSpace(c.PostForm("node_key_" + idx)),
			Type:  strings.TrimSpace(c.PostForm("node_type_" + idx)),
			Param: strings.TrimSpace(c.PostForm("param_" + idx)),
			Tag:   strings.TrimSpace(c.PostForm("param_tag_" + idx)),
			Unit:  strings.TrimSpace(c.PostForm("param_unit_" + idx)),
			Value: strings.TrimSpace(c.PostForm("param_unit_value_" + idx)),
			Yes:   strings.TrimSpace(c.PostForm("yes_" + idx)),
			No:    strings.TrimSpace(c.PostForm("no_" + idx)),
		})
	}
	return steps
}

// automationStepAction 步骤行的服务端动作（增 / 删 / 上移 / 下移）。
type automationStepAction struct {
	Kind string // add / remove / move_up / move_down
	Step int    // 目标步号（add 不用）
}

// stepActionOf 从提交里识别步骤动作。
//
// 做成服务端动作而不是 JS 直改 DOM：增删一步会改变后面所有步的编号，而「第 N 步」
// 正是错误文案与跳转选择的定位 —— 前端自己改 DOM 但服务端还按旧编号读，用户加的那步
// 提交后就白了。表单只有一个提交地址，动作字段决定分支。
func stepActionOf(c *gin.Context) (automationStepAction, bool) {
	if strings.TrimSpace(c.PostForm("add_step")) != "" {
		return automationStepAction{Kind: "add"}, true
	}
	if v := strings.TrimSpace(c.PostForm("remove_step")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return automationStepAction{Kind: "remove", Step: n}, true
		}
		return automationStepAction{Kind: "remove"}, true
	}
	if v := strings.TrimSpace(c.PostForm("switch_step")); v != "" {
		// 换控件：只看这一行的类型有没有改（类型值本身就在提交里），重渲染即可。
		if n, err := strconv.Atoi(v); err == nil {
			return automationStepAction{Kind: "switch", Step: n}, true
		}
		return automationStepAction{Kind: "switch"}, true
	}
	// 取值形态 `N:up` / `N:down`
	if v := strings.TrimSpace(c.PostForm("move_step")); v != "" {
		parts := strings.SplitN(v, ":", 2)
		if len(parts) != 2 {
			return automationStepAction{}, false
		}
		n, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil {
			return automationStepAction{}, false
		}
		switch strings.TrimSpace(parts[1]) {
		case "up":
			return automationStepAction{Kind: "move_up", Step: n}, true
		case "down":
			return automationStepAction{Kind: "move_down", Step: n}, true
		}
	}
	return automationStepAction{}, false
}

// applyStepAction 把动作作用在当前提交的步骤上。
//
// 空行（用户新加一步却没填）先被剔除：界面上的「第 3 步」指的是**有内容的步骤**，
// 不是表格里的第 3 行 —— 否则删一步会删掉一行空白，用户看到的是「点了没反应」。
func applyStepAction(steps []automationStep, act automationStepAction) []automationStep {
	rows := make([]automationStep, 0, len(steps))
	for _, s := range steps {
		if s.Type == "" && s.Key == "" && s.Param == "" && s.Tag == "" && s.Value == "" {
			continue
		}
		rows = append(rows, s)
	}
	switch act.Kind {
	case "add":
		if len(rows) < maxAutomationNodes {
			rows = append(rows, automationStep{})
		}
	case "remove":
		if act.Step >= 1 && act.Step <= len(rows) {
			rows = append(rows[:act.Step-1], rows[act.Step:]...)
		}
	case "switch":
		// 改类型只需要按新类型重渲染这一行的控件；行列表与顺序都不动。
	case "move_up":
		if i := act.Step - 1; i > 0 && i < len(rows) {
			rows[i-1], rows[i] = rows[i], rows[i-1]
		}
	case "move_down":
		if i := act.Step - 1; i >= 0 && i < len(rows)-1 {
			rows[i], rows[i+1] = rows[i+1], rows[i]
		}
	}
	// 重排后行号重算：行号是界面的定位，不是身份（key 才是身份，且它跟着步骤走）。
	for i := range rows {
		rows[i].Index = i + 1
	}
	return rows
}

// buildAutomationDefinition 把步骤列表换算成引擎的图定义（JSON 原文）。
//
// pos 是既有节点的画布位置（key → X/Y）：保存图定义不该把用户摆好的位置清掉 ——
// 旧实现整份重写定义，改一次流程，画布上所有节点都会回到原点。
func buildAutomationDefinition(tr mailTr, rawEntry string, rows []automationStep, pos map[string][2]float64) ([]byte, error) {
	steps := make([]automationStep, 0, len(rows))
	for _, s := range rows {
		if s.Type == "" {
			continue
		}
		steps = append(steps, s)
	}
	if len(steps) == 0 {
		return nil, errors.New(mailLabel(tr, mailenums.AutomationFormErrStepsRequired))
	}
	if len(steps) > maxAutomationNodes {
		return nil, errors.New(mailLabel(tr, mailenums.AutomationFormErrStepsTooMany))
	}

	// key 分配：先占入口，再给步骤（新建步骤按步号生成 n2 / n3…，避开入口占用的 n1）。
	used := make(map[string]bool, len(steps)+2)
	entryKey := freeKey(strings.TrimSpace(rawEntry), used)
	keys := make([]string, len(steps))
	for i, s := range steps {
		base := strings.TrimSpace(s.Key)
		if base == "" {
			base = fmt.Sprintf("n%d", i+2)
		}
		keys[i] = freeKey(base, used)
	}
	// 结束节点：末步本身就是「结束」时复用它，否则补一个。
	lastIsEnd := steps[len(steps)-1].Type == "end"
	endKey := ""
	if lastIsEnd {
		endKey = keys[len(keys)-1]
	} else {
		endKey = freeKey(automationEndKeyBase, used)
	}

	nodes := make([]any, 0, len(steps)+2)
	nodes = append(nodes, newNode(entryKey, "trigger", nil, keys[0], "", "", pos[entryKey]))
	for i, s := range steps {
		stepNo := i + 1
		params, err := stepParams(tr, s, stepNo)
		if err != nil {
			return nil, err
		}
		next, yes, no := "", "", ""
		switch s.Type {
		case "end":
			// 结束之后再排步骤 = 那些步骤永远走不到（图上不可达，保存会被拒）。
			// 与其让用户看 service 的「有节点从入口走不到: n5」，不如在这里点明步号。
			if stepNo != len(steps) {
				return nil, stepErr(tr, mailenums.AutomationFormErrEndNotLast, stepNo)
			}
		case "branch":
			if yes, err = resolveStepTarget(tr, s.Yes, i, len(steps), keys, endKey, stepNo); err != nil {
				return nil, err
			}
			if no, err = resolveStepTarget(tr, s.No, i, len(steps), keys, endKey, stepNo); err != nil {
				return nil, err
			}
		default:
			if i+1 < len(steps) {
				next = keys[i+1]
			} else {
				next = endKey
			}
		}
		nodes = append(nodes, newNode(keys[i], s.Type, params, next, yes, no, pos[keys[i]]))
	}
	if !lastIsEnd {
		nodes = append(nodes, newNode(endKey, "end", nil, "", "", "", pos[endKey]))
	}

	raw, merr := json.Marshal(map[string]any{"entry": entryKey, "nodes": nodes})
	if merr != nil {
		// 理论上不可达（节点只含字符串 / 整数 / 切片），但这一层的返回值会经
		// mailFormErrText 原样进重定向 —— 所以不把 Go 的原文交出去（判据同 mailErrPageText）。
		logger.Scene(mailErrScene).Error(merr, "邮箱自动化步骤组装失败")
		return nil, errors.New(mailLabel(tr, mailenums.AutomationFormErrAssemble))
	}
	return raw, nil
}

// newNode 组装一个节点（空字段不写进 JSON，保持定义干净）。
func newNode(key, typ string, params map[string]any, next, yes, no string, xy [2]float64) map[string]any {
	n := map[string]any{"key": key, "type": typ}
	if len(params) > 0 {
		n["params"] = params
	}
	if next != "" {
		n["next"] = next
	}
	if yes != "" {
		n["yes"] = yes
	}
	if no != "" {
		n["no"] = no
	}
	// X / Y 是画布布局（引擎忽略）：保留原节点的位置，摆好的画布不会因为改一次流程就散架。
	if xy[0] != 0 || xy[1] != 0 {
		n["x"], n["y"] = xy[0], xy[1]
	}
	return n
}

// freeKey 取一个没被占用的 key：沿用作者原值，为空或冲突时按 base 生成（base_2、base_3…）。
func freeKey(base string, used map[string]bool) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = automationEntryKeyDefault
	}
	if !used[base] {
		used[base] = true
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s_%d", base, i)
		if !used[candidate] {
			used[candidate] = true
			return candidate
		}
	}
}

// stepParams 按类型把界面控件换算成引擎参数，顺带做行级校验（错误带「第 N 步」定位）。
func stepParams(tr mailTr, s automationStep, stepNo int) (map[string]any, error) {
	switch s.Type {
	case "delay":
		unit, ok := waitUnitByValue(s.Unit)
		if !ok {
			return nil, stepErr(tr, mailenums.AutomationFormErrStepDelayUnit, stepNo)
		}
		value, err := strconv.Atoi(s.Value)
		if err != nil || value <= 0 {
			return nil, stepErr(tr, mailenums.AutomationFormErrStepDelayValue, stepNo)
		}
		// 界面单位 → 引擎的 minutes 整数（引擎只认分钟）。
		return map[string]any{"minutes": value * unit.Minutes}, nil
	case "email":
		if s.Param == "" {
			return nil, stepErr(tr, mailenums.AutomationFormErrStepTemplate, stepNo)
		}
		return map[string]any{"template_key": s.Param}, nil
	case "tag":
		tags := splitFormList(s.Param)
		if len(tags) == 0 {
			return nil, stepErr(tr, mailenums.AutomationFormErrStepTag, stepNo)
		}
		return map[string]any{"add": tags}, nil
	case "branch":
		cond, ok := conditionByValue(s.Param)
		if !ok {
			return nil, stepErr(tr, mailenums.AutomationFormErrStepCondition, stepNo)
		}
		code := cond.Value
		if cond.NeedsTag {
			tag := strings.TrimSpace(s.Tag)
			if tag == "" {
				return nil, stepErr(tr, mailenums.AutomationFormErrStepTag, stepNo)
			}
			// 内部语法 has_tag:<标签> 只在这里拼：用户选「带着某个标签」再填标签名。
			code = cond.Value + ":" + tag
		}
		return map[string]any{"conditions": []string{code}}, nil
	case "end", "trigger":
		// 结束节点没有参数；入口节点由服务端补，不来自步骤行（见文件头）。
		return nil, nil
	default:
		return nil, stepErr(tr, mailenums.AutomationFormErrStepTypeRequired, stepNo)
	}
}

// stepErr 组装一条带「第 N 步」定位的错误。
//
// 词条里没有 %d 时**不追加步号**：译文漏了占位符时 fmt 会输出 `%!(EXTRA int=3)`，
// 那比少一个定位更难解释（用户会照抄进搜索框）。
func stepErr(tr mailTr, pair mailenums.LabelPair, stepNo int) error {
	tpl := mailLabel(tr, pair)
	if strings.Contains(tpl, "%d") {
		return errors.New(fmt.Sprintf(tpl, stepNo))
	}
	return errors.New(tpl)
}

// resolveStepTarget 把「下一步 / 第 N 步 / 结束」换算成节点 key。
//
// 只允许向后跳（含下一步）：往前跳会在图上造环，而环在运行期就是无限循环发邮件。
// 这层拦下来，用户就不必去看 service 的「流程里有环: n3 → n2」这种图内部术语。
//
// 两种失败分开报：往回跳时目标步号明明在页面上（措辞必须说「只能往后」），
// 只有目标步被删掉后步号才真的失效（那才叫「不存在」）。
func resolveStepTarget(tr mailTr, raw string, pos, total int, keys []string, endKey string, stepNo int) (string, error) {
	backward := stepErr(tr, mailenums.AutomationFormErrStepTargetBackward, stepNo)
	missing := stepErr(tr, mailenums.AutomationFormErrStepTargetInvalid, stepNo)
	switch raw {
	case "end":
		if endKey == "" {
			return "", missing
		}
		return endKey, nil
	case "":
		if pos+1 < total {
			return keys[pos+1], nil
		}
		if endKey != "" {
			return endKey, nil
		}
		return "", missing
	}
	target, err := strconv.Atoi(raw)
	if err != nil || target > total {
		// 解析不出步号、或步号超出末尾：多半是下拉里那一步刚被删掉。
		return "", missing
	}
	if target <= pos+1 {
		return "", backward
	}
	return keys[target-1], nil
}

// automationStepsFromGraph 把既有流程还原成步骤行（第二返回值 false = 超出表单能表达的子集）。
//
// **不静默丢弃节点**：还原不出来时调用方走只读兜底，把原定义原样留着 —— 把兜底当空流程，
// 用户下一次保存就会把整条流程删成一行。
func automationStepsFromGraph(item *maildto.AutomationItem) ([]automationStep, bool) {
	if item == nil || len(item.Nodes) == 0 {
		return nil, false
	}
	byKey := make(map[string]maildto.AutomationNodeItem, len(item.Nodes))
	for _, n := range item.Nodes {
		key := strings.TrimSpace(n.Key)
		if key == "" {
			return nil, false
		}
		if _, dup := byKey[key]; dup {
			return nil, false
		}
		byKey[key] = n
	}
	entry := strings.TrimSpace(item.Entry)
	head, ok := byKey[entry]
	if !ok || head.Type != "trigger" {
		return nil, false
	}

	// 表单只认一个「结束」目标，多个结束节点在界面上无处区分。
	endKey := ""
	for _, n := range item.Nodes {
		if n.Type != "end" {
			continue
		}
		if endKey != "" {
			return nil, false
		}
		endKey = n.Key
	}

	// 主链：入口 → next → …；条件分支沿「指向后续步骤」的那条出边继续。
	// 顺序分支用 next 表达，分支节点的下一步就是它的 yes（或 no）目标，所以遍历不能只认 next。
	chain := make([]maildto.AutomationNodeItem, 0, len(item.Nodes))
	seen := make(map[string]bool, len(item.Nodes))
	for cur := entry; cur != ""; {
		if seen[cur] {
			return nil, false // 有环：表单里的顺序表达不了
		}
		seen[cur] = true
		n, ok := byKey[cur]
		if !ok {
			return nil, false // 悬空边
		}
		chain = append(chain, n)
		if n.Type == "end" {
			break
		}
		next := strings.TrimSpace(n.Next)
		if n.Type == "branch" {
			forward := make([]string, 0, 2)
			for _, raw := range []string{strings.TrimSpace(n.Yes), strings.TrimSpace(n.No)} {
				target, exist := byKey[raw]
				if !exist {
					return nil, false // 分支必须有两条存在的出边
				}
				if target.Type == "end" {
					continue // 「满足时结束」这类目标不进主链
				}
				if seen[raw] {
					return nil, false // 往前跳（也是环），表单表达不了
				}
				forward = append(forward, raw)
			}
			switch len(forward) {
			case 0:
				next = ""
			case 1:
				next = forward[0]
			default:
				// 两条臂都指向后续步骤：表格的行序把「先执行的那条」排在前，
				// 只有能沿出边走到另一条时才连得上，否则线性表格表达不了。
				switch {
				case graphReaches(byKey, forward[0], forward[1]):
					next = forward[0]
				case graphReaches(byKey, forward[1], forward[0]):
					next = forward[1]
				default:
					return nil, false
				}
			}
		}
		cur = next
	}
	if len(seen) != len(item.Nodes) {
		return nil, false // 有节点不在主链上（分支跳到了链外，或存在不可达节点）
	}
	steps := chain[1:]
	if len(steps) > 0 && steps[len(steps)-1].Type == "end" {
		steps = steps[:len(steps)-1]
	}
	if len(steps) == 0 {
		return nil, false // 「入口即结束」这类流程表单表达不了（留只读态更诚实）
	}

	stepPos := make(map[string]int, len(steps))
	for i, n := range steps {
		stepPos[n.Key] = i + 1
	}

	out := make([]automationStep, 0, len(steps))
	for i, n := range steps {
		stepNo := i + 1
		step := automationStep{Index: stepNo, Key: n.Key, Type: n.Type}
		switch n.Type {
		case "delay":
			minutes, ok := paramInt(n.Params["minutes"])
			if !ok || minutes <= 0 {
				return nil, false
			}
			unit := pickWaitUnit(minutes)
			step.Unit, step.Value = unit.Value, strconv.Itoa(minutes/unit.Minutes)
		case "email":
			step.Param = strOf(n.Params["template_key"])
			if step.Param == "" {
				return nil, false
			}
		case "tag":
			add := strSlice(n.Params["add"])
			// 表单只表达「加标签」：既有流程若还带 remove，还原成步骤行会丢掉那半句。
			if len(add) == 0 || len(strSlice(n.Params["remove"])) > 0 {
				return nil, false
			}
			step.Param = strings.Join(add, ", ")
		case "branch":
			conditions := strSlice(n.Params["conditions"])
			if len(conditions) != 1 {
				return nil, false // 多条件是表单表达不了的（引擎支持，界面不暴露）
			}
			cond, tag, ok := parseConditionCode(conditions[0])
			if !ok {
				return nil, false
			}
			step.Param, step.Tag = cond, tag
			if step.Yes, ok = stepTargetValue(n.Yes, stepPos, endKey, stepNo); !ok {
				return nil, false
			}
			if step.No, ok = stepTargetValue(n.No, stepPos, endKey, stepNo); !ok {
				return nil, false
			}
		default:
			// 中间放「入口」，或引擎将来新增的类型：界面不认识 → 只读兜底。
			return nil, false
		}
		out = append(out, step)
	}
	return out, true
}

// stepTargetValue 节点 key → 界面的跳转取值（""=下一步、"end"=结束、其余=步号）。
//
// 不认识的出边（链外的节点 / 往前跳）返回 false：那些图表单表达不了，必须兜底。
func stepTargetValue(raw string, stepPos map[string]int, endKey string, stepNo int) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false // 分支必须有两条出边；缺一条说明这条流程不是表单建的
	}
	if endKey != "" && raw == endKey {
		return "end", true
	}
	target, ok := stepPos[raw]
	if !ok || target <= stepNo {
		return "", false
	}
	if target == stepNo+1 {
		return "", true // 就是「下一步」，界面默认值
	}
	return strconv.Itoa(target), true
}

// graphReaches 沿出边（分支看 yes/no，其它看 next）判断能否从 from 走到 to。
//
// 只用于给分叉后的两条臂排行序：能走到对方的那条在前，否则表格的线性行序表达不了这张图。
func graphReaches(byKey map[string]maildto.AutomationNodeItem, from, to string) bool {
	queue := []string{from}
	seen := map[string]bool{from: true}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		n, ok := byKey[cur]
		if !ok {
			continue
		}
		for _, next := range outgoingKeys(n) {
			if next == to {
				return true
			}
			if seen[next] {
				continue
			}
			seen[next] = true
			queue = append(queue, next)
		}
	}
	return false
}

func outgoingKeys(n maildto.AutomationNodeItem) []string {
	var out []string
	if n.Type == "branch" {
		for _, raw := range []string{n.Yes, n.No} {
			if raw = strings.TrimSpace(raw); raw != "" {
				out = append(out, raw)
			}
		}
		return out
	}
	if n.Type == "end" {
		return nil
	}
	if next := strings.TrimSpace(n.Next); next != "" {
		out = append(out, next)
	}
	return out
}

// pickWaitUnit 分钟数反解成「数值 + 单位」：优先能整除的大单位（1440 → 1 天）。
//
// 倒序遍历：AutomationWaitUnits 按界面顺序排（分钟在前），正序会先把 1440 拆成 24 小时。
func pickWaitUnit(minutes int) mailenums.AutomationWaitUnitOption {
	units := mailenums.AutomationWaitUnits
	for i := len(units) - 1; i >= 0; i-- {
		if units[i].Minutes > 1 && minutes%units[i].Minutes == 0 {
			return units[i]
		}
	}
	return units[0]
}

// parseConditionCode 引擎的 conditions 编码 →（条件取值, 标签名）。
func parseConditionCode(code string) (value, tag string, ok bool) {
	code = strings.TrimSpace(code)
	for _, c := range mailenums.AutomationConditions {
		if c.Value == code {
			return c.Value, "", true
		}
		if c.NeedsTag && strings.HasPrefix(code, c.Value+":") {
			t := strings.TrimSpace(strings.TrimPrefix(code, c.Value+":"))
			if t == "" {
				return "", "", false
			}
			return c.Value, t, true
		}
	}
	return "", "", false
}

// waitUnitByValue 单位取值 → 选项。
func waitUnitByValue(value string) (mailenums.AutomationWaitUnitOption, bool) {
	for _, u := range mailenums.AutomationWaitUnits {
		if u.Value == value {
			return u, true
		}
	}
	return mailenums.AutomationWaitUnitOption{}, false
}

// conditionByValue 条件取值 → 选项。
func conditionByValue(value string) (mailenums.AutomationConditionOption, bool) {
	for _, c := range mailenums.AutomationConditions {
		if c.Value == value {
			return c, true
		}
	}
	return mailenums.AutomationConditionOption{}, false
}

// paramInt 取 JSONB 解出来的整数（float64 / int / int64）。
func paramInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	}
	return 0, false
}

// trimAutomationSteps 裁掉尾部多余的空步骤。
//
// 表单固定读 12 行（maxAutomationNodes），直接回显会摆出 12 行空表格。**尾部留一个空行**：
// 它是「继续添加」的落点 —— 但只留一个，多出来的都是噪音。
func trimAutomationSteps(steps []automationStep) []automationStep {
	last := len(steps)
	for last > 1 && steps[last-1].Type == "" && steps[last-2].Type == "" {
		last--
	}
	if last <= 0 {
		return []automationStep{{}}
	}
	out := make([]automationStep, last)
	copy(out, steps[:last])
	for i := range out {
		out[i].Index = i + 1
	}
	return out
}

// automationStepRows 步骤行的渲染数据（选项都在 Go 侧生成：Jet 的内层 range 拿不到外层变量）。
func automationStepRows(tr mailTr, steps []automationStep) []gin.H {
	rows := make([]gin.H, 0, len(steps))
	for i, s := range steps {
		stepNo := i + 1
		nextLabel := mailLabel(tr, mailenums.AutomationNextEnd)
		if stepNo < len(steps) {
			nextLabel = fmt.Sprintf(mailLabel(tr, mailenums.AutomationNextStepLabel), stepNo+1)
		}
		rows = append(rows, gin.H{
			"Index": stepNo,
			"Key":   s.Key,
			"Type":  s.Type,
			// 行号不在选项里：类型下拉与行号无关，选中态按行数据判。
			"TypeOptions":      stepTypeOptions(tr, s.Type),
			"Param":            s.Param,
			"Tag":              s.Tag,
			"UnitValue":        s.Value,
			"UnitOptions":      waitUnitOptions(tr, s.Unit),
			"ConditionOptions": conditionOptions(tr, s.Param),
			"Yes":              s.Yes,
			"No":               s.No,
			"YesOptions":       branchTargetOptions(tr, stepNo, s.Yes),
			"NoOptions":        branchTargetOptions(tr, stepNo, s.No),
			"StepLabel":        fmt.Sprintf(mailLabel(tr, mailenums.AutomationStepLabel), stepNo),
			"NextLabel":        nextLabel,
			"IsDelay":          s.Type == "delay",
			"IsEmail":          s.Type == "email",
			"IsBranch":         s.Type == "branch",
			"IsTag":            s.Type == "tag",
			"IsEnd":            s.Type == "end",
			"IsBlank":          s.Type == "",
			"CanMoveUp":        i > 0,
			"CanMoveDown":      i < len(steps)-1,
		})
	}
	return rows
}

// stepTypeOptions 步骤类型下拉。
//
// **不含入口（trigger）**：入口由基本信息里的触发方式决定，用户在中间放一个「入口」
// 没有语义（引擎从 entry 开始跑，中间那个 trigger 节点不会重新触发任何人）。
func stepTypeOptions(tr mailTr, selected string) []gin.H {
	opts := make([]gin.H, 0, len(mailenums.AutomationNodeTypes))
	opts = append(opts, gin.H{"Value": "", "Label": mailLabel(tr, mailenums.AutomationStepNone), "Selected": selected == ""})
	for _, o := range mailenums.AutomationNodeTypes {
		if o.Value == "trigger" {
			continue
		}
		opts = append(opts, gin.H{"Value": o.Value, "Label": mailLabel(tr, o.Label), "Selected": o.Value == selected})
	}
	return opts
}

// waitUnitOptions 等待单位下拉（默认分钟）。
func waitUnitOptions(tr mailTr, selected string) []gin.H {
	if selected == "" {
		selected = mailenums.AutomationWaitUnits[0].Value
	}
	opts := make([]gin.H, 0, len(mailenums.AutomationWaitUnits))
	for _, u := range mailenums.AutomationWaitUnits {
		opts = append(opts, gin.H{"Value": u.Value, "Label": mailLabel(tr, u.Label), "Selected": u.Value == selected})
	}
	return opts
}

// conditionOptions 条件下拉（第一项是空的「请选择判断条件」）。
func conditionOptions(tr mailTr, selected string) []gin.H {
	opts := make([]gin.H, 0, len(mailenums.AutomationConditions)+1)
	opts = append(opts, gin.H{"Value": "", "Label": mailLabel(tr, mailenums.AutomationConditionNone), "Selected": selected == ""})
	for _, c := range mailenums.AutomationConditions {
		opts = append(opts, gin.H{"Value": c.Value, "Label": mailLabel(tr, c.Label), "Selected": c.Value == selected})
	}
	return opts
}

// branchTargetOptions 分支跳转下拉：下一步 / 之后各步 / 结束。
//
// 只列**当前步之后**的步（从 stepNo+2 起：stepNo+1 就是「下一步」）—— 能选的目标与
// resolveStepTarget 的判据必须一致，否则界面上能选、提交却报目标非法。
func branchTargetOptions(tr mailTr, stepNo int, selected string) []gin.H {
	opts := make([]gin.H, 0, maxAutomationNodes+2)
	opts = append(opts, gin.H{"Value": "", "Label": mailLabel(tr, mailenums.AutomationTargetNext), "Selected": selected == ""})
	for k := stepNo + 2; k <= maxAutomationNodes; k++ {
		opts = append(opts, gin.H{
			"Value":    strconv.Itoa(k),
			"Label":    fmt.Sprintf(mailLabel(tr, mailenums.AutomationStepLabel), k),
			"Selected": selected == strconv.Itoa(k),
		})
	}
	opts = append(opts, gin.H{"Value": "end", "Label": mailLabel(tr, mailenums.AutomationTargetEnd), "Selected": selected == "end"})
	return opts
}
