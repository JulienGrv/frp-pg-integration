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

package ssh

import (
	"testing"

	v1 "github.com/fatedier/frp/pkg/config/v1"
)

// The pool must be fully warmed at boot: a cold pool during a pod-restart
// reconnect storm made hundreds of concurrent auth lookups each pay the
// connection-handshake latency and blow queryTimeoutMs ("authorized keys db
// lookup error: context deadline exceeded" bursts on frps startup).
func TestBuildPoolConfigWarmsPool(t *testing.T) {
	cfg, err := buildPoolConfig(v1.AuthorizedKeysDBConfig{
		DSN:      "host=localhost dbname=test",
		MaxConns: 10,
	})
	if err != nil {
		t.Fatalf("build pool config: %v", err)
	}
	if cfg.MaxConns != 10 {
		t.Fatalf("MaxConns = %d, want 10", cfg.MaxConns)
	}
	if cfg.MinConns != cfg.MaxConns {
		t.Fatalf("MinConns = %d, want %d (fully warm pool)", cfg.MinConns, cfg.MaxConns)
	}

	// Without an explicit maxConns the pgx default applies; the pool must
	// still be fully warm.
	cfg, err = buildPoolConfig(v1.AuthorizedKeysDBConfig{DSN: "host=localhost dbname=test"})
	if err != nil {
		t.Fatalf("build pool config: %v", err)
	}
	if cfg.MaxConns <= 0 {
		t.Fatalf("MaxConns = %d, want pgx default > 0", cfg.MaxConns)
	}
	if cfg.MinConns != cfg.MaxConns {
		t.Fatalf("MinConns = %d, want %d (fully warm pool)", cfg.MinConns, cfg.MaxConns)
	}
}
