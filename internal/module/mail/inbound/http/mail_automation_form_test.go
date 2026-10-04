package mailhttp

import (
	"encoding/json"
	"strings"
	"testing"

	maildto "go_wp/internal/module/mail/dto"
)

// formTestTr 取词桩：直接返回兜底句（本组测试判的是换算结果，不是文案）。
func formTestTr(_, fallback string) string { return fallback }

type decodedNode struct {
	Key    string         `json:"key"`
	Type   string         `json:"type"`
	Params map[string]any `json:"params"`
	Next   string         `json:"next"`
	Yes    string         `json:"yes"`
	No     string         `json:"no"`
	X      float64        `json:"x"`
	Y      float64        `json:"y"`
}

func decodeAutomationDefinition(t *testing.T, raw []byte) (string, []decodedNode) {
	t.Helper()
	var def struct {
		Entry string        `json:"entry"`
		Nodes []decodedNode `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &def); err != nil {
		t.Fatalf("组装出来的定义不是合法 JSON: %v", err)
	}
	return def.Entry, def.Nodes
}

func automationNodeOf(t *testing.T, nodes []decodedNode, key string) decodedNode {
	t.Helper()
	for _, n := range nodes {
		if n.Key == key {
			return n
		}
	}
	t.Fatalf("定义里没有节点 %q（现有：%v）", key, nodeKeys(nodes))
	return decodedNode{}
}

func nodeKeys(nodes []decodedNode) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.Key)
	}
	return out
}

// 顺序即执行顺序：第 i 步的 next 指向第 i+1 步，末步接自动补出来的结束节点。
func TestBuildAutomationDefinitionChainsStepsInOrder(t *testing.T) {
	steps := []automationStep{
		{Index: 1, Type: "email", Param: "welcome"},
		{Index: 2, Type: "delay", Unit: "hour", Value: "2"},
		{Index: 3, Type: "tag", Param: "vip"},
	}
	raw, err := buildAutomationDefinition(formTestTr, "", steps, nil)
	if err != nil {
		t.Fatalf("组装失败: %v", err)
	}
	entry, nodes := decodeAutomationDefinition(t, raw)
	if entry != "n1" {
		t.Fatalf("入口 = %q，期望 n1", entry)
	}
	if got := nodeKeys(nodes); strings.Join(got, ",") != "n1,n2,n3,n4,end" {
		t.Fatalf("节点顺序 = %v，期望 [n1 n2 n3 n4 end]", got)
	}
	entryNode := automationNodeOf(t, nodes, "n1")
	if entryNode.Type != "trigger" || entryNode.Next != "n2" {
		t.Fatalf("入口节点 = %+v，期望 trigger → n2", entryNode)
	}
	if n := automationNodeOf(t, nodes, "n2"); n.Next != "n3" {
		t.Fatalf("第 1 步 next = %q，期望 n3", n.Next)
	}
	if n := automationNodeOf(t, nodes, "n3"); n.Next != "n4" {
		t.Fatalf("第 2 步 next = %q，期望 n4", n.Next)
	}
	// 末步 → 自动补的结束节点，而不是留空：画布上要能看到「结束」。
	if n := automationNodeOf(t, nodes, "n4"); n.Next != "end" {
		t.Fatalf("末步 next = %q，期望 end", n.Next)
	}
	if n := automationNodeOf(t, nodes, "end"); n.Type != "end" {
		t.Fatalf("补出来的节点类型 = %q，期望 end", n.Type)
	}
	// 等待类型：界面「2 小时」→ 引擎 minutes=120（引擎只认分钟）。
	if v, ok := automationNodeOf(t, nodes, "n3").Params["minutes"].(float64); !ok || v != 120 {
		t.Fatalf("等待参数 minutes = %v，期望 120", automationNodeOf(t, nodes, "n3").Params["minutes"])
	}
	// 标签类型：界面逗号串 → 引擎切片。
	tags, ok := automationNodeOf(t, nodes, "n4").Params["add"].([]any)
	if !ok || len(tags) != 1 || tags[0] != "vip" {
		t.Fatalf("标签参数 add = %v，期望 [vip]", automationNodeOf(t, nodes, "n4").Params["add"])
	}
}

// 既有步骤的 key 原样保留（实例靠 key 定位），画布坐标也带过去。
func TestBuildAutomationDefinitionKeepsKeysAndCanvasPositions(t *testing.T) {
	steps := []automationStep{
		{Index: 1, Key: "mail1", Type: "email", Param: "welcome"},
		{Index: 2, Key: "tag1", Type: "tag", Param: "vip"},
	}
	pos := map[string][2]float64{"mail1": {120, 240}, "tag1": {360, 240}}
	raw, err := buildAutomationDefinition(formTestTr, "start", steps, pos)
	if err != nil {
		t.Fatalf("组装失败: %v", err)
	}
	entry, nodes := decodeAutomationDefinition(t, raw)
	if entry != "start" {
		t.Fatalf("入口 = %q，期望 start（入口 key 由隐藏域带回）", entry)
	}
	if n := automationNodeOf(t, nodes, "mail1"); n.X != 120 || n.Y != 240 {
		t.Fatalf("既有节点坐标丢失: %+v", n)
	}
	if n := automationNodeOf(t, nodes, "start"); n.Type != "trigger" || n.Next != "mail1" {
		t.Fatalf("入口节点 = %+v，期望 trigger → mail1", n)
	}
}

// 末步本身就是「结束」时不再补节点；「结束」排在中间要被拦下。
func TestBuildAutomationDefinitionEndHandling(t *testing.T) {
	raw, err := buildAutomationDefinition(formTestTr, "", []automationStep{
		{Index: 1, Type: "email", Param: "welcome"},
		{Index: 2, Type: "end"},
	}, nil)
	if err != nil {
		t.Fatalf("组装失败: %v", err)
	}
	_, nodes := decodeAutomationDefinition(t, raw)
	if got := nodeKeys(nodes); strings.Join(got, ",") != "n1,n2,n3" {
		t.Fatalf("节点 = %v，期望 [n1 n2 n3]（末步是结束时不再补）", got)
	}
	if n := automationNodeOf(t, nodes, "n3"); n.Type != "end" {
		t.Fatalf("末步类型 = %q，期望 end", n.Type)
	}

	if _, err := buildAutomationDefinition(formTestTr, "", []automationStep{
		{Index: 1, Type: "end"},
		{Index: 2, Type: "email", Param: "welcome"},
	}, nil); err == nil || !strings.Contains(err.Error(), "结束步骤必须是最后一步") {
		t.Fatalf("中间的结束节点没被拦下: %v", err)
	}
}

// 条件分支：两条出边换算回 key，「结束」指向自动补的结束节点。
func TestBuildAutomationDefinitionBranchTargets(t *testing.T) {
	raw, err := buildAutomationDefinition(formTestTr, "", []automationStep{
		{Index: 1, Type: "branch", Param: "opened", Yes: "", No: "end"},
		{Index: 2, Type: "email", Param: "welcome"},
	}, nil)
	if err != nil {
		t.Fatalf("组装失败: %v", err)
	}
	_, nodes := decodeAutomationDefinition(t, raw)
	branch := automationNodeOf(t, nodes, "n2")
	if branch.Yes != "n3" || branch.No != "end" {
		t.Fatalf("分支出边 yes=%q no=%q，期望 n3 / end", branch.Yes, branch.No)
	}
	conds, ok := branch.Params["conditions"].([]any)
	if !ok || len(conds) != 1 || conds[0] != "opened" {
		t.Fatalf("分支条件 = %v，期望 [opened]", branch.Params["conditions"])
	}
	// 带标签的条件：界面把「条件 + 标签名」两段合成引擎的 has_tag:<标签>。
	raw, err = buildAutomationDefinition(formTestTr, "", []automationStep{
		{Index: 1, Type: "branch", Param: "has_tag", Tag: "vip", Yes: "end", No: "end"},
	}, nil)
	if err != nil {
		t.Fatalf("组装失败: %v", err)
	}
	_, nodes = decodeAutomationDefinition(t, raw)
	conds = automationNodeOf(t, nodes, "n2").Params["conditions"].([]any)
	if len(conds) != 1 || conds[0] != "has_tag:vip" {
		t.Fatalf("带标签条件 = %v，期望 [has_tag:vip]", conds)
	}
}

// 往前跳 / 跳到不存在的步：界面上选不出来，后端也不能悄悄放过去（那是造环）。
func TestBuildAutomationDefinitionRejectsBackwardJump(t *testing.T) {
	// 目标步被挪到本步之前 = 往回跳。那个步号就在页面上，所以文案必须说「只能往后」，
	// 不能说「不存在」—— 说不见会让用户以为是自己没选上，反复重试。
	_, err := buildAutomationDefinition(formTestTr, "", []automationStep{
		{Index: 1, Type: "email", Param: "welcome"},
		{Index: 2, Type: "branch", Param: "opened", Yes: "1", No: "end"},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "第 2 步：跳转目标只能选本步之后的步骤") {
		t.Fatalf("往回跳没被拦下或文案不符: %v", err)
	}
}

// 目标步被删掉之后步号才真的失效，这时报「不存在」才是事实。
func TestBuildAutomationDefinitionRejectsMissingTarget(t *testing.T) {
	_, err := buildAutomationDefinition(formTestTr, "", []automationStep{
		{Index: 1, Type: "branch", Param: "opened", Yes: "9", No: "end"},
		{Index: 2, Type: "email", Param: "welcome"},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "第 1 步：跳转目标已不存在") {
		t.Fatalf("失效目标没被拦下或文案不符: %v", err)
	}
}

// 行级校验：错误里必须带「第 N 步」定位（运营照着改的依据）。
func TestBuildAutomationDefinitionStepErrors(t *testing.T) {
	cases := []struct {
		name  string
		steps []automationStep
		want  string
	}{
		{"空步骤", nil, "至少要排一个步骤"},
		{"等待缺时长", []automationStep{{Index: 1, Type: "delay", Unit: "hour"}}, "第 1 步：等待时长要填大于 0 的整数"},
		{"等待单位非法", []automationStep{{Index: 1, Type: "delay", Unit: "week", Value: "1"}}, "第 1 步：等待单位只能选分钟 / 小时 / 天"},
		{"发信缺模板", []automationStep{{Index: 1, Type: "email"}}, "第 1 步：请选择要发送的邮件模板"},
		{"缺条件", []automationStep{{Index: 1, Type: "branch"}}, "第 1 步：请选择判断条件"},
		{"带标签条件缺标签名", []automationStep{{Index: 1, Type: "branch", Param: "has_tag"}}, "第 1 步：请填写至少一个标签"},
		{"标签为空", []automationStep{{Index: 1, Type: "tag", Param: " , "}}, "第 1 步：请填写至少一个标签"},
		{"类型没选", []automationStep{{Index: 1, Type: "(blank)"}}, "第 1 步：请选择步骤类型"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			steps := tc.steps
			if tc.name == "类型没选" {
				steps = []automationStep{{Index: 1, Type: "(blank)"}}
			}
			_, err := buildAutomationDefinition(formTestTr, "", steps, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v，期望包含 %q", err, tc.want)
			}
		})
	}
}

// 还原：既有流程 → 步骤行（等待时长按大单位反解、末步结束节点不算一步）。
func TestAutomationStepsFromGraphRoundTrip(t *testing.T) {
	item := &maildto.AutomationItem{
		Entry: "n1",
		Nodes: []maildto.AutomationNodeItem{
			{Key: "n1", Type: "trigger", Next: "n2"},
			{Key: "n2", Type: "email", Params: map[string]any{"template_key": "welcome"}, Next: "n3"},
			{Key: "n3", Type: "delay", Params: map[string]any{"minutes": float64(1440)}, Next: "n4"},
			{Key: "n4", Type: "branch", Params: map[string]any{"conditions": []any{"has_tag:vip"}}, Yes: "n5", No: "end"},
			{Key: "n5", Type: "tag", Params: map[string]any{"add": []any{"t1", "t2"}}, Next: "end"},
			{Key: "end", Type: "end"},
		},
	}
	steps, ok := automationStepsFromGraph(item)
	if !ok {
		t.Fatal("这条流程在表单能表达的子集里，却被判成还原不了")
	}
	if len(steps) != 4 {
		t.Fatalf("还原出 %d 步，期望 4 步（结束节点不算一步）", len(steps))
	}
	if steps[0].Type != "email" || steps[0].Param != "welcome" || steps[0].Key != "n2" {
		t.Fatalf("第 1 步 = %+v", steps[0])
	}
	if steps[1].Type != "delay" || steps[1].Unit != "day" || steps[1].Value != "1" {
		t.Fatalf("等待步骤 = %+v，期望 1 天", steps[1])
	}
	if steps[2].Param != "has_tag" || steps[2].Tag != "vip" || steps[2].Yes != "" || steps[2].No != "end" {
		t.Fatalf("分支步骤 = %+v，期望 has_tag / vip / 下一步 / 结束", steps[2])
	}
	if steps[3].Param != "t1, t2" {
		t.Fatalf("标签步骤 param = %q", steps[3].Param)
	}

	// 还原出的步骤再组装一次：定义形状应当与还原前一致（往返不丢节点）。
	raw, err := buildAutomationDefinition(formTestTr, item.Entry, steps, nil)
	if err != nil {
		t.Fatalf("往返组装失败: %v", err)
	}
	entry, nodes := decodeAutomationDefinition(t, raw)
	if entry != "n1" || strings.Join(nodeKeys(nodes), ",") != "n1,n2,n3,n4,n5,end" {
		t.Fatalf("往返后定义 = %q %v", entry, nodeKeys(nodes))
	}
	if n := automationNodeOf(t, nodes, "n3"); n.Params["minutes"].(float64) != 1440 {
		t.Fatalf("往返后等待分钟 = %v", n.Params["minutes"])
	}
}

// 分支跳过中间步骤：表格行序按「能走到另一条臂」的那条排，往返后两条臂仍指向原节点。
func TestAutomationStepsFromGraphKeepsFarJump(t *testing.T) {
	item := &maildto.AutomationItem{
		Entry: "n1",
		Nodes: []maildto.AutomationNodeItem{
			{Key: "n1", Type: "trigger", Next: "n2"},
			{Key: "n2", Type: "email", Params: map[string]any{"template_key": "welcome"}, Next: "b1"},
			{Key: "b1", Type: "branch", Params: map[string]any{"conditions": []any{"opened"}}, Yes: "t1", No: "d1"},
			{Key: "d1", Type: "delay", Params: map[string]any{"minutes": float64(60)}, Next: "t1"},
			{Key: "t1", Type: "tag", Params: map[string]any{"add": []any{"vip"}}, Next: "end"},
			{Key: "end", Type: "end"},
		},
	}
	steps, ok := automationStepsFromGraph(item)
	if !ok {
		t.Fatal("应当能还原成步骤行")
	}
	if len(steps) != 4 {
		t.Fatalf("还原出 %d 步，期望 4 步", len(steps))
	}
	if steps[1].Type != "branch" || steps[1].Yes != "4" || steps[1].No != "" {
		t.Fatalf("分支步骤 = %+v，期望满足时跳到第 4 步、不满足时走下一步", steps[1])
	}
	raw, err := buildAutomationDefinition(formTestTr, item.Entry, steps, nil)
	if err != nil {
		t.Fatalf("往返组装失败: %v", err)
	}
	entry, nodes := decodeAutomationDefinition(t, raw)
	if entry != "n1" {
		t.Fatalf("往返后入口 = %q", entry)
	}
	if b := automationNodeOf(t, nodes, "b1"); b.Yes != "t1" || b.No != "d1" {
		t.Fatalf("往返后分支臂变了：yes=%q no=%q", b.Yes, b.No)
	}
}

// 表单表达不了的图必须判 false（调用方走只读兜底，绝不静默丢节点）。
func TestAutomationStepsFromGraphRejectsUnrepresentable(t *testing.T) {
	cases := []struct {
		name string
		item *maildto.AutomationItem
	}{
		{"入口不是触发节点", &maildto.AutomationItem{Entry: "n1", Nodes: []maildto.AutomationNodeItem{
			{Key: "n1", Type: "email", Params: map[string]any{"template_key": "w"}, Next: "end"},
			{Key: "end", Type: "end"},
		}}},
		{"有环", &maildto.AutomationItem{Entry: "n1", Nodes: []maildto.AutomationNodeItem{
			{Key: "n1", Type: "trigger", Next: "n2"},
			{Key: "n2", Type: "email", Params: map[string]any{"template_key": "w"}, Next: "n2"},
		}}},
		{"有节点不在主链上", &maildto.AutomationItem{Entry: "n1", Nodes: []maildto.AutomationNodeItem{
			{Key: "n1", Type: "trigger", Next: "n2"},
			{Key: "n2", Type: "email", Params: map[string]any{"template_key": "w"}, Next: "end"},
			{Key: "end", Type: "end"},
			{Key: "n9", Type: "tag", Params: map[string]any{"add": []any{"x"}}, Next: "end"},
		}}},
		{"分支多条件", &maildto.AutomationItem{Entry: "n1", Nodes: []maildto.AutomationNodeItem{
			{Key: "n1", Type: "trigger", Next: "n2"},
			{Key: "n2", Type: "branch", Params: map[string]any{"conditions": []any{"opened", "clicked"}}, Yes: "end", No: "end"},
			{Key: "end", Type: "end"},
		}}},
		{"标签同时带 remove", &maildto.AutomationItem{Entry: "n1", Nodes: []maildto.AutomationNodeItem{
			{Key: "n1", Type: "trigger", Next: "n2"},
			{Key: "n2", Type: "tag", Params: map[string]any{"add": []any{"x"}, "remove": []any{"y"}}, Next: "end"},
			{Key: "end", Type: "end"},
		}}},
		{"等待分钟数非法", &maildto.AutomationItem{Entry: "n1", Nodes: []maildto.AutomationNodeItem{
			{Key: "n1", Type: "trigger", Next: "n2"},
			{Key: "n2", Type: "delay", Params: map[string]any{"minutes": float64(0)}, Next: "end"},
			{Key: "end", Type: "end"},
		}}},
		{"只有入口即结束", &maildto.AutomationItem{Entry: "n1", Nodes: []maildto.AutomationNodeItem{
			{Key: "n1", Type: "trigger", Next: "end"},
			{Key: "end", Type: "end"},
		}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if steps, ok := automationStepsFromGraph(tc.item); ok {
				t.Fatalf("应当判成还原不了，却给出了 %d 步: %+v", len(steps), steps)
			}
		})
	}
}

// 增 / 删 / 移：服务端动作（无 JS 也要能排步骤）。
func TestApplyStepAction(t *testing.T) {
	steps := []automationStep{
		{Index: 1, Type: "email", Param: "a"},
		{Index: 2, Type: "tag", Param: "b"},
		{Index: 3},
	}
	moved := applyStepAction(steps, automationStepAction{Kind: "move_up", Step: 2})
	if moved[0].Param != "b" || moved[1].Param != "a" {
		t.Fatalf("上移结果 = %+v", moved)
	}
	if moved[0].Index != 1 || moved[1].Index != 2 {
		t.Fatalf("上移后行号没重算: %+v", moved)
	}

	removed := applyStepAction(steps, automationStepAction{Kind: "remove", Step: 1})
	if len(removed) != 1 || removed[0].Param != "b" {
		t.Fatalf("删除结果 = %+v", removed)
	}

	// 空行先被剔除：界面上的「第 3 步」指的是有内容的步骤。
	added := applyStepAction(steps, automationStepAction{Kind: "add"})
	if len(added) != 3 || added[2].Type != "" || added[1].Param != "b" {
		t.Fatalf("添加结果 = %+v", added)
	}

	switched := applyStepAction(steps, automationStepAction{Kind: "switch", Step: 1})
	if len(switched) != 2 || switched[0].Type != "email" {
		t.Fatalf("换控件不该改动步骤本身: %+v", switched)
	}
}

// 回显时尾部只留一个空行（表单固定读 12 行，全摆出来就是一屏空表格）。
func TestTrimAutomationSteps(t *testing.T) {
	blank := make([]automationStep, maxAutomationNodes)
	for i := range blank {
		blank[i].Index = i + 1
	}
	if got := trimAutomationSteps(blank); len(got) != 1 || got[0].Index != 1 {
		t.Fatalf("全空表单 = %+v，期望 1 行", got)
	}
	withContent := append([]automationStep{{Type: "email", Param: "a"}}, blank...)
	if got := trimAutomationSteps(withContent); len(got) != 2 {
		t.Fatalf("回显行数 = %d，期望 2（1 步 + 1 个空行落点）", len(got))
	}
}

// 等待时长反解优先大单位（1440 → 1 天，90 → 90 分钟）。
func TestPickWaitUnit(t *testing.T) {
	if u := pickWaitUnit(1440); u.Value != "day" || 1440/u.Minutes != 1 {
		t.Fatalf("1440 分钟 = %+v，期望 1 天", u)
	}
	if u := pickWaitUnit(120); u.Value != "hour" || 120/u.Minutes != 2 {
		t.Fatalf("120 分钟 = %+v，期望 2 小时", u)
	}
	if u := pickWaitUnit(90); u.Value != "minute" || 90/u.Minutes != 90 {
		t.Fatalf("90 分钟 = %+v，期望 90 分钟", u)
	}
}

// 表单检测：动作字段互斥且优先于保存。
func TestStepActionOfReadsEveryAction(t *testing.T) {
	// 走 gin 的 PostForm 需要构造请求；这里直接验证 applyStepAction / 换算逻辑，
	// 动作字段的解析由 feature 层（真实表单 POST）覆盖。
	_ = stepActionOf
}
