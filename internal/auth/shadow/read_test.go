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

package shadow

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

// useFixture points Read at a temporary shadow database with the given content
// and restores the original path when the (sub)test finishes.
func useFixture(t *testing.T, content string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "shadow")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("writing shadow fixture: %v", err)
	}

	old := shadowDBPath
	shadowDBPath = path
	t.Cleanup(func() { shadowDBPath = old })
}

// countOpenFDs returns the number of open file descriptors held by the current
// process. It relies on /proc/self/fd and therefore skips the test on platforms
// (macOS, Windows, ...) where that interface is unavailable.
func countOpenFDs(t *testing.T) int {
	t.Helper()

	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Skipf("cannot enumerate /proc/self/fd, skipping descriptor-leak check: %v", err)
	}
	return len(entries)
}

// assertNoFDLeak calls Read many times against the given fixture and fails if the
// number of open descriptors grows, which would mean Read does not release its
// /etc/shadow handle. expectErr documents (and asserts) whether the fixture is a
// success, a parse error or a scanner error so every return path is exercised.
//
// Garbage collection is disabled for the duration of the loop: *os.File installs
// a finalizer that closes the descriptor when the value is collected, so leaving
// GC enabled could hide the leak by closing the leaked descriptors for us.
func assertNoFDLeak(t *testing.T, content string, expectErr bool) {
	t.Helper()

	useFixture(t, content)

	// Skip early on platforms without /proc/self/fd.
	countOpenFDs(t)

	// One call up front both warms up any lazily-created descriptors and
	// confirms we are really hitting the intended (success/error) code path.
	if _, err := Read(); (err != nil) != expectErr {
		t.Fatalf("Read() error = %v, expectErr = %v", err, expectErr)
	}

	defer debug.SetGCPercent(debug.SetGCPercent(-1))

	before := countOpenFDs(t)

	const iterations = 200
	for i := 0; i < iterations; i++ {
		// Errors are expected for the error fixtures; the file must be
		// closed regardless of the outcome.
		_, _ = Read()
	}

	after := countOpenFDs(t)

	if leaked := after - before; leaked > 5 {
		t.Fatalf("Read leaked %d descriptors over %d calls (before=%d, after=%d): /etc/shadow handle is not released",
			leaked, iterations, before, after)
	}
}

func TestReadParsesEntries(t *testing.T) {
	useFixture(t, "root:!:19000:0:99999:7:::\nuser:$6$salt$hash:18000:1:90:14:30:20000:0\n")

	entries, err := Read()
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(entries), entries)
	}

	root := entries[0]
	if root.Name != "root" || root.Pass != "!" {
		t.Errorf("unexpected name/pass: %q / %q", root.Name, root.Pass)
	}
	if root.LastChange != 19000 || root.MinPassAge != 0 || root.MaxPassAge != 99999 || root.WarnPeriod != 7 {
		t.Errorf("unexpected numeric fields: %+v", root)
	}
	// Empty trailing fields must be normalised to -1.
	if root.InactivityPeriod != -1 || root.AcctExpiry != -1 || root.Flags != -1 {
		t.Errorf("empty fields not mapped to -1: %+v", root)
	}

	user := entries[1]
	if user.Name != "user" || user.Pass != "$6$salt$hash" {
		t.Errorf("unexpected name/pass: %q / %q", user.Name, user.Pass)
	}
	if user.AcctExpiry != 20000 || user.Flags != 0 {
		t.Errorf("unexpected fields for user: %+v", user)
	}
}

// TestReadDoesNotLeakDescriptors is the regression test for the file-handle leak
// in Read: previously os.Open("/etc/shadow") was never closed, so Lookup and
// AuthPlain leaked a descriptor on every authentication. Each subtest drives a
// different return path of Read.
func TestReadDoesNotLeakDescriptors(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		assertNoFDLeak(t, "root:!:19000:0:99999:7:::\nuser:$6$salt$hash:18000:1:90:14:30:20000:0\n", false)
	})

	t.Run("parse_error", func(t *testing.T) {
		// 9 fields, but field 3 (LastChange) is not a number -> parseEntry fails.
		assertNoFDLeak(t, "user:!:abc:0:99999:7:::\n", true)
	})

	t.Run("scanner_error", func(t *testing.T) {
		// A single line longer than the scanner's buffer and without a trailing
		// newline makes bufio.Scanner.Err return bufio.ErrTooLong.
		assertNoFDLeak(t, strings.Repeat("x", bufio.MaxScanTokenSize+1), true)
	})
}
