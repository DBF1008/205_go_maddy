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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// countOpenFDs returns the number of open file descriptors for the current
// process by reading /proc/self/fd. Returns -1 if the count cannot be
// determined (e.g. on systems without procfs).
func countOpenFDs(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return -1
	}
	return len(entries)
}

// writeShadowFile creates a temporary shadow-format file and overrides
// shadowPath to point at it. The caller must call the returned cleanup
// function (typically via defer) to restore the original path.
func writeShadowFile(t *testing.T, content string) (cleanup func()) {
	t.Helper()
	origPath := shadowPath
	dir := t.TempDir()
	path := filepath.Join(dir, "shadow")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	shadowPath = path
	return func() { shadowPath = origPath }
}

// TestRead_NoFDLeak verifies that Read() does not leak file descriptors when
// called repeatedly on the success path.
func TestRead_NoFDLeak(t *testing.T) {
	cleanup := writeShadowFile(t,
		"testuser:$6$rounds=5000$saltsalt$hashhashhashhashhashhashhashhashhashhashhashhashhashhashhashhashhashha:18000:0:99999:7:::\n"+
			"anotheruser:$6$rounds=5000$pepperpepper$hashhashhashhashhashhashhashhashhashhashhashhashhashhashhashhashhash:18000:0:99999:7:::\n")
	defer cleanup()

	// Warm up — the first call may open internal runtime fds (e.g. locale
	// data). We only care about steady-state growth.
	if _, err := Read(); err != nil {
		t.Fatal(err)
	}

	fdsBefore := countOpenFDs(t)
	if fdsBefore == -1 {
		t.Skip("cannot determine open fd count (no /proc/self/fd)")
	}

	const iterations = 200
	for i := 0; i < iterations; i++ {
		entries, err := Read()
		if err != nil {
			t.Fatalf("Read() iteration %d: %v", i, err)
		}
		if len(entries) != 2 {
			t.Fatalf("expected 2 entries, got %d at iteration %d", len(entries), i)
		}
	}

	fdsAfter := countOpenFDs(t)

	// Allow a small margin for unrelated runtime activity (e.g. GC
	// background workers opening a pipe). A leak of 200 unclosed files
	// would be unambiguous.
	leaked := fdsAfter - fdsBefore
	if leaked > 5 {
		t.Errorf("fd leak detected: %d fds opened before, %d after %d Read() calls (%d leaked)",
			fdsBefore, fdsAfter, iterations, leaked)
	}
}

