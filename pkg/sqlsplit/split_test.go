package sqlsplit

import (
	"strings"
	"testing"
)

func TestSplitKeepsStatementBoundaries(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want int
	}{
		{"simple", "CREATE TABLE a (id int);\nINSERT INTO a VALUES (1);", 2},
		{"semicolon in string", "INSERT INTO a VALUES ('a;b');", 1},
		{"escaped quote", "INSERT INTO a VALUES ('it''s; ok');", 1},
		{"line comment", "-- a; b\nSELECT 1;", 1},
		{"block comment", "/* a; b */ SELECT 1;", 1},
		{"dollar body", "CREATE FUNCTION f() RETURNS void AS $$ BEGIN PERFORM 1; END $$ LANGUAGE plpgsql;", 1},
		{"tagged dollar body", "CREATE FUNCTION f() RETURNS void AS $fn$ BEGIN PERFORM 1; END $fn$ LANGUAGE plpgsql;", 1},
		{"double quoted identifier", "CREATE TABLE \"a;b\" (id int);", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Split(tc.sql)
			if len(got) != tc.want {
				t.Fatalf("语句数 = %d，期望 %d：%#v", len(got), tc.want, got)
			}
			for _, stmt := range got {
				if strings.HasSuffix(strings.TrimSpace(stmt), ";") {
					t.Fatalf("语句不应以分号结尾：%q", stmt)
				}
			}
		})
	}
}
