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
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	v1 "github.com/fatedier/frp/pkg/config/v1"
)

// AuthorizedKeysDB performs per-connect SSH public key lookups against a
// Postgres database. It owns a connection pool that is shared across
// connections so each auth attempt is a single indexed query — no
// in-memory snapshot of the keyset is kept on the frps side.
type AuthorizedKeysDB struct {
	pool         *pgxpool.Pool
	lookupQuery  string
	queryTimeout time.Duration
}

// NewAuthorizedKeysDB initializes the connection pool and verifies it
// can reach the database. The caller owns the returned value and must
// call Close on shutdown.
func NewAuthorizedKeysDB(ctx context.Context, cfg v1.AuthorizedKeysDBConfig) (*AuthorizedKeysDB, error) {
	if cfg.DSN == "" {
		return nil, errors.New("authorizedKeysDB.dsn is required")
	}
	if cfg.LookupQuery == "" {
		return nil, errors.New("authorizedKeysDB.lookupQuery is required")
	}

	poolCfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("parse authorizedKeysDB dsn: %w", err)
	}
	if cfg.MaxConns > 0 {
		poolCfg.MaxConns = cfg.MaxConns
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("open authorizedKeysDB pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping authorizedKeysDB: %w", err)
	}

	timeout := 5 * time.Second
	if cfg.QueryTimeoutMs > 0 {
		timeout = time.Duration(cfg.QueryTimeoutMs) * time.Millisecond
	}

	return &AuthorizedKeysDB{
		pool:         pool,
		lookupQuery:  cfg.LookupQuery,
		queryTimeout: timeout,
	}, nil
}

// LookupUser runs the configured query with the marshaled SSH public
// key (the binary wire format from ssh.PublicKey.Marshal) and returns
// the matching username. found=false means the key is unknown (not an
// error condition — auth simply fails).
//
// The lookup parameter is the raw key blob, expected to be stored as
// BYTEA in Postgres. An equally valid alternative is to look up by
// SHA256 fingerprint (ssh.FingerprintSHA256(key)) — that's smaller and
// plays nicer with logging/audit tools, at the cost of needing a
// derived column. We chose raw bytes here for directness; switching is
// a one-line change at the call site.
func (a *AuthorizedKeysDB) LookupUser(ctx context.Context, pubKeyBlob []byte) (user string, found bool, err error) {
	queryCtx, cancel := context.WithTimeout(ctx, a.queryTimeout)
	defer cancel()

	err = a.pool.QueryRow(queryCtx, a.lookupQuery, pubKeyBlob).Scan(&user)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return user, true, nil
}

func (a *AuthorizedKeysDB) Close() {
	if a != nil && a.pool != nil {
		a.pool.Close()
	}
}
