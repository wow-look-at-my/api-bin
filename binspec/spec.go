// Package binspec reads a declarative description of a binary layout and
// generates the Go reader for it.
package binspec

import (
	"fmt"
	"strconv"
	"strings"

	apidsl "github.com/wow-look-at-my/api-dsl"
	"github.com/wow-look-at-my/go-containers/set"
)

// Spec is one parsed document: the bindings to the consumer's code, then
// the layouts.
type Spec struct {
	Go       GoTarget
	Types    map[string]WireType
	Versions map[string]string // stream -> Go test with a {name} hole
	Flags    map[string]string // flag -> Go test
	Structs  []*Struct
	Enums    []*Enum
	Arrays   []*Array
}

// Enum is one value-to-name table.
type Enum struct {
	Name    string
	Func    string
	GoType  string
	Unknown string
	Values  []EnumValue
}

// EnumValue is one row of an Enum.
type EnumValue struct {
	N    int64
	Name string
}

// GoTarget binds the generated file to the consumer's package.
type GoTarget struct {
	Package string
	Params  string   // parameter list of every generated reader
	Model   string   // prefix of a struct's Go type, "" or "pkg."
	Imports []string // import paths
}

// WireType is one type a field may name. Read is the Go expression that
// reads it, with a {label} hole. GoType is the conversion an as= emits.
type WireType struct {
	Read   string
	GoType string
}

// Struct is one layout. Func names the generated reader.
type Struct struct {
	Name      string
	Func      string
	Label     string
	LabelExpr string
	Params    string // "" means the <go> params
	Pointer   bool   // the reader returns a pointer, and may return nil
	Init      string // field list of the value's composite literal
	Items     []Item
}

// Item kinds.
const (
	KindField  = "field"
	KindSkip   = "skip"
	KindGroup  = "group"
	KindCall   = "call"
	KindNullIf = "null-if"
	KindBits   = "bits"
)

// Item is one step of a layout: a stored field or a discarded read.
type Item struct {
	Kind  string
	Name  string
	Type  string
	As    string
	Label string
	Go    string // statement of a call, with {label} and {out} holes
	Cond  string // Go expression, "" when the item always applies
	Items []Item
	Bits  []Bit
}

// Bit is one flag of a <bits> step: bit N of the value sets member Name.
type Bit struct {
	N    int
	Name string
}

// Parse reads a spec document.
func Parse(src []byte) (*Spec, error) {
	root, err := apidsl.ParseDOM(src)
	if err != nil {
		return nil, err
	}
	if root.Name() != "api-bin" {
		return nil, fmt.Errorf("root element is <%s>, want <api-bin>", root.Name())
	}
	if err := apidsl.CheckAttrs(root); err != nil {
		return nil, err
	}
	spec := &Spec{Types: map[string]WireType{}, Versions: map[string]string{}, Flags: map[string]string{}}
	var structs, arrays []*apidsl.Node
	for _, n := range root.Children() {
		switch n.Name() {
		case "go":
			err = spec.parseGo(n)
		case "type":
			err = spec.parseType(n)
		case "version":
			err = parseBinding(n, "stream", spec.Versions)
		case "flag":
			err = parseBinding(n, "name", spec.Flags)
		case "struct":
			structs = append(structs, n)
		case "enum":
			err = spec.parseEnum(n)
		case "array":
			arrays = append(arrays, n)
		default:
			err = fmt.Errorf("<api-bin>: unknown element <%s>", n.Name())
		}
		if err != nil {
			return nil, err
		}
	}
	if spec.Go.Package == "" {
		return nil, fmt.Errorf("<api-bin> needs a <go> element")
	}
	if len(structs) == 0 && len(spec.Enums) == 0 && len(arrays) == 0 {
		return nil, fmt.Errorf("<api-bin> declares no <struct>, <enum> or <array>")
	}
	for _, n := range arrays {
		if err := spec.parseArray(n); err != nil {
			return nil, err
		}
	}
	// Layouts parse last, so a binding may follow the struct that uses it.
	for _, n := range structs {
		s, err := spec.parseStruct(n)
		if err != nil {
			return nil, err
		}
		spec.Structs = append(spec.Structs, s)
	}
	return spec, nil
}

func (spec *Spec) parseGo(n *apidsl.Node) error {
	if err := apidsl.CheckAttrs(n, "package", "params", "model"); err != nil {
		return err
	}
	if spec.Go.Package != "" {
		return fmt.Errorf("<api-bin> has more than one <go>")
	}
	spec.Go = GoTarget{Package: n.Attr("package"), Params: n.Attr("params"), Model: n.Attr("model")}
	if spec.Go.Package == "" {
		return fmt.Errorf("<go> needs a package")
	}
	for _, c := range n.Children() {
		if c.Name() != "import" {
			return fmt.Errorf("<go>: unknown element <%s>", c.Name())
		}
		if err := apidsl.CheckAttrs(c, "path"); err != nil {
			return err
		}
		if c.Attr("path") == "" {
			return fmt.Errorf("<import> needs a path")
		}
		spec.Go.Imports = append(spec.Go.Imports, c.Attr("path"))
	}
	return nil
}

