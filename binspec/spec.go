// Package binspec reads a declarative description of a binary layout and
// generates the Go reader for it.
package binspec

import (
	"fmt"
	"strings"

	apidsl "github.com/wow-look-at-my/api-dsl"
)

// Spec is one parsed spec document.
type Spec struct {
	Structs []*Struct
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

// wireTypes maps a spec type to the Reader call that reads it. A %q verb
// receives the trace label.
var wireTypes = map[string]string{
	"i32":    "r.ReadInt32(%q)",
	"u32":    "r.ReadUint32(%q)",
	"i64":    "r.ReadInt64(%q)",
	"bool32": "r.ReadBool32(%q)",
	"guid":   "r.ReadGuid(%q)",
	"fname":  "r.ReadFName(names, %q)",
}

// widenTypes maps an as= value to the Go conversion it emits.
var widenTypes = map[string]string{
	"i64": "int64",
}

// unlessFlags maps an unless= value to the summary predicate it negates.
var unlessFlags = map[string]string{
	"filter-editor-only": "summary.FilterEditorOnly()",
	"unversioned":        "summary.IsUnversioned",
}

// Parse reads a spec document.
func Parse(src []byte) (*Spec, error) {
	root, err := apidsl.ParseDOM(src)
	if err != nil {
		return nil, err
	}
	if root.Name() != "binspec" {
		return nil, fmt.Errorf("root element is <%s>, want <binspec>", root.Name())
	}
	if err := apidsl.CheckAttrs(root); err != nil {
		return nil, err
	}
	spec := &Spec{}
	for _, n := range root.Children() {
		if n.Name() != "struct" {
			return nil, fmt.Errorf("<binspec>: unknown element <%s>", n.Name())
		}
		s, err := parseStruct(n)
		if err != nil {
			return nil, err
		}
		spec.Structs = append(spec.Structs, s)
	}
	if len(spec.Structs) == 0 {
		return nil, fmt.Errorf("<binspec> declares no <struct>")
	}
	return spec, nil
}

func parseStruct(n *apidsl.Node) (*Struct, error) {
	if err := apidsl.CheckAttrs(n, "name", "func", "label"); err != nil {
		return nil, err
	}
	s := &Struct{Name: n.Attr("name"), Func: n.Attr("func"), Label: n.Attr("label")}
	if s.Name == "" || s.Func == "" || s.Label == "" {
		return nil, fmt.Errorf("<struct> needs name, func and label")
	}
	items, err := parseItems(n, s.Name)
	if err != nil {
		return nil, err
	}
	s.Items = items
	return s, nil
}

func parseItems(parent *apidsl.Node, owner string) ([]Item, error) {
	var items []Item
	for _, n := range parent.Children() {
		it, err := parseItem(n, owner)
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

func parseItem(n *apidsl.Node, owner string) (Item, error) {
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
	if err := apidsl.CheckAttrs(n, append(allowed, "since", "until", "unless")...); err != nil {
		return it, fmt.Errorf("%s: %w", owner, err)
	}
	cond, err := parseCond(n)
	if err != nil {
		return it, fmt.Errorf("%s: %w", owner, err)
	}
	it.Cond = cond
	if it.Kind == KindGroup {
		it.Items, err = parseItems(n, owner)
		return it, err
	}
	it.Name, it.Type, it.As, it.Label = n.Attr("name"), n.Attr("type"), n.Attr("as"), n.Attr("label")
	if _, ok := wireTypes[it.Type]; !ok {
		return it, fmt.Errorf("%s: <%s> has unknown type %q", owner, it.Kind, it.Type)
	}
	if it.As != "" {
		if _, ok := widenTypes[it.As]; !ok {
			return it, fmt.Errorf("%s: <field> has unknown as %q", owner, it.As)
		}
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
func parseCond(n *apidsl.Node) (string, error) {
	var parts []string
	if u := n.Attr("unless"); u != "" {
		pred, ok := unlessFlags[u]
		if !ok {
			return "", fmt.Errorf("<%s>: unknown unless %q", n.Name(), u)
		}
		parts = append(parts, "!"+pred)
	}
	if v := n.Attr("since"); v != "" {
		pred, err := versionPredicate(v)
		if err != nil {
			return "", err
		}
		parts = append(parts, pred)
	}
	if v := n.Attr("until"); v != "" {
		pred, err := versionPredicate(v)
		if err != nil {
			return "", err
		}
		parts = append(parts, "!"+pred)
	}
	return strings.Join(parts, " && "), nil
}

// versionPredicate turns "ue4:NAME" or "ue5:NAME" into the summary check
// that is true from that object version on.
func versionPredicate(ref string) (string, error) {
	stream, name, ok := strings.Cut(ref, ":")
	if !ok || name == "" {
		return "", fmt.Errorf("version %q is not ue4:NAME or ue5:NAME", ref)
	}
	switch stream {
	case "ue4":
		return "summary.UE4Ver(uasset.VER_UE4_" + name + ")", nil
	case "ue5":
		return "summary.UE5Ver(uasset.UE5_" + name + ")", nil
	}
	return "", fmt.Errorf("version %q is not ue4:NAME or ue5:NAME", ref)
}
