// array.go holds Array.

package binspec

import (
	"fmt"
	"strconv"
	"strings"

	apidsl "github.com/wow-look-at-my/api-dsl"
)

// Array is one counted-array reader: a count of type CountType, then that
// many values of type Elem. The reader returns a slice of Of.
type Array struct {
	Of        string
	Func      string
	Label     string
	LabelExpr string
	Params    string
	CountType string
	Elem      string
	Nullable  bool   // Elem reads a pointer, and a nil one is dropped
	Alloc     bool   // the slice is allocated even when the count is zero
	Check     string // statement with {count} and {label} holes, "" for none
	CountName string // whole label of the count, "" for the label plus ".count"
}

func (spec *Spec) parseArray(n *apidsl.Node) error {
	if err := apidsl.CheckAttrs(n, "of", "func", "label", "label-expr", "params", "count", "elem", "nullable", "alloc", "check", "count-label"); err != nil {
		return err
	}
	a := &Array{
		Of: n.Attr("of"), Func: n.Attr("func"), Params: n.Attr("params"),
		Label: n.Attr("label"), LabelExpr: n.Attr("label-expr"),
		CountType: n.Attr("count"), Elem: n.Attr("elem"), Check: n.Attr("check"),
		CountName: n.Attr("count-label"),
	}
	if a.Of == "" || a.Func == "" {
		return fmt.Errorf("<array> needs of and func")
	}
	if (a.Label == "") == (a.LabelExpr == "") {
		return fmt.Errorf("<array> %s needs exactly one of label and label-expr", a.Func)
	}
	if a.Params == "" {
		a.Params = spec.Go.Params
	}
	if a.Params == "" {
		return fmt.Errorf("<array> %s has no params, and <go> gives none", a.Func)
	}
	for _, t := range []string{a.CountType, a.Elem} {
		if _, ok := spec.Types[t]; !ok {
			return fmt.Errorf("<array> %s has unknown type %q", a.Func, t)
		}
	}
	var err error
	if a.Nullable, err = boolAttr(n, "nullable"); err != nil {
		return err
	}
	if a.Alloc, err = boolAttr(n, "alloc"); err != nil {
		return err
	}
	spec.Arrays = append(spec.Arrays, a)
	return nil
}

// boolAttr reads an optional true or false attribute. Absent is false.
func boolAttr(n *apidsl.Node, name string) (bool, error) {
	switch v := n.Attr(name); v {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, fmt.Errorf("<%s>: %s is %q, want true or false", n.Name(), name, v)
	}
}

// labelExpr returns the array's label prefix as a Go expression.
func (a *Array) labelExpr() string {
	if a.LabelExpr != "" {
		return a.LabelExpr
	}
	return strconv.Quote(a.Label)
}

// countExpr returns the Go expression that reads the element count.
func (spec *Spec) countExpr(a *Array) string {
	label := strconv.Quote(a.Label + ".count")
	if a.LabelExpr != "" {
		label = a.LabelExpr + "+" + strconv.Quote(".count")
	}
	if a.CountName != "" {
		label = strconv.Quote(a.CountName)
	}
	return strings.ReplaceAll(spec.Types[a.CountType].Read, "{label}", label)
}

// indexed reports whether the element read takes a label, and so the index.
func (spec *Spec) indexed(a *Array) bool {
	return strings.Contains(spec.Types[a.Elem].Read, "{label}")
}

// elemExpr returns the Go expression that reads element i.
func (spec *Spec) elemExpr(a *Array) string {
	label := `fmt.Sprintf("%s[%d]", ` + a.LabelExpr + `, i)`
	if a.LabelExpr == "" {
		label = "fmt.Sprintf(" + strconv.Quote(strings.ReplaceAll(a.Label, "%", "%%")+"[%d]") + ", i)"
	}
	return strings.ReplaceAll(spec.Types[a.Elem].Read, "{label}", label)
}

// checkStmt fills the holes of the array's check statement.
func checkStmt(a *Array) string {
	return strings.NewReplacer("{count}", "count", "{label}", a.labelExpr()).Replace(a.Check)
}
