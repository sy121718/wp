// Command schema-split 把已有终态数据库切成「一表一文件」的结构描述。
//
// 产物：
//
//	public/schema/base.sql          schema、扩展、触发器函数、sys_baseline 水位表
//	public/schema/tables/<表>.sql   该表的序列、建表、索引、触发器、策略、外键与系统数据
//	public/schema/order.txt         系统数据的装载顺序（父表先于子表）
//
// 每个表文件用 -- @phase pre|ddl|constr|idx|fk|comment|data 分段；运行器按段执行：
// 全部 pre（序列）→ 全部 ddl（建表）→ constr（约束/触发器/策略）→ idx（索引）→
// fk（外键）→ comment（注释）→ 按 order 装载系统数据。
// 生成器只用于首次切分与漂移核对，日常改结构直接改表文件；源库必须是空库载入
// public/schema 后的结果，不能指向业务库（会把业务数据与自定义译文带进结构文件）。
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var monthlyPartition = regexp.MustCompile("_[0-9]{4}_[0-9]{2}$")

type opts struct {
	host, port, user, db, out string
}

func main() {
	host := flag.String("host", envOr("PGHOST", "127.0.0.1"), "数据库主机")
	port := flag.String("port", envOr("PGPORT", "5432"), "数据库端口")
	user := flag.String("user", envOr("PGUSER", "sky"), "数据库用户")
	db := flag.String("db", "", "终态源库（必填：空库载入 public/schema 后的库，不要指向业务库）")
	out := flag.String("out", "public/schema", "输出目录")
	flag.Parse()

	if *db == "" {
		fmt.Fprintln(os.Stderr, "✗ 必须显式指定 -db：源库应是空库载入 public/schema 后的结果，不能默认业务库")
		os.Exit(1)
	}
	o := opts{host: *host, port: *port, user: *user, db: *db, out: *out}
	if err := os.MkdirAll(filepath.Join(o.out, "tables"), 0o755); err != nil {
		fail("创建输出目录失败", err)
	}

	tables, err := listTables(o)
	if err != nil {
		fail("列出表失败", err)
	}

	if err := writeBase(o); err != nil {
		fail("生成 base.sql 失败", err)
	}

	withData := make(map[string]bool, len(tables))
	for _, t := range tables {
		hasData, err := writeTable(o, t)
		if err != nil {
			fail("生成表文件失败: "+t, err)
		}
		if hasData {
			withData[t] = true
		}
	}

	order := loadOrder(o, tables, withData)
	if err := os.WriteFile(filepath.Join(o.out, "order.txt"), []byte(strings.Join(order, "\n")+"\n"), 0o644); err != nil {
		fail("写 order.txt 失败", err)
	}

	fmt.Printf("✓ 切分完成：%d 张表，%d 张有系统数据，输出 %s\n", len(tables), len(order), o.out)
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func fail(prefix string, err error) {
	fmt.Fprintf(os.Stderr, "✗ %s：%v\n", prefix, err)
	os.Exit(1)
}

func (o opts) dump(extra ...string) (string, error) {
	args := []string{"-h", o.host, "-p", o.port, "-U", o.user, "-d", o.db}
	args = append(args, extra...)
	cmd := exec.Command("pg_dump", args...)
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

func (o opts) query(sql string) (string, error) {
	cmd := exec.Command("psql", "-h", o.host, "-p", o.port, "-U", o.user, "-d", o.db, "-Atc", sql)
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

func listTables(o opts) ([]string, error) {
	raw, err := o.query("SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace\n" +
		"WHERE n.nspname = 'public' AND c.relkind IN ('r','p') AND c.relname <> 'sys_baseline'\n" +
		"ORDER BY c.relname")
	if err != nil {
		return nil, err
	}
	var tables []string
	for _, line := range strings.Split(raw, "\n") {
		name := strings.TrimSpace(line)
		if name == "" || monthlyPartition.MatchString(name) {
			continue
		}
		tables = append(tables, name)
	}
	return tables, nil
}

// cleanup 去掉 pg_dump 的环境噪声与所有权声明，返回从第一条语句开始的正文。
func cleanup(dump string) string {
	lines := strings.Split(strings.ReplaceAll(dump, "\r\n", "\n"), "\n")
	start := -1
	for i, line := range lines {
		if hasStatementPrefix(strings.TrimSpace(line)) {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}
	out := make([]string, 0, len(lines)-start)
	for _, line := range lines[start:] {
		t := strings.TrimSpace(line)
		switch {
		case t == "":
			out = append(out, "")
		case strings.HasPrefix(t, "\\restrict"), strings.HasPrefix(t, "\\unrestrict"):
			continue
		case strings.HasPrefix(t, "SET "):
			continue
		case strings.HasPrefix(t, "SELECT pg_catalog.set_config("):
			continue
		case strings.HasPrefix(t, "ALTER TABLE") && strings.Contains(t, " OWNER TO "):
			continue
		case strings.HasPrefix(t, "ALTER SEQUENCE") && strings.Contains(t, " OWNER TO "):
			continue
		case strings.HasPrefix(t, "--"):
			continue
		default:
			out = append(out, line)
		}
	}
	var b strings.Builder
	blank := false
	for _, line := range out {
		if strings.TrimSpace(line) == "" {
			if b.Len() == 0 || blank {
				continue
			}
			blank = true
			b.WriteString("\n")
			continue
		}
		blank = false
		b.WriteString(line)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func hasStatementPrefix(t string) bool {
	for _, p := range []string{"CREATE ", "ALTER ", "COMMENT ", "INSERT INTO ", "SELECT pg_catalog.setval", "COPY "} {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}

func chunks(text string) []string {
	var out []string
	for _, part := range strings.Split(text, "\n\n") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// phaseOf 按语句形态判定执行段。段序必须复现 pg_dump 的依赖顺序：
// 建表/挂分区 → 主键与唯一约束 → 索引与索引挂载 → 外键。
// 反例：分区子表先建主键再挂载会被父表的自动索引挡成 multiple primary keys。
func phaseOf(chunk string) string {
	upper := strings.ToUpper(chunk)
	switch {
	case strings.HasPrefix(upper, "CREATE SEQUENCE"):
		return "pre"
	case strings.Contains(upper, "ADD CONSTRAINT") && strings.Contains(upper, "FOREIGN KEY"):
		return "fk"
	case strings.Contains(upper, "ADD CONSTRAINT"):
		return "constr"
	case strings.HasPrefix(upper, "CREATE INDEX"), strings.Contains(upper, "ATTACH PARTITION") && strings.HasPrefix(upper, "ALTER INDEX"):
		return "idx"
	case strings.HasPrefix(upper, "COMMENT ON "):
		// 注释可指向索引、约束、触发器、策略，必须等这些对象都建好。
		return "comment"
	default:
		return "ddl"
	}
}

func writeBase(o opts) error {
	structDump, err := o.dump("--schema-only", "--table=public.sys_baseline")
	if err != nil {
		return err
	}
	var pre, ddl []string
	for _, ch := range chunks(cleanup(structDump)) {
		if strings.HasPrefix(strings.ToUpper(ch), "CREATE SEQUENCE") {
			pre = append(pre, ch)
			continue
		}
		ddl = append(ddl, ch)
	}

	funcs, err := listFunctions(o)
	if err != nil {
		return err
	}
	if len(funcs) == 0 {
		return fmt.Errorf("库里没有找到触发器函数")
	}

	var b strings.Builder
	b.WriteString("-- 基础对象：schema、扩展、触发器函数与初始化水位表。\n")
	b.WriteString("-- 由 cmd/schema-split 生成；新增全局对象时改本文件。\n\n")
	b.WriteString("CREATE SCHEMA IF NOT EXISTS ext_shared;\n\n")
	b.WriteString("CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA ext_shared;\n\n")
	b.WriteString(strings.Join(funcs, "\n\n"))
	b.WriteString("\n\n")
	b.WriteString(strings.Join(pre, "\n\n"))
	if len(pre) > 0 {
		b.WriteString("\n\n")
	}
	b.WriteString(strings.Join(ddl, "\n\n"))
	b.WriteString("\n")
	return os.WriteFile(filepath.Join(o.out, "base.sql"), []byte(b.String()), 0o644)
}

// listFunctions 用 pg_get_functiondef 取完整函数定义；按空行切分会被函数体里的
// 空行截断，必须让服务端回显整条语句。
func listFunctions(o opts) ([]string, error) {
	raw, err := o.query("SELECT p.proname FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace " +
		"WHERE n.nspname = 'public' AND p.prokind = 'f' ORDER BY p.proname")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, name := range strings.Split(raw, "\n") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		def, err := o.query("SELECT pg_get_functiondef(p.oid) FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace " +
			"WHERE n.nspname = 'public' AND p.proname = '" + strings.ReplaceAll(name, "'", "''") + "'")
		if err != nil {
			return nil, fmt.Errorf("读取函数 %s 失败: %w", name, err)
		}
		if def != "" {
			// pg_get_functiondef 回显的语句不带结尾分号，必须补上，否则下一条 CREATE 会被粘住。
			out = append(out, strings.TrimRight(def, " \n\t")+";")
		}
	}
	return out, nil
}

func writeTable(o opts, table string) (bool, error) {
	schemaDump, err := o.dump("--schema-only", "--table=public."+table)
	if err != nil {
		return false, err
	}
	sections := map[string][]string{}
	for _, ch := range chunks(cleanup(schemaDump)) {
		phase := phaseOf(ch)
		sections[phase] = append(sections[phase], ch)
	}

	dataDump, err := o.dump("--data-only", "--inserts", "--rows-per-insert=100", "--on-conflict-do-nothing", "--table=public."+table)
	if err != nil {
		return false, err
	}
	data := cleanup(dataDump)

	var b strings.Builder
	fmt.Fprintf(&b, "-- table: %s（由 cmd/schema-split 生成；改结构直接改本文件）\n", table)
	writePhase := func(name string, parts []string) {
		if len(parts) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n-- @phase %s\n%s\n", name, strings.Join(parts, "\n\n"))
	}
	for _, phase := range []string{"pre", "ddl", "constr", "idx", "fk", "comment"} {
		writePhase(phase, sections[phase])
	}
	if data != "" {
		fmt.Fprintf(&b, "\n-- @phase data\n%s\n", data)
	}
	path := filepath.Join(o.out, "tables", table+".sql")
	return data != "", os.WriteFile(path, []byte(b.String()), 0o644)
}

// loadOrder 复用现有系统数据的装载顺序：父表必须先于引用它的子表。
func loadOrder(o opts, tables []string, withData map[string]bool) []string {
	have := make(map[string]bool, len(tables))
	for _, t := range tables {
		have[t] = true
	}
	var ordered []string
	seen := make(map[string]bool, len(tables))
	add := func(name string) {
		if have[name] && withData[name] && !seen[name] {
			seen[name] = true
			ordered = append(ordered, name)
		}
	}

	// 已有 order.txt 是唯一装载顺序来源：切分器重跑时必须保持它，
	// 否则父表/子表或触发器写入顺序会在无提示的情况下改变。
	if raw, err := os.ReadFile(filepath.Join(o.out, "order.txt")); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			add(strings.TrimSpace(line))
		}
	}

	var rest []string
	for _, t := range tables {
		if withData[t] && !seen[t] {
			rest = append(rest, t)
		}
	}
	sort.Strings(rest)
	ordered = append(ordered, rest...)

	// sys_menus 的插入触发器会写 sys_menu_permission；它必须先装，
	// 否则显式权限行会先占满序列，触发器再空推一轮 nextval。
	return ensureBefore(ordered, "sys_menus", "sys_menu_permission")
}

func ensureBefore(order []string, before, after string) []string {
	bi, ai := -1, -1
	for i, name := range order {
		switch name {
		case before:
			bi = i
		case after:
			ai = i
		}
	}
	if bi < 0 || ai < 0 || bi < ai {
		return order
	}
	out := make([]string, 0, len(order))
	for _, name := range order {
		if name == before {
			continue
		}
		if name == after {
			out = append(out, before)
		}
		out = append(out, name)
	}
	return out
}
