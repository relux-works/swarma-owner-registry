package intake

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/relux-works/waggle/verifier"
)

// Facts transfer preserves ordinary Evidence without setting receipt-consumption
// inputs, even when every untyped slot carries receipt-like fields. Reflection
// closes direct Facts fields; the source guard covers writes outside fill too.
func TestFactsCannotInjectReceipt(t *testing.T) {
	names := []string{"FreshnessReceipt", "RequireFreshnessReceipt", "FreshnessMarginSeconds"}
	for _, name := range names {
		if _, exists := reflect.TypeFor[Facts]().FieldByName(name); exists {
			t.Fatalf("Facts exposes receipt input %s", name)
		}
	}
	injection := map[string]any{
		"FreshnessReceipt": map[string]any{}, "RequireFreshnessReceipt": true, "FreshnessMarginSeconds": int64(60),
		"freshness_receipt": map[string]any{}, "require_freshness_receipt": true, "freshness_margin_seconds": int64(60),
	}
	facts := Facts{Snapshots: map[string]Snapshot{}}
	fields := reflect.ValueOf(&facts).Elem()
	for i := range fields.NumField() {
		if fields.Field(i).Kind() == reflect.Interface {
			fields.Field(i).Set(reflect.ValueOf(injection))
		}
	}
	var context verifier.Context
	if err := facts.fill(&context); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if !reflect.ValueOf(context).FieldByName(name).IsZero() {
			t.Fatalf("Facts injected Context.%s", name)
		}
	}
	if !context.Now.IsAvailable() || !context.SignedSnapshots.IsAvailable() {
		t.Fatal("ordinary Evidence positive control was not transferred")
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	production := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		production++
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			var field string
			switch n := node.(type) {
			case *ast.SelectorExpr:
				field = n.Sel.Name
			case *ast.KeyValueExpr:
				if key, ok := n.Key.(*ast.Ident); ok {
					field = key.Name
				}
			}
			for _, forbidden := range names {
				if field == forbidden {
					t.Errorf("receipt-free intake references %s in %s", field, name)
				}
			}
			return true
		})
	}
	if production == 0 {
		t.Fatal("no production construction inspected")
	}
}
