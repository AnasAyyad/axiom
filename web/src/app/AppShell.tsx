import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState, type ReactNode } from "react";
import { NavLink, useNavigate } from "react-router-dom";

import {
  parseStreamEvent,
  postAPI,
  setCSRFToken,
  type APIModel,
} from "../api/client";
import {
  binanceQuery,
  incidentsQuery,
  riskQuery,
  systemQuery,
} from "../api/queries";
import styles from "./Shell.module.css";

const navigation = [
  ["/", "Command Center"],
  ["/exchanges/binance", "Binance"],
  ["/portfolios", "Portfolio"],
  ["/risk", "Risk Center"],
  ["/strategies/trend", "Trend"],
  ["/backtests", "Backtest Lab"],
  ["/replays", "Replay Lab"],
  ["/shadow", "Shadow Center"],
  ["/incidents", "Incidents"],
  ["/audit", "Audit"],
] as const;

interface AppShellProps {
  readonly children: ReactNode;
  readonly user: APIModel<"SessionUser">;
}

export function AppShell({ children, user }: AppShellProps) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const system = useQuery(systemQuery);
  const binance = useQuery(binanceQuery);
  const risk = useQuery(riskQuery);
  const incidents = useQuery(incidentsQuery);
  const [streamState, setStreamState] = useState<"live" | "reconnecting">(
    "reconnecting",
  );
  const [theme, setTheme] = useState<"dark" | "light">("dark");
  const [timeMode, setTimeMode] = useState<"UTC" | "local">("UTC");
  const logout = useMutation({
    mutationFn: () => postAPI<"CommandAccepted">("/api/v1/session/logout", {}),
    onSettled: () => {
      setCSRFToken("");
      queryClient.clear();
      navigate("/login", { replace: true });
    },
  });
  useEffect(() => {
    document.documentElement.dataset.theme = theme;
  }, [theme]);
  useEffect(() => {
    let source: EventSource | undefined;
    let reconnect: number | undefined;
    let disposed = false;
    let lastRevision = BigInt(
      sessionStorage.getItem("axiom_stream_revision") ?? "0",
    );
    const connect = () => {
      if (disposed) return;
      const after =
        lastRevision > 0n ? `?after_revision=${lastRevision.toString()}` : "";
      source = new EventSource(`/api/v1/stream${after}`);
      source.onopen = () => setStreamState("live");
      source.onmessage = (event) => {
        try {
          const parsed = parseStreamEvent(event.data);
          if (!parsed.success) throw new Error("invalid_stream_event");
          const revision = BigInt(parsed.data.revision);
          if (revision <= lastRevision) return;
          if (lastRevision > 0n && revision !== lastRevision + 1n) {
            setStreamState("reconnecting");
            void queryClient.refetchQueries({ type: "active" });
          }
          lastRevision = revision;
          sessionStorage.setItem("axiom_stream_revision", revision.toString());
          void queryClient.invalidateQueries();
        } catch {
          setStreamState("reconnecting");
          void queryClient.refetchQueries({ type: "active" });
        }
      };
      source.onerror = () => {
        source?.close();
        setStreamState("reconnecting");
        void queryClient.refetchQueries({ type: "active" });
        reconnect = window.setTimeout(connect, 1_500);
      };
    };
    void queryClient.refetchQueries({ type: "active" }).finally(() => {
      const snapshot = queryClient.getQueryData<APIModel<"SystemStatus">>([
        "system",
      ]);
      if (snapshot?.revision !== undefined) {
        const snapshotRevision = BigInt(snapshot.revision);
        if (snapshotRevision > lastRevision) lastRevision = snapshotRevision;
      }
      connect();
    });
    return () => {
      disposed = true;
      source?.close();
      if (reconnect !== undefined) window.clearTimeout(reconnect);
    };
  }, [queryClient]);
  const serverTime =
    system.data?.server_time === undefined
      ? "Unavailable"
      : timeMode === "UTC"
        ? system.data.server_time
        : new Date(system.data.server_time).toLocaleString();
  return (
    <div className={styles.application}>
      <div className={styles.safetyBanner} role="status">
        <strong>SHADOW · VIRTUAL</strong>
        <span>REAL TRADING DISABLED</span>
      </div>
      <aside className={styles.sidebar}>
        <div className={styles.brand}>
          <span>A</span>
          <strong>AXIOM</strong>
        </div>
        <nav aria-label="Research console">
          {navigation.map(([to, label]) => (
            <NavLink
              key={to}
              to={to}
              end={to === "/"}
              className={({ isActive }) =>
                isActive ? styles.active : undefined
              }
            >
              {label}
            </NavLink>
          ))}
        </nav>
        <div className={styles.identity}>
          <dl className={styles.statusFacts}>
            <div>
              <dt>Environment</dt>
              <dd>production_public · shadow</dd>
            </div>
            <div>
              <dt>Engine</dt>
              <dd>
                {system.data?.engine_state ??
                  system.data?.lifecycle_state ??
                  "Unavailable"}
              </dd>
            </div>
            <div>
              <dt>Binance</dt>
              <dd>{binance.data?.websocket_state ?? "Unavailable"}</dd>
            </div>
            <div>
              <dt>Risk</dt>
              <dd>{risk.data?.state ?? "Unavailable"}</dd>
            </div>
            <div>
              <dt>Active</dt>
              <dd>{system.data?.active_resource_id ?? "None"}</dd>
            </div>
            <div>
              <dt>Critical</dt>
              <dd>
                {String(
                  system.data?.critical_incidents ??
                    incidents.data?.items.filter(
                      (item) =>
                        item.severity === "critical" &&
                        item.state !== "resolved",
                    ).length ??
                    0,
                )}
              </dd>
            </div>
            <div>
              <dt>Clock</dt>
              <dd>{serverTime}</dd>
            </div>
          </dl>
          <span data-live={streamState === "live"}>{streamState}</span>
          <small>{user.email}</small>
          <div className={styles.preferences}>
            <button
              type="button"
              onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
            >
              {theme === "dark" ? "Light" : "Dark"} theme
            </button>
            <button
              type="button"
              onClick={() => setTimeMode(timeMode === "UTC" ? "local" : "UTC")}
            >
              {timeMode === "UTC" ? "Local time" : "UTC time"}
            </button>
            <button
              type="button"
              onClick={() => logout.mutate()}
              disabled={logout.isPending}
            >
              Log out
            </button>
          </div>
        </div>
      </aside>
      <main className={styles.content}>{children}</main>
    </div>
  );
}
