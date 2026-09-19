package architecture

// tx_boundary_scan_test.go — 写路径的事务边界静态扫描（AGENTS.md「写操作的事务与回滚」的配套门禁）。
//
// 判据：一个 service 函数里出现**两处及以上写调用**、却看不到事务标记（Transaction / InProjectScope /
// Begin / ...Tx），就是候选 —— 这类地方中途失败会留下半截状态（真实案例：商品的 Create 先提交商品与变体，
// 随后在另一个事务里建库存行，后者失败就留下「有商品、没有库存行」）。
//
// **它是启发式，不是证明**：识别表按方法名匹配，命名不在表里（Persist / MarkX / attachX / saveX …）会漏，
// 事务标记也可能来自被调用方（例如某个 model 方法自己开了事务）。所以：
//   · 门禁绿 ≠ 事务没问题，评审仍要按 AGENTS.md 的判据人看一遍；
//   · 门禁红 ≠ 一定是 bug，可能是「这几处写天然独立」，那就往允许清单里加一条**写明理由**的豁免。
//
// **一处已实测的盲区（2026-09-19 库存/订单批报回）**：只要函数体里出现任何 Transaction( 调用就算「有事务标记」——
// 于是「主体写一个事务 + 失败后**在另一个事务里**补偿」这种形态会被判为通过。实测四个方法
// （RegisterReceipt / CancelOrder / ReceiveReturn / 建单扣减）就是这么漏过去的：它们的补偿各自包在
// 自己的事务里，扫描器看到标记就放行，而真正的缺陷是**跨模块 DB 补偿**（AGENTS.md 明确禁止）。
// 所以扫描器之后若要加严，方向是「判断同一批写入是否在**同一个**事务标记内」，而不是继续堆方法名。
//
// 允许清单的键是「仓库相对路径#函数名」而不是行号：行号会随改动漂移，用行号做键的清单很快就会腐化。
// 清单里若有条目已经不是候选（方法被包进事务了 / 被删了），测试会失败要求删掉它 —— 清单只增不减就是另一种腐化。

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"go_wp/public/test/support"
)

