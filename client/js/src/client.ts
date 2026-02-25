export type FlashValue = unknown;

export interface SetResult {
  key: string;
  value: FlashValue;
  version: number;
}

export interface ConflictResult {
  key: string;
  currentValue: FlashValue;
  currentVersion: number;
  conflict: true;
}

export type MutationResult = SetResult | ConflictResult;

type OutMessage = {
  id: string;
  op: "ack" | "error" | "event" | "pong";
  key?: string;
  value?: FlashValue;
  version?: number;
  error?: string;
  conflict?: boolean;
};

type InMessage = {
  id: string;
  op:
    | "get"
    | "set"
    | "cas"
    | "delete"
    | "sub"
    | "unsub"
    | "ping";
  key?: string;
  value?: FlashValue;
  version?: number;
};

type Listener = (
  value: FlashValue,
  version: number,
) => void;
type PendingResolve = (msg: OutMessage) => void;

export interface FlashDBOptions {
  url: string;
  token?: string;
  reconnectDelay?: number;
  maxReconnectDelay?: number;
}

export class FlashDB {
  private ws: WebSocket | null = null;
  private url: string;
  private token: string;
  private reconnectDelay: number;
  private maxReconnectDelay: number;
  private currentDelay: number;

  private pending = new Map<string, PendingResolve>();

  private listeners = new Map<string, Set<Listener>>();

  private cache = new Map<
    string,
    { value: FlashValue; version: number }
  >();

  private sendQueue: string[] = [];

  private connected = false;
  private destroyed = false;

  private onConnectCbs = new Set<() => void>();
  private onDisconnectCbs = new Set<() => void>();

  constructor(options: FlashDBOptions) {
    this.url = options.url;
    this.token = options.token ?? "";
    this.reconnectDelay = options.reconnectDelay ?? 1000;
    this.maxReconnectDelay =
      options.maxReconnectDelay ?? 30000;
    this.currentDelay = this.reconnectDelay;
    this.connect();
  }

  private connect() {
    if (this.destroyed) return;
    const wsUrl = this.token
      ? `${this.url}${this.url.includes("?") ? "&" : "?"}token=${encodeURIComponent(this.token)}`
      : this.url;
    this.ws = new WebSocket(wsUrl);

    this.ws.onopen = () => {
      console.log("[FlashDB] connected");
      this.connected = true;
      this.currentDelay = this.reconnectDelay;

      const q = this.sendQueue.splice(0);
      for (const msg of q) this.ws!.send(msg);

      for (const key of this.listeners.keys()) {
        this.rawSend({ id: this.newId(), op: "sub", key });
      }

      for (const cb of this.onConnectCbs) {
        cb();
      }
    };

    this.ws.onmessage = (e) => {
      let msg: OutMessage;
      try {
        msg = JSON.parse(e.data);
      } catch {
        return;
      }
      this.handleMessage(msg);
    };

    this.ws.onclose = () => {
      this.connected = false;
      for (const cb of this.onDisconnectCbs) {
        cb();
      }
      if (!this.destroyed) {
        console.log(
          `[FlashDB] disconnected, reconnecting in ${this.currentDelay}ms`,
        );
        setTimeout(() => this.connect(), this.currentDelay);
        this.currentDelay = Math.min(
          this.currentDelay * 2,
          this.maxReconnectDelay,
        );
      }
    };

    this.ws.onerror = (err) => {
      console.error("[FlashDB] ws error", err);
    };
  }

  private handleMessage(msg: OutMessage) {
    if (msg.op === "event") {
      if (msg.key !== undefined) {
        this.updateCache(
          msg.key,
          msg.value,
          msg.version ?? 0,
        );
        this.notifyListeners(
          msg.key,
          msg.value,
          msg.version ?? 0,
        );
      }
      return;
    }

    if (msg.op === "pong") return;

    if (msg.id && this.pending.has(msg.id)) {
      const resolve = this.pending.get(msg.id)!;
      this.pending.delete(msg.id);
      resolve(msg);

      if (msg.op === "ack" && msg.key && !msg.conflict) {
        if (
          msg.value !== undefined ||
          msg.version !== undefined
        ) {
          this.updateCache(
            msg.key,
            msg.value,
            msg.version ?? 0,
          );
          this.notifyListeners(
            msg.key,
            msg.value,
            msg.version ?? 0,
          );
        }
      }
    }
  }