func (spec *Spec) parseType(n *apidsl.Node) error {
	if err := apidsl.CheckAttrs(n, "name", "read", "go"); err != nil {
		return err
	}
	name, read := n.Attr("name"), n.Attr("read")
	if name == "" || !strings.Contains(read, "{label}") {
		return fmt.Errorf("<type> needs a name and a read with a {label} hole")
	}
	if _, dup := spec.Types[name]; dup {
		return fmt.Errorf("<type> %q is declared twice", name)
	}
	spec.Types[name] = WireType{Read: read, GoType: n.Attr("go")}
	return nil
}

func (spec *Spec) parseEnum(n *apidsl.Node) error {
	if err := apidsl.CheckAttrs(n, "name", "func", "go", "unknown"); err != nil {
		return err
	}
	e := &Enum{Name: n.Attr("name"), Func: n.Attr("func"), GoType: n.Attr("go"), Unknown: n.Attr("unknown")}
	if e.Name == "" || e.Func == "" || e.GoType == "" {
		return fmt.Errorf("<enum> needs name, func and go")
	}
	if strings.Count(e.Unknown, "%d") != 1 || strings.Count(e.Unknown, "%") != 1 {
		return fmt.Errorf("<enum> %s: unknown needs exactly one %%d", e.Name)
	}
	seen := set.New[int64]()
	for _, c := range n.Children() {
		if c.Name() != "value" {
			return fmt.Errorf("<enum> %s: unknown element <%s>", e.Name, c.Name())
		}
		if err := apidsl.CheckAttrs(c, "n", "name"); err != nil {
			return err
		}
		num, err := strconv.ParseInt(c.Attr("n"), 10, 64)
		if err != nil || c.Attr("name") == "" {
			return fmt.Errorf("<enum> %s: <value> needs an integer n and a name", e.Name)
		}
		if seen.Contains(num) {
			return fmt.Errorf("<enum> %s: value %d is declared twice", e.Name, num)
		}
		seen.Add(num)
		e.Values = append(e.Values, EnumValue{N: num, Name: c.Attr("name")})
	}
	if len(e.Values) == 0 {
		return fmt.Errorf("<enum> %s has no <value>", e.Name)
	}
	spec.Enums = append(spec.Enums, e)
	return nil
}

// parseBinding reads a <version> or a <flag>: a key attribute plus a test.
func parseBinding(n *apidsl.Node, keyAttr string, into map[string]string) error {
	if err := apidsl.CheckAttrs(n, keyAttr, "test"); err != nil {
		return err
	}
	key, test := n.Attr(keyAttr), n.Attr("test")
	if key == "" || test == "" {
		return fmt.Errorf("<%s> needs %s and test", n.Name(), keyAttr)
	}
	if keyAttr == "stream" && !strings.Contains(test, "{name}") {
		return fmt.Errorf("<version> %q: test has no {name} hole", key)
	}
	if _, dup := into[key]; dup {
		return fmt.Errorf("<%s> %q is declared twice", n.Name(), key)
	}
	into[key] = test
	return nil
}

func (spec *Spec) parseStruct(n *apidsl.Node) (*Struct, error) {
	if err := apidsl.CheckAttrs(n, "name", "func", "label", "label-expr", "params", "pointer", "init"); err != nil {
		return nil, err
	}
	s := &Struct{
		Name: n.Attr("name"), Func: n.Attr("func"), Params: n.Attr("params"),
		Label: n.Attr("label"), LabelExpr: n.Attr("label-expr"),
		Pointer: n.Attr("pointer") == "true", Init: n.Attr("init"),
	}
	if p := n.Attr("pointer"); p != "" && p != "true" && p != "false" {
		return nil, fmt.Errorf("<struct> %s: pointer is %q, want true or false", s.Name, p)
	}
	if s.Name == "" || s.Func == "" {
		return nil, fmt.Errorf("<struct> needs name and func")
	}
	if (s.Label == "") == (s.LabelExpr == "") {
		return nil, fmt.Errorf("<struct> %s needs exactly one of label and label-expr", s.Name)
	}
	if s.Params == "" {
		s.Params = spec.Go.Params
	}
	if s.Params == "" {
		return nil, fmt.Errorf("<struct> %s has no params, and <go> gives none", s.Name)
	}
	items, err := spec.parseItems(n, s.Name)
	if err != nil {
		return nil, err
	}
	s.Items = items
	if !s.Pointer && hasKind(items, KindNullIf) {
		return nil, fmt.Errorf("<struct> %s: <null-if> needs pointer=\"true\"", s.Name)
	}
	return s, nil
}

// hasKind reports whether items, or a group inside them, holds a step of kind.
func hasKind(items []Item, kind string) bool {
	for _, it := range items {
		if it.Kind == kind || hasKind(it.Items, kind) {
			return true
		}
	}
	return false
}

