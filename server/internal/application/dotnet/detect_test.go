package dotnet_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/dotnet"
)

func materialise(t *testing.T, files ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, rel := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		body := ""
		if filepath.Base(rel) == "global.json" {
			body = `{"sdk":{"version":"10.0.100"}}`
		}
		require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
	}
	return root
}

func TestHasProject(t *testing.T) {
	cases := []struct {
		name  string
		files []string
		want  bool
	}{
		{"csproj", []string{"Api.csproj"}, true},
		{"fsproj", []string{"Lib.fsproj"}, true},
		{"sln", []string{"Shop.sln"}, true},
		{"slnx", []string{"Shop.slnx"}, true},
		{"directory build props", []string{"Directory.Build.props"}, true},
		{"global.json with sdk", []string{"global.json"}, true},
		{"project only one level down", []string{"src/Api/Api.csproj"}, false},
		{"go module", []string{"go.mod"}, false},
		{"unity project is not a dotnet build", []string{"Assets/Player.cs", "ProjectSettings/ProjectVersion.txt", "Game.sln", "Assembly-CSharp.csproj"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, dotnet.HasProject(materialise(t, tc.files...)))
		})
	}
}

func TestHasProject_GlobalJSONWithoutSDKIsNotDotnet(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "global.json"), []byte(`{"name":"x"}`), 0o644))
	require.False(t, dotnet.HasProject(root))
}

func TestBuildTarget(t *testing.T) {
	cases := []struct {
		name   string
		files  []string
		want   string
		wantOK bool
	}{
		{"single solution", []string{"Shop.sln", "src/Api/Api.csproj"}, "Shop.sln", true},
		{"sln and slnx of the same solution prefer slnx", []string{"Shop.sln", "Shop.slnx"}, "Shop.slnx", true},
		{"two different solutions are ambiguous", []string{"A.sln", "B.sln"}, "", false},
		{"single project", []string{"Api.csproj"}, "Api.csproj", true},
		{"several projects without a solution are ambiguous", []string{"Api.csproj", "Worker.csproj"}, "", false},
		{"solution one level down", []string{"Directory.Build.props", "src/Shop.sln"}, "src/Shop.sln", true},
		{"nothing to build", []string{"Directory.Build.props"}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := dotnet.BuildTarget(materialise(t, tc.files...))
			require.Equal(t, tc.wantOK, ok)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestIsUnityProject(t *testing.T) {
	require.True(t, dotnet.IsUnityProject(materialise(t, "Assets/a.cs", "ProjectSettings/ProjectVersion.txt")))
	require.False(t, dotnet.IsUnityProject(materialise(t, "Assets/a.cs")))
}
