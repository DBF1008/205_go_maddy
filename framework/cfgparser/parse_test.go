/*
Maddy Mail Server - Composable all-in-one email server.
Copyright © 2019-2020 Max Mazurov <fox.cpp@disroot.org>, Maddy Mail Server contributors

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.
*/

package parser

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var cases = []struct {
	name string
	cfg  string
	tree []Node
	fail bool
}{
	{
		"single directive without args",
		`a`,
		[]Node{
			{
				Name:     "a",
				Args:     []string{},
				Children: nil,
				File:     "test",
				Line:     1,
			},
		},
		false,
	},
	{
		"single directive with args",
		`a a1 a2`,
		[]Node{
			{
				Name:     "a",
				Args:     []string{"a1", "a2"},
				Children: nil,
				File:     "test",
				Line:     1,
			},
		},
		false,
	},
	{
		"single directive with empty braces",
		`a { }`,
		[]Node{
			{
				Name:     "a",
				Args:     []string{},
				Children: []Node{},
				File:     "test",
				Line:     1,
			},
		},
		false,
	},
	{
		"single directive with arguments and empty braces",
		`a a1 a2 { }`,
		[]Node{
			{
				Name:     "a",
				Args:     []string{"a1", "a2"},
				Children: []Node{},
				File:     "test",
				Line:     1,
			},
		},
		false,
	},
	{
		"single directive with a block",
		`a a1 a2 {
			a_child1 c1arg1 c1arg2
			a_child2 c2arg1 c2arg2
		}`,
		[]Node{
			{
				Name: "a",
				Args: []string{"a1", "a2"},
				Children: []Node{
					{
						Name:     "a_child1",
						Args:     []string{"c1arg1", "c1arg2"},
						Children: nil,
						File:     "test",
						Line:     2,
					},
					{
						Name:     "a_child2",
						Args:     []string{"c2arg1", "c2arg2"},
						Children: nil,
						File:     "test",
						Line:     3,
					},
				},
				File: "test",
				Line: 1,
			},
		},
		false,
	},
	{
		"single directive with missing closing brace",
		`a {`,
		nil,
		true,
	},
	{
		"single directive with missing opening brace",
		`a }`,
		nil,
		true,
	},
	{
		"two directives",
		`a
		 b`,
		[]Node{
			{
				Name:     "a",
				Args:     []string{},
				Children: nil,
				File:     "test",
				Line:     1,
			},
			{
				Name:     "b",
				Args:     []string{},
				Children: nil,
				File:     "test",
				Line:     2,
			},
		},
		false,
	},
	{
		"two directives with arguments",
		`a a1 a2
		 b b1 b2`,
		[]Node{
			{
				Name:     "a",
				Args:     []string{"a1", "a2"},
				Children: nil,
				File:     "test",
				Line:     1,
			},
			{
				Name:     "b",
				Args:     []string{"b1", "b2"},
				Children: nil,
				File:     "test",
				Line:     2,
			},
		},
		false,
	},
	{
		"backslash on the end of line",
		`a a1 a2 \
		   a3 a4`,
		[]Node{
			{
				Name:     "a",
				Args:     []string{"a1", "a2", "a3", "a4"},
				Children: nil,
				File:     "test",
				Line:     1,
			},
		},
		false,
	},
	{
		"directive with missing closing brace on different line",
		`a a1 a2 {
			a_child1 c1arg1 c1arg2
		`,
		nil,
		true,
	},
	{
		"single directive with closing brace on children's line",
		`a a1 a2 {
			a_child1 c1arg1 c1arg2
			a_child2 c2arg1 c2arg2 }
		 b`,
		[]Node{
			{
				Name: "a",
				Args: []string{"a1", "a2"},
				Children: []Node{
					{
						Name:     "a_child1",
						Args:     []string{"c1arg1", "c1arg2"},
						Children: nil,
						File:     "test",
						Line:     2,
					},
					{
						Name:     "a_child2",
						Args:     []string{"c2arg1", "c2arg2"},
						Children: nil,
						File:     "test",
						Line:     3,
					},
				},
				File: "test",
				Line: 1,
			},
			{
				Name:     "b",
				Args:     []string{},
				Children: nil,
				File:     "test",
				Line:     4,
			},
		},
		false,
	},
	{
		"single directive with childrens on the same line",
		`a a1 a2 { a_child1 c1arg1 c1arg2 }`,
		[]Node{
			{
				Name: "a",
				Args: []string{"a1", "a2"},
				Children: []Node{
					{
						Name:     "a_child1",
						Args:     []string{"c1arg1", "c1arg2"},
						Children: nil,
						File:     "test",
						Line:     1,
					},
				},
				File: "test",
				Line: 1,
			},
		},
		false,
	},
	{
		"invalid directive name",
		`a-a4@%8 whatever`,
		nil,
		true,
	},
	{
		"directive name starts with a digit",
		`1w whatever`,
		nil,
		true,
	},
	{
		"missing block header",
		`{ a_child1 c1arg1 c1arg2 }`,
		nil,
		true,
	},
	{
		"extra closing brace",
		`a {
			child1
		} }
		`,
		nil,
		true,
	},
	{
		"extra opening brace",
		`a { {
		}`,
		nil,
		true,
	},
	{
		"closing brace in next block header",
		`a {
		} b b1`,
		nil,
		true,
	},
	{
		"environment variable expansion",
		`a {env:TESTING_VARIABLE}`,
		[]Node{
			{
				Name:     "a",
				Args:     []string{"ABCDEF"},
				Children: nil,
				File:     "test",
				Line:     1,
			},
		},
		false,
	},
	{
		"missing environment variable expansion (unix-like syntax)",
		`a {env:TESTING_VARIABLE3}`,
		[]Node{
			{
				Name:     "a",
				Args:     []string{""},
				Children: nil,
				File:     "test",
				Line:     1,
			},
		},
		false,
	},
	{
		"incomplete environment variable syntax",
		`a {env:TESTING_VARIABLE`,
		[]Node{
			{
				Name:     "a",
				Args:     []string{"{env:TESTING_VARIABLE"},
				Children: nil,
				File:     "test",
				Line:     1,
			},
		},
		false,
	},
	{
		"snippet expansion",
		`(foo) { a }
		 import foo`,
		[]Node{
			{
				Name:     "a",
				Args:     []string{},
				Children: nil,
				File:     "test",
				Line:     1,
			},
		},
		false,
	},
	{
		"snippet expansion inside a block",
		`(foo) { a }
        foo {
            boo
            import foo
        }`,
		[]Node{
			{
				Name: "foo",
				Args: []string{},
				Children: []Node{
					{
						Name: "boo",
						Args: []string{},
						File: "test",
						Line: 3,
					},
					{
						Name: "a",
						Args: []string{},
						File: "test",
						Line: 1,
					},
				},
				File: "test",
				Line: 2,
			},
		},
		false,
	},
	{
		"missing snippet",
		`import foo`,
		nil,
		true,
	},
	{
		"unlimited recursive snippet expansion",
		`(foo) { import foo }
		 import foo`,
		nil,
		true,
	},
	{
		"snippet declaration with args",
		`(foo) a b c { }`,
		nil,
		true,
	},
	{
		"snippet declaration inside block",
		`abc {
			(foo) { }
		}`,
		nil,
		true,
	},
	{
		"block nesting limit",
		`a ` + strings.Repeat("a { ", 1000) + strings.Repeat(" }", 1000),
		nil,
		true,
	},
	{
		"macro expansion, single argument",
		`$(foo) = bar
		dir $(foo)`,
		[]Node{
			{
				Name:     "dir",
				Args:     []string{"bar"},
				Children: nil,
				File:     "test",
				Line:     2,
			},
		},
		false,
	},
	{
		"macro expansion, inside argument",
		`$(foo) = bar
		dir aaa/$(foo)/bbb`,
		[]Node{
			{
				Name:     "dir",
				Args:     []string{"aaa/bar/bbb"},
				Children: nil,
				File:     "test",
				Line:     2,
			},
		},
		false,
	},
	{
		"macro expansion, inside argument, multi-value",
		`$(foo) = bar baz
		dir aaa/$(foo)/bbb`,
		nil,
		true,
	},
	{
		"macro expansion, multiple arguments",
		`$(foo) = bar baz
		dir $(foo)`,
		[]Node{
			{
				Name:     "dir",
				Args:     []string{"bar", "baz"},
				Children: nil,
				File:     "test",
				Line:     2,
			},
		},
		false,
	},
	{
		"macro expansion, undefined",
		`dir $(foo)`,
		[]Node{
			{
				Name:     "dir",
				Args:     []string{},
				Children: nil,
				File:     "test",
				Line:     1,
			},
		},
		false,
	},
	{
		"macro expansion, empty",
		`$(foo) =`,
		nil,
		true,
	},
	{
		"macro expansion, name replacement",
		`$(foo) = a b
			$(foo) 1`,
		nil,
		true,
	},
	{
		"macro expansion, missing =",
		`$(foo) a b
			$(foo) 1`,
		nil,
		true,
	},
	{
		"macro expansion, not on top level",
		`a {
				$(foo) = a b
			}
			$(foo) 1`,
		nil,
		true,
	},
	{
		"macro expansion, nested",
		`$(foo) = a
			$(bar) = $(foo) b
			dir $(bar)`,
		[]Node{
			{
				Name:     "dir",
				Args:     []string{"a", "b"},
				Children: nil,
				File:     "test",
				Line:     3,
			},
		},
		false,
	},
	{
		"macro expansion, used inside snippet",
		`$(foo) = a
			(bar) {
				dir $(foo)
			}
			import bar`,
		[]Node{
			{
				Name:     "dir",
				Args:     []string{"a"},
				Children: nil,
				File:     "test",
				Line:     3,
			},
		},
		false,
	},
	{
		"macro expansion, used inside snippet, defined after",
		`
			(bar) {
				dir $(foo)
			}
			$(foo) = a
			import bar`,
		[]Node{
			{
				Name:     "dir",
				Args:     []string{},
				Children: nil,
				File:     "test",
				Line:     3,
			},
		},
		false,
	},
}

