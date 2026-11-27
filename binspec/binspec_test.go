package binspec

import _ "embed"

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// header is the smallest set of bindings a test spec needs.
const header = `<go package="p" params="r *Reader"/>
<type name="i32" read="r.I32({label})"/>
<type name="i64" go="int64" read="r.I64({label})"/>
<version stream="v" test="ver >= {name}"/>
<flag name="cooked" test="cooked"/>`

func wrap(body string) []byte {
	return []byte(`<?xml version="1.1" encoding="UTF-8"?><api-bin>` + header + body + `</api-bin>`)
}

// TestGoldenCorpus holds every spec under testdata to its generated file.
func TestGoldenCorpus(t *testing.T) {
	specs, err := filepath.Glob("../testdata/*.xml")
	require.NoError(t, err)
	require.NotEmpty(t, specs)
	for _, path := range specs {
		name := strings.TrimSuffix(filepath.Base(path), ".xml")
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(path)
			require.NoError(t, err)
			want, err := os.ReadFile(strings.TrimSuffix(path, ".xml") + ".golden")
			require.NoError(t, err, "a spec needs a .golden partner")
			spec, err := Parse(src)
			require.NoError(t, err)
			got, err := Generate(spec, "testdata/"+name+".xml")
			require.NoError(t, err)
			assert.Equal(t, string(want), string(got))
		})
	}
	goldens, err := filepath.Glob("../testdata/*.golden")
	require.NoError(t, err)
	assert.Len(t, goldens, len(specs), "a .golden file needs a spec partner")
}

func TestGenerateShapes(t *testing.T) {
	spec, err := Parse(wrap(testGenerateShapesXml))
	require.NoError(t, err)
	out, err := Generate(spec, "x.xml")
	require.NoError(t, err)
	s := string(out)
	assert.Contains(t, s, "package p\n")
	assert.NotContains(t, s, "import")
	assert.Contains(t, s, "func readRec(r *Reader) Rec {")
	assert.Contains(t, s, `out.A = r.I32("Rec.A")`)
	assert.Contains(t, s, "if cooked && !(ver >= 7) {")
	assert.Contains(t, s, `out.B = int64(r.I32("Rec.B32"))`)
	assert.Contains(t, s, "if !cooked && ver >= 9 {")
	assert.Contains(t, s, `_ = r.I64("Rec.Pad")`)
}

func TestGenerateBareLabels(t *testing.T) {
	spec, err := Parse(wrap(`<struct name="Rec" func="readRec" label=""><field name="A" type="i32"/><call go="fix({label}, &amp;{out})"/></struct>`))
	require.NoError(t, err)
	out, err := Generate(spec, "x.xml")
	require.NoError(t, err)
	assert.Contains(t, string(out), `out.A = r.I32("A")`)
	assert.Contains(t, string(out), `fix("", &out)`)
}

func TestGenerateBranchShapes(t *testing.T) {
	spec, err := Parse(wrap(`<struct name="Rec" go="rec" func="readRec" label="Rec">
	<switch type="i32" label="Kind" unknown="fail({value}, {label})">
		<case value="-1"/>
		<case value="2"><when type="i32" label="Has"><field name="A" type="i32"/></when></case>
	</switch>
</struct>`))
	require.NoError(t, err)
	out, err := Generate(spec, "x.xml")
	require.NoError(t, err)
	s := string(out)
	assert.Contains(t, s, "func readRec(r *Reader) rec {")
	assert.Contains(t, s, `switch tag := r.I32("Rec.Kind"); tag {`)
	assert.Contains(t, s, "case -1:\n\tcase 2:")
	assert.Contains(t, s, `if r.I32("Rec.Has") {`)
	assert.Contains(t, s, `fail(tag, "Rec")`)
}

func TestGenerateArrayShapes(t *testing.T) {
	spec, err := Parse(wrap(`<array of="int32" func="readInts" label="Ints" count="i32" elem="i32" alloc="true" check="clamp(&amp;{count}, {label})"/>`))
	require.NoError(t, err)
	out, err := Generate(spec, "x.xml")
	require.NoError(t, err)
	s := string(out)
	assert.Contains(t, s, `import "fmt"`)
	assert.Contains(t, s, "func readInts(r *Reader) []int32 {")
	assert.Contains(t, s, `count := r.I32("Ints.count")`)
	assert.Contains(t, s, `clamp(&count, "Ints")`)
	assert.Contains(t, s, "out := make([]int32, 0, count)")
	assert.Contains(t, s, `out = append(out, r.I32(fmt.Sprintf("Ints[%d]", i)))`)
}