// TestRead_NoFDLeak_ParseError verifies that Read() does not leak file
// descriptors when a malformed entry causes a parse error mid-file.
func TestRead_NoFDLeak_ParseError(t *testing.T) {
	// The second line has only 3 colon-separated fields instead of the
	// required 9, so parseEntry will return an error.
	cleanup := writeShadowFile(t,
		"gooduser:$6$rounds=5000$salt$hashhashhashhashhashhashhashhashhashhashhashhashhashhashhashhashhashhash:18000:0:99999:7:::\n"+
			"baduser:only:three_fields\n")
	defer cleanup()

	fdsBefore := countOpenFDs(t)
	if fdsBefore == -1 {
		t.Skip("cannot determine open fd count (no /proc/self/fd)")
	}

	const iterations = 200
	for i := 0; i < iterations; i++ {
		_, err := Read()
		if err == nil {
			t.Fatal("expected parse error from malformed entry, got nil")
		}
		if !strings.Contains(err.Error(), "malformed entry") {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	fdsAfter := countOpenFDs(t)
	leaked := fdsAfter - fdsBefore
	if leaked > 5 {
		t.Errorf("fd leak on parse-error path: %d fds before, %d after %d calls (%d leaked)",
			fdsBefore, fdsAfter, iterations, leaked)
	}
}

// TestRead_NoFDLeak_FileOpenError verifies that Read() returns an error
// without leaking when the shadow file cannot be opened.
func TestRead_NoFDLeak_FileOpenError(t *testing.T) {
	origPath := shadowPath
	defer func() { shadowPath = origPath }()

	shadowPath = filepath.Join(t.TempDir(), "nonexistent_shadow")

	fdsBefore := countOpenFDs(t)
	if fdsBefore == -1 {
		t.Skip("cannot determine open fd count (no /proc/self/fd)")
	}

	const iterations = 200
	for i := 0; i < iterations; i++ {
		_, err := Read()
		if err == nil {
			t.Fatal("expected error opening nonexistent file, got nil")
		}
	}

	fdsAfter := countOpenFDs(t)
	leaked := fdsAfter - fdsBefore
	if leaked > 5 {
		t.Errorf("fd leak on open-error path: %d fds before, %d after %d calls (%d leaked)",
			fdsBefore, fdsAfter, iterations, leaked)
	}
}

// TestRead_ValidEntries verifies that Read() correctly parses a well-formed
// shadow file and returns the expected entries.
func TestRead_ValidEntries(t *testing.T) {
	cleanup := writeShadowFile(t,
		"alice:$6$rounds=5000$salt$hashhashhashhashhashhashhashhashhashhashhashhashhashhashhashhashhashhash:19000:0:99999:7:30:-1:0\n"+
			"bob:$6$rounds=5000$pepper$hashhashhashhashhashhashhashhashhashhashhashhashhashhashhashhashhashha:-1:-1:-1:-1:-1:-1:0\n")
	defer cleanup()

	entries, err := Read()
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	alice := entries[0]
	if alice.Name != "alice" {
		t.Errorf("expected Name=alice, got %q", alice.Name)
	}
	if alice.LastChange != 19000 {
		t.Errorf("expected LastChange=19000, got %d", alice.LastChange)
	}
	if alice.MinPassAge != 0 {
		t.Errorf("expected MinPassAge=0, got %d", alice.MinPassAge)
	}
	if alice.MaxPassAge != 99999 {
		t.Errorf("expected MaxPassAge=99999, got %d", alice.MaxPassAge)
	}
	if alice.WarnPeriod != 7 {
		t.Errorf("expected WarnPeriod=7, got %d", alice.WarnPeriod)
	}
	if alice.InactivityPeriod != 30 {
		t.Errorf("expected InactivityPeriod=30, got %d", alice.InactivityPeriod)
	}
	if alice.AcctExpiry != -1 {
		t.Errorf("expected AcctExpiry=-1, got %d", alice.AcctExpiry)
	}

	bob := entries[1]
	if bob.Name != "bob" {
		t.Errorf("expected Name=bob, got %q", bob.Name)
	}
	// Empty fields should be parsed as -1.
	if bob.LastChange != -1 {
		t.Errorf("expected LastChange=-1 for empty field, got %d", bob.LastChange)
	}
	if bob.MinPassAge != -1 {
		t.Errorf("expected MinPassAge=-1 for empty field, got %d", bob.MinPassAge)
	}
}

// TestLookup_Found verifies that Lookup() returns the correct entry for a
// user that exists in the shadow file.
func TestLookup_Found(t *testing.T) {
	cleanup := writeShadowFile(t,
		"alice:$6$rounds=5000$salt$hashhashhashhashhashhashhashhashhashhashhashhashhashhashhashhashhashhash:19000:0:99999:7:::\n"+
			"bob:$6$rounds=5000$pepper$hashhashhashhashhashhashhashhashhashhashhashhashhashhashhashhashhashha:18000:0:99999:7:::\n")
	defer cleanup()

	ent, err := Lookup("bob")
	if err != nil {
		t.Fatal(err)
	}
	if ent.Name != "bob" {
		t.Errorf("expected Name=bob, got %q", ent.Name)
	}
	if ent.LastChange != 18000 {
		t.Errorf("expected LastChange=18000, got %d", ent.LastChange)
	}
}

// TestLookup_NotFound verifies that Lookup() returns ErrNoSuchUser for a
// user that does not exist in the shadow file.
func TestLookup_NotFound(t *testing.T) {
	cleanup := writeShadowFile(t,
		"alice:$6$rounds=5000$salt$hashhashhashhashhashhashhashhashhashhashhashhashhashhashhashhashhashhash:19000:0:99999:7:::\n")
	defer cleanup()

	_, err := Lookup("nonexistent")
	if err != ErrNoSuchUser {
		t.Errorf("expected ErrNoSuchUser, got %v", err)
	}
}