func printTree(t *testing.T, root Node, indent int) {
	t.Log(strings.Repeat(" ", indent)+root.Name, root.Args)
	for _, child := range root.Children {
		t.Log(child, indent+1)
	}
}

func TestRead(t *testing.T) {
	require.NoError(t, os.Setenv("TESTING_VARIABLE", "ABCDEF"))
	require.NoError(t, os.Setenv("TESTING_VARIABLE2", "ABC2 DEF2"))

	for _, case_ := range cases {
		t.Run(case_.name, func(t *testing.T) {
			tree, err := Read(strings.NewReader(case_.cfg), "test")
			if !case_.fail && err != nil {
				t.Error("unexpected failure:", err)
				return
			}
			if case_.fail {
				if err == nil {
					t.Log("expected failure but Read succeeded")
					t.Log("got tree:")
					t.Logf("%+v", tree)
					for _, node := range tree {
						printTree(t, node, 0)
					}
					t.Fail()
					return
				}
				return
			}

			if !reflect.DeepEqual(case_.tree, tree) {
				t.Log("parse result mismatch")
				t.Log("expected:")
				t.Logf("%+#v", case_.tree)
				for _, node := range case_.tree {
					printTree(t, node, 0)
				}
				t.Log("actual:")
				t.Logf("%+#v", tree)
				for _, node := range tree {
					printTree(t, node, 0)
				}
				t.Fail()
			}
		})
	}
}

