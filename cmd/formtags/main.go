// Command formtags 给 DTO 请求结构补 form tag（与既有 json tag 同名）。
//
// 为什么需要它：写表单发的是 formdata，而服务端 `binding.JSON[T]` 靠 `form:"x"` tag 绑定。
// 缺 form tag 的字段在表单路径上**绑不到**（实测：没有 form tag 时 gin 只能用字段名匹配，
// 而 JSON 的驼峰名与字段名大小写不同，绑不上）。此前靠前端 `data-write-number` 之类的
// 声明 + 后端各自 `BindForm` 兜底，是两处定义。
//
// 补成双 tag 后「这个字段的表单名是什么」只有一处定义，门禁才能对账到底。
// 前置实测：同一字段同时有 json 与 form tag 时，JSON 体与 formdata 都能正确绑定。
//
// 只动**请求结构**（名字以 Req 结尾的 struct），不动 Resp：响应不需要 form tag。
//
// 用法（仓库根目录）：
//
//	go run ./cmd/formtags            # 补 tag（幂等：已有 form tag 的字段跳过）
//	go run ./cmd/formtags -check     # 只报告还缺多少，不写文件
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

func main() {
	check := flag.Bool("check", false, "只报告缺失，不写文件")
	flag.Parse()
	if err := run(*check); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(check bool) error {
	var files []string
	err := filepath.WalkDir("internal", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		// 只处理 dto 包
		if !strings.Contains(path, "/dto/") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(files)

	totalMissing, totalFiles := 0, 0
	for _, path := range files {
		changed, missing, err := processFile(path, check)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if missing > 0 {
			totalMissing += missing
			totalFiles++
			if check {
				fmt.Printf("%-64s 缺 %d 个 form tag\n", path, missing)
			}
		}
		if changed && !check {
			fmt.Printf("已补: %s\n", path)
		}
	}
	if check {
		if totalMissing == 0 {
			fmt.Println("✓ 全部 dto 请求结构都已有 form tag")
			return nil
		}
		fmt.Printf("共 %d 个文件缺 %d 个 form tag；执行 go run ./cmd/formtags 补齐\n", totalFiles, totalMissing)
		return fmt.Errorf("仍有 %d 个字段缺 form tag", totalMissing)
	}
	fmt.Printf("完成：补齐 %d 个字段\n", totalMissing)
	return nil
}

// processFile 处理单个文件。返回（是否写了文件，缺多少个，错误）。
func processFile(path string, check bool) (bool, int, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return false, 0, err
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return false, 0, nil // 解析不了就跳过（不阻断整批）
	}

	// 收集需要改写的字段（按 struct tag 的位置）。
	type edit struct {
		tagStart int // tag 字面量起始偏移（不含反引号）
		newTag   string
	}
	var edits []edit
	missing := 0

	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || !strings.HasSuffix(ts.Name.Name, "Req") {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok || st.Fields == nil {
			return true
		}
		for _, fld := range st.Fields.List {
			if fld.Tag == nil || len(fld.Names) == 0 {
				continue
			}
			raw := fld.Tag.Value // 含反引号
			tag := parseTag(raw)
			jsonName := strings.Split(tag["json"], ",")[0]
			if jsonName == "" || jsonName == "-" {
				continue // 服务端自填字段（json:"-"），表单不该发
			}
			if tag["form"] != "" {
				continue // 已有
			}
			missing++
			newRaw := addFormTag(raw, jsonName)
			pos := fset.Position(fld.Tag.Pos())
			edits = append(edits, edit{tagStart: pos.Offset, newTag: newRaw})
		}
		return true
	})

	if missing == 0 || check {
		return false, missing, nil
	}

	// 从后往前替换，避免偏移错位。
	//
	// 必须按**偏移量**替换，不能用 bytes.Replace：同一个结构体里常有多个字段的 tag
	// 完全相同（比如两处 `json:"projectId"`），内容匹配会改错位置，
	// 实测表现为「格式化失败: expected ';', found json」——改到了别的字段的引号上。
	out := append([]byte(nil), src...)
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		oldLit := findTagLiteralAt(out, e.tagStart)
		if oldLit == "" {
			continue
		}
		tail := append([]byte(nil), out[e.tagStart+len(oldLit):]...)
		out = append(out[:e.tagStart], []byte(e.newTag)...)
		out = append(out, tail...)
	}

	formatted, err := format.Source(out)
	if err != nil {
		return false, missing, fmt.Errorf("格式化失败（改动已丢弃）: %w", err)
	}
	if err := os.WriteFile(path, formatted, 0o644); err != nil {
		return false, missing, err
	}
	return true, missing, nil
}

// findTagLiteralAt 从偏移处取出完整的反引号/双引号 tag 字面量。
func findTagLiteralAt(src []byte, off int) string {
	if off < 0 || off >= len(src) {
		return ""
	}
	quote := src[off]
	if quote != '`' && quote != '"' {
		return ""
	}
	for i := off + 1; i < len(src); i++ {
		if src[i] == quote {
			return string(src[off : i+1])
		}
	}
	return ""
}

// addFormTag 在已有的 tag 串里插入 form:"<name>"，位置紧随 json tag 之后。
func addFormTag(raw, name string) string {
	quote := ""
	body := raw
	if strings.HasPrefix(raw, "`") {
		quote = "`"
		body = strings.Trim(raw, "`")
	} else {
		quote = `"`
		body = strings.Trim(raw, `"`)
	}
	// 找 json:"..." 的结束位置，插在它后面（保持 json、form 相邻便于阅读）。
	idx := strings.Index(body, `json:"`)
	if idx >= 0 {
		end := idx + len(`json:"`)
		for end < len(body) && body[end] != '"' {
			if body[end] == '\\' {
				end++
			}
			end++
		}
		if end < len(body) {
			body = body[:end+1] + ` form:"` + name + `"` + body[end+1:]
			return quote + body + quote
		}
	}
	// 没有 json tag：直接追加。
	return quote + body + ` form:"` + name + `"` + quote
}

// parseTag 解析 struct tag 字面量，返回 tag key → 值的 map。
//
// 关键：`ast.BasicLit` 里的 `Tag.Value` 是 Go 源码里的**字面量形式**，
// 反引号 tag 会被写成 `"`json:\\"x\\""`（外层双引号 + 内部转义）。
// 直接当反引号串剥会把 `\\"` 留在 body 里，`reflect.StructTag.Lookup` 就取不到值 ——
// 实测表现为「tag 里明明有 form，check 却报缺失」，然后写入时无条件再加一个，
// 最终会得到 `form:"x" form:"x"` 这种重复 tag。
// 正确的去引号方式只有一种：strconv.Unquote。
func parseTag(lit string) map[string]string {
	body, err := strconv.Unquote(lit)
	if err != nil {
		// 不是合法的 Go 字符串字面量：退化为保守处理（当没有 tag）。
		return map[string]string{}
	}
	tag := reflect.StructTag(body)
	out := map[string]string{}
	for _, key := range []string{"json", "form", "binding", "query", "uri"} {
		if v, ok := tag.Lookup(key); ok {
			out[key] = v
		}
	}
	return out
}
