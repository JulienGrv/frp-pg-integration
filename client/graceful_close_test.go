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

	"github.com/fatedier/frp/pkg/config/source"
	v1 "github.com/fatedier/frp/pkg/config/v1"
)

func newIdleTestService(t *testing.T) *Service {
	t.Helper()

	svr, err := NewService(ServiceOptions{
		Common:                 &v1.ClientCommonConfig{},
		ConfigSourceAggregator: source.NewAggregator(source.NewConfigSource()),
		ConnectorCreator: func(context.Context, *v1.ClientCommonConfig) Connector {
			return &failingConnector{err: errors.New("dial disabled in test")}
		},
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return svr
}

// TestCloseBeforeRun reproduces the frps crash from the SSH gateway:
// TunnelServer.Run starts the virtual client's Run() in a goroutine and calls
// Close() from its own goroutine (e.g. after "wait proxy status ready
// timeout" during a reconnect storm), so Close() can execute before Run() has
// assigned svr.cancel. GracefulClose then called the nil cancel func and the
// whole frps process died with SIGSEGV (client/service.go:430).
func TestCloseBeforeRun(t *testing.T) {
	svr := newIdleTestService(t)

	// Must not panic even though Run was never started.
	svr.Close()

	// A Run that starts after Close must observe the closed state and return
	// instead of running a closed service forever.
	done := make(chan error, 1)
	go func() { done <- svr.Run(context.Background()) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after Close")
	}
}

// TestCloseRacesRun drives Close and Run concurrently so the race detector
// can see an unsynchronized cancel handoff between the two goroutines.
func TestCloseRacesRun(t *testing.T) {
	for range 200 {
		svr := newIdleTestService(t)

		start := make(chan struct{})
		done := make(chan error, 1)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			svr.Close()
		}()
		go func() {
			defer wg.Done()
			<-start
			done <- svr.Run(context.Background())
		}()
		close(start)
		wg.Wait()

		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("Run did not return after Close")
		}
	}
}
