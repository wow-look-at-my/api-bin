package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const specPath = "testdata/uasset_package_tables.xml"

func runGen(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetArgs(append([]string{"gen"}, args...))
	err := rootCmd.Execute()
	return stdout.String(), err
}

func TestGenWritesStdout(t *testing.T) {
	want, err := os.ReadFile("testdata/uasset_package_tables.golden")
	require.NoError(t, err)
	got, err := runGen(t, specPath, "--out", "")
	require.NoError(t, err)
	assert.Equal(t, string(want), got)
}

func TestGenWritesFile(t *testing.T) {
	want, err := os.ReadFile("testdata/uasset_package_tables.golden")
	require.NoError(t, err)
	out := filepath.Join(t.TempDir(), "gen.go")
	stdout, err := runGen(t, specPath, "--out", out)
	require.NoError(t, err)
	assert.Empty(t, stdout)
	got, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Equal(t, string(want), string(got))
}

func TestGenReportsBadInput(t *testing.T) {
	_, err := runGen(t, filepath.Join(t.TempDir(), "missing.xml"), "--out", "")
	require.Error(t, err)

	bad := filepath.Join(t.TempDir(), "bad.xml")
	require.NoError(t, os.WriteFile(bad, []byte(`<binspec/>`), 0o644))
	_, err = runGen(t, bad, "--out", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), bad)
}
