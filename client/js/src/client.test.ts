import { describe, it, expect, vi, beforeEach } from 'vitest';
import { FlashDB } from './client';

describe('FlashDB Client', () => {
  beforeEach(() => {
    // Mock WebSocket
    global.WebSocket = vi.fn();
  });

  it('should create an instance of FlashDB', () => {
    const client = new FlashDB({
      url: 'ws://localhost:8080',
      jwtSecret: 'test-secret'
    });
    expect(client).toBeInstanceOf(FlashDB);
  });

  it('should initialize with correct options', () => {
    const client = new FlashDB({
      url: 'ws://localhost:8080',
      token: 'test-token',
      jwtSecret: 'test-secret',
      jwtExpiresIn: 3600,
      reconnectDelay: 1000,
      maxReconnectDelay: 30000
    });
    
    expect(client['token']).toBe('test-token');
    expect(client['jwtSecret']).toBe('test-secret');
    expect(client['jwtExpiresIn']).toBe(3600);
    expect(client['reconnectDelay']).toBe(1000);
    expect(client['maxReconnectDelay']).toBe(30000);
  });

  it('should auto-detect TLS from URL', () => {
    const client1 = new FlashDB({
      url: 'https://localhost:8080',
      jwtSecret: 'test-secret'
    });
    expect(client1['url']).toBe('https://localhost:8080');

    const client2 = new FlashDB({
      url: 'wss://localhost:8080',
      jwtSecret: 'test-secret'
    });
    expect(client2['url']).toBe('wss://localhost:8080');
  });

  it('should handle TLS option', () => {
    const client = new FlashDB({
      url: 'http://localhost:8080',
      jwtSecret: 'test-secret',
      tls: true
    });
    expect(client['url']).toBe('https://localhost:8080');
  });

  it('should generate JWT token correctly', async () => {
    const client = new FlashDB({
      url: 'ws://localhost:8080',
      jwtSecret: 'test-secret'
    });
    
    const token = await client['generateToken']();
    expect(token).toBeDefined();
    expect(token).not.toBe('');
    expect(token.split('.')).toHaveLength(3);
  });
});