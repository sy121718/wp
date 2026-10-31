package architecture

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"go_wp/public/test/support"
)

var writeCalls = map[string]bool{
	"Create": true, "CreateIn": true, "CreateWithVariants": true, "CreateWithRevision": true,
	"Update": true, "Updates": true, "UpdateFields": true, "Save": true, "SaveConfig": true,
	"Delete": true, "DeleteBy": true, "DeleteByIDs": true, "SoftDelete": true, "Insert": true, "Upsert": true,
	"Replace": true, "ReplaceAll": true, "SetStatus": true, "SetExternalSKU": true,
	"ChangeStock": true, "DeductStock": true, "RecordChanges": true, "RecordChangesTx": true,
	"BindExternalSKU": true, "Publish": true, "Activate": true, "Rotate": true,
	"Bump": true, "Incr": true, "Increment": true,

	"ActivateRole": true, "DeactivateRole": true, "ReplacePermissionDefinition": true,
	"DeleteRoleAllPolicies": true, "DeleteUserAllPolicies": true, "UpdateAncestors": true,

	"DeleteByRuleID": true,

	"UpdateLogResult": true, "UpdateContactFields": true, "UpdateContactStatusByEmail": true,
	"UpdateCampaignFields": true, "UpdateVariantCost": true,
	"UpdateTag": true, "UpdateSource": true, "UpdateAutomation": true,
	"DeleteAutomation": true, "DeleteSource": true, "DeleteByAttachment": true,
	"SetCampaignEventTotalsIfUnset": true, "AddSuppression": true,

	"recordChanges": true,
}

var writePrefixes = []string{"Ensure", "Create", "Upsert", "Replace", "Mark", "Apply", "Attach", "Bulk", "Batch", "Incr", "Stop", "Persist", "Import", "Dispatch", "Purge", "Save", "Delete", "Update"}

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

