package all

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// exceptionMap holds those plugins which differ in conventions when defining plugins.
// Most plugins follow the convention of pkg-name to plugin-name.
// For ex, Pivot processor pkg github.com/influxdata/telegraf/plugins/processors/pivot maps directly to
// the last element of the pkg i.e "pivot"
// But in case of "aws_ec2" processor, the pkg is defined as "github.com/influxdata/telegraf/plugins/processors/aws/ec2".
// This ensures package names are not tied with plugin names.
// it should be of the form <pkg-name>: <plugin-name>
var exceptionMap = map[string]string{
	"github.com/influxdata/telegraf/plugins/processors/aws/ec2": "aws_ec2",
}

func Test_AllPlugins(t *testing.T) {
	pluginDirs := []string{"aggregators", "inputs", "outputs", "parsers", "processors", "secretstores"}
	for _, dir := range pluginDirs {
		testPluginDirectory(t, dir)
	}
}

func testPluginDirectory(t *testing.T, directory string) {
	allDir := filepath.Join(directory, "all")
	err := filepath.WalkDir(allDir, func(goPluginFile string, d fs.DirEntry, walkErr error) error {
		require.NoError(t, walkErr)
		if d.IsDir() || strings.HasSuffix(d.Name(), "_test.go") || strings.EqualFold(d.Name(), "all.go") {
			return nil
		}
		t.Run(goPluginFile, func(t *testing.T) {
			parseSourceFile(t, goPluginFile, directory)
		})
		return nil
	})
	require.NoError(t, err)
}

func parseSourceFile(t *testing.T, goPluginFile, pluginCategory string) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, goPluginFile, nil, parser.ParseComments)
	require.NoError(t, err)

	foundGoBuild := false
	for _, cg := range node.Comments {
		for _, comm := range cg.List {
			if !strings.HasPrefix(comm.Text, "//go:build") {
				continue
			}
			foundGoBuild = true
			plugin := resolvePluginFromImports(t, node.Imports)
			testBuildTags(t, comm.Text, pluginCategory, plugin)
		}
	}
	require.Truef(t, foundGoBuild, "%s does not contain go:build tag", goPluginFile)
}

func resolvePluginFromImports(t *testing.T, imports []*ast.ImportSpec) string {
	// should contain one or more import statements
	require.NotEmpty(t, imports)

	// trim the path surrounded by quotes
	importPath := strings.Trim(imports[0].Path.Value, "\"")

	// check if present in exceptionMap
	plugin, ok := exceptionMap[importPath]
	if ok {
		return plugin
	}
	return filepath.Base(importPath)
}

func testBuildTags(t *testing.T, buildComment, pluginCategory, plugin string) {
	tags := strings.Split(buildComment, "||")
	// tags might contain spaces and hence trim
	tags = stringMap(tags, strings.TrimSpace)

	require.Len(t, tags, 3)
	require.Contains(t, tags, "!custom")
	require.Contains(t, tags, pluginCategory)

	actual := getPluginBuildTag(tags, pluginCategory)
	expected := fmt.Sprintf("%s.%s", pluginCategory, plugin)
	require.Equal(t, expected, actual, "invalid build tag")
}

// getPluginBuildTag takes a slice of tags and returns the build tag corresponding to this plugin type.
//
// For ex ["!custom", "inputs", "inputs.docker"] returns "inputs.docker"
func getPluginBuildTag(tags []string, pluginCategory string) string {
	for _, tag := range tags {
		if strings.HasPrefix(tag, pluginCategory+".") {
			return tag
		}
	}
	return ""
}

func stringMap(elems []string, transFormFunc func(string) string) []string {
	result := make([]string, len(elems))
	for i, elem := range elems {
		result[i] = strings.TrimPrefix(transFormFunc(elem), "//go:build ")
	}
	return result
}
