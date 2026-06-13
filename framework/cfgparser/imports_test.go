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
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestImport_FileContentsExpanded is a baseline check that imports referencing
// real files (both with an explicit extension and via the implicit ".conf"
// fallback) are expanded into the surrounding tree. It guards the happy path
// exercised by the fd-leak regression test below.
func TestImport_FileContentsExpanded(t *testing.T) {
	dir := t.TempDir()

	directPath := filepath.Join(dir, "imported_direct.conf")
	require.NoError(t, os.WriteFile(directPath, []byte("direct_opt direct_val\n"), 0o600))

	// Referenced without the ".conf" suffix so resolveImport must fall back to
	// opening "<base>.conf".
	fallbackBase := filepath.Join(dir, "imported_fallback")
	require.NoError(t, os.WriteFile(fallbackBase+".conf", []byte("fallback_opt fallback_val\n"), 0o600))

	cfg := "import " + directPath + "\nimport " + fallbackBase + "\n"

	tree, err := Read(strings.NewReader(cfg), filepath.Join(dir, "main.conf"))
	require.NoError(t, err)

	names := make([]string, 0, len(tree))
	for _, n := range tree {
		names = append(names, n.Name)
	}
	require.Equal(t, []string{"direct_opt", "fallback_opt"}, names)
}

// TestImport_NoFileDescriptorLeak is a regression test for a file descriptor
// leak in resolveImport: every "import <file>" directive used to os.Open the
// target (or its ".conf" fallback) and hand the *os.File to readTree without
// ever closing it. Repeatedly reading a config with imports (as ReadConfig and
// the hot-reload path do) leaked one fd per imported file, eventually exhausting
// the process fd table.
//
// The test expands the same imports many times and asserts the process-wide
// open fd count stays flat. With the leak present, the count grows roughly
// linearly with the number of expansions; with it fixed, it stays constant.
//
// It relies on /proc/self/fd and is therefore Linux-only; it is skipped
// elsewhere.
func TestImport_NoFileDescriptorLeak(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("fd-leak check relies on /proc/self/fd, which is Linux-only")
	}

	dir := t.TempDir()

	directPath := filepath.Join(dir, "imported_direct.conf")
	require.NoError(t, os.WriteFile(directPath, []byte("direct_opt direct_val\n"), 0o600))

	fallbackBase := filepath.Join(dir, "imported_fallback")
	require.NoError(t, os.WriteFile(fallbackBase+".conf", []byte("fallback_opt fallback_val\n"), 0o600))

	// Main config is passed as an in-memory reader, so the only files opened
	// during Read are the two imports opened (and now closed) by resolveImport.
	// This exercises both the direct os.Open and the ".conf" fallback path.
	cfg := "import " + directPath + "\nimport " + fallbackBase + "\n"
	location := filepath.Join(dir, "main.conf")

	readOnce := func() {
		t.Helper()
		_, err := Read(strings.NewReader(cfg), location)
		require.NoError(t, err)
	}

	countOpenFDs := func() int {
		t.Helper()
		entries, err := os.ReadDir("/proc/self/fd")
		require.NoError(t, err)
		return len(entries)
	}

	// Warm up once so any lazy, one-time fd allocations settle before the
	// baseline measurement.
	readOnce()

	before := countOpenFDs()

	const iterations = 100
	for i := 0; i < iterations; i++ {
		readOnce()
	}

	after := countOpenFDs()

	// Each iteration opens two import files. With the leak, the delta would be
	// on the order of 2*iterations; a small tolerance absorbs unrelated fd
	// churn from the test runtime.
	const tolerance = 10
	if after > before+tolerance {
		t.Fatalf("file descriptor leak detected: "+
			"open fds grew from %d to %d after %d import expansions (delta %d, tolerance %d)",
			before, after, iterations, after-before, tolerance)
	}
}