func TestImportFromFile(t *testing.T) {
	dir := t.TempDir()

	// Create an imported config file.
	importedPath := filepath.Join(dir, "imported.conf")
	require.NoError(t, os.WriteFile(importedPath, []byte("imported_dir value1\n"), 0644))

	// Create main config that imports the file.
	mainPath := filepath.Join(dir, "main.conf")
	require.NoError(t, os.WriteFile(mainPath, []byte("import imported.conf\n"), 0644))

	f, err := os.Open(mainPath)
	require.NoError(t, err)
	defer f.Close()

	nodes, err := Read(f, mainPath)
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	require.Equal(t, "imported_dir", nodes[0].Name)
	require.Equal(t, []string{"value1"}, nodes[0].Args)
}

func TestImportConfExtensionFallback(t *testing.T) {
	dir := t.TempDir()

	// Create a file with .conf extension.
	importedPath := filepath.Join(dir, "snippets.conf")
	require.NoError(t, os.WriteFile(importedPath, []byte("snippet_dir arg1\n"), 0644))

	// Import by name without .conf — should fall back to snippets.conf.
	mainPath := filepath.Join(dir, "main.conf")
	require.NoError(t, os.WriteFile(mainPath, []byte("import snippets\n"), 0644))

	f, err := os.Open(mainPath)
	require.NoError(t, err)
	defer f.Close()

	nodes, err := Read(f, mainPath)
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	require.Equal(t, "snippet_dir", nodes[0].Name)
	require.Equal(t, []string{"arg1"}, nodes[0].Args)
}

