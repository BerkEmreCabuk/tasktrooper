package prompt_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mapFile(body string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(body)}
}

func TestLoadFSParsesAndRenders(t *testing.T) {
	fsys := fstest.MapFS{
		"prompts/orchestrator/planner_system.md": mapFile(
			"---\nkey: orchestrator.planner_system\nversion: 1\ninputs: [Task]\n---\nPlan for: {{.Task}}\n",
		),
	}
	lib, err := prompt.LoadFS(fsys)
	require.NoError(t, err)

	out, err := lib.Render("orchestrator.planner_system", struct{ Task string }{Task: "ship it"})
	require.NoError(t, err)
	assert.Equal(t, "Plan for: ship it", out)
}

func TestDeriveKeyForGuardsAndTools(t *testing.T) {
	fsys := fstest.MapFS{
		"guards/no_secrets_on_argv.md": mapFile("---\nkey: guard.no_secrets_on_argv\n---\nRefused: secrets never go on argv.\n"),
		"tools/read_file.md":           mapFile("---\nkey: tool.read_file\n---\nReads a file.\n"),
	}
	lib, err := prompt.LoadFS(fsys)
	require.NoError(t, err)

	out, err := lib.Render("guard.no_secrets_on_argv", struct{}{})
	require.NoError(t, err)
	assert.Equal(t, "Refused: secrets never go on argv.", out)

	out, err = lib.Render("tool.read_file", struct{}{})
	require.NoError(t, err)
	assert.Equal(t, "Reads a file.", out)
}

func TestLoadFSKeyPathMismatch(t *testing.T) {
	fsys := fstest.MapFS{
		"prompts/orchestrator/planner_system.md": mapFile(
			"---\nkey: orchestrator.wrong_name\n---\nBody\n",
		),
	}
	_, err := prompt.LoadFS(fsys)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prompts/orchestrator/planner_system.md")
	assert.Contains(t, err.Error(), "orchestrator.wrong_name")
	assert.Contains(t, err.Error(), "orchestrator.planner_system")
}

func TestLoadFSMissingFrontMatter(t *testing.T) {
	fsys := fstest.MapFS{
		"prompts/orchestrator/planner_system.md": mapFile("no front matter here\n"),
	}
	_, err := prompt.LoadFS(fsys)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prompts/orchestrator/planner_system.md")
}

func TestLoadFSUnrecognizedPath(t *testing.T) {
	fsys := fstest.MapFS{
		"stray.md": mapFile("---\nkey: whatever\n---\nBody\n"),
	}
	_, err := prompt.LoadFS(fsys)
	require.Error(t, err)
}

func TestLoadFSSkipsReadmeSchemasAndDotfiles(t *testing.T) {
	fsys := fstest.MapFS{
		"README.md":            mapFile("not a template"),
		"schemas/planner.json": mapFile(`{"type":"object"}`),
		"partials/.gitkeep":    mapFile(""),
		"prompts/a/b.md":       mapFile("---\nkey: a.b\n---\nHi\n"),
	}
	lib, err := prompt.LoadFS(fsys)
	require.NoError(t, err)
	out, err := lib.Render("a.b", struct{}{})
	require.NoError(t, err)
	assert.Equal(t, "Hi", out)
}

