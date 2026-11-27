// gen.go holds itemScope.

package binspec

import (
	"bytes"
	_ "embed"
	"fmt"
	"go/format"
	"text/template"
)

//go:embed reader.go.tmpl
var readerTemplateText string

// itemScope carries the owning struct into the recursive items template.
type itemScope struct {
	Struct *Struct
	Items  []Item
}

var readerTemplate = template.Must(template.New("reader").Funcs(template.FuncMap{
	"scope": func(s *Struct, items []Item) itemScope { return itemScope{Struct: s, Items: items} },
	"read":  readExpr,
}).Parse(readerTemplateText))

// readExpr returns the Go expression that reads one item off the wire.
func readExpr(s *Struct, it Item) string {
	expr := fmt.Sprintf(wireTypes[it.Type], s.Label+"."+it.Label)
	if it.As != "" {
		return widenTypes[it.As] + "(" + expr + ")"
	}
	return expr
}

// Generate returns the gofmt-ed Go source of the readers in spec. The source
// argument names the spec file in the generated header.
func Generate(spec *Spec, source string) ([]byte, error) {
	var buf bytes.Buffer
	data := struct {
		Spec   *Spec
		Source string
	}{spec, source}
	if err := readerTemplate.Execute(&buf, data); err != nil {
		return nil, err
	}
	out, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("generated source does not parse: %w", err)
	}
	return out, nil
}
