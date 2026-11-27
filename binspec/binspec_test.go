package binspec

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
	spec, err := Parse(wrap(`
<struct name="Rec" func="readRec" label="Rec">
	<field name="A" type="i32"/>
	<field name="B" type="i32" as="i64" label="B32" until="v:7" if="cooked"/>
	<group since="v:9" unless="cooked">
		<skip type="i64" label="Pad"/>
	</group>
</struct>`))
	require.NoError(t, err)
	out, err := Generate(spec, "x.xml")
	require.NoError(t, err)
	s := string(out)
	assert.Contains(t, s, "package p\n")
	assert.NotContains(t, s, "import")
	assert.Contains(t, s, "func readRec(r *Reader) Rec {")
	assert.Contains(t, s, `v.A = r.I32("Rec.A")`)
	assert.Contains(t, s, "if cooked && !ver >= 7 {")
	assert.Contains(t, s, `v.B = int64(r.I32("Rec.B32"))`)
	assert.Contains(t, s, "if !cooked && ver >= 9 {")
	assert.Contains(t, s, `_ = r.I64("Rec.Pad")`)
}

func TestParseRejects(t *testing.T) {
	ok := `<struct name="R" func="f" label="R"><field name="A" type="i32"/></struct>`
	cases := map[string]struct {
		doc  []byte
		want string
	}{
		"wrong root":       {[]byte(`<binspec/>`), "want <api-bin>"},
		"no go":            {[]byte(`<api-bin>` + ok + `</api-bin>`), "needs a <go>"},
		"no struct":        {wrap(``), "declares no <struct>"},
		"unknown element":  {wrap(`<table/>` + ok), "unknown element <table>"},
		"unknown attr":     {wrap(`<struct name="R" func="f" label="R"><field name="A" type="i32" size="4"/></struct>`), `unknown attribute "size"`},
		"unknown type":     {wrap(`<struct name="R" func="f" label="R"><field name="A" type="u8"/></struct>`), `unknown type "u8"`},
		"unknown stream":   {wrap(`<struct name="R" func="f" label="R"><field name="A" type="i32" since="w:1"/></struct>`), `unknown version stream "w"`},
		"bad version ref":  {wrap(`<struct name="R" func="f" label="R"><field name="A" type="i32" since="v"/></struct>`), "not stream:NAME"},
		"unknown flag":     {wrap(`<struct name="R" func="f" label="R"><field name="A" type="i32" if="x"/></struct>`), `unknown flag "x"`},
		"as without go":    {wrap(`<struct name="R" func="f" label="R"><field name="A" type="i32" as="i32"/></struct>`), "names no <type>"},
		"field no name":    {wrap(`<struct name="R" func="f" label="R"><field type="i32"/></struct>`), "<field> needs a name"},
		"skip no label":    {wrap(`<struct name="R" func="f" label="R"><skip type="i32"/></struct>`), "<skip> needs a label"},
		"empty group":      {wrap(`<struct name="R" func="f" label="R"><group/></struct>`), "<group> is empty"},
		"struct attrs":     {wrap(`<struct name="R"><field name="A" type="i32"/></struct>`), "needs name, func and label"},
		"duplicate type":   {wrap(`<type name="i32" read="x({label})"/>` + ok), "declared twice"},
		"type no hole":     {wrap(`<type name="u8" read="x()"/>` + ok), "{label} hole"},
		"version no hole":  {wrap(`<version stream="w" test="x"/>` + ok), "{name} hole"},
		"second go":        {wrap(`<go package="q" params="r"/>` + ok), "more than one <go>"},
		"import no path":   {[]byte(`<api-bin><go package="p" params="r"><import/></go>` + ok + `</api-bin>`), "<import> needs a path"},
		"go child":         {[]byte(`<api-bin><go package="p" params="r"><x/></go>` + ok + `</api-bin>`), "unknown element <x>"},
		"go no params":     {[]byte(`<api-bin><go package="p"/>` + ok + `</api-bin>`), "needs package and params"},
		"flag no test":     {wrap(`<flag name="z"/>` + ok), "needs name and test"},
		"malformed xml":    {[]byte(`<api-bin>`), "EOF"},
		"unknown item":     {wrap(`<struct name="R" func="f" label="R"><array/></struct>`), "unknown element <array>"},
		"root attr":        {[]byte(`<api-bin version="1">` + ok + `</api-bin>`), `unknown attribute "version"`},
		"bad generated go": {nil, ""},
	}
	delete(cases, "bad generated go")
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(c.doc)
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.want)
		})
	}
}

func TestGenerateRejectsUnparsableOutput(t *testing.T) {
	spec, err := Parse([]byte(`<api-bin><go package="p" params="r *Reader"/>
<type name="i32" read="r.I32({label}"/>
<struct name="R" func="f" label="R"><field name="A" type="i32"/></struct></api-bin>`))
	require.NoError(t, err)
	_, err = Generate(spec, "x.xml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not parse")
}
