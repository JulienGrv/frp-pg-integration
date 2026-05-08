### SSH Tunnel Gateway

*Added in v0.53.0*

### Concept

SSH supports reverse proxy capabilities [rfc](https://www.rfc-editor.org/rfc/rfc4254#page-16).

frp supports listening on an SSH port on the frps side to achieve TCP protocol proxying using the SSH -R protocol. This mode does not rely on frpc.

SSH reverse tunneling proxying and proxying SSH ports through frp are two different concepts. SSH reverse tunneling proxying is essentially a basic reverse proxying accomplished by connecting to frps via an SSH client when you don't want to use frpc.

```toml
# frps.toml
sshTunnelGateway.bindPort = 0
sshTunnelGateway.privateKeyFile = ""
sshTunnelGateway.autoGenPrivateKeyPath = ""
sshTunnelGateway.authorizedKeysFile = ""

# optional: Postgres-backed authorized keys lookup
# [sshTunnelGateway.authorizedKeysDB]
# dsn         = "postgres://user:pass@host:5432/db?sslmode=require"
# lookupQuery = "SELECT username FROM ssh_keys WHERE pubkey = $1 AND active = true"
```

| Field | Type | Description | Required |
| :--- | :--- | :--- | :--- |
| bindPort| int | The ssh server port that frps listens on.| Yes |
| privateKeyFile | string | Default value is empty. The private key file used by the ssh server. If it is empty, frps will read the private key file under the autoGenPrivateKeyPath path. It can reuse the /home/user/.ssh/id_rsa file on the local machine, or a custom path can be specified.| No |
| autoGenPrivateKeyPath  | string |Default value is ./.autogen_ssh_key. If the file does not exist or its content is empty, frps will automatically generate RSA private key file content and store it in this file.|No|
| authorizedKeysFile  | string |Default value is empty. If it is empty, ssh client authentication is not authenticated. If it is not empty, it can implement ssh password-free login authentication. It can reuse the local /home/user/.ssh/authorized_keys file or a custom path can be specified.| No |
| authorizedKeysDB  | object | Optional Postgres-backed lookup for authorized keys. When set, takes precedence over `authorizedKeysFile` and avoids loading the keyset into memory on every connect. See [Postgres-Backed Authorized Keys](#postgres-backed-authorized-keys) below. | No |

### Basic Usage

#### Server-side frps

Minimal configuration:

```toml
sshTunnelGateway.bindPort = 2200
```

Place the above configuration in frps.toml and run `./frps -c frps.toml`. It will listen on port 2200 and accept SSH reverse proxy requests.

Note:

1. When using the minimal configuration, a `.autogen_ssh_key` private key file will be automatically created in the current working directory. The SSH server of frps will use this private key file for encryption and decryption. Alternatively, you can reuse an existing private key file on your local machine, such as `/home/user/.ssh/id_rsa`.

2. When running frps in the minimal configuration mode, connecting to frps via SSH does not require authentication. It is strongly recommended to configure a token in frps and specify the token in the SSH command line.

#### Client-side SSH

The command format is:

```bash
ssh -R :80:{local_ip:port} v0@{frps_address} -p {frps_ssh_listen_port} {tcp|http|https|stcp|tcpmux} --remote_port {real_remote_port} --proxy_name {proxy_name} --token {frp_token}
```

1. `--proxy_name` is optional, and if left empty, a random one will be generated.
2. The username for logging in to frps is always "v0" and currently has no significance, i.e., `v0@{frps_address}`.
3. The server-side proxy listens on the port determined by `--remote_port`.
4. `{tcp|http|https|stcp|tcpmux}` supports the complete command parameters, which can be obtained by using `--help`. For example: `ssh -R :80::8080 v0@127.0.0.1 -p 2200 http --help`.
5. The token is optional, but for security reasons, it is strongly recommended to configure the token in frps.

#### TCP Proxy

```bash
ssh -R :80:127.0.0.1:8080 v0@{frp_address} -p 2200 tcp --proxy_name "test-tcp" --remote_port 9090
```

This sets up a proxy on frps that listens on port 9090 and proxies local service on port 8080.

```bash
frp (via SSH) (Ctrl+C to quit)

User: 
ProxyName: test-tcp
Type: tcp
RemoteAddress: :9090
```

Equivalent to:

```bash
frpc tcp --proxy_name "test-tcp" --local_ip 127.0.0.1 --local_port 8080 --remote_port 9090
```

More parameters can be obtained by executing `--help`.

#### HTTP Proxy

```bash
ssh -R :80:127.0.0.1:8080 v0@{frp address} -p 2200 http --proxy_name "test-http"  --custom_domain test-http.frps.com
```

Equivalent to:
```bash
frpc http --proxy_name "test-http" --custom_domain test-http.frps.com
```

You can access the HTTP service using the following command:

curl 'http://test-http.frps.com'

More parameters can be obtained by executing --help.

#### HTTPS/STCP/TCPMUX Proxy

To obtain the usage instructions, use the following command:

```bash
ssh -R :80:127.0.0.1:8080 v0@{frp address} -p 2200 {https|stcp|tcpmux} --help
```

### Advanced Usage

#### Reusing the id_rsa File on the Local Machine

```toml
# frps.toml
sshTunnelGateway.bindPort = 2200
sshTunnelGateway.privateKeyFile = "/home/user/.ssh/id_rsa"
```

During the SSH protocol handshake, public keys are exchanged for data encryption. Therefore, the SSH server on the frps side needs to specify a private key file, which can be reused from an existing file on the local machine. If the privateKeyFile field is empty, frps will automatically create an RSA private key file.

#### Specifying the Auto-Generated Private Key File Path

```toml
# frps.toml
sshTunnelGateway.bindPort = 2200
sshTunnelGateway.autoGenPrivateKeyPath = "/var/frp/ssh-private-key-file"
```

frps will automatically create a private key file and store it at the specified path.

Note: Changing the private key file in frps can cause SSH client login failures. If you need to log in successfully, you can delete the old records from the `/home/user/.ssh/known_hosts` file.

#### Using an Existing authorized_keys File for SSH Public Key Authentication

```toml
# frps.toml
sshTunnelGateway.bindPort = 2200
sshTunnelGateway.authorizedKeysFile = "/home/user/.ssh/authorized_keys"
```

The authorizedKeysFile is the file used for SSH public key authentication, which contains the public key information for users, with one key per line.

If authorizedKeysFile is empty, frps won't perform any authentication for SSH clients. Frps does not support SSH username and password authentication.

You can reuse an existing `authorized_keys` file on your local machine for client authentication.

Note: authorizedKeysFile is for user authentication during the SSH login phase, while the token is for frps authentication. These two authentication methods are independent. SSH authentication comes first, followed by frps token authentication. It is strongly recommended to enable at least one of them. If authorizedKeysFile is empty, it is highly recommended to enable token authentication in frps to avoid security risks.

#### Using a Custom authorized_keys File for SSH Public Key Authentication

```toml
# frps.toml
sshTunnelGateway.bindPort = 2200
sshTunnelGateway.authorizedKeysFile = "/var/frps/custom_authorized_keys_file"
```

Specify the path to a custom `authorized_keys` file.

### Postgres-Backed Authorized Keys

For large keysets (thousands of keys or more), `authorizedKeysFile` becomes a bottleneck — frps reloads and parses the entire file on every incoming SSH connection. A Postgres-backed lookup replaces this with a single indexed query per connect, with no in-memory snapshot of the keyset on the frps side.

When `sshTunnelGateway.authorizedKeysDB` is configured, it takes precedence over `authorizedKeysFile`. If only `authorizedKeysFile` is set, behavior is unchanged.

#### Configuration

```toml
# frps.toml
sshTunnelGateway.bindPort = 2200

[sshTunnelGateway.authorizedKeysDB]
dsn            = "postgres://frp:secret@db.internal:5432/frp?sslmode=require"
lookupQuery    = "SELECT username FROM ssh_keys WHERE pubkey = $1 AND active = true"
maxConns       = 10      # optional, pgxpool max connection count
queryTimeoutMs = 3000    # optional, per-lookup timeout in milliseconds (default 5000)
```

| Field | Type | Description | Required |
| :--- | :--- | :--- | :--- |
| dsn | string | Standard Postgres connection string consumed by pgx. | Yes |
| lookupQuery | string | SQL run on every SSH auth attempt. Receives the marshaled SSH public key as `$1` (BYTEA) and must return a single column: the username to associate with the connection. Zero rows means auth is rejected. | Yes |
| maxConns | int32 | Maximum size of the pgxpool connection pool. Defaults to pgx's library default. | No |
| queryTimeoutMs | int | Per-lookup query timeout in milliseconds. Defaults to 5000. | No |

The `lookupQuery` is fully under your control — it can join other tables, filter on activation flags, IP allowlists, expirations, or any other column relevant to your environment. The username returned is exposed downstream as the SSH `Permissions.Extensions["user"]` value, just like the comment field of the file-based loader.

#### Schema

The default contract uses raw key bytes as the lookup key:

```sql
CREATE TABLE ssh_keys (
  pubkey   BYTEA PRIMARY KEY,
  username TEXT  NOT NULL,
  active   BOOLEAN NOT NULL DEFAULT true
);
```

The `pubkey` column stores the binary wire format of the SSH public key (the same bytes as `ssh.PublicKey.Marshal()` in Go). Postgres' B-tree handles 270-byte (RSA-2048) or 540-byte (RSA-4096) keys without issue.

##### Alternative: SHA256 fingerprint as the lookup key

A common variant is to index by SHA256 fingerprint (`SHA256:<base64>` strings produced by `ssh-keygen -lf`) instead of raw bytes. Fingerprints are smaller, fixed size, and friendlier for log/audit pipelines. The trade-off is that fingerprints are one-way hashes, so the DB alone can't reconstruct keys.

To switch, change one line at the call site in `pkg/ssh/gateway.go` from:

```go
user, ok, err := authDB.LookupUser(ctx, key.Marshal())
```

to:

```go
user, ok, err := authDB.LookupUser(ctx, []byte(ssh.FingerprintSHA256(key)))
```

and store the fingerprint in a `TEXT` column instead of `BYTEA`.

#### Importing an Existing authorized_keys File

The `frps-import-keys` CLI parses an OpenSSH `authorized_keys` file via `golang.org/x/crypto/ssh` (handles options prefixes, quoted comments, blank lines, and embedded spaces correctly) and bulk-loads it into the lookup table using `COPY` into a TEMP staging table followed by an upsert.

```bash
go build -o /usr/local/bin/frps-import-keys ./cmd/frps-import-keys

frps-import-keys \
  -file /etc/frp/authorized_keys \
  -dsn "postgres://frp:secret@db.internal:5432/frp?sslmode=require" \
  -table ssh_keys \
  -on-conflict update
```

| Flag | Default | Description |
| :--- | :--- | :--- |
| `-file` | (required) | Path to the OpenSSH authorized_keys file. |
| `-dsn` | (required) | Postgres DSN. |
| `-table` | `ssh_keys` | Target table name. |
| `-pubkey-col` | `pubkey` | BYTEA column holding the marshaled public key. |
| `-username-col` | `username` | TEXT column holding the username/comment. |
| `-on-conflict` | `update` | Conflict strategy: `update` (overwrite username), `ignore` (keep existing), or `error` (abort on duplicate). |
| `-truncate` | `false` | TRUNCATE the target table before import. |

#### Operational Notes

- **Hot updates.** Inserts, updates, and deletes against the lookup table take effect immediately — frps doesn't cache. `UPDATE ssh_keys SET active = false WHERE ...` revokes a key on the next connection attempt, no restart required.
- **DB reachability at startup.** The pool is created at gateway start and pings the database. If Postgres is unreachable at boot, frps fails fast with a clear error rather than silently disabling auth.
- **DB outages during operation.** A failed lookup is surfaced as an internal error to the SSH client and logged on the frps side. The connection is rejected, never silently allowed.
- **Token authentication.** As with `authorizedKeysFile`, this only governs SSH login auth. It is independent from frps token authentication and the two stack in the documented order (SSH first, then token).

Note that changes to the authorizedKeysFile file may result in SSH authentication failures. You may need to re-add the public key information to the authorizedKeysFile.
