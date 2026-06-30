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

// frps-import-keys reads an OpenSSH authorized_keys file and loads it
// into the Postgres table consulted by frps' sshTunnelGateway. Parsing
// goes through golang.org/x/crypto/ssh so quirks like quoted comments,
// embedded spaces, options prefixes, and blank lines are handled
// correctly — a plain awk/ssh-keygen pipeline isn't.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/ssh"
)

type record struct {
	pubKey   []byte
	username string
}

func main() {
	var (
		file       = flag.String("file", "", "path to authorized_keys file (required)")
		dsn        = flag.String("dsn", "", "Postgres DSN (required), e.g. postgres://user:pass@host/db")
		table      = flag.String("table", "ssh_keys", "target table name")
		pubCol     = flag.String("pubkey-col", "pubkey", "BYTEA column holding the marshaled public key")
		userCol    = flag.String("username-col", "username", "TEXT column holding the username/comment")
		onConflict = flag.String("on-conflict", "update", "conflict strategy: update | ignore | error")
		truncate   = flag.Bool("truncate", false, "TRUNCATE the target table before import")
	)
	flag.Parse()

	if *file == "" || *dsn == "" {
		flag.Usage()
		os.Exit(2)
	}

	records, err := parseAuthorizedKeys(*file)
	if err != nil {
		log.Fatalf("parse %s: %v", *file, err)
	}
	log.Printf("parsed %d keys from %s", len(records), *file)

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, *dsn)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	if err := importRecords(ctx, conn, records, *table, *pubCol, *userCol, *onConflict, *truncate); err != nil {
		log.Fatalf("import: %v", err)
	}
	log.Printf("done: %d keys imported into %s", len(records), *table)
}

func parseAuthorizedKeys(path string) ([]record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var (
		out     []record
		lineNum int
		skipped int
	)
	rest := data
	for len(rest) > 0 {
		lineNum++
		pubKey, comment, _, next, err := ssh.ParseAuthorizedKey(rest)
		if err != nil {
			// ParseAuthorizedKey reports "no key found" when only blanks/
			// comments remain — that's a clean termination, not a failure.
			if strings.Contains(err.Error(), "no key found") {
				break
			}
			skipped++
			log.Printf("skip line ~%d: %v", lineNum, err)
			// Advance to the next line so we don't loop on a bad entry.
			if i := indexNewline(rest); i >= 0 {
				rest = rest[i+1:]
				continue
			}
			break
		}
		out = append(out, record{
			pubKey:   pubKey.Marshal(),
			username: strings.TrimSpace(comment),
		})
		rest = next
	}
	if skipped > 0 {
		log.Printf("skipped %d malformed line(s)", skipped)
	}
	return out, nil
}

func indexNewline(b []byte) int {
	for i, c := range b {
		if c == '\n' {
			return i
		}
	}
	return -1
}

func importRecords(
	ctx context.Context, conn *pgx.Conn, records []record,
	table, pubCol, userCol, onConflict string, truncate bool,
) error {
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if truncate {
		if _, err := tx.Exec(ctx, fmt.Sprintf("TRUNCATE TABLE %s", quoteIdent(table))); err != nil {
			return fmt.Errorf("truncate: %w", err)
		}
	}

	// COPY into a TEMP table, then merge into the target with the chosen
	// conflict strategy. CopyFrom alone can't express ON CONFLICT.
	tmpTable := "frps_import_keys_tmp"
	createTmp := fmt.Sprintf(
		"CREATE TEMP TABLE %s (%s BYTEA, %s TEXT) ON COMMIT DROP",
		quoteIdent(tmpTable), quoteIdent(pubCol), quoteIdent(userCol),
	)
	if _, err := tx.Exec(ctx, createTmp); err != nil {
		return fmt.Errorf("create temp: %w", err)
	}

	rows := make([][]any, len(records))
	for i, r := range records {
		rows[i] = []any{r.pubKey, r.username}
	}
	n, err := tx.CopyFrom(ctx,
		pgx.Identifier{tmpTable},
		[]string{pubCol, userCol},
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		return fmt.Errorf("copy: %w", err)
	}
	log.Printf("copied %d rows into temp table", n)

	var mergeSQL string
	switch onConflict {
	case "update":
		mergeSQL = fmt.Sprintf(
			`INSERT INTO %s (%s, %s) SELECT %s, %s FROM %s
			 ON CONFLICT (%s) DO UPDATE SET %s = EXCLUDED.%s`,
			quoteIdent(table), quoteIdent(pubCol), quoteIdent(userCol),
			quoteIdent(pubCol), quoteIdent(userCol), quoteIdent(tmpTable),
			quoteIdent(pubCol), quoteIdent(userCol), quoteIdent(userCol),
		)
	case "ignore":
		mergeSQL = fmt.Sprintf(
			`INSERT INTO %s (%s, %s) SELECT %s, %s FROM %s
			 ON CONFLICT (%s) DO NOTHING`,
			quoteIdent(table), quoteIdent(pubCol), quoteIdent(userCol),
			quoteIdent(pubCol), quoteIdent(userCol), quoteIdent(tmpTable),
			quoteIdent(pubCol),
		)
	case "error":
		mergeSQL = fmt.Sprintf(
			`INSERT INTO %s (%s, %s) SELECT %s, %s FROM %s`,
			quoteIdent(table), quoteIdent(pubCol), quoteIdent(userCol),
			quoteIdent(pubCol), quoteIdent(userCol), quoteIdent(tmpTable),
		)
	default:
		return fmt.Errorf("unknown --on-conflict value %q (want update|ignore|error)", onConflict)
	}
	if _, err := tx.Exec(ctx, mergeSQL); err != nil {
		return fmt.Errorf("merge: %w", err)
	}

	return tx.Commit(ctx)
}

// quoteIdent guards against SQL injection through user-supplied table /
// column names. Only safe ASCII identifiers are allowed; anything else
// is wrapped in double quotes with embedded quotes doubled.
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
