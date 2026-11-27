// gen.go holds itemScope.

package binspec

import (
	"bytes"
	_ "embed"
	"fmt"
	"go/format"
	"slices"
	"strconv"
	"strings"
	"text/template"
)

//go:embed reader.go.tmpl
var readerTemplateText string

// itemScope carries the owning struct into the recursive items template.
type itemScope struct {
	Struct *Struct
	Items  []Item
}

// readExpr returns the Go expression that reads one item off the wire.
func (spec *Spec) readExpr(s *Struct, it Item) string {
	label := strconv.Quote(s.Label + "." + it.Label)
	expr := strings.ReplaceAll(spec.Types[it.Type].Read, "{label}", label)
	if it.As != "" {
		return spec.Types[it.As].GoType + "(" + expr + ")"
	}
	return expr
}

// Generate returns the gofmt-ed Go source of the readers in spec. The source
// argument names the spec file in the generated header.
func Generate(spec *Spec, source string) ([]byte, error) {
	tmpl, err := template.New("reader").Funcs(template.FuncMap{
		"scope": func(s *Struct, items []Item) itemScope { return itemScope{Struct: s, Items: items} },
		"read":  spec.readExpr,
	}).Parse(readerTemplateText)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	imports := spec.Go.Imports
	if len(spec.Enums) > 0 && !slices.Contains(imports, "fmt") {
		imports = append([]string{"fmt"}, imports...)
	}
	data := struct {
		Spec    *Spec
		Source  string
		Imports []string
	}{spec, source, imports}
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	out, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("generated source does not parse: %w", err)
	}
	return out, nil
}