// writeCalls 一眼能认出是「持久化写入」的方法名（精确匹配）。
var writeCalls = map[string]bool{
	"Create": true, "CreateIn": true, "CreateWithVariants": true, "CreateWithRevision": true,
	"Update": true, "Updates": true, "UpdateFields": true, "Save": true, "SaveConfig": true,
	"Delete": true, "DeleteBy": true, "DeleteByIDs": true, "SoftDelete": true, "Insert": true, "Upsert": true,
	"Replace": true, "ReplaceAll": true, "SetStatus": true, "SetExternalSKU": true,
	"ChangeStock": true, "DeductStock": true, "RecordChanges": true, "RecordChangesTx": true,
	"BindExternalSKU": true, "Publish": true, "Activate": true, "Rotate": true,
	"Bump": true, "Incr": true, "Increment": true,
	// 2026-09-19 补漏：这六个是「策略行 / 子孙行」的写入口，此前不在识别表里 ——
	// RoleCreate / RoleUpdate / RoleDelete（ActivateRole / DeactivateRole / DeleteRoleAllPolicies）、
	// PermUpdate（ReplacePermissionDefinition）、AdminDelete（DeleteUserAllPolicies）、
	// DeptUpdate（UpdateAncestors）改回「两处各自提交」的旧形态时，扫描器一声不响。
	// 现在 service 已改调对应的 …Tx 变体（那种形态带 Tx 后缀，本身就是事务标记），
	// 所以这几个名字再被命中，就等于有人把非事务形态写了回来。
	"ActivateRole": true, "DeactivateRole": true, "ReplacePermissionDefinition": true,
	"DeleteRoleAllPolicies": true, "DeleteUserAllPolicies": true, "UpdateAncestors": true,
	// 2026-09-19 第二批补漏：DeleteByRuleID 是「规则分配行」的写入口。
	// admin 的 RuleDelete 此前是「先逐条删 sys_rule_assignment（各自提交）+ 再删 sys_rule」——
	// DeleteByRuleID 不在表里，扫描器只数得到 DeleteByIDs 一处写，于是这条「第二步失败留下
	// 有规则没分配（数据权限静默放开）」的路径一直是绿的。现在 service 改调 DeleteByRuleIDTx
	//（带 Tx 后缀，本身就是事务标记），所以这个名字再被命中，就等于有人把非事务形态写了回来。
	"DeleteByRuleID": true,
	// 2026-09-19 第三批补漏（事务 burn-down 第一批的实测发现，见 T 批报告第五节）：
	// **门禁此前对 mail 模块整体失明**。原因有两条，都不是 mail 的代码有问题，而是识别表的漏洞：
	//   · Update* / Delete* **不是前缀**（表里只有精确的 Update / Delete / Updates），
	//     所以 UpdateLogResult / UpdateContactFields / DeleteAutomation 这些写方法一个都认不出来；
	//   · Incr*/Add*/Stop* 同理（Incr 只在精确表里，Add/Stop 完全没有）。
	// 后果是：把两处写改回「各自提交」的旧形态，门禁一声不响 —— 修复前它是绿的，修完依然绿。
	// 「门禁绿」因此不能当作事务正确的证明，这只是把盲区补上（漏网名单仍只能靠人看，见文件头）。
	//
	// 这些名字现在都只在 …Tx 变体上出现（service 已改调 Tx 形态，那种写法本身就带 txMark），
	// 所以它们再被命中 = 有人把非事务形态写了回来。
	"UpdateLogResult": true, "UpdateContactFields": true, "UpdateContactStatusByEmail": true,
	"UpdateCampaignFields": true, "UpdateVariantCost": true,
	"UpdateTag": true, "UpdateSource": true, "UpdateAutomation": true,
	"DeleteAutomation": true, "DeleteSource": true, "DeleteByAttachment": true,
	"SetCampaignEventTotalsIfUnset": true, "AddSuppression": true,
	// 2026-09-19 第四批：**小写**的 recordChanges 此前完全不在识别表里（表里的 RecordChanges /
	// RecordChangesTx 是 masterdata 侧的名字）。product 与 product/inventory 的 service 各有一份
	// 「func (s *Service) recordChanges(...)」—— 端口未注入时空转的留痕走法。识别表看不见它的调用，
	// 于是「业务行已提交 + 留痕在另一个事务里」这种形态扫描器一声不响 —— 与 AGENTS.md 明令禁止的
	// 跨模块 DB 补偿同形。调用方现在都走带 Tx 后缀的 recordChangesTx（那种形态本身就是事务标记），
	// 所以这个名字再被命中，就等于有人把非事务形态写了回来。
	"recordChanges": true,
	// 2026-09-19 第五批补漏：五个保留期清理的真写方法（mail 的 DeleteEventsBefore /
	// DeleteLogsBefore / DeleteNodeLogsBefore、analytics 的 DeleteViewsBefore、
	// page 的 DeleteStaleRevisions），此前不在识别表里，所在函数各只有 1 处可识别写，
	// 暂不构成候选 —— 但函数里一旦再加一处写，扫描器就会静默漏掉。
	//
	// **刻意不加 Delete* / Update* 前缀**（与上面 Add* 的实测同因）：前缀会把
	// time.AddDate 一类的同名非持久化方法一起带进来（Add* 实测 17 处噪音）。
	// 代价同样是真实的：其它 Delete 变体（DeleteAutomation 之外的未登记写入口，
	// 如 DeleteXxx 的新命名）仍然漏扫 —— 每次发现都要像这一批一样按**精确名字**
	// 逐个补进来，加完立刻跑一遍看新命中（见 writePrefixes 注释的「反面教训」）。
	"DeleteEventsBefore": true, "DeleteLogsBefore": true, "DeleteNodeLogsBefore": true,
	"DeleteViewsBefore": true, "DeleteStaleRevisions": true,
}

