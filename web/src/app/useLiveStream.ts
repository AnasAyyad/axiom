import type { QueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";

import { parseStreamEvent, type APIModel } from "../api/client";

/** useLiveStream reconciles the resumable event stream with REST snapshots. */
export function useLiveStream(queryClient: QueryClient) {
  const [state, setState] = useState<"live" | "reconnecting">("reconnecting");

  useEffect(() => {
    let source: EventSource | undefined;
    let reconnect: number | undefined;
    let disposed = false;
    let recovering = false;
    let lastRevision = BigInt(
      sessionStorage.getItem("axiom_stream_revision") ?? "0",
    );
    const synchronizeSnapshot = async (reset: boolean) => {
      await queryClient.refetchQueries({ type: "active" });
      const snapshot = queryClient.getQueryData<APIModel<"SystemStatus">>([
        "system",
      ]);
      if (snapshot?.revision === undefined) return;
      const snapshotRevision = BigInt(snapshot.revision);
      if (reset || snapshotRevision > lastRevision) {
        lastRevision = snapshotRevision;
        if (lastRevision > 0n) {
          sessionStorage.setItem(
            "axiom_stream_revision",
            lastRevision.toString(),
          );
        } else {
          sessionStorage.removeItem("axiom_stream_revision");
        }
      }
    };
    const connect = () => {
      if (disposed) return;
      if (!window.navigator.onLine) {
        setState("reconnecting");
        return;
      }
      const after =
        lastRevision > 0n ? `?after_revision=${lastRevision.toString()}` : "";
      source = new EventSource(`/api/v1/stream${after}`);
      source.onopen = () => setState("live");
      source.onmessage = (event) => {
        try {
          const parsed = parseStreamEvent(event.data);
          if (!parsed.success) throw new Error("invalid_stream_event");
          const revision = BigInt(parsed.data.revision);
          if (revision <= lastRevision) return;
          if (lastRevision > 0n && revision !== lastRevision + 1n) {
            setState("reconnecting");
            void queryClient.refetchQueries({ type: "active" });
          }
          lastRevision = revision;
          sessionStorage.setItem("axiom_stream_revision", revision.toString());
          void queryClient.invalidateQueries();
        } catch {
          setState("reconnecting");
          void queryClient.refetchQueries({ type: "active" });
        }
      };
      source.onerror = () => {
        if (recovering || disposed) return;
        recovering = true;
        source?.close();
        setState("reconnecting");
        // EventSource hides the HTTP status, including an expired-cursor 410.
        // Converge on authoritative snapshots and restart from their global
        // outbox revision so the client cannot loop forever on an expired ID.
        void synchronizeSnapshot(true).finally(() => {
          if (disposed) return;
          reconnect = window.setTimeout(() => {
            recovering = false;
            connect();
          }, 1_500);
        });
      };
    };
    const handleOffline = () => {
      recovering = true;
      source?.close();
      source = undefined;
      if (reconnect !== undefined) {
        window.clearTimeout(reconnect);
        reconnect = undefined;
      }
      setState("reconnecting");
    };
    const handleOnline = () => {
      if (disposed) return;
      source?.close();
      source = undefined;
      void synchronizeSnapshot(false).finally(() => {
        if (disposed || !window.navigator.onLine) return;
        recovering = false;
        connect();
      });
    };
    window.addEventListener("offline", handleOffline);
    window.addEventListener("online", handleOnline);
    void synchronizeSnapshot(false).finally(connect);
    return () => {
      disposed = true;
      source?.close();
      if (reconnect !== undefined) window.clearTimeout(reconnect);
      window.removeEventListener("offline", handleOffline);
      window.removeEventListener("online", handleOnline);
    };
  }, [queryClient]);

  return state;
}
