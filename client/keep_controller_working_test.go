// Copyright 2026 The frp Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package client

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	v1 "github.com/fatedier/frp/pkg/config/v1"
)

// TestKeepControllerWorkingRaceWithStop reproduces the crash reported in
// production: keepControllerWorking dereferenced svr.ctl without holding
// svr.ctlMu, racing with stop() (which sets svr.ctl = nil under the lock when
// the control connection drops mid-reconnect). The unsynchronized read could
// observe a nil *Control and panic in (*Control).Done() with
// "invalid memory address or nil pointer dereference".
//
// Run with -race; before the fix this both trips the race detector and can
// panic the test process. After the fix keepControllerWorking snapshots
// svr.ctl under the lock, so neither happens.
func TestKeepControllerWorkingRaceWithStop(t *testing.T) {
	// Many iterations with a start barrier so the read in keepControllerWorking
	// and the nil assignment fire concurrently, widening the race window.
	for i := 0; i < 2000; i++ {
		ctx, cancel := context.WithCancelCause(context.Background())

		ctl := &Control{doneCh: make(chan struct{})}
		svr := &Service{
			ctx:    ctx,
			cancel: cancel,
			ctl:    ctl,
			common: &v1.ClientCommonConfig{},
			// If keepControllerWorking proceeds into its reconnect loop before
			// the context cancellation lands, the dial fails fast at Open()
			// instead of touching the network.
			connectorCreator: func(context.Context, *v1.ClientCommonConfig) Connector {
				return &failingConnector{err: errors.New("dial disabled in test")}
			},
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)

		// Reader: the goroutine that crashed in production.
		go func() {
			defer wg.Done()
			<-start
			svr.keepControllerWorking()
		}()

		// Writer: mimics stop() niling svr.ctl under the lock while the control
		// connection is torn down (doneCh closed). Cancelling the context lets
		// keepControllerWorking's BackoffUntil return promptly.
		go func() {
			defer wg.Done()
			<-start
			svr.ctlMu.Lock()
			svr.ctl = nil
			svr.ctlMu.Unlock()
			close(ctl.doneCh)
			svr.cancel(nil)
		}()

		close(start)

		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatalf("keepControllerWorking did not return (iteration %d)", i)
		}
	}
}