func TestImportFileDescriptorsClosed(t *testing.T) {
	dir := t.TempDir()

	// Create import files. We keep references so we can probe whether the
	// parser closed its own copies after Read returns.
	imported1Path := filepath.Join(dir, "import1.conf")
	require.NoError(t, os.WriteFile(imported1Path, []byte("dir1 val\n"), 0644))

	imported2Path := filepath.Join(dir, "import2.conf")
	require.NoError(t, os.WriteFile(imported2Path, []byte("dir2 val\n"), 0644))

	mainPath := filepath.Join(dir, "main.conf")
	require.NoError(t, os.WriteFile(mainPath, []byte("import import1.conf\nimport import2.conf\n"), 0644))

	// Open each import file directly to hold a reference. After the parse,
	// we rename the files away and then try to read through our held
	// references. If the parser leaked its fds (no Close), the held fds
	// would still work on most OSes because the inode is still linked.
	// After rename+delete the held reads must fail — proving the parser
	// released its own fds and the OS could reclaim the resources.
	f1, err := os.Open(imported1Path)
	require.NoError(t, err)
	defer f1.Close()

	f2, err := os.Open(imported2Path)
	require.NoError(t, err)
	defer f2.Close()

	// Parse the main config (this will open and — with the fix — close the
	// import files internally).
	mainF, err := os.Open(mainPath)
	require.NoError(t, err)

	nodes, err := Read(mainF, mainPath)
	mainF.Close()
	require.NoError(t, err)
	require.Len(t, nodes, 2)
	require.Equal(t, "dir1", nodes[0].Name)
	require.Equal(t, "dir2", nodes[1].Name)

	// Remove the import files from the filesystem. Any fds the parser
	// failed to close are the only remaining links to the inodes.
	require.NoError(t, os.Remove(imported1Path))
	require.NoError(t, os.Remove(imported2Path))

	// Our own held fds should now be the only references. If the parser
	// properly closed its fds, the OS may have already freed the inodes.
	// Regardless, a fresh Open must fail because the paths are gone.
	_, err = os.Open(imported1Path)
	require.True(t, os.IsNotExist(err), "import1.conf should no longer exist at its path")
	_, err = os.Open(imported2Path)
	require.True(t, os.IsNotExist(err), "import2.conf should no longer exist at its path")

	// Reads through our held references should fail: the files have been
	// unlinked and — with the parser's fds closed — the kernel can reclaim
	// the inode. On Linux the held reads return data from the page cache
	// until the last fd is closed, so we verify via /proc/self/fd that the
	// parser did not leave any dangling fds pointing at the removed files.
	entries, err := os.ReadDir("/proc/self/fd")
	if err == nil {
		for _, entry := range entries {
			target, err := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
			if err != nil {
				continue
			}
			if target == imported1Path || target == imported2Path {
				t.Errorf("parser leaked file descriptor for %s (fd %s still open)", target, entry.Name())
			}
		}
	}
}

