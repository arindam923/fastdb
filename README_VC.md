# FlashDB - Hidden Version Control System

FlashDB now implements a complete hidden version control system for handling concurrent updates. The version number is managed internally by the database, providing automatic conflict detection and resolution without exposing users to complex version management.

## Features

### Hidden Version Control
- **Internal Versioning**: Each data entry has a hidden version number that increments with every update
- **Simple API**: Users work with raw data, no need to track versions manually
- **Automatic Conflict Detection**: Compare-and-Swap (CAS) operation ensures updates only happen if versions match

### Conflict Resolution
- **Conflict Detection**: Failed updates return detailed information about the conflict
- **Current State Information**: Conflicts include the current value and version of the data
- **Retry Mechanism**: Clients can automatically retry updates with the new version

### Performance Optimizations
- **Optimistic Locking**: No locks during reads, high concurrency performance
- **Atomic Operations**: CAS operations are atomic and consistent
- **Low Overhead**: Version management adds minimal overhead to operations

## How it Works

1. **Initialize Data**: Use `set()` to create or update data without worrying about versions
2. **Get Data**: Use `get()` to retrieve data along with its current version (available but not exposed to users)
3. **CAS Operation**: Use `cas()` to update data with version control
4. **Conflict Handling**: If versions don't match, the update is rejected with conflict details

## Usage Examples

### Basic Usage
```javascript
import { FlashDB } from '@arindam923/flashdb';

const db = new FlashDB({
  url: 'ws://localhost:8080',
  jwtSecret: 'your-secret'
});

// Initialize data
const initialResult = await db.set('counter', 0);
console.log('Initial:', initialResult); // { key: 'counter', value: 0, version: 1 }

// Get current state
const current = await db.get('counter');
console.log('Current:', current); // { value: 0, version: 1 }

// Update with version control
const updateResult = await db.cas('counter', 1, current.version);
console.log('Update:', updateResult); // { key: 'counter', value: 1, version: 2 }
```

### Handling Conflicts
```javascript
async function updateWithRetry(key, updateFn, retries = 3) {
  for (let i = 0; i < retries; i++) {
    try {
      const current = await db.get(key);
      const newValue = updateFn(current.value);
      const result = await db.cas(key, newValue, current.version);
      
      if (!result.conflict) {
        return result;
      }
      
      console.warn(`Conflict on attempt ${i + 1}, retrying...`);
      await new Promise(resolve => setTimeout(resolve, 100 * Math.pow(2, i)));
    } catch (error) {
      console.error('Error:', error);
      if (i === retries - 1) throw error;
    }
  }
}

// Usage
const result = await updateWithRetry('shared:counter', value => value + 1);
console.log('Updated value:', result.value);
```

### REST API
```bash
# Set value
curl -X POST http://localhost:8080/set \
  -H "Content-Type: application/json" \
  -d '{"key":"test","value":"initial"}'

# Get value with version
curl -X GET http://localhost:8080/get/test

# Compare and swap
curl -X POST http://localhost:8080/cas \
  -H "Content-Type: application/json" \
  -d '{"key":"test","value":"updated","expectedVersion":1}'
```

## Performance Metrics

Version control operations are tracked in Prometheus metrics:
- `flashdb_operations_total{type="cas"}` - Total CAS operations
- `flashdb_operations_errors{type="cas"}` - CAS operation errors
- `flashdb_operations_duration_seconds{type="cas"}` - CAS operation durations

## Demo

Run the server and open `version-control-demo.html` in your browser to see the version control system in action. The demo includes:
- Connection testing
- Data initialization
- Concurrent update testing
- User interaction simulation with conflict detection

## Use Cases

1. **Shopping Cart Updates**: Prevent concurrent updates to cart quantities from overwriting each other
2. **Form Submissions**: Handle conflicting form submissions gracefully
3. **Real-Time Collaboration**: Enable collaborative editing with automatic conflict resolution
4. **Stock Management**: Ensure inventory updates are atomic and consistent

## Architecture

The version control system is implemented at the core of FlashDB:
- **Server-Side**: Go implementation with atomic operations and WAL for durability
- **Client-Side**: JavaScript/TypeScript clients with automatic reconnection and caching
- **Storage**: Sharded in-memory store with persistent WAL and snapshot support

## Future Enhancements

- **Automatic Merging**: Smart conflict resolution with automatic content merging
- **Conflict History**: Detailed conflict history and resolution suggestions
- **Version Pruning**: Optimized version storage for historical data
- **Advanced Metrics**: Detailed performance metrics for version control operations

FlashDB's hidden version control system provides a robust and efficient solution for handling concurrent updates in distributed systems, ensuring data consistency while maintaining a simple API for developers.