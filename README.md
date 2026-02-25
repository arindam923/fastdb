# FlashDB

A realtime key-value database with WebSocket API, built in Go. Perfect for building collaborative applications, live dashboards, and real-time features.

## Features

- **Realtime Subscriptions** - Subscribe to key changes and receive instant updates
- **Versioned Keys** - Built-in versioning with Compare-And-Swap (CAS) for safe concurrent updates
- **JWT Authentication** - Secure WebSocket connections with JWT tokens
- **Namespaces** - Multi-tenant support with namespace isolation
- **Persistence** - Automatic disk persistence with WAL (Write-Ahead Log)
- **Sharded Storage** - High-performance sharded in-memory storage

## Quick Start

### Running with Docker

```bash
docker-compose up -d
```

### Running from Source

```bash
cd server
go build -o flashdb .
JWT_SECRET=your-secret-key ./flashdb
```

The server starts on `http://localhost:8080`.

### WebSocket Connection

Connect to the WebSocket endpoint:

```
ws://localhost:8080/ws?token=<JWT_TOKEN>
```

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
npm install @arindam923/flashdb-client
```

### Usage

```tsx
import { FlashDB, FlashDBProvider, useValue, useMutation } from '@arindam923/flashdb-client';

const db = new FlashDB({
  url: 'ws://localhost:8080/ws',
  token: 'your-jwt-token'
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

### Hooks

- `useValue(key)` - Subscribe to a key's value
- `useMutation(key)` - Set/delete with optimistic updates
- `useConnection()` - Track connection state
- `useOptimisticValue(key)` - Instant local updates

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `PORT` | 8080 | Server port |
| `JWT_SECRET` | - | Required. Secret for JWT validation |
| `PERSIST_INTERVAL` | 5s | How often to persist to disk |

## Architecture

```
flashdb/
├── server/           # Go server
│   ├── main.go       # Entry point
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
