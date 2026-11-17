import { useCallback, useEffect, useRef, useState } from "react";

import type { Result } from "@/lib/api/call";

export type Resource<T> = { status: "loading" } | { status: "error" } | { status: "ok"; data: T };

const loading = { status: "loading" } as const;

// useResource reads one thing from the API while key is not null, and again whenever key
// changes or reload is called. A reload keeps showing the old value until the new one comes,
// so a refetch does not flash the loading state. set changes the loaded value in place, for an
// optimistic update; the latest load wins over an older one.
export function useResource<T>(key: string | null, load: () => Promise<Result<T>>) {
  const latest = useRef(load);
  useEffect(() => {
    latest.current = load;
  });
  const [version, setVersion] = useState(0);
  const [state, setState] = useState<{ key: string | null; value: Resource<T> }>({ key: null, value: loading });

  useEffect(() => {
    if (key === null) return;
    let current = true;
    void latest.current().then((res) => {
      if (!current) return;
      setState((prev) => ({
        key,
        // A failed reload keeps what is shown; only a first load shows the error.
        value: res.ok ? { status: "ok", data: res.data } : prev.key === key && prev.value.status === "ok" ? prev.value : { status: "error" },
      }));
    });
    return () => {
      current = false;
    };
  }, [key, version]);

  const reload = useCallback(() => setVersion((v) => v + 1), []);
  const set = useCallback(
    (change: (data: T) => T) =>
      setState((prev) => (prev.value.status === "ok" ? { key: prev.key, value: { status: "ok", data: change(prev.value.data) } } : prev)),
    [],
  );

  const value: Resource<T> = key !== null && state.key === key ? state.value : loading;
  return { value, reload, set };
}

// optimistic shows a change at once, sends it, and undoes it with a toast when the server
// refuses. It answers whether the server took it.
export async function optimistic<T>({
  apply,
  rollback,
  send,
  onError,
}: {
  apply: () => void;
  rollback: () => void;
  send: () => Promise<Result<T>>;
  onError: (code: string) => void;
}): Promise<Result<T>> {
  apply();
  const res = await send();
  if (!res.ok) {
    rollback();
    onError(res.code);
  }
  return res;
}

// ResourceHandle is what useResource answers, for a component that shows one.
export type ResourceHandle<T> = ReturnType<typeof useResource<T>>;
