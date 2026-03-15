# FlashDB Version Control System

FlashDB implements a hidden version control system to handle concurrent updates to the same data. The version number is managed internally by the database and is not exposed to users unless they explicitly request it.

## How It Works

### Hidden Versions
- Each data entry has an internal version number that is automatically incremented with every update
- The version number is not part of the user data
- Users work with the raw data, and the database manages versioning behind the scenes

### Compare-and-Swap (CAS) Operation
The version control system uses a `cas` (compare-and-swap) operation to ensure:
1. An update will only succeed if the expected version matches the actual version in the database
2. If versions don't match, the update is rejected with a conflict error
3. The database returns detailed information about the conflict, including the current value and version

## API Usage

### Setting Data (Simple Set)
```javascript
import { FlashDB } from '@arindam923/flashdb';

const db = new FlashDB({
  url: 'ws://localhost:8080',
  jwtSecret: 'your-secret'
});

// Set value without worrying about versions
const result = await db.set('key', 'value');
console.log('Set result:', result);
// Output: { key: 'key', value: 'value', version: 1 }
```

### Getting Data (Includes Version)
```javascript
import { FlashDB } from '@arindam923/flashdb';

const db = new FlashDB({
  url: 'ws://localhost:8080',
  jwtSecret: 'your-secret'
});

// Get value with version information
const result = await db.get('key');
console.log('Get result:', result);
// Output: { value: 'value', version: 1 }
```

### CAS Operation (Version Control)
```javascript
import { FlashDB } from '@arindam923/flashdb';

const db = new FlashDB({
  url: 'ws://localhost:8080',
  jwtSecret: 'your-secret'
});

// Get current value and version
const current = await db.get('key');
console.log('Current state:', current); // { value: 'current-value', version: 5 }

// Try to update - will only succeed if version is still 5
const result = await db.cas('key', 'new-value', current.version);

if (result.conflict) {
  console.error('❌ Conflict!');
  console.error('Current value:', result.currentValue);
  console.error('Current version:', result.currentVersion);
  console.error('Error:', result.error);
} else {
  console.log('✅ Success!');
  console.log('New value:', result.value);
  console.log('New version:', result.version);
}
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

## Handling Concurrent Updates

### Example Scenario
```javascript
import { FlashDB } from '@arindam923/flashdb';

const db = new FlashDB({
  url: 'ws://localhost:8080',
  jwtSecret: 'your-secret'
});

async function user1Updates() {
  const current = await db.get('shared:counter');
  await db.cas('shared:counter', current.value + 1, current.version);
}

async function user2Updates() {
  const current = await db.get('shared:counter');
  await db.cas('shared:counter', current.value + 1, current.version);
}

// Both users update the counter at the same time
Promise.all([user1Updates(), user2Updates()])
  .then(results => {
    console.log('Results:', results);
  })
  .catch(error => {
    console.error('Error:', error);
  });
```

### Conflict Resolution Strategies
1. **Automatic Retry**: Implement exponential backoff with retries
2. **User Notification**: Show conflict and allow user to resolve
3. **Merge Strategy**: Automatically merge changes if possible
4. **Optimistic Updates**: Show updates immediately while validating in background

### Automatic Retry Example
```javascript
async function updateWithRetry(key, updateFn, retries = 3, delay = 100) {
  for (let i = 0; i < retries; i++) {
    try {
      const current = await db.get(key);
      const newValue = updateFn(current.value);
      const result = await db.cas(key, newValue, current.version);
      
      if (!result.conflict) {
        return result;
      }
      
      console.warn(`Conflict on attempt ${i + 1}, retrying...`);
      await new Promise(resolve => setTimeout(resolve, delay * Math.pow(2, i)));
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

## Server-Side Implementation

### Store Structure
```go
// Entry represents a single key-value pair with version
type Entry struct {
    mu      sync.RWMutex
    data    json.RawMessage
    version int64  // Hidden version number
    updated time.Time
}

// CAS updates a value only if the expected version matches
func (s *Store) CAS(key string, value json.RawMessage, expectedVersion int64) (SetResult, bool) {
    sh := s.shard(key)
    sh.mu.Lock()
    e, ok := sh.entries[key]
    if !ok {
        if expectedVersion != 0 {
            sh.mu.Unlock()
            return SetResult{}, false
        }
        e = &Entry{}
        sh.entries[key] = e
    }
    sh.mu.Unlock()

    e.mu.Lock()
    if e.version != expectedVersion {
        e.mu.Unlock()
        return SetResult{Key: key, Value: e.data, Version: e.version}, false
    }
    e.version++
    ver := e.version
    e.data = value
    e.updated = time.Now()
    e.mu.Unlock()

    s.wal.writeEntry(&WalEntry{
        Type:      WalOpCAS,
        Key:       key,
        Value:     value,
        Version:   ver,
        Timestamp: time.Now(),
    })

    return SetResult{Key: key, Value: value, Version: ver}, true
}
```

## Version Persistence
- Versions are included in the Write-Ahead Log (WAL) for crash recovery
- When loading from disk, both data and versions are restored
- Snapshots include complete version information

## Metrics
Version control operations are tracked in Prometheus metrics:
- `flashdb_operations_total{type="cas"}` - Total CAS operations
- `flashdb_operations_errors{type="cas"}` - CAS operation errors
- `flashdb_operations_duration_seconds{type="cas"}` - CAS operation durations

## Performance Considerations

### Concurrency vs Consistency
- The system uses optimistic locking (no locks during reads)
- Updates are atomic and consistent
- Performance is excellent for high concurrency scenarios

### Best Practices
1. **Use CAS for Critical Data**: When updates must be atomic and consistent
2. **Keep Operations Small**: Each CAS operation should modify a single field or small document
3. **Implement Retries**: Handle conflicts gracefully with automatic retries
4. **Monitor Metrics**: Track CAS operation rates and errors

## Conflict Scenarios

### Common Causes of Conflicts
1. **Concurrent Updates**: Two or more users update the same data at the same time
2. **Network Latency**: One user's request is delayed and arrives after another's
3. **Optimistic Updates**: Client shows update before validation completes

### Conflict Error Structure
```javascript
{
  key: 'test',
  currentValue: 'version 1',
  currentVersion: 2,
  conflict: true,
  error: 'version mismatch'
}
```

## Use Cases

### 1. Shopping Cart Updates
```javascript
// Update cart quantity with conflict detection
async function updateCartQuantity(productId, newQuantity) {
  return updateWithRetry(`cart:${userId}`, cart => {
    return {
      ...cart,
      items: cart.items.map(item => 
        item.productId === productId 
          ? { ...item, quantity: newQuantity }
          : item
      )
    };
  });
}
```

### 2. Form Submissions
```javascript
// Handle form submission with version control
async function handleSubmit(data) {
  try {
    const result = await updateWithRetry('form:data', currentData => {
      return {
        ...currentData,
        ...data
      };
    });
    
    showSuccess('Form submitted successfully');
  } catch (error) {
    showError('Your changes conflict with other updates');
    refreshData();
  }
}
```

### 3. Real-Time Collabaration
```javascript
// Collaborative editing with version control
function onTextChange(text) {
  updateWithRetry('document:123', current => {
    const merged = mergeText(current.text, text, userSelection);
    return {
      ...current,
      text: merged
    };
  });
}
```

## Summary

FlashDB's hidden version control system provides a simple and effective way to handle concurrent updates without exposing users to complex version management. The `cas` operation ensures atomicity and consistency, while the hidden versions make the API clean and intuitive. This approach works particularly well for real-time applications, collaborative editing, and any scenario where data consistency is critical.