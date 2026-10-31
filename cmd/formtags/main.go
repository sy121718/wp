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

func processFile(path string, check bool) (bool, int, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return false, 0, err
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return false, 0, nil
	}

	type edit struct {
		tagStart int
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
			raw := fld.Tag.Value
			tag := parseTag(raw)
			jsonName := strings.Split(tag["json"], ",")[0]
			if jsonName == "" || jsonName == "-" {
				continue
			}
			if tag["form"] != "" {
				continue
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

	return quote + body + ` form:"` + name + `"` + quote
}

func parseTag(lit string) map[string]string {
	body, err := strconv.Unquote(lit)
	if err != nil {

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