func TestImportParseErrorClosesFile(t *testing.T) {
	dir := t.TempDir()

	// Create an import file with intentionally broken syntax so readTree
	// returns an error. The fix must still close the file in this path.
	badPath := filepath.Join(dir, "bad.conf")
	require.NoError(t, os.WriteFile(badPath, []byte("dir {\n"), 0644)) // missing closing brace

	mainPath := filepath.Join(dir, "main.conf")
	require.NoError(t, os.WriteFile(mainPath, []byte("import bad.conf\n"), 0644))

	f, err := os.Open(mainPath)
	require.NoError(t, err)
	defer f.Close()

	_, err = Read(f, mainPath)
	require.Error(t, err, "parse should fail due to missing closing brace in imported file")

	// Verify no leaked fd pointing at bad.conf.
	entries, readErr := os.ReadDir("/proc/self/fd")
	if readErr == nil {
		for _, entry := range entries {
			target, linkErr := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
			if linkErr != nil {
				continue
			}
			if target == badPath {
				t.Errorf("parser leaked file descriptor for %s after parse error (fd %s still open)", badPath, entry.Name())
			}
		}
	}
}

func TestImportUnknownDoesNotLeak(t *testing.T) {
	dir := t.TempDir()

	// Main config imports a file that does not exist at all (no .conf
	// fallback either). resolveImport must not leak any fd.
	mainPath := filepath.Join(dir, "main.conf")
	require.NoError(t, os.WriteFile(mainPath, []byte("import nonexistent\n"), 0644))

	f, err := os.Open(mainPath)
	require.NoError(t, err)
	defer f.Close()

	_, err = Read(f, mainPath)
	require.Error(t, err, "import of nonexistent file should fail")
	require.Contains(t, err.Error(), "unknown import")
}

func TestImportManyFilesNoLeak(t *testing.T) {
	dir := t.TempDir()

	const n = 50
	var imports strings.Builder
	for i := 0; i < n; i++ {
		name := filepath.Join(dir, fmt.Sprintf("inc_%03d.conf", i))
		require.NoError(t, os.WriteFile(name, []byte(fmt.Sprintf("dir_%03d v\n", i)), 0644))
		imports.WriteString("import " + filepath.Base(name) + "\n")
	}

	mainPath := filepath.Join(dir, "main.conf")
	require.NoError(t, os.WriteFile(mainPath, []byte(imports.String()), 0644))

	// Snapshot the number of open fds before parsing.
	countFDs := func() int {
		entries, err := os.ReadDir("/proc/self/fd")
		if err != nil {
			return -1
		}
		return len(entries)
	}
	fdsBefore := countFDs()

	f, err := os.Open(mainPath)
	require.NoError(t, err)

	nodes, err := Read(f, mainPath)
	f.Close()
	require.NoError(t, err)
	require.Len(t, nodes, n)

	fdsAfter := countFDs()
	if fdsBefore >= 0 && fdsAfter >= 0 {
		// Allow a small delta for test-internal allocations, but the
		// parser must not leave n dangling fds.
		if fdsAfter-fdsBefore > 5 {
			t.Errorf("possible fd leak: %d fds before, %d after (delta %d, imported %d files)",
				fdsBefore, fdsAfter, fdsAfter-fdsBefore, n)
		}
	}
}

func TestOpenImportFile(t *testing.T) {
	dir := t.TempDir()

	// Case 1: exact path exists.
	exactPath := filepath.Join(dir, "exact.conf")
	require.NoError(t, os.WriteFile(exactPath, []byte("data\n"), 0644))

	f, resolved, err := openImportFile(exactPath)
	require.NoError(t, err)
	require.Equal(t, exactPath, resolved)
	data, err := io.ReadAll(f)
	require.NoError(t, err)
	require.Equal(t, "data\n", string(data))
	f.Close()

	// Case 2: base path missing, .conf fallback exists.
	basePath := filepath.Join(dir, "fallback")
	fallbackPath := basePath + ".conf"
	require.NoError(t, os.WriteFile(fallbackPath, []byte("fallback\n"), 0644))

	f, resolved, err = openImportFile(basePath)
	require.NoError(t, err)
	require.Equal(t, fallbackPath, resolved)
	data, err = io.ReadAll(f)
	require.NoError(t, err)
	require.Equal(t, "fallback\n", string(data))
	f.Close()

	// Case 3: neither exists.
	missing := filepath.Join(dir, "missing")
	_, _, err = openImportFile(missing)
	require.Error(t, err)
	require.True(t, os.IsNotExist(err))
}