// writePrefixes 前缀命中即视为写入（Ensure* / Create* / Upsert* / Replace* / Mark* / Apply* / Attach*）。
// Incr* / Stop* 是 2026-09-19 第三批补进的前缀（理由见 writeCalls 里那段注释：mail 的
// IncrLogRetry / IncrCampaignCounts / StopRunsOfAutomation 此前一个都不认）。
//
// 实测收窄过两次，都记在这里，免得后来人再踩：
//
//	· **不加 Lock***：加锁读（SELECT … FOR UPDATE）不是写入。把锁算成一处写，会让
//	  「一处写 + 一次加锁读」被误报成两处写 —— 假红会让这个门禁失去信任。
//	· **不加 Add***：`Add` 在服务层是高频非持久化方法名 —— 实测一次就冒出 3 条假条目
//	  （time.Time.AddDate ×2、以及 WaitGroup/计数器式的 Add）。mail 那边真正需要的是
//	  `AddSuppression` 这一个具体名字，放 writeCalls 精确匹配即可。
//
// 反面教训：**前缀是启发式的放大器，一次只放一个、放完立刻看命中**。
// 一次加三个前缀，就得从 10 条噪音里挑出 6 条真信号。
//
// 2026-09-19 第四批补漏：Persist* / Import* / Dispatch* / Purge* / Save*。
// 这一批的真实写入口此前一个都认不出来（Save 只在精确表里，只能认 Save / SaveConfig；
// page 的 persistDependencies*、order 的 persistOrder、mail 的 DispatchCampaign、
// page/mail/analytics 的 PurgeRetention / PurgeExpiredViews 全部漏扫）。
// **加完立刻跑一遍全量命中，逐条核实结果**：
//
//	· Save*：新命中 1 条 —— page/service/page_publish_kernel.go#syncKernel（3 处 SaveDraftInput，
//	  互斥分支，已进允许清单并写明理由）。其余 SaveXxx 调用点要么自带 Tx 后缀、要么在事务标记内。
//	· Persist*：零新命中 —— persistDependencies / persistDependenciesTx / persistDependenciesFromManifest /
//	  persistMultiLangArtifacts / persistOrder 都在事务标记内或本身就是 …Tx 变体。
//	· Dispatch* / Purge*：零新命中（各自所在函数只认出 1 处写，或已有事务标记）。
//	· Import*：当前在 service 目录内**零命中** —— 真实的写入口是 mail 的 ImportContacts，
//	  调用点在 inbound/http（不在扫描范围）。加它是为「service 内互调导入类方法」防漏，
//	  属预防性覆盖，不是已发现的缺陷。
//
// **已知盲区（本轮明确不加，写在这里免得后来人以为覆盖全了）**：
//
//	· Delete* / Update* 仍**不是**前缀（与 Add* 同理：Add 一次就冒出 17 处 time.AddDate 噪音）。
//	  代价是真实的写变体仍在漏 —— 实测 service 目录里有 DeleteEventsBefore / DeleteLogsBefore /
//	  DeleteNodeLogsBefore（mail 保留期）、DeleteViewsBefore（analytics）、DeleteStaleRevisions（page）
//	  这类**真写**方法，一个都不在识别表里。它们所在函数目前多数只有这一处写，所以不产生候选；
//	  一旦有人在这些函数里再加一处写，扫描器同样看不见。
//	· 同样**不加 Claim* / Release***：webhook 的 DeliverDelivery 是「认领 → 出站 HTTP → 落定」的
//	  跨系统边界流程（ClaimDelivery / ReleaseDeliveryClaim / MarkDeliveryResult 三处写**刻意不共事务**：
//	  包进事务会让「投递中」的租约行锁横跨一次网络往返，正是 AGENTS.md 允许补偿/非事务的跨系统场景）。
//	  它现在只被识别出 MarkDeliveryResult 一处写，所以不构成候选；一旦有人给 Claim* / Release* 加前缀，
//	  它就会变成一条**假红** —— 那时的正确动作是往允许清单里写明理由，而不是去「修」它。
//	· 同名前缀的放大效应没变：前台任何一次加前缀，都必须像这一批一样「加完立刻看命中」。
var writePrefixes = []string{"Ensure", "Create", "Upsert", "Replace", "Mark", "Apply", "Attach", "Bulk", "Batch", "Incr", "Stop", "Persist", "Import", "Dispatch", "Purge", "Save"}

// libraryReceivers 是「同名方法挂在标准库/工具包上」的接收方 —— 它们不是持久化写入。
//
// 实测误报：product 的四个 CreateX 里都有 `strings.ReplaceAll(uuid.NewString(), "-", "")` 生成 slug，
// 被 Replace* 前缀命中，于是清单里凭空多了四条「实体 + 关联全量替换」的假条目。
// 判据只看方法名注定会遇到这种同名冲突，所以在这里显式列掉。
var libraryReceivers = map[string]bool{
	"strings": true, "bytes": true, "fmt": true, "strconv": true, "sort": true,
	"time": true, "json": true, "path": true, "filepath": true, "regexp": true,
	"slices": true, "maps": true, "errors": true, "url": true, "uuid": true, "math": true,
}

