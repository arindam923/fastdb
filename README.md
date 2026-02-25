# FlashDB

A realtime key-value database with WebSocket API, built in Go. Perfect for building collaborative applications, live dashboards, and real-time features.

## Download

### macOS
```bash
curl -L https://github.com/arindam923/flashdb/releases/latest/download/flashdb_darwin_amd64.tar.gz | tar xz
./flashdb
```

### Linux
```bash
curl -L https://github.com/arindam923/flashdb/releases/latest/download/flashdb_linux_amd64.tar.gz | tar xz
./flashdb
```

### Windows (PowerShell)
```powershell
Invoke-WebRequest -Uri "https://github.com/arindam923/flashdb/releases/latest/download/flashdb_windows_amd64.zip" -OutFile flashdb.zip
Expand-Archive flashdb.zip -DestinationPath .
.\flashdb.exe
```

## Features

- **Realtime Subscriptions** - Subscribe to key changes and receive instant updates
- **Versioned Keys** - Built-in versioning with Compare-And-Swap (CAS) for safe concurrent updates
- **JWT Authentication** - Secure WebSocket connections with JWT tokens
- **Namespaces** - Multi-tenant support with namespace isolation
- **Persistence** - Automatic disk persistence with WAL (Write-Ahead Log)
- **Sharded Storage** - High-performance sharded in-memory storage

## Quick Start

```bash
# Run with defaults (auto-generates JWT secret)
./flashdb

# Custom port
./flashdb --port 3000
./flashdb -p 3000

# Custom data directory (for production)
./flashdb --data-dir /var/lib/flashdb

# Show all options
./flashdb --help
```

On first run, FlashDB creates a config file at `./flashdb-data/flashdb.yaml` with your JWT secret.

## Configuration

### CLI Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--port` | `-p` | 8080 | Server port |
| `--host` | `-h` | 0.0.0.0 | Bind address |
| `--data-dir` | `-d` | ./flashdb-data | Data directory |
| `--jwt-secret` | `-j` | (auto-generate) | JWT signing secret |
| `--config` | `-c` | - | Config file path |
| `--persist-interval` | - | 5s | Persist interval |
| `--log-level` | - | info | Log level |

### Environment Variables

All CLI flags can also be set via environment variables:

```bash
PORT=3000 DATA_DIR=/var/lib/flashdb ./flashdb
```

## WebSocket Connection

Connect to the WebSocket endpoint using the JWT token from your config file:

```
ws://localhost:8080/ws?token=<JWT_TOKEN>
```

The JWT token is saved in `flashdb-data/flashdb.yaml` on first run.

## API

### Operations

| Op | Description |
|---|---|
| `get` | Get value by key |
| `set` | Set value (overwrites) |
| `cas` | Compare-and-swap (version check) |
| `delete` | Delete a key |
| `sub` | Subscribe to key changes |
| `unsub` | Unsubscribe from key |

### Messages

**Client → Server:**

```json
{
  "id": "req-123",
  "op": "set",
  "key": "users:1",
  "value": { "name": "Alice" }
}
```

**Server → Client:**

```json
{
  "id": "req-123",
  "op": "ack",
  "key": "users:1",
  "value": { "name": "Alice" },
  "version": 1
}
```

### Realtime Events

When a subscribed key changes, the server pushes:

```json
{
  "op": "event",
  "key": "users:1",
  "value": { "name": "Alice" },
  "version": 2
}
```

## React SDK

Install:

```bash
npm install @arindam923/flashdb
```

### Usage

```tsx
import { FlashDB, FlashDBProvider, useValue, useMutation } from '@arindam923/flashdb';

const db = new FlashDB({
  url: 'ws://localhost:8080/ws',
  // Option 1: Pass JWT secret directly - SDK generates token automatically
  jwtSecret: 'your-jwt-secret-from-flashdb-data/flashdb.yaml',
  jwtExpiresIn: 3600, // optional, token expiry in seconds
});

function App() {
  return (
    <FlashDBProvider client={db}>
      <Counter />
    </FlashDBProvider>
  );
}

function Counter() {
  const { value: count, version } = useValue('counter');
  const { mutate, loading } = useMutation('counter');
  
  return (
    <button onClick={() => mutate(count ?? 0 + 1)} disabled={loading}>
      Count: {count ?? 0}
    </button>
  );
}
```

### Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `url` | string | - | WebSocket URL |
| `token` | string | - | Pre-generated JWT token |
| `jwtSecret` | string | - | Plain text JWT secret (SDK auto-generates token) |
| `jwtExpiresIn` | number | 3600 | Token expiry in seconds |
| `reconnectDelay` | number | 1000 | Initial reconnect delay (ms) |
| `maxReconnectDelay` | number | 30000 | Max reconnect delay (ms) |

### Hooks

- `useValue(key)` - Subscribe to a key's value
- `useMutation(key)` - Set/delete with optimistic updates
- `useConnection()` - Track connection state
- `useOptimisticValue(key)` - Instant local updates

## Running with Docker

```bash
docker-compose up -d
```

## Running from Source

```bash
cd server
go build -o flashdb .
./flashdb
```

## Architecture

```
flashdb/
├── server/           # Go server
│   ├── main.go       # Entry point
│   ├── config.go     # CLI & config
│   ├── hub.go        # WebSocket hub & message handling
│   └── store.go      # Sharded in-memory store with WAL
├── client/js/        # React/TypeScript SDK
└── .github/          # CI/CD workflows
```

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Submit a pull request

## License

MIT
