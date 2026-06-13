/*
Maddy Mail Server - Composable all-in-one email server.
Copyright © 2019-2026 Max Mazurov <fox.cpp@disroot.org>, Maddy Mail Server contributors

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

package maddy

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/foxcpp/maddy/framework/container"
	"github.com/foxcpp/maddy/framework/log"
)

// newTestContainer returns a container with a non-nil, discarding log output.
// moduleReload restores log.DefaultLogger.Out from the old container's logger
// during rollback, which panics if that output is nil (as it is for a bare
// container.New()); in production it is set up by moduleConfigure.
func newTestContainer() *container.C {
	c := container.New()
	c.DefaultLogger.Out = log.NopOutput{}
	return c
}

// saveReloadSeams snapshots the reload indirection points (plus the global
// state that moduleReload mutates) and returns a function that restores them.
// It also installs a recorder for systemd status notifications, returning a
// getter for the recorded sequence.
func saveReloadSeams(t *testing.T) (statuses func() []SDStatus, restore func()) {
	t.Helper()

	origConfigure := reloadConfigure
	origEarlyStop := reloadEarlyStop
	origStart := reloadStart
	origStatus := reloadSystemdStatus
	origGlobal := container.Global
	origLogOut := log.DefaultLogger.Out

	var (
		mu  sync.Mutex
		rec []SDStatus
	)
	reloadSystemdStatus = func(status SDStatus, _ string) {
		mu.Lock()
		rec = append(rec, status)
		mu.Unlock()
	}

	statuses = func() []SDStatus {
		mu.Lock()
		defer mu.Unlock()
		return append([]SDStatus(nil), rec...)
	}
	restore = func() {
		reloadConfigure = origConfigure
		reloadEarlyStop = origEarlyStop
		reloadStart = origStart
		reloadSystemdStatus = origStatus
		container.Global = origGlobal
		log.DefaultLogger.Out = origLogOut
	}
	return statuses, restore
}

// TestModuleReloadFailureRestoresReadyStatus verifies that when any reload step
// fails the old configuration keeps running AND systemd is told the service is
// ready again, instead of being left stuck in the reloading state.
func TestModuleReloadFailureRestoresReadyStatus(t *testing.T) {
	failErr := errors.New("reload step failed")

	cases := []struct {
		name string
		// fail overrides the seam that should fail for this case. The other
		// seams are left as successful stubs installed by the test body.
		fail func()
	}{
		{
			name: "configure",
			fail: func() {
				reloadConfigure = func(string) (*container.C, error) {
					return nil, failErr
				}
			},
		},
		{
			name: "early-stop",
			fail: func() {
				reloadEarlyStop = func(*container.C) error { return failErr }
			},
		},
		{
			name: "start",
			fail: func() {
				reloadStart = func(*container.C) error { return failErr }
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			statuses, restore := saveReloadSeams(t)
			defer restore()

			oldContainer := newTestContainer()
			newContainer := newTestContainer()

			// Default: every step succeeds. moduleConfigure normally
			// reassigns container.Global, so mimic that to verify the
			// rollback restores it on failure.
			reloadConfigure = func(string) (*container.C, error) {
				container.Global = newContainer
				return newContainer, nil
			}
			reloadEarlyStop = func(*container.C) error { return nil }
			reloadStart = func(*container.C) error { return nil }

			container.Global = oldContainer

			// Make the step under test fail.
			tc.fail()

			var wg sync.WaitGroup
			got := moduleReload(oldContainer, "maddy.conf", &wg)

			// The old configuration must keep running.
			if got != oldContainer {
				t.Errorf("moduleReload returned a new container after a failed reload; want the old one preserved")
			}

			// container.Global must point back at the old configuration.
			if container.Global != oldContainer {
				t.Errorf("container.Global was not restored to the old container after a failed reload")
			}

			// No asynchronous stop of the old configuration may be scheduled
			// when the reload fails.
			done := make(chan struct{})
			go func() { wg.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("asyncStopWg has pending work after a failed reload")
			}

			// The crux of the fix: the last status sent to systemd must be
			// READY, not RELOADING.
			seq := statuses()
			if len(seq) == 0 {
				t.Fatal("no systemd status notifications were emitted")
			}
			if seq[0] != SDReloading {
				t.Errorf("first systemd status = %q, want %q", seq[0], SDReloading)
			}
			if last := seq[len(seq)-1]; last != SDReady {
				t.Errorf("final systemd status = %q, want %q (status left in reloading state)", last, SDReady)
			}
		})
	}
}

// TestModuleReloadSuccessSignalsReady is a regression guard ensuring the happy
// path still adopts the new configuration and eventually reports READY.
func TestModuleReloadSuccessSignalsReady(t *testing.T) {
	statuses, restore := saveReloadSeams(t)
	defer restore()

	oldContainer := newTestContainer()
	newContainer := newTestContainer()

	reloadConfigure = func(string) (*container.C, error) {
		container.Global = newContainer
		return newContainer, nil
	}
	reloadEarlyStop = func(*container.C) error { return nil }
	reloadStart = func(*container.C) error { return nil }

	var wg sync.WaitGroup
	got := moduleReload(oldContainer, "maddy.conf", &wg)
	if got != newContainer {
		t.Fatalf("moduleReload returned the old container after a successful reload; want the new one")
	}

	// Wait for the background stop of the old configuration to finish.
	wg.Wait()

	seq := statuses()
	if len(seq) == 0 {
		t.Fatal("no systemd status notifications were emitted")
	}
	if last := seq[len(seq)-1]; last != SDReady {
		t.Errorf("final systemd status = %q, want %q", last, SDReady)
	}
}