func isWriteCall(name string) bool {
	if writeCalls[name] {
		return true
	}
	for _, p := range writePrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// txBoundaryAllow 豁免清单：键「仓库相对路径#函数名」→ 放行理由（必须写清楚，这张表是给下一个读代码的人看的）。
//
// 本轮的条目来自 2026-09-19 的全模块事务审计（docs/14 §1.5 与审计报告）：标「已登记」的等对应批次修完就删。
var txBoundaryAllow = map[string]string{
	// —— 已登记待修（2026-09-19 全模块事务审计的高/中优先项，按批处理；修完请删掉对应条目）——
	// admin 权限批的三条已修（PermUpdate / DeptUpdate 从本清单移出，RoleCreate / RoleUpdate 本就不在扫描器识别
	// 范围内 —— 它们的第二处写是 casbin.ActivateRole/DeactivateRole，方法名不在 writeCalls 表里）：
	// 三处都改成「业务行 + 策略行同事务」，见各 service 函数的注释与 public/test/admin/unit/tx_rollback_test.go。
	// page 发布批的三条已修（Publish / Rollback / markProjectStaleForSlot 从本清单移出）：
	// FS 切换仍在事务外，但 DB 各步收进一个事务 + 登记**切换前**的 pending 回执，
	// 恢复例程按回执动作分派补齐（含改 URL 的新形态）；见 page_publish_ledger.go 的
	// recoverableReceiptAction 与 page_publish_recover.go，用例见
	// public/test/page/unit/page_publish_ledger_test.go 与 page_url_rollback_ledger_test.go。
	// 媒体批的两条已修（Upload / GenerateVariants 从本清单移出）：DB 段已收进同一事务，
	// 扫描器不再把它们算作候选（条目过期即失败，所以必须删掉而不是留着）。
	"internal/module/plugin/service/plugin_install.go#Install": "待修（中）：L1 迁移（已提交）+ 落盘 + registry 写入；失败会留孤儿 schema，需巡检或补偿。归插件批",
	// —— 已知可接受（写明「为什么天然独立」，不是待办）——
	"internal/module/build/service/build_worker.go#execute":                         "误报：MarkFailed / MarkSucceeded 是 if/else 互斥分支，同一执行路径只会触发一支，不存在半截状态",
	"internal/module/page/service/page_artifact_rebuild.go#GarbageCollectArtifacts": "可接受：产物文件删除 + MarkPayloadState 逐条记录失败原因，且能按 source_document 重建 —— 幂等可重跑",

	"internal/module/presentation/service/presentation_stale.go#MarkStaleByDependency": "误报：两处写是互斥分支（模板换代只标 template 模式，其余依赖源两种模式都标），单次调用只执行一支；跨工程循环内每工程各一条带 RLS 作用域的 UPDATE，无需合并事务。",
	// —— 2026-09-19 第三批（识别表补漏后新命中的三条，逐条核实结论）——
	// 这三条是「扩了识别表才看得见」的：Update*/Incr* 此前不在表里，它们一个都不会被扫到。
	"internal/module/mail/service/mail_campaign.go#SaveCampaign":  "误报：CreateCampaign 与 UpdateCampaignFields 是**新建 / 更新二选一的互斥分支**（req.ID > 0 走更新、否则走新建），同一次调用只执行一支，不存在半截状态 —— 与 build_worker.go#execute 同形",
	"internal/module/mail/service/mail_campaign.go#StartCampaign": "可接受：两处 UpdateCampaignFields 是「置为发送中」与「降级也失败时退回草稿」的**补偿**，成功路径只写一次；补偿原先被 `_ =` 吞掉，本轮已改为结构化日志留痕（带 campaign_id）。队列是跨系统（Redis/asynq），按 AGENTS.md 的跨库条款处理：补偿幂等（写固定状态值）+ 留痕 + 可在后台重新保存收敛",
	"internal/module/admin/service/admin_login.go#AdminLogin":     "可接受：同一行上的两个**互相独立**的属性 —— 失败计数清零（ResetLoginFailure，原子 UPDATE）与最近登录时间 / IP 戳（Updates）。各自单独成立、没有跨行不变量，中间失败不会被误读（下次登录自然收敛）；失败分支在写之前就 return，两条路径不会同时发生。可以合并成一条 UPDATE，但收益只是少一次往返，不值得为此改动登录代码",

	// —— 2026-09-19 第四批（识别表补漏后新命中的一条，逐条核实结论）——
	// Save* 前缀是本批新加的；它让这个方法第一次被扫到 —— 此前识别表只认精确的 Save / SaveConfig，
	// 看不出 publisher.SaveDraftInput 是一次持久化写入。
	"internal/module/page/service/page_publish_kernel.go#syncKernel": "误报：3 处 SaveDraftInput 落在**互斥分支**里 —— ErrPageNotFound 分支、路径/语言落后分支各自写完就 return，只有两个前置条件都不成立时才走第三处。同一执行路径最多写一次，不存在半截状态 —— 与 build_worker.go#execute（MarkFailed / MarkSucceeded 二选一）同形",

	// —— 2026-09-19 第五批（识别表补进 5 个保留期 Delete 后唯一的新命中，逐条核实结论）——
	"internal/module/mail/service/mail_retention.go#PurgeRetention": "可接受：保留期清理任务 —— 先固化（EnsureCampaignTotals，SetCampaignEventTotalsIfUnset 幂等 IfUnset、单条失败跳过留痕），再按 retention.Task 逐表成批删「早于 cutoff 的历史行」（DeleteEventsBefore / DeleteLogsBefore / DeleteNodeLogsBefore，各表独立任务、按时间阈值成批删、幂等可重跑，部分失败仅告警）。四处写之间没有跨行不变量：固化失败时明确「跳过该活动的固化但继续清理」是设计选择（明细可按保留期内数据重算）；包进一个大事务反而让行锁横跨三张表的成批 DELETE。analytics / page 的同类保留期函数（DeleteViewsBefore / DeleteStaleRevisions 所在函数）各只有 1 处写，本轮不构成候选",
}

func TestServiceWritePathsHaveTransactionBoundary(t *testing.T) {
	root := support.RepoRoot(t)
	serviceRoot := filepath.Join(root, "internal", "module")

	type finding struct{ key, detail string }
	var findings []finding
	seen := map[string]bool{}

	walkErr := filepath.Walk(serviceRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if !strings.Contains(path, string(filepath.Separator)+"service"+string(filepath.Separator)) {
			return nil
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || fn.Recv == nil {
				continue
			}
			var writes []string
			txMark := false
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				name := ""
				switch fun := call.Fun.(type) {
				case *ast.SelectorExpr:
					// strings.ReplaceAll / bytes.Buffer… 这类同名方法不是持久化写入。
					if pkg, ok := fun.X.(*ast.Ident); ok && libraryReceivers[pkg.Name] {
						return true
					}
					name = fun.Sel.Name
				case *ast.Ident:
					name = fun.Name
				default:
					return true
				}
				if strings.Contains(name, "Transaction") || strings.HasSuffix(name, "Tx") || name == "Begin" {
					txMark = true
				}
				if isWriteCall(name) {
					writes = append(writes, name)
				}
				return true
			})
			if len(writes) >= 2 && !txMark {
				key := rel + "#" + fn.Name.Name
				if _, allowed := txBoundaryAllow[key]; allowed {
					seen[key] = true
					continue
				}
				sort.Strings(writes)
				findings = append(findings, finding{key: key, detail: fmt.Sprintf("%d 处写调用且看不到事务标记：%s", len(writes), strings.Join(dedupeStrings(writes), ", "))})
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("扫描失败: %v", walkErr)
	}

	sort.Slice(findings, func(i, j int) bool { return findings[i].key < findings[j].key })
	for _, f := range findings {
		t.Errorf("写路径缺少事务边界：%s（%s）—— 要么把它包进一个事务（service 起事务，tx 透传给 model 与其它模块的 …Tx 方法），要么在 txBoundaryAllow 里写明「这几处写天然独立」的理由。判据见 AGENTS.md「写操作的事务与回滚」", f.key, f.detail)
	}

	var stale []string
	for key := range txBoundaryAllow {
		if !seen[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)
	for _, key := range stale {
		t.Errorf("允许清单里的 %s 已经不是候选了（方法被包进事务 / 被改名 / 被删）—— 请从 txBoundaryAllow 删掉这一条，清单只增不减会掩盖回归", key)
	}
}

func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range in {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
