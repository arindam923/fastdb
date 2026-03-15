# FlashDB

**FlashDB** is a lightweight **real-time key-value database with a WebSocket API**, designed for building collaborative applications, live dashboards, and real-time features.

It combines **in-memory performance**, **versioned updates**, and **push-based subscriptions** so clients can react instantly to data changes.

FlashDB is ideal for applications where **state synchronization between multiple clients** is critical.

---

# Why FlashDB?

Modern applications often need real-time state sharing across clients.

FlashDB removes the complexity of building:

- WebSocket infrastructure
- State synchronization
- Pub/Sub systems
- Optimistic updates
- Concurrency-safe writes

Instead, it provides a **simple key-value model with built-in realtime subscriptions**.

### Typical use cases

- Collaborative applications
- Live dashboards
- Multiplayer state sync
- Real-time UI state
- Feature flags
- Presence systems
- Shared counters
- Reactive frontends

---

# Features

### Realtime Subscriptions
Subscribe to key changes and receive **instant push updates** over WebSocket.

### Versioned Keys
Every key is versioned and supports **Compare-And-Swap (CAS)** to prevent race conditions.

### JWT Authentication
Secure WebSocket connections using **JWT tokens**.

### Namespaces
Multi-tenant data isolation through **namespaces**.

### Persistence
Data is persisted using a **Write-Ahead Log (WAL)** for durability.

### Sharded Storage
High-performance **sharded in-memory store** for concurrent access.

---

# Quick Start

### Run the server

```bash
./flashdb
```

Default configuration:
- port: 8080
- host: 0.0.0.0
- data-dir: ./flashdb-data

FlashDB automatically generates a JWT secret on first run.

#### Custom port
```bash
./flashdb --port 3000
# or
./flashdb -p 3000
```

#### Custom data directory
```bash
./flashdb --data-dir /var/lib/flashdb
```

#### Show all options
```bash
./flashdb --help
```

#### Docker
```bash
docker-compose up -d
```

#### Download Pre-built Binaries

Pre-built binaries are available for multiple platforms:

- **Linux (AMD64)**
- **Linux (ARM64)**  
- **Windows (AMD64)**
- **macOS (AMD64)**
- **macOS (ARM64)**

Download the latest release from:  
[https://github.com/arindam923/fastdb/releases/tag/v1.0.0](https://github.com/arindam923/fastdb/releases/tag/v1.0.0)

#### Build From Source
```bash
git clone https://github.com/arindam923/fastdb
cd fastdb/server
go build -o fastdb .
./fastdb
```

### Configuration

FlashDB can be configured via CLI flags, environment variables, or config file.

#### CLI Flags

| Flag               | Short | Default          | Description               |
|---------------------|-------|------------------|---------------------------|
| --port              | -p    | 8080             | Server port               |
| --host              | -h    | 0.0.0.0          | Bind address              |
| --data-dir          | -d    | ./flashdb-data   | Data directory            |
| --jwt-secret        | -j    | auto-generated   | JWT signing secret        |
| --config            | -c    | -                | Config file path          |
| --persist-interval  |       | 5s               | Persistence interval      |
| --log-level         |       | info             | Log level                 |

#### Environment Variables

All flags can also be set via environment variables:
```bash
PORT=3000 DATA_DIR=/var/lib/flashdb ./flashdb
```

### Authentication

FlashDB uses JWT tokens for WebSocket authentication.

When the server starts for the first time it generates:
`flashdb-data/flashdb.yaml`

Example:
```yaml
jwt_secret: super-secret-key
```

Use this secret to generate client tokens.

### WebSocket Connection

```
ws://localhost:8080/ws?token=<JWT_TOKEN>
```

---

# API

Clients communicate with FlashDB using JSON messages over WebSocket.

### Supported Operations

| Operation | Description              |
|-----------|--------------------------|
| get       | Retrieve value by key    |
| set       | Set value (overwrite)    |
| cas       | Compare-and-swap update  |
| delete    | Delete key               |
| sub       | Subscribe to key updates |
| unsub     | Unsubscribe from key     |

### Client → Server

Example request:
```json
{
  "id": "req-123",
  "op": "set",
  "key": "users:1",
  "value": {
    "name": "Alice"
  }
}
```

### Server → Client

Acknowledgement:
```json
{
  "id": "req-123",
  "op": "ack",
  "key": "users:1",
  "value": {
    "name": "Alice"
  },
  "version": 1
}
```

### Realtime Events

When a subscribed key changes, clients receive:
```json
{
  "op": "event",
  "key": "users:1",
  "value": {
    "name": "Alice"
  },
  "version": 2
}
```

---

# React SDK

FlashDB ships with a React SDK for real-time state synchronization.

### Install
```bash
npm install @arindam923/flashdb
```

### Example
```tsx
import { FlashDB, FlashDBProvider, useValue, useMutation } from '@arindam923/flashdb'

const db = new FlashDB({
  url: 'ws://localhost:8080/ws',
  jwtSecret: 'your-jwt-secret',
  jwtExpiresIn: 3600
})

function App() {
  return (
    <FlashDBProvider client={db}>
      <Counter />
    </FlashDBProvider>
  )
}

function Counter() {
  const { value: count } = useValue('counter')
  const { mutate, loading } = useMutation('counter')

  return (
    <button
      disabled={loading}
      onClick={() => mutate((count ?? 0) + 1)}
    >
      Count: {count ?? 0}
    </button>
  )
}
```

### SDK Options

| Option            | Type   | Description               |
|-------------------|--------|---------------------------|
| url               | string | WebSocket server URL      |
| token             | string | Pre-generated JWT token   |
| jwtSecret         | string | Secret used to generate token |
| jwtExpiresIn      | number | Token expiry time         |
| reconnectDelay    | number | Initial reconnect delay   |
| maxReconnectDelay | number | Maximum reconnect delay   |

### React Hooks

| Hook                  | Description                  |
|-----------------------|------------------------------|
| useValue(key)         | Subscribe to a key           |
| useMutation(key)      | Update or delete a key       |
| useConnection()       | Track connection state       |
| useOptimisticValue(key)| Local optimistic updates     |

---

# Architecture

```
flashdb/
├── server/
│   ├── main.go       # Entry point
│   ├── config.go     # Configuration management
│   ├── hub.go        # WebSocket connection handler
│   └── store.go      # In-memory storage
│
├── client/
│   └── js/
│       └── React SDK
│
└── .github/
    └── CI/CD
```

### Server Components

#### Hub
Handles WebSocket connections, routing messages, and managing subscriptions.

#### Store
Sharded in-memory key-value store with versioning and WAL persistence.

#### WAL
Write-Ahead Log ensures durability and crash recovery.

---

# Contributing

Contributions are welcome!

1. Fork the repository
2. Create a feature branch
3. Commit your changes
4. Submit a pull request

Please read CONTRIBUTING.md before submitting.

---

# License

MIT License