var txBoundaryAllow = map[string]string{

	"internal/module/plugin/service/plugin.go#Install": "待修（中）：L1 迁移（已提交）+ 落盘 + registry 写入；失败会留孤儿 schema，需巡检或补偿。归插件批",

	"internal/module/build/service/build_worker.go#execute":                            "误报：MarkFailed / MarkSucceeded 是 if/else 互斥分支，同一执行路径只会触发一支，不存在半截状态",
	"internal/module/page/service/page_publish.go#GarbageCollectArtifacts":             "可接受：产物文件删除（DeleteArtifact，第六批新识别；另有 defer 里的孤儿内容对象回收）+ MarkPayloadState 逐条记录失败原因，且能按 source_document 重建 —— 幂等可重跑",
	"internal/module/presentation/service/presentation_stale.go#MarkStaleByDependency": "误报：两处写是互斥分支（模板换代只标 template 模式，其余依赖源两种模式都标），单次调用只执行一支；跨工程循环内每工程各一条带 RLS 作用域的 UPDATE，无需合并事务。",

	"internal/module/mail/service/mail_campaign.go#SaveCampaign":  "误报：CreateCampaign 与 UpdateCampaignFields 是**新建 / 更新二选一的互斥分支**（req.ID > 0 走更新、否则走新建），同一次调用只执行一支，不存在半截状态 —— 与 build_worker.go#execute 同形",
	"internal/module/mail/service/mail_campaign.go#StartCampaign": "可接受：两处 UpdateCampaignFields 是「置为发送中」与「降级也失败时退回草稿」的**补偿**，成功路径只写一次；补偿原先被 `_ =` 吞掉，本轮已改为结构化日志留痕（带 campaign_id）。队列是跨系统（Redis/asynq），按 AGENTS.md 的跨库条款处理：补偿幂等（写固定状态值）+ 留痕 + 可在后台重新保存收敛",
	"internal/module/admin/service/admin.go#AdminLogin":           "可接受：同一行上的两个**互相独立**的属性 —— 失败计数清零（ResetLoginFailure，原子 UPDATE）与最近登录时间 / IP 戳（Updates）。各自单独成立、没有跨行不变量，中间失败不会被误读（下次登录自然收敛）；失败分支在写之前就 return，两条路径不会同时发生。可以合并成一条 UPDATE，但收益只是少一次往返，不值得为此改动登录代码",

	"internal/module/page/service/page_publish.go#syncKernel": "误报：3 处 SaveDraftInput 落在**互斥分支**里 —— ErrPageNotFound 分支、路径/语言落后分支各自写完就 return，只有两个前置条件都不成立时才走第三处。同一执行路径最多写一次，不存在半截状态 —— 与 build_worker.go#execute（MarkFailed / MarkSucceeded 二选一）同形",

	"internal/module/mail/service/mail_campaign.go#PurgeRetention": "可接受：保留期清理任务 —— 先固化（EnsureCampaignTotals，SetCampaignEventTotalsIfUnset 幂等 IfUnset、单条失败跳过留痕），再按 retention.Task 逐表成批删「早于 cutoff 的历史行」（DeleteEventsBefore / DeleteLogsBefore / DeleteNodeLogsBefore，各表独立任务、按时间阈值成批删、幂等可重跑，部分失败仅告警）。四处写之间没有跨行不变量：固化失败时明确「跳过该活动的固化但继续清理」是设计选择（明细可按保留期内数据重算）；包进一个大事务反而让行锁横跨三张表的成批 DELETE。analytics 的同类保留期函数（DeleteViewsBefore 所在函数）只有 1 处写，本轮不构成候选；page 的同类函数（PurgeRetention）在定时上下线下线后回到单个 retention.Task（page_revisions），同样只有 1 处写",

	"internal/module/admin/service/admin.go#AdminEdit":                                    "可接受：两处是**不同存储边界** —— sys_admin 行更新（Updates，DB）与会话失效（auth.DeleteUserSession，Redis）。Redis 不在数据库事务的覆盖范围内，本函数已按该边界写成「先提交 DB 主操作、再撤会话、撤失败只记结构化日志」，重登 / 再次编辑自然收敛",
	"internal/module/mail/service/mail_account.go#TestSend":                               "误报：两处 UpdateAccountFields 是 sendErr 的 if/else **互斥分支**（成功写 last_check_error=nil、失败写错误原因），同一路径只执行一支；写的是同一行上的「最近检查结果」诊断字段，无跨行不变量",
	"internal/module/mail/service/mail_automation.go#SaveAutomation":                      "误报：CreateAutomation 与 UpdateAutomationFields 是**新建 / 更新二选一的互斥分支**（req.ID > 0 走更新、否则走新建），同一次调用只执行一支 —— 与 mail_campaign.go#SaveCampaign 同形",
	"internal/module/mail/service/mail_automation.go#RunAutomation":                       "误报：两处 UpdateRunFields 落在不同节点类型的分支里（delay 节点写完 waiting 即挂起返回 / email 节点发信失败写 error 后重试），同一次调用只落一支。发信与入队是跨系统边界（不可回滚），本函数按「一步一提交 + node_logs 幂等判重（NodeLogExists）+ 游标先推进」设计，包成一个大事务反而让行锁横跨 SMTP 往返",
	"internal/module/presentation/service/presentation_instance.go#EnsureArchiveInstance": "可接受：**幂等可重跑** —— CreateInstance 对「同实体同角色」自身幂等（presentation_instance.go:51 已有实例即返回），第二处 UpdateURL 只在 slug 变化时把归档页迁到新路径；失败后重跑会走幂等分支再修正。且 CreateInstance 内部含编译 + 发布 + 文件系统激活（跨系统），包进一个 DB 事务不现实",

	"internal/module/product/service/product_translations_page.go#SaveProductTranslations": "可接受（跨模块的派生状态刷新，不是跨行不变量）：两处写在**不同存储边界**上 —— ① 译文落 sys_translation（pkg/i18n 的 ContentWriter，默认库连接），② 页面待重建标记走 page 契约的 MarkStaleForI18n。后者是**自足入口**（自己枚举工程、逐工程在各自的 rls.InProjectScope 事务里提交，见 page/service/page_lang.go），根本没法与外层共用一个事务；它标的是派生状态（pages.stale 一个布尔位），幂等、可重放，写顺序也已按「先落译文、后标重建」摆好 —— 失败只留结构化日志（含 project），不会让译文变成半截（译文要么整批写进去、要么整批被逐行校验挡在门外）。同形的先例就在隔壁：page 模块的翻译工作台（inbound/http/page_translations_handle.go）保存后同样是「译文写入 + MarkStaleForI18n」，只是它还在控制器里、扫描器看不见。真要收口该做的是把这类「保存后扇出重建」统一下沉成 outbox（同 product_invalidation.go 的形态），那是独立一批，不夹在取数下沉里做",
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

	slices.SortFunc(findings, func(a, b finding) int { return cmp.Compare(a.key, b.key) })
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
