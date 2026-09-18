package components

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	webApiCsproj = `<Project Sdk="Microsoft.NET.Sdk.Web">
  <PropertyGroup><TargetFramework>net9.0</TargetFramework></PropertyGroup>
  <ItemGroup>
    <PackageReference Include="Microsoft.EntityFrameworkCore.SqlServer" Version="9.0.0" />
  </ItemGroup>
</Project>`
	xunitCsproj = `<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup><TargetFramework>net9.0</TargetFramework></PropertyGroup>
  <ItemGroup>
    <PackageReference Include="Microsoft.NET.Test.Sdk" Version="17.12.0" />
    <PackageReference Include="xunit" Version="2.9.2" />
    <PackageReference Include="Microsoft.EntityFrameworkCore.InMemory" Version="9.0.0" />
  </ItemGroup>
</Project>`
	workerCsproj = `<Project Sdk="Microsoft.NET.Sdk.Worker"><PropertyGroup><TargetFramework>net9.0</TargetFramework></PropertyGroup></Project>`
)

func commandFor(comp domain.DetectedComponent, purpose domain.CommandPurpose) string {
	for _, c := range comp.Commands {
		if c.Purpose == purpose {
			return c.Command
		}
	}
	return ""
}

func TestDotnetSolutionIsOneBackend(t *testing.T) {
	res := Detect(treeFrom(t, fileset{
		"Shop.sln":                         "",
		"src/Api/Api.csproj":               webApiCsproj,
		"src/Api/Program.cs":               "var app = WebApplication.Create();",
		"tests/Api.Tests/Api.Tests.csproj": xunitCsproj,
	}))

	require.Equal(t, domain.RepoShapeSingle, res.Shape)
	require.Len(t, res.Components, 1)
	comp := res.Components[0]
	require.Equal(t, ".", comp.Path)
	require.Equal(t, "Shop", comp.PackageName)
	require.Equal(t, domain.ComponentRoleBackend, comp.Role)
	require.Equal(t, "dotnet build Shop.sln", commandFor(comp, domain.CommandBuild))
	require.Equal(t, "dotnet test Shop.sln", commandFor(comp, domain.CommandTest))
	require.Equal(t, "dotnet restore Shop.sln", commandFor(comp, domain.CommandInstall))
	require.Equal(t, "dotnet run --project src/Api/Api.csproj", commandFor(comp, domain.CommandDev))
	require.Equal(t, "dotnet ef database update --project src/Api/Api.csproj", commandFor(comp, domain.CommandMigrate))
}

func TestDotnetProjectsUnderOneSolutionAreNotAMonorepo(t *testing.T) {
	res := Detect(treeFrom(t, fileset{
		"Shop.slnx":            "",
		"Api/Api.csproj":       webApiCsproj,
		"Worker/Worker.csproj": workerCsproj,
	}))

	require.Equal(t, domain.RepoShapeSingle, res.Shape)
	require.Len(t, res.Components, 1)
	require.Empty(t, commandFor(res.Components[0], domain.CommandDev), "two runnable projects: dotnet run cannot pick one")
}

func TestDotnetApiBesideAWebAppIsAMonorepo(t *testing.T) {
	res := Detect(treeFrom(t, fileset{
		"api/Api.csproj":   webApiCsproj,
		"web/package.json": `{"dependencies":{"react":"^18.0.0","react-dom":"^18.0.0"}}`,
	}))

	require.Equal(t, domain.RepoShapeMonorepo, res.Shape)
	roles := map[string]domain.ComponentRole{}
	for _, c := range res.Components {
		roles[c.Path] = c.Role
	}
	require.Equal(t, domain.ComponentRoleBackend, roles["api"])
	require.Equal(t, "dotnet build Api.csproj", commandFor(res.Components[0], domain.CommandBuild))
}

func TestDotnetWorkerSdkIsAWorker(t *testing.T) {
	res := Detect(treeFrom(t, fileset{"Worker.csproj": workerCsproj}))

	require.Len(t, res.Components, 1)
	require.Equal(t, domain.ComponentRoleWorker, res.Components[0].Role)
	require.Equal(t, "net9.0", res.Components[0].Stack.Runtime.Version)
}

func TestDotnetSkipsUnityProjects(t *testing.T) {
	res := Detect(treeFrom(t, fileset{
		"Game.sln":                           "",
		"Assembly-CSharp.csproj":             `<Project ToolsVersion="4.0"/>`,
		"Assets/Scripts/Player.cs":           "public class Player {}",
		"ProjectSettings/ProjectVersion.txt": "m_EditorVersion: 2022.3.10f1",
	}))

	for _, c := range res.Components {
		require.Empty(t, commandFor(c, domain.CommandBuild), "a Unity project is built by the editor, not dotnet")
	}
}
