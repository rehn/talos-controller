"use client";

import { useCallback, useEffect, useRef, useState } from "react";

export interface Polled<T> {
  data: T | null;
  error: string | null;
  loading: boolean;
  refresh: () => Promise<void>;
  lastUpdated: Date | null;
}

/**
 * Fetches data on mount and every `intervalMs` (0 disables polling).
 * Polling pauses while the tab is hidden. Changing `deps` resets the data.
 */
export function usePoll<T>(fetcher: () => Promise<T>, intervalMs = 15000, deps: unknown[] = []): Polled<T> {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);
  const fetcherRef = useRef(fetcher);
  fetcherRef.current = fetcher;
  const gen = useRef(0);

  const refresh = useCallback(async () => {
    const my = gen.current;
    setLoading(true);
    try {
      const d = await fetcherRef.current();
      if (my !== gen.current) return;
      setData(d);
      setError(null);
      setLastUpdated(new Date());
    } catch (e) {
      if (my !== gen.current) return;
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      if (my === gen.current) setLoading(false);
    }
  }, []);

  const depKey = JSON.stringify(deps);

  useEffect(() => {
    gen.current++;
    setData(null);
    setError(null);
    void refresh();
    if (!intervalMs) return;
    const t = setInterval(() => {
      if (typeof document === "undefined" || document.visibilityState === "visible") void refresh();
    }, intervalMs);
    return () => clearInterval(t);
  }, [intervalMs, refresh, depKey]);

  return { data, error, loading, refresh, lastUpdated };
}
