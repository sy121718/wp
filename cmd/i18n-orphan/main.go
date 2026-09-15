// Package main i18n 译文孤儿行的运维命令（审计 I18N-024）。
//
// 为什么是「运维命令」而不是自动任务：审计 I18N-024 的结论是**不做自动清理**。
// 源数据可能只是暂时缺失（页面回滚草稿、实体临时下架、全站扫描超限被跳过），
// 那些时刻译文行看起来就是孤儿，自动删除丢掉的是编辑者逐条人工翻译的成果。
// 所以清理只能由人显式触发，并且必须先看到「检出多少行」再决定要不要删。
//
// 用法（默认只检出不删除）：
//
//	go run ./cmd/i18n-orphan -project <工程uuid>
//	go run ./cmd/i18n-orphan -project <工程uuid> -lang en-US -verbose
//	go run ./cmd/i18n-orphan -project <工程uuid> -keep-file keep.txt   # 追加「源引用消失」判据
//	go run ./cmd/i18n-orphan -project <工程uuid> -apply                # 显式执行删除
//
// 输出固定包含「检出 N 行、清理 M 行」；-verbose 再逐行列出（原文 / 译文 / 判据）。
//
// keep-file 格式（每行一个候选键，UTF-8 文本）：
//
//	<64 位十六进制 hash><空白><context>
//
// 由构建期候选收集 / 工作台全站扫描导出；不提供该文件时只跑「hash 自洽破损」判据
// （见 pkg/i18n/content_orphan.go 的判据定义）。
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"go_wp/config"
	"go_wp/pkg/database"
	"go_wp/pkg/i18n"
)

// options 命令行参数。
type options struct {
	configPath string
	project    string
	lang       string
	keepFile   string
	apply      bool
	verbose    bool
	limit      int
}

func main() {
	opts := options{}
	flag.StringVar(&opts.configPath, "config", "config.yaml", "配置文件路径")
	flag.StringVar(&opts.project, "project", "", "目标工程 id（必填：没有工程就没有孤儿参照系）")
	flag.StringVar(&opts.lang, "lang", "", "目标语言（可空：空 = 该工程全部语言）")
	flag.StringVar(&opts.keepFile, "keep-file", "", "候选保留键清单文件（提供后启用「源引用消失」判据）")
	flag.BoolVar(&opts.apply, "apply", false, "显式执行删除（缺省为只检出的 dry run）")
	flag.BoolVar(&opts.verbose, "verbose", false, "逐行列出检出的孤儿译文")
	flag.IntVar(&opts.limit, "limit", 0, "单次检出上限（0 = 默认 5000）")
	flag.Parse()

	if err := run(opts); err != nil {
		log.Fatalf("i18n 孤儿清理失败: %v", err)
	}
}

// run 实际流程：初始化组件 → 读候选清单 → 检出（必要时删除）→ 输出计数。
func run(opts options) error {
	project := strings.TrimSpace(opts.project)
	if project == "" {
		return errors.New("缺少 -project（孤儿清理必须限定在一个工程内）")
	}
	keep, err := loadKeepFile(opts.keepFile)
	if err != nil {
		return err
	}

	if ierr := config.Init(opts.configPath); ierr != nil {
		return ierr
	}
	defer config.CloseComponents()
	if ierr := config.InitComponents(); ierr != nil {
		return ierr
	}
	db, derr := database.GetDB()
	if derr != nil {
		return derr
	}

	// 默认 dry run：检出即输出，只有 -apply 才真的删。
	writer := i18n.NewContentWriter(db)
	scope := i18n.OrphanScope{ProjectID: project, Lang: opts.lang, Keep: keep, Limit: opts.limit}
	result, perr := writer.PurgeOrphans(context.Background(), scope, opts.apply)
	if perr != nil {
		return perr
	}

	fmt.Printf("工程 %s / 语言 %s\n", project, langLabel(opts.lang))
	fmt.Println(result.OrphanPurgeText())
	if !opts.apply && result.Detected > 0 {
		fmt.Println("（当前是 dry run：确认无误后加 -apply 才会真正删除）")
	}
	if opts.verbose {
		for _, row := range result.Rows {
			fmt.Printf("  [%s] %s\t%s\t%q -> %q\n", row.Reason, row.Lang, row.Context, row.SourceText, row.TargetText)
		}
	}
	return nil
}

// langLabel 语言展示（空 = 全部语言）。
func langLabel(lang string) string {
	if strings.TrimSpace(lang) == "" {
		return "全部语言"
	}
	return lang
}

// loadKeepFile 读取候选保留键清单（路径为空 = 只跑判据 A，返回 nil）。
//
// 行格式：<64 位十六进制 hash><空白><context>。任何不合法行都直接报错而不是跳过 ——
// 静默忽略一部分候选会让它们对应的译文被误判为孤儿后删掉。
func loadKeepFile(path string) (map[string]bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	keep := map[string]bool{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if len(line) < 66 {
			return nil, fmt.Errorf("候选清单第 %d 行过短（期望 <64 位 hash><空白><context>）", lineNo)
		}
		hash := line[:64]
		if !isLowerHex64(hash) {
			return nil, fmt.Errorf("候选清单第 %d 行的 hash 非法（应为 64 位小写十六进制）", lineNo)
		}
		sep := line[64]
		if sep != ' ' && sep != '\t' {
			return nil, fmt.Errorf("候选清单第 %d 行缺少 hash 与 context 之间的空白分隔", lineNo)
		}
		contextName := strings.TrimSpace(line[65:])
		if contextName == "" {
			return nil, fmt.Errorf("候选清单第 %d 行缺少 context", lineNo)
		}
		keep[i18n.ContentIndexKey(hash, contextName)] = true
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(keep) == 0 {
		return nil, errors.New("候选清单为空：空集合会把该工程全部译文判为孤儿，拒绝执行")
	}
	fmt.Printf("候选保留键 %d 条（来自 %s）\n", len(keep), path)
	return keep, nil
}

// isLowerHex64 判断 64 位小写十六进制（与 066 迁移的 CHECK 同口径）。
func isLowerHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') {
			continue
		}
		return false
	}
	return true
}
