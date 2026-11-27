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

// Struct is one layout. Func names the generated reader, and Label prefixes
// each trace label.
type Struct struct {
	Name  string
	Func  string
	Label string
	Items []Item
}

// Item kinds.
const (
	KindField = "field"
	KindSkip  = "skip"
	KindGroup = "group"
)

// Item is one step of a layout: a stored field, a discarded read, or a group
// of steps under one condition.
type Item struct {
	Kind  string
	Name  string
	Type  string
	As    string
	Label string
	Cond  string // Go expression, "" when the item always applies
	Items []Item
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
	var structs []*apidsl.Node
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
	if len(structs) == 0 && len(spec.Enums) == 0 {
		return nil, fmt.Errorf("<api-bin> declares no <struct> and no <enum>")
	}
	if len(structs) > 0 && spec.Go.Params == "" {
		return nil, fmt.Errorf("<go> needs params when the spec has a <struct>")
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
	if err := apidsl.CheckAttrs(n, "name", "func", "label"); err != nil {
		return nil, err
	}
	s := &Struct{Name: n.Attr("name"), Func: n.Attr("func"), Label: n.Attr("label")}
	if s.Name == "" || s.Func == "" || s.Label == "" {
		return nil, fmt.Errorf("<struct> needs name, func and label")
	}
	items, err := spec.parseItems(n, s.Name)
	if err != nil {
		return nil, err
	}
	s.Items = items
	return s, nil
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
	case KindSkip:
		allowed = []string{"type", "label"}
	case KindGroup:
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
		return it, fmt.Errorf("%s: <skip> needs a label", owner)
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
