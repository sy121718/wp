package unit

import (
	"reflect"
	"testing"

	migrations "go_wp/public/migrations"
)

func TestSplitStatementsRespectsDollarQuoteBlocks(t *testing.T) {
	sql := `
CREATE TABLE a (id int);
DO $$ BEGIN
    ALTER TABLE a ADD CONSTRAINT c CHECK (id > 0);
EXCEPTION WHEN duplicate_object THEN NULL; END $$;
INSERT INTO a VALUES (1);
`
	got := migrations.SplitStatements(sql)
	want := []string{
		"CREATE TABLE a (id int)",
		"DO $$ BEGIN\n    ALTER TABLE a ADD CONSTRAINT c CHECK (id > 0);\nEXCEPTION WHEN duplicate_object THEN NULL; END $$",
		"INSERT INTO a VALUES (1)",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("切分结果不符:\n got=%#v\nwant=%#v", got, want)
	}
}

func TestSplitStatementsSkipsCommentsAndStrings(t *testing.T) {
	sql := "-- 注释里的分号; 不应切分\nCREATE TABLE b (note text DEFAULT 'a;b');"
	got := migrations.SplitStatements(sql)
	want := []string{"-- 注释里的分号; 不应切分\nCREATE TABLE b (note text DEFAULT 'a;b')"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("注释与字符串处理错误: %#v", got)
	}
}