func TestRenderMissingKeyErrors(t *testing.T) {
	fsys := fstest.MapFS{
		"prompts/a/b.md": mapFile("---\nkey: a.b\n---\nValue: {{.Missing}}"),
	}
	lib, err := prompt.LoadFS(fsys)
	require.NoError(t, err)

	_, err = lib.Render("a.b", map[string]any{"Other": "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "a.b")
}

func TestRenderUnknownKey(t *testing.T) {
	lib, err := prompt.LoadFS(fstest.MapFS{})
	require.NoError(t, err)
	_, err = lib.Render("does.not.exist", struct{}{})
	require.Error(t, err)
}

func TestPartialsAreIncludedButNotRequiredToBeDefined(t *testing.T) {
	fsys := fstest.MapFS{
		"partials/footer.md": mapFile("---\nkey: partial.footer\n---\n-- {{.Name}} --"),
		"prompts/a/b.md":     mapFile("---\nkey: a.b\n---\nBody\n{{partial \"footer\" .}}"),
	}
	lib, err := prompt.LoadFS(fsys)
	require.NoError(t, err)

	out, err := lib.Render("a.b", struct{ Name string }{Name: "sig"})
	require.NoError(t, err)
	assert.Equal(t, "Body\n-- sig --", out)
}

func TestPartialNotFound(t *testing.T) {
	fsys := fstest.MapFS{
		"prompts/a/b.md": mapFile("---\nkey: a.b\n---\n{{partial \"missing\" .}}"),
	}
	lib, err := prompt.LoadFS(fsys)
	require.NoError(t, err)
	_, err = lib.Render("a.b", struct{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing")
}

// TestByteExactBody pins the exact rule documented in
// catalog/system/README.md: everything after the closing "---" line is kept
// verbatim except exactly one trailing "\n", stripped only if present.
func TestByteExactBody(t *testing.T) {
	cases := []struct {
		name string
		file string
		want string
	}{
		{
			name: "single trailing newline stripped",
			file: "---\nkey: a.b\n---\nline one\nline two\n",
			want: "line one\nline two",
		},
		{
			name: "no trailing newline left untouched",
			file: "---\nkey: a.b\n---\nline one\nline two",
			want: "line one\nline two",
		},
		{
			name: "blank line before body preserved",
			file: "---\nkey: a.b\n---\n\nBody starts after a blank line\n",
			want: "\nBody starts after a blank line",
		},
		{
			name: "trailing blank line preserved (only ONE newline is stripped)",
			file: "---\nkey: a.b\n---\nBody\n\n",
			want: "Body\n",
		},
		{
			name: "leading spaces preserved",
			file: "---\nkey: a.b\n---\n   indented on purpose\n",
			want: "   indented on purpose",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lib, err := prompt.LoadFS(fstest.MapFS{"prompts/a/b.md": mapFile(tc.file)})
			require.NoError(t, err)
			out, err := lib.Render("a.b", struct{}{})
			require.NoError(t, err)
			assert.Equal(t, tc.want, out)
		})
	}
}

func TestHelperFuncs(t *testing.T) {
	body := "---\nkey: a.b\n---\n" +
		"{{join \", \" .Items}}\n" +
		"{{bullets .Items}}\n" +
		"{{trunc 5 .Long}}\n" +
		"{{indent 2 .Multi}}\n" +
		"{{plural (len .Items) \"item\" \"items\"}}\n" +
		"{{lower .Upper}} {{upper .Lower}}"
	lib, err := prompt.LoadFS(fstest.MapFS{"prompts/a/b.md": mapFile(body)})
	require.NoError(t, err)

	data := struct {
		Items []string
		Long  string
		Multi string
		Upper string
		Lower string
	}{
		Items: []string{"a", "b", "c"},
		Long:  "abcdefgh",
		Multi: "x\ny",
		Upper: "SHOUT",
		Lower: "whisper",
	}
	out, err := lib.Render("a.b", data)
	require.NoError(t, err)

	want := strings.Join([]string{
		"a, b, c",
		"- a\n- b\n- c",
		"abcde",
		"  x\n  y",
		"items",
		"shout WHISPER",
	}, "\n")
	assert.Equal(t, want, out)
}

func TestMustRenderPanicsOnError(t *testing.T) {
	lib, err := prompt.LoadFS(fstest.MapFS{})
	require.NoError(t, err)
	assert.Panics(t, func() {
		lib.MustRender("missing.key", struct{}{})
	})
}

// --- typed keys / registry ---

var testGreetKey = prompt.Define[struct{ Name string }]("test.library.greet", struct{ Name string }{Name: "sample"})

func TestDefineAndDefinedKeys(t *testing.T) {
	found := false
	for _, k := range prompt.DefinedKeys() {
		if k.Name == testGreetKey.Name() {
			found = true
		}
	}
	assert.True(t, found, "Define must register into DefinedKeys()")
}

func TestDefineDuplicatePanics(t *testing.T) {
	assert.Panics(t, func() {
		prompt.Define[struct{}]("test.library.greet", struct{}{})
	})
}

func TestDefinedKeyRenderWithAgainstCandidateLibrary(t *testing.T) {
	lib, err := prompt.LoadFS(fstest.MapFS{
		"prompts/test/library/greet.md": mapFile("---\nkey: test.library.greet\n---\nHello, {{.Name}}!"),
	})
	require.NoError(t, err)

	for _, k := range prompt.DefinedKeys() {
		if k.Name != testGreetKey.Name() {
			continue
		}
		out, err := k.RenderWith(lib)
		require.NoError(t, err)
		assert.Equal(t, "Hello, sample!", out)
		return
	}
	t.Fatal("key not found in registry")
}

func TestSetDefaultOverridesKeyRender(t *testing.T) {
	lib, err := prompt.LoadFS(fstest.MapFS{
		"prompts/test/library/greet.md": mapFile("---\nkey: test.library.greet\n---\nOverlay hi, {{.Name}}."),
	})
	require.NoError(t, err)
	prompt.SetDefault(lib)

	assert.Equal(t, "Overlay hi, sample.", testGreetKey.Render(struct{ Name string }{Name: "sample"}))
}
