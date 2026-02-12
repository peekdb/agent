# PeekDB Agent

Open-source agent for connecting private databases to [PeekDB](https://peekdb.com).

## Quick Start

```bash
docker run -d ghcr.io/peekdb/agent \
  --token=YOUR_TOKEN \
  --db="postgres://user:pass@localhost:5432/mydb"
```

## Features

- **Outbound-only connections** — No inbound ports or firewall changes needed
- **Secure** — Token-based auth, all traffic over TLS
- **Lightweight** — Single binary, minimal resource usage
- **Open source** — Audit the code that runs in your network

## Installation

### Docker (recommended)

```bash
docker run -d --name peekdb-agent \
  --network=host \
  -e PEEKDB_TOKEN=pdb_xxx \
  -e DATABASE_URL=postgres://user:pass@localhost:5432/mydb \
  peekdb/agent
```

Or if your database is in another container:

```bash
docker run -d --name peekdb-agent \
  --network=my-network \
  -e PEEKDB_TOKEN=pdb_xxx \
  -e DATABASE_URL=postgres://user:pass@db:5432/mydb \
  peekdb/agent
```

### Binary

Download from [releases](https://github.com/peekdb/agent/releases):

```bash
# Linux (amd64)
curl -L https://github.com/peekdb/agent/releases/latest/download/peekdb-agent-linux-amd64 -o peekdb-agent
chmod +x peekdb-agent

# Run
./peekdb-agent --token=pdb_xxx --db="postgres://..."
```

### Build from source

```bash
git clone https://github.com/peekdb/agent.git
cd agent
go build -o peekdb-agent .
```

## Configuration

| Flag | Env Var | Description |
|------|---------|-------------|
| `--token` | `PEEKDB_TOKEN` | Your PeekDB connection token (required) |
| `--db` | `DATABASE_URL` | PostgreSQL connection URL (required) |
| `--hub` | - | Hub URL (default: wss://connect.peekdb.com/agent) |
| `--name` | - | Connection name for display in PeekDB |

## How it works

1. Agent connects **outbound** to PeekDB's hub via WebSocket
2. Authenticates using your token
3. PeekDB sends SQL queries through the WebSocket
4. Agent executes queries against your local database
5. Results are sent back through the same connection

```
┌─────────────────────────────────────────────────────────┐
│                    Your Network                          │
│  ┌──────────────┐       ┌─────────────────────────────┐ │
│  │   Database   │◄─────►│  peekdb-agent               │ │
│  │  PostgreSQL  │ local │  Connects outbound only     │ │
│  └──────────────┘       └──────────────┬──────────────┘ │
└────────────────────────────────────────┼────────────────┘
                                         │ WSS (outbound)
                                         ▼
                              ┌──────────────────────┐
                              │   PeekDB Cloud       │
                              │   peekdb.com         │
                              └──────────────────────┘
```

## Security

- **No inbound ports** — Agent only makes outbound connections
- **Token-based auth** — Revoke anytime from PeekDB dashboard
- **TLS encryption** — All traffic encrypted
- **Read-only by default** — Only SELECT queries (configurable)
- **Open source** — Full audit of what runs in your network

## Troubleshooting

### Connection refused

Make sure your database URL is correct and the database is accessible from where the agent runs.

```bash
# Test database connection
psql "postgres://user:pass@localhost:5432/mydb"
```

### Authentication failed

Check that your token is correct and hasn't been revoked in the PeekDB dashboard.

### Agent keeps reconnecting

Check your network allows outbound WebSocket connections to `connect.peekdb.com:443`.

## License

Apache 2.0 — See [LICENSE](LICENSE)
