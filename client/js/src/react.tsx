import {
  useState,
  useEffect,
  useCallback,
  useRef,
  useContext,
  createContext,
} from "react";
import {
  FlashDB,
  FlashValue,
  MutationResult,
} from "./client";

const FlashDBContext = createContext<FlashDB | null>(null);

export function FlashDBProvider({
  client,
  children,
}: {
  client: FlashDB;
  children: React.ReactNode;
}) {
  return (
    <FlashDBContext.Provider value={client}>
      {children}
    </FlashDBContext.Provider>
  );
}

export function useFlashDB(): FlashDB {
  const db = useContext(FlashDBContext);
  if (!db)
    throw new Error(
      "useFlashDB must be used inside <FlashDBProvider>",
    );
  return db;
}

export interface UseValueResult<T> {
  value: T | undefined;
  version: number;
  loading: boolean;
  error: Error | null;
}

export function useValue<T = FlashValue>(
  key: string,
): UseValueResult<T> {
  const db = useFlashDB();
  const [state, setState] = useState<UseValueResult<T>>(
    () => {
      const cached = db.getCached<T>(key);
      return {
        value: cached?.value,
        version: cached?.version ?? 0,
        loading: !cached,
        error: null,
      };
    },
  );

  useEffect(() => {
    let cancelled = false;

    const unsub = db.subscribe<T>(key, (value, version) => {
      if (!cancelled) {
        setState({
          value,
          version,
          loading: false,
          error: null,
        });
      }
    });

    return () => {
      cancelled = true;
      unsub();
    };
  }, [db, key]);

  return state;
}

export interface UseMutationOptions {
  maxRetries?: number;
}

export interface UseMutationResult {
  mutate: (
    valueOrUpdater:
      | FlashValue
      | ((current: FlashValue) => FlashValue),
  ) => Promise<MutationResult>;
  delete: () => Promise<void>;
  loading: boolean;
  error: Error | null;
}

export function useMutation(
  key: string,
  options: UseMutationOptions = {},
): UseMutationResult {
  const db = useFlashDB();
  const maxRetries = options.maxRetries ?? 10;
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const mutate = useCallback(
    async (
      valueOrUpdater:
        | FlashValue
        | ((current: FlashValue) => FlashValue),
    ): Promise<MutationResult> => {
      setLoading(true);
      setError(null);
      try {
        if (typeof valueOrUpdater === "function") {
          return await casRetry(
            db,
            key,
            valueOrUpdater as (current: FlashValue) => FlashValue,
            maxRetries,
          );
        } else {
          return await db.set(key, valueOrUpdater);
        }
      } catch (e) {
        const err =
          e instanceof Error ? e : new Error(String(e));
        setError(err);
        throw err;
      } finally {
        setLoading(false);
      }
    },
    [db, key, maxRetries],
  );

  const del = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      await db.delete(key);
    } catch (e) {
      const err =
        e instanceof Error ? e : new Error(String(e));
      setError(err);
      throw err;
    } finally {
      setLoading(false);
    }
  }, [db, key]);

  return { mutate, delete: del, loading, error };
}

async function casRetry(
  db: FlashDB,
  key: string,
  updater: (current: FlashValue) => FlashValue,
  maxRetries: number,
): Promise<MutationResult> {
  for (let attempt = 0; attempt < maxRetries; attempt++) {
    const current = await db.get(key);
    const currentValue = current?.value ?? null;
    const currentVersion = current?.version ?? 0;

    const newValue = updater(currentValue);
    const result = await db.cas(
      key,
      newValue,
      currentVersion,
    );

    if (!("conflict" in result)) {
      return result;
    }

    await sleep(10 + Math.random() * 50 * attempt);
  }
  throw new Error(
    `CAS failed after ${maxRetries} retries — too much contention on key "${key}"`,
  );
}

function sleep(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}

export function useConnection() {
  const db = useFlashDB();
  const [connected, setConnected] = useState(
    db.isConnected(),
  );

  useEffect(() => {
    const unCon = db.onConnect(() => setConnected(true));
    const unDis = db.onDisconnect(() =>
      setConnected(false),
    );
    return () => {
      unCon();
      unDis();
    };
  }, [db]);

  return { connected };
}

export function useOptimisticValue<T = FlashValue>(
  key: string,
) {
  const db = useFlashDB();
  const {
    value: serverValue,
    version,
    loading,
  } = useValue<T>(key);
  const [optimistic, setOptimisticState] = useState<
    T | undefined
  >(undefined);
  const inFlight = useRef(false);
  const lastVersion = useRef(version);

  useEffect(() => {
    if (version !== lastVersion.current) {
      lastVersion.current = version;
      if (!inFlight.current) {
        setOptimisticState(undefined);
      }
    }
  }, [version]);

  const setOptimistic = useCallback(
    async (
      updater: T | ((current: T | undefined) => T),
    ) => {
      const current = optimistic ?? serverValue;
      const next =
        typeof updater === "function"
          ? (updater as (current: T | undefined) => T)(current)
          : updater;

      setOptimisticState(next);
      inFlight.current = true;

      try {
        await casRetry(db, key, () => next, 10);
      } finally {
        inFlight.current = false;
        setOptimisticState(undefined);
      }
    },
    [db, key, optimistic, serverValue],
  );

  return {
    value: optimistic ?? serverValue,
    version,
    loading,
    isOptimistic: optimistic !== undefined,
    setOptimistic,
  };
}