  private updateCache(
    key: string,
    value: FlashValue,
    version: number,
  ) {
    const current = this.cache.get(key);
    if (!current || version >= current.version) {
      this.cache.set(key, { value, version });
    }
  }

  private notifyListeners(
    key: string,
    value: FlashValue,
    version: number,
  ) {
    const listeners = this.listeners.get(key);
    if (listeners) {
      for (const cb of listeners) {
        cb(value, version);
      }
    }
  }

  private rawSend(msg: InMessage) {
    const str = JSON.stringify(msg);
    if (
      this.connected &&
      this.ws?.readyState === WebSocket.OPEN
    ) {
      this.ws.send(str);
    } else {
      this.sendQueue.push(str);
    }
  }

  private newId(): string {
    return (
      Math.random().toString(36).slice(2) +
      Date.now().toString(36)
    );
  }

  private request(
    msg: Omit<InMessage, "id">,
  ): Promise<OutMessage> {
    return new Promise((resolve) => {
      const id = this.newId();
      this.pending.set(id, resolve);
      this.rawSend({ ...msg, id } as InMessage);
    });
  }

  async get<T = FlashValue>(
    key: string,
  ): Promise<{ value: T; version: number } | null> {
    const res = await this.request({ op: "get", key });
    if (res.op === "error") throw new Error(res.error);
    if (res.value === undefined) return null;
    return {
      value: res.value as T,
      version: res.version ?? 0,
    };
  }

  getCached<T = FlashValue>(
    key: string,
  ): { value: T; version: number } | null {
    const c = this.cache.get(key);
    return c
      ? { value: c.value as T, version: c.version }
      : null;
  }

  async set(
    key: string,
    value: FlashValue,
  ): Promise<SetResult> {
    const res = await this.request({
      op: "set",
      key,
      value,
    });
    if (res.op === "error") throw new Error(res.error);
    return {
      key: res.key!,
      value: res.value,
      version: res.version!,
    };
  }

  async cas(
    key: string,
    value: FlashValue,
    expectedVersion: number,
  ): Promise<MutationResult> {
    const res = await this.request({
      op: "cas",
      key,
      value,
      version: expectedVersion,
    });
    if (res.op === "error") throw new Error(res.error);
    if (res.conflict) {
      return {
        key: res.key!,
        currentValue: res.value,
        currentVersion: res.version!,
        conflict: true,
      };
    }
    return {
      key: res.key!,
      value: res.value,
      version: res.version!,
    };
  }

  async delete(
    key: string,
  ): Promise<{ key: string; version: number }> {
    const res = await this.request({ op: "delete", key });
    if (res.op === "error") throw new Error(res.error);
    this.cache.delete(key);
    return { key: res.key!, version: res.version! };
  }

  subscribe<T = FlashValue>(
    key: string,
    listener: (value: T, version: number) => void,
  ): () => void {
    if (!this.listeners.has(key)) {
      this.listeners.set(key, new Set());
      this.request({ op: "sub", key }).then((res) => {
        if (res.value !== undefined) {
          this.updateCache(
            key,
            res.value,
            res.version ?? 0,
          );
          (listener as Listener)(
            res.value,
            res.version ?? 0,
          );
        }
      });
    } else {
      const cached = this.cache.get(key);
      if (cached) {
        setTimeout(
          () =>
            (listener as Listener)(
              cached.value,
              cached.version,
            ),
          0,
        );
      }
    }

    this.listeners.get(key)!.add(listener as Listener);

    return () => {
      const set = this.listeners.get(key);
      if (!set) return;
      set.delete(listener as Listener);
      if (set.size === 0) {
        this.listeners.delete(key);
        this.rawSend({
          id: this.newId(),
          op: "unsub",
          key,
        });
      }
    };
  }

  onConnect(cb: () => void): () => void {
    this.onConnectCbs.add(cb);
    return () => { this.onConnectCbs.delete(cb); };
  }

  onDisconnect(cb: () => void): () => void {
    this.onDisconnectCbs.add(cb);
    return () => { this.onDisconnectCbs.delete(cb); };
  }

  isConnected(): boolean {
    return this.connected;
  }

  destroy() {
    this.destroyed = true;
    this.ws?.close();
  }
}
