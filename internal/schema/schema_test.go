package schema

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/aneviaro/stratz-mcp/internal/stratz"
	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

type fixtureExecutor struct {
	data    json.RawMessage
	request stratz.Request
	used    int
}

func (executor *fixtureExecutor) Execute(
	_ context.Context,
	budget *stratz.RequestBudget,
	request stratz.Request,
) (*stratz.Response, error) {
	executor.request = request
	executor.used = budget.Remaining()
	return &stratz.Response{HTTPStatus: 200, Data: executor.data}, nil
}

func TestPullIsDeterministicAndRestricted(t *testing.T) {
	data := fixtureData(t)
	first := filepath.Join(t.TempDir(), "first")
	second := filepath.Join(t.TempDir(), "second")

	firstExecutor := &fixtureExecutor{data: data}
	firstManifest, err := Pull(context.Background(), firstExecutor, first)
	if err != nil {
		t.Fatal(err)
	}
	secondManifest, err := Pull(
		context.Background(),
		&fixtureExecutor{data: data},
		second,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(firstManifest, secondManifest) {
		t.Fatalf("manifests differ:\n%#v\n%#v", firstManifest, secondManifest)
	}
	if firstManifest.SchemaHash != "sha256:54b24b6f5a82cc1528cc668d28f0d6dc90919ffedcb8a10e336f5539e9fe5a20" {
		t.Fatalf("schema hash = %q", firstManifest.SchemaHash)
	}
	if !firstManifest.Restricted {
		t.Fatal("manifest did not mark fetched data restricted")
	}
	if firstExecutor.request.OperationName != OperationName ||
		firstExecutor.request.Query != IntrospectionQuery ||
		firstExecutor.request.Mode != stratz.ModeCurated ||
		firstExecutor.request.AllowRetries {
		t.Fatalf("introspection request = %#v", firstExecutor.request)
	}

	firstFiles := readTree(t, first)
	secondFiles := readTree(t, second)
	if !reflect.DeepEqual(firstFiles, secondFiles) {
		t.Fatal("fixture pulls produced different files")
	}
	for _, path := range []string{
		ManifestFile,
		IntrospectionFile,
		FullSchemaFile,
		MetadataFile,
		RestrictionFile,
		"schema/player.graphql",
		"schema/match.graphql",
		"schema/hero.graphql",
		"schema/league.graphql",
		"schema/live.graphql",
		"schema/constants.graphql",
	} {
		if _, ok := firstFiles[path]; !ok {
			t.Errorf("missing generated artifact %s", path)
		}
	}
	if !bytes.Contains(firstFiles[RestrictionFile], []byte("STRATZ_RESTRICTED_UPSTREAM_DATA")) {
		t.Fatal("restriction marker is absent")
	}
	if !bytes.Contains(
		firstFiles[IntrospectionFile],
		[]byte(`"source": "authenticated STRATZ GraphQL introspection"`),
	) {
		t.Fatal("introspection snapshot lacks embedded restriction metadata")
	}
	if _, err := gqlparser.LoadSchema(&ast.Source{
		Name:  FullSchemaFile,
		Input: string(firstFiles[FullSchemaFile]),
	}); err != nil {
		t.Fatalf("generated full SDL is invalid: %v", err)
	}
	for path, data := range firstFiles {
		info, err := os.Stat(filepath.Join(first, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s permissions = %o, want owner-only", path, info.Mode().Perm())
		}
		if strings.HasSuffix(path, ".graphql") &&
			!bytes.Contains(data, []byte("Restricted upstream data")) {
			t.Errorf("%s lacks restriction notice", path)
		}
		if strings.HasSuffix(path, ".graphql") {
			if _, err := gqlparser.LoadSchema(&ast.Source{
				Name:  path,
				Input: string(data),
			}); err != nil {
				t.Errorf("%s is invalid GraphQL SDL: %v", path, err)
			}
		}
	}
}

func TestGenerateRejectsInvalidIntrospection(t *testing.T) {
	if _, _, err := Generate(Document{}); err == nil {
		t.Fatal("Generate accepted an empty document")
	}
}

func TestWriteBundleRejectsSymlinkDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permission behavior")
	}
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := WriteBundle(link, map[string][]byte{"x": []byte("x")}); err == nil {
		t.Fatal("WriteBundle accepted a symlink output directory")
	}
}

func fixtureData(t *testing.T) json.RawMessage {
	t.Helper()
	data, err := os.ReadFile("testdata/introspection-data.json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func readTree(t *testing.T, root string) map[string][]byte {
	t.Helper()
	result := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result[filepath.ToSlash(relative)] = data
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
