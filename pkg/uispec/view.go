package uispec

import (
	"context"
	"fmt"
	"strings"
)

type Resolver interface {
	Resolve(ctx context.Context, source string, params map[string]string, limit int) (any, error)
}

type TableRow struct {
	Cells []TableCell
}

type TableCell struct {
	Text string

	Num bool
}

type TableView struct {
	Columns []Column
	Rows    []TableRow
	Empty   string
}

type ListView struct {
	Items []Item
	Empty string
}

type AccordionView struct {
	Groups []Group
	Empty  string
}

type StatView struct {
	Items []Stat
	Empty string
}

type View struct {
	Type   string
	Title  string
	Stat   *StatView
	Table  *TableView
	List   *ListView
	Accord *AccordionView
}

const (
	NoteOK       = "ok"
	NoteNotFound = "not_found"
	NoteShape    = "shape"
	NoteFailed   = "failed"
)

type Note struct {
	Index  int
	Source string
	Kind   string

	Msg string
}

func Execute(ctx context.Context, s *Spec, r Resolver) ([]View, []Note) {
	if s == nil || len(s.Blocks) == 0 {
		return nil, nil
	}
	views := make([]View, 0, len(s.Blocks))
	notes := make([]Note, 0, len(s.Blocks))
	for i, b := range s.Blocks {
		if r == nil {
			notes = append(notes, Note{Index: i, Source: b.Source, Kind: NoteFailed, Msg: "没有可用的数据源解析器"})
			continue
		}
		data, err := r.Resolve(ctx, b.Source, b.Params, b.Limit)
		if err != nil {
			notes = append(notes, Note{Index: i, Source: b.Source, Kind: NoteFailed, Msg: err.Error()})
			continue
		}
		v, ok := BuildView(b, data)
		if !ok {
			notes = append(notes, Note{
				Index: i, Source: b.Source, Kind: NoteShape,
				Msg: fmt.Sprintf("数据形状 %T 与组件 %s 不匹配", data, b.Type),
			})
			continue
		}
		views = append(views, v)
		notes = append(notes, Note{Index: i, Source: b.Source, Kind: NoteOK})
	}
	return views, notes
}

func BuildView(b Block, data any) (View, bool) {
	v := View{Type: b.Type, Title: strings.TrimSpace(b.Title)}
	switch b.Type {
	case TypeStat:
		d, ok := asStatData(data)
		if !ok {
			return v, false
		}
		v.Stat = &StatView{Items: d.Items, Empty: d.Empty}
	case TypeTable:
		d, ok := asTableData(data)
		if !ok {
			return v, false
		}

		if len(d.Columns) == 0 {
			return v, false
		}
		tv := buildTableView(d)
		v.Table = &tv
	case TypeList:
		d, ok := asListData(data)
		if !ok {
			return v, false
		}
		v.List = &ListView{Items: d.Items, Empty: d.Empty}
	case TypeAccordion:
		d, ok := asAccordionData(data)
		if !ok {
			return v, false
		}
		v.Accord = &AccordionView{Groups: d.Groups, Empty: d.Empty}
	default:

		return v, false
	}
	return v, true
}

func buildTableView(d TableData) TableView {
	cols := d.Columns
	num := make([]bool, len(cols))
	for i, c := range cols {
		num[i] = c.Align == "right"
	}
	rows := make([]TableRow, 0, len(d.Rows))
	for _, r := range d.Rows {
		cells := make([]TableCell, len(cols))
		for i := range cols {
			text := ""
			if i < len(r) {
				text = r[i]
			}
			cells[i] = TableCell{Text: text, Num: num[i]}
		}
		rows = append(rows, TableRow{Cells: cells})
	}
	return TableView{Columns: cols, Rows: rows, Empty: d.Empty}
}

func asStatData(data any) (StatData, bool) {
	switch d := data.(type) {
	case StatData:
		return d, true
	case *StatData:
		if d == nil {
			return StatData{}, false
		}
		return *d, true
	default:
		return StatData{}, false
	}
}

func asTableData(data any) (TableData, bool) {
	switch d := data.(type) {
	case TableData:
		return d, true
	case *TableData:
		if d == nil {
			return TableData{}, false
		}
		return *d, true
	default:
		return TableData{}, false
	}
}

func asListData(data any) (ListData, bool) {
	switch d := data.(type) {
	case ListData:
		return d, true
	case *ListData:
		if d == nil {
			return ListData{}, false
		}
		return *d, true
	default:
		return ListData{}, false
	}
}

func asAccordionData(data any) (AccordionData, bool) {
	switch d := data.(type) {
	case AccordionData:
		return d, true
	case *AccordionData:
		if d == nil {
			return AccordionData{}, false
		}
		return *d, true
	default:
		return AccordionData{}, false
	}
}
