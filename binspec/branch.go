// branch.go holds Case.

package binspec

import (
	"fmt"
	"strconv"
	"strings"

	apidsl "github.com/wow-look-at-my/api-dsl"
	"github.com/wow-look-at-my/go-containers/set"
)

// Case is one arm of a <switch>: the steps that run when the tag is Value.
type Case struct {
	Value int64
	Items []Item
}

// parseCases reads the <case> arms of a <switch>. An arm may be empty.
func (spec *Spec) parseCases(n *apidsl.Node, owner string) ([]Case, error) {
	var cases []Case
	seen := set.New[int64]()
	for _, c := range n.Children() {
		if c.Name() != "case" {
			return nil, fmt.Errorf("%s: <switch>: unknown element <%s>", owner, c.Name())
		}
		if err := apidsl.CheckAttrs(c, "value"); err != nil {
			return nil, fmt.Errorf("%s: %w", owner, err)
		}
		value, err := strconv.ParseInt(c.Attr("value"), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%s: <case> needs an integer value", owner)
		}
		if seen.Contains(value) {
			return nil, fmt.Errorf("%s: <case> %d is declared twice", owner, value)
		}
		seen.Add(value)
		arm := Case{Value: value}
		if len(c.Children()) > 0 {
			if arm.Items, err = spec.parseItems(c, owner); err != nil {
				return nil, err
			}
		}
		cases = append(cases, arm)
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("%s: <switch> has no <case>", owner)
	}
	return cases, nil
}

// unknownStmt fills the holes of a switch's unknown statement: {value} is
// the tag that matched no case, and {label} is the struct's label prefix.
func unknownStmt(s *Struct, it Item) string {
	label := s.LabelExpr
	if label == "" {
		label = strconv.Quote(s.Label)
	}
	return strings.NewReplacer("{value}", "tag", "{label}", label, "{out}", "out").Replace(it.Unknown)
}
