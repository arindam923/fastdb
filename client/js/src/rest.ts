import { FlashValue } from './client';

export interface SetResult {
  key: string;
  value: FlashValue;
  version: number;
}

export interface ConflictResult {
  key: string;
  value: FlashValue;
  version: number;
  error: string;
  conflict: boolean;
}

export type MutationResult = SetResult | ConflictResult;

export interface RESTClientOptions {
  baseUrl: string;
  token?: string;
  jwtSecret?: string;
  jwtExpiresIn?: number;
  rejectUnauthorized?: boolean;
}

export class RESTClient {
  private baseUrl: string;
  private token: string;
  private jwtSecret: string;
  private jwtExpiresIn: number;
  private rejectUnauthorized: boolean;

  constructor(options: RESTClientOptions) {
    this.baseUrl = options.baseUrl.replace(/\/$/, '');
    this.token = options.token ?? '';
    this.jwtSecret = options.jwtSecret ?? '';
    this.jwtExpiresIn = options.jwtExpiresIn ?? 3600;
    this.rejectUnauthorized = options.rejectUnauthorized ?? true;
  }

  private async generateToken(): Promise<string> {
    if (!this.jwtSecret) return '';

    const header = btoa(JSON.stringify({ alg: 'HS256', typ: 'JWT' }));
    const payload = btoa(
      JSON.stringify({
        exp: Math.floor(Date.now() / 1000) + this.jwtExpiresIn,
        iat: Math.floor(Date.now() / 1000),
      }),
    );

    const encoder = new TextEncoder();
    const keyData = encoder.encode(this.jwtSecret);
    const messageData = encoder.encode(`${header}.${payload}`);

    const key = await crypto.subtle.importKey(
      'raw',
      keyData,
      { name: 'HMAC', hash: 'SHA-256' },
      false,
      ['sign'],
    );

    const signature = await crypto.subtle.sign(
      'HMAC',
      key,
      messageData,
    );

    const signatureBase64 = btoa(String.fromCharCode(...new Uint8Array(signature)));
    return `${header}.${payload}.${signatureBase64}`;
  }

  private async getAuthHeaders(): Promise<Record<string, string>> {
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
    };

    if (this.token) {
      headers['Authorization'] = `Bearer ${this.token}`;
    } else if (this.jwtSecret) {
      const token = await this.generateToken();
      headers['Authorization'] = `Bearer ${token}`;
    }

    return headers;
  }

  private async fetchWithAuth(
    endpoint: string,
    options: RequestInit = {},
  ): Promise<Response> {
    const url = `${this.baseUrl}${endpoint}`;
    const headers = await this.getAuthHeaders();

    const response = await fetch(url, {
      ...options,
      headers: {
        ...headers,
        ...options.headers,
      },
    });

    if (!response.ok) {
      let errorMessage = `HTTP ${response.status}`;
      try {
        const errorData = await response.json();
        if (errorData.error) {
          errorMessage = errorData.error;
        }
      } catch {
        errorMessage = response.statusText || errorMessage;
      }
      throw new Error(errorMessage);
    }

    return response;
  }

  async get<T = FlashValue>(key: string): Promise<{ value: T; version: number }> {
    const response = await this.fetchWithAuth(`/get/${encodeURIComponent(key)}`);
    const data = await response.json();
    return {
      value: data.value,
      version: data.version,
    };
  }

  async set(key: string, value: FlashValue): Promise<SetResult> {
    const response = await this.fetchWithAuth('/set', {
      method: 'POST',
      body: JSON.stringify({
        key,
        value,
      }),
    });
    return await response.json();
  }

  async cas(
    key: string,
    value: FlashValue,
    expectedVersion: number,
  ): Promise<MutationResult> {
    const response = await this.fetchWithAuth('/cas', {
      method: 'POST',
      body: JSON.stringify({
        key,
        value,
        expectedVersion,
      }),
    });

    if (response.status === 409) {
      const data = await response.json();
      return data;
    }

    return await response.json();
  }

  async delete(key: string): Promise<{ key: string; version: number }> {
    const response = await this.fetchWithAuth(`/delete/${encodeURIComponent(key)}`, {
      method: 'DELETE',
    });
    return await response.json();
  }

  async keys(): Promise<{ keys: string[]; count: number }> {
    const response = await this.fetchWithAuth('/keys');
    return await response.json();
  }

  async snapshot(): Promise<void> {
    await this.fetchWithAuth('/snapshot', {
      method: 'POST',
    });
  }

  async health(): Promise<{ status: string }> {
    const response = await this.fetchWithAuth('/health');
    return await response.json();
  }
}

// Helper function to create REST client from options
export function createRESTClient(options: RESTClientOptions): RESTClient {
  return new RESTClient(options);
}