func TestParseRejects(t *testing.T) {
	ok := `<struct name="R" func="f" label="R"><field name="A" type="i32"/></struct>`
	cases := map[string]struct {
		doc  []byte
		want string
	}{
		"wrong root":      {[]byte(`<binspec/>`), "want <api-bin>"},
		"no go":           {[]byte(`<api-bin>` + ok + `</api-bin>`), "needs a <go>"},
		"no struct":       {wrap(``), "declares no <struct>"},
		"unknown element": {wrap(`<table/>` + ok), "unknown element <table>"},
		"array attrs":     {wrap(`<array func="f" label="A" count="i32" elem="i32"/>`), "<array> needs of and func"},
		"array labels":    {wrap(`<array of="T" func="f" count="i32" elem="i32"/>`), "exactly one of label and label-expr"},
		"array type":      {wrap(`<array of="T" func="f" label="A" count="u8" elem="i32"/>`), `unknown type "u8"`},
		"array bool":      {wrap(`<array of="T" func="f" label="A" count="i32" elem="i32" alloc="yes"/>`), "want true or false"},
		"array nullable":  {wrap(`<array of="T" func="f" label="A" count="i32" elem="i32" nullable="1"/>`), "want true or false"},
		"array attr":      {wrap(`<array of="T" func="f" label="A" count="i32" elem="i32" max="4"/>`), `unknown attribute "max"`},
		"array params":    {[]byte(`<api-bin><go package="p"/><type name="i32" read="r({label})"/><array of="T" func="f" label="A" count="i32" elem="i32"/></api-bin>`), "has no params"},
		"unknown attr":    {wrap(`<struct name="R" func="f" label="R"><field name="A" type="i32" size="4"/></struct>`), `unknown attribute "size"`},
		"unknown type":    {wrap(`<struct name="R" func="f" label="R"><field name="A" type="u8"/></struct>`), `unknown type "u8"`},
		"unknown stream":  {wrap(`<struct name="R" func="f" label="R"><field name="A" type="i32" since="w:1"/></struct>`), `unknown version stream "w"`},
		"bad version ref": {wrap(`<struct name="R" func="f" label="R"><field name="A" type="i32" since="v"/></struct>`), "not stream:NAME"},
		"unknown flag":    {wrap(`<struct name="R" func="f" label="R"><field name="A" type="i32" if="x"/></struct>`), `unknown flag "x"`},
		"as without go":   {wrap(`<struct name="R" func="f" label="R"><field name="A" type="i32" as="i32"/></struct>`), "names no <type>"},
		"field no name":   {wrap(`<struct name="R" func="f" label="R"><field type="i32"/></struct>`), "<field> needs a name"},
		"skip no label":   {wrap(`<struct name="R" func="f" label="R"><skip type="i32"/></struct>`), "<skip> needs a label"},
		"empty group":     {wrap(`<struct name="R" func="f" label="R"><group/></struct>`), "<group> is empty"},
		"struct attrs":    {wrap(`<struct name="R"><field name="A" type="i32"/></struct>`), "needs name and func"},
		"no label":        {wrap(`<struct name="R" func="f"><field name="A" type="i32"/></struct>`), "exactly one of label and label-expr"},
		"two labels":      {wrap(`<struct name="R" func="f" label="R" label-expr="l"><field name="A" type="i32"/></struct>`), "exactly one of label and label-expr"},
		"call no go":      {wrap(`<struct name="R" func="f" label="R"><call/></struct>`), "<call> needs a go statement"},
		"null-if value":   {wrap(`<struct name="R" func="f" label="R"><group><null-if type="i32" label="N"/></group></struct>`), `<null-if> needs pointer="true"`},
		"bad pointer":     {wrap(`<struct name="R" func="f" label="R" pointer="yes"><field name="A" type="i32"/></struct>`), "want true or false"},
		"bits empty":      {wrap(`<struct name="R" func="f" label="R"><bits type="i32" label="B"/></struct>`), "<bits> has no <bit>"},
		"bits child":      {wrap(`<struct name="R" func="f" label="R"><bits type="i32" label="B"><flag/></bits></struct>`), "unknown element <flag>"},
		"bit range":       {wrap(`<struct name="R" func="f" label="R"><bits type="i32" label="B"><bit n="64" name="X"/></bits></struct>`), "n from 0 to 63"},
		"bit attr":        {wrap(`<struct name="R" func="f" label="R"><bits type="i32" label="B"><bit n="1" name="X" doc="d"/></bits></struct>`), `unknown attribute "doc"`},
		"bits no label":   {wrap(`<struct name="R" func="f" label="R"><bits type="i32"><bit n="1" name="X"/></bits></struct>`), "<bits> needs a label"},
		"duplicate type":  {wrap(`<type name="i32" read="x({label})"/>` + ok), "declared twice"},
		"switch empty":    {wrap(`<struct name="R" func="f" label="R"><switch type="i32" label="K"/></struct>`), "<switch> has no <case>"},
		"switch child":    {wrap(`<struct name="R" func="f" label="R"><switch type="i32" label="K"><arm/></switch></struct>`), "unknown element <arm>"},
		"case value":      {wrap(`<struct name="R" func="f" label="R"><switch type="i32" label="K"><case value="x"/></switch></struct>`), "<case> needs an integer value"},
		"case twice":      {wrap(`<struct name="R" func="f" label="R"><switch type="i32" label="K"><case value="1"/><case value="1"/></switch></struct>`), "declared twice"},
		"case attr":       {wrap(`<struct name="R" func="f" label="R"><switch type="i32" label="K"><case value="1" doc="d"/></switch></struct>`), `unknown attribute "doc"`},
		"case body":       {wrap(`<struct name="R" func="f" label="R"><switch type="i32" label="K"><case value="1"><field type="i32"/></case></switch></struct>`), "<field> needs a name"},
		"when empty":      {wrap(`<struct name="R" func="f" label="R"><when type="i32" label="H"/></struct>`), "<when> is empty"},
		"null-if in case": {wrap(`<struct name="R" func="f" label="R"><switch type="i32" label="K"><case value="1"><null-if type="i32" label="N"/></case></switch></struct>`), `<null-if> needs pointer="true"`},
		"type no hole":    {wrap(`<type name="u8" read="x()"/>` + ok), "{label} hole"},
		"version no hole": {wrap(`<version stream="w" test="x"/>` + ok), "{name} hole"},
		"second go":       {wrap(`<go package="q" params="r"/>` + ok), "more than one <go>"},
		"import no path":  {[]byte(`<api-bin><go package="p" params="r"><import/></go>` + ok + `</api-bin>`), "<import> needs a path"},
		"go child":        {[]byte(`<api-bin><go package="p" params="r"><x/></go>` + ok + `</api-bin>`), "unknown element <x>"},
		"go no params":    {[]byte(`<api-bin><go package="p"/>` + ok + `</api-bin>`), "has no params"},
		"go no package":   {[]byte(`<api-bin><go params="r"/>` + ok + `</api-bin>`), "<go> needs a package"},
		"enum attrs":      {wrap(`<enum name="E" func="f" unknown="x%d"><value n="0" name="A"/></enum>`), "needs name, func and go"},
		"enum unknown":    {wrap(`<enum name="E" func="f" go="uint8" unknown="x"><value n="0" name="A"/></enum>`), "exactly one %d"},
		"enum bad n":      {wrap(`<enum name="E" func="f" go="uint8" unknown="x%d"><value n="a" name="A"/></enum>`), "integer n and a name"},
		"enum dup":        {wrap(`<enum name="E" func="f" go="uint8" unknown="x%d"><value n="1" name="A"/><value n="1" name="B"/></enum>`), "declared twice"},
		"enum empty":      {wrap(`<enum name="E" func="f" go="uint8" unknown="x%d"/>`), "has no <value>"},
		"enum child":      {wrap(`<enum name="E" func="f" go="uint8" unknown="x%d"><item/></enum>`), "unknown element <item>"},
		"enum value attr": {wrap(`<enum name="E" func="f" go="uint8" unknown="x%d"><value n="1" name="A" doc="d"/></enum>`), `unknown attribute "doc"`},
		"flag no test":    {wrap(`<flag name="z"/>` + ok), "needs name and test"},
		"malformed xml":   {[]byte(`<api-bin>`), "EOF"},
		"unknown item":    {wrap(`<struct name="R" func="f" label="R"><array/></struct>`), "unknown element <array>"},
		"root attr":       {[]byte(`<api-bin version="1">` + ok + `</api-bin>`), `unknown attribute "version"`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(c.doc)
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.want)
		})
	}
}

func TestGenerateRejectsUnparsableOutput(t *testing.T) {
	spec, err := Parse([]byte(testGenerateRejectsUnparsableOutputXml))
	require.NoError(t, err)
	_, err = Generate(spec, "x.xml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not parse")
}

//go:embed testdata/test-generate-shapes.xml
var testGenerateShapesXml string

//go:embed testdata/test-generate-rejects-unparsable-output.xml
var testGenerateRejectsUnparsableOutputXml string