// parseBits reads the <bit> rows of a <bits> step.
func parseBits(n *apidsl.Node, owner string) ([]Bit, error) {
	var bits []Bit
	for _, c := range n.Children() {
		if c.Name() != "bit" {
			return nil, fmt.Errorf("%s: <bits>: unknown element <%s>", owner, c.Name())
		}
		if err := apidsl.CheckAttrs(c, "n", "name"); err != nil {
			return nil, fmt.Errorf("%s: %w", owner, err)
		}
		num, err := strconv.Atoi(c.Attr("n"))
		if err != nil || num < 0 || num > 63 || c.Attr("name") == "" {
			return nil, fmt.Errorf("%s: <bit> needs n from 0 to 63 and a name", owner)
		}
		bits = append(bits, Bit{N: num, Name: c.Attr("name")})
	}
	if len(bits) == 0 {
		return nil, fmt.Errorf("%s: <bits> has no <bit>", owner)
	}
	return bits, nil
}

func (spec *Spec) parseItems(parent *apidsl.Node, owner string) ([]Item, error) {
	var items []Item
	for _, n := range parent.Children() {
		it, err := spec.parseItem(n, owner)
		if err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("%s: <%s> is empty", owner, parent.Name())
	}
	return items, nil
}

func (spec *Spec) parseItem(n *apidsl.Node, owner string) (Item, error) {
	it := Item{Kind: n.Name()}
	var allowed []string
	switch it.Kind {
	case KindField:
		allowed = []string{"name", "type", "as", "label"}
	case KindSkip, KindNullIf, KindBits:
		allowed = []string{"type", "label"}
	case KindGroup:
	case KindCall:
		allowed = []string{"go"}
	default:
		return it, fmt.Errorf("%s: unknown element <%s>", owner, it.Kind)
	}
	if err := apidsl.CheckAttrs(n, append(allowed, "since", "until", "if", "unless")...); err != nil {
		return it, fmt.Errorf("%s: %w", owner, err)
	}
	cond, err := spec.parseCond(n)
	if err != nil {
		return it, fmt.Errorf("%s: %w", owner, err)
	}
	it.Cond = cond
	if it.Kind == KindGroup {
		it.Items, err = spec.parseItems(n, owner)
		return it, err
	}
	if it.Kind == KindCall {
		it.Go = n.Attr("go")
		if it.Go == "" {
			return it, fmt.Errorf("%s: <call> needs a go statement", owner)
		}
		return it, nil
	}
	it.Name, it.Type, it.As, it.Label = n.Attr("name"), n.Attr("type"), n.Attr("as"), n.Attr("label")
	if _, ok := spec.Types[it.Type]; !ok {
		return it, fmt.Errorf("%s: <%s> has unknown type %q", owner, it.Kind, it.Type)
	}
	if it.As != "" && spec.Types[it.As].GoType == "" {
		return it, fmt.Errorf("%s: as=%q names no <type> with a go attribute", owner, it.As)
	}
	if it.Kind == KindField && it.Name == "" {
		return it, fmt.Errorf("%s: <field> needs a name", owner)
	}
	if it.Label == "" {
		it.Label = it.Name
	}
	if it.Label == "" {
		return it, fmt.Errorf("%s: <%s> needs a label", owner, it.Kind)
	}
	if it.Kind == KindBits {
		it.Bits, err = parseBits(n, owner)
		return it, err
	}
	return it, nil
}

// parseCond joins the condition attributes of n into one Go expression.
func (spec *Spec) parseCond(n *apidsl.Node) (string, error) {
	var parts []string
	for _, c := range []struct {
		attr    string
		negate  bool
		resolve func(string) (string, error)
	}{
		{"if", false, spec.flagTest},
		{"unless", true, spec.flagTest},
		{"since", false, spec.versionTest},
		{"until", true, spec.versionTest},
	} {
		v := n.Attr(c.attr)
		if v == "" {
			continue
		}
		test, err := c.resolve(v)
		if err != nil {
			return "", fmt.Errorf("<%s> %s: %w", n.Name(), c.attr, err)
		}
		if c.negate {
			test = "!" + test
		}
		parts = append(parts, test)
	}
	return strings.Join(parts, " && "), nil
}

func (spec *Spec) flagTest(name string) (string, error) {
	test, ok := spec.Flags[name]
	if !ok {
		return "", fmt.Errorf("unknown flag %q", name)
	}
	return test, nil
}

// versionTest turns "stream:NAME" into the test that is true from that
// version on.
func (spec *Spec) versionTest(ref string) (string, error) {
	stream, name, ok := strings.Cut(ref, ":")
	if !ok || name == "" {
		return "", fmt.Errorf("version %q is not stream:NAME", ref)
	}
	test, ok := spec.Versions[stream]
	if !ok {
		return "", fmt.Errorf("unknown version stream %q", stream)
	}
	return strings.ReplaceAll(test, "{name}", name), nil
}
