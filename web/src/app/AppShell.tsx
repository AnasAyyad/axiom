import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState, type ReactNode } from "react";
import { useNavigate } from "react-router";

import { postAPI, setCSRFToken, type APIModel } from "../api/client";
import {
  binanceQuery,
  exchangesQuery,
  incidentsQuery,
  riskQuery,
  systemQuery,
} from "../api/queries";
import { SafetyHeader } from "./SafetyHeader";
import { SidebarNavigation } from "./SidebarNavigation";
import styles from "./Shell.module.css";
import { useLiveStream } from "./useLiveStream";

interface AppShellProps {
  readonly children: ReactNode;
  readonly user: APIModel<"SessionUser">;
}

export function AppShell({ children, user }: AppShellProps) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const system = useQuery(systemQuery);
  const binance = useQuery(binanceQuery);
  const exchanges = useQuery(exchangesQuery);
  const risk = useQuery(riskQuery);
  const incidents = useQuery(incidentsQuery);
  const streamState = useLiveStream(queryClient);
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
  const serverTime =
    system.data?.server_time === undefined
      ? "Unavailable"
      : timeMode === "UTC"
        ? system.data.server_time
        : new Date(system.data.server_time).toLocaleString();
  const criticalIncidents =
    system.data?.critical_incidents ??
    incidents.data?.items.filter(
      (item) => item.severity === "critical" && item.state !== "resolved",
    ).length ??
    0;
  return (
    <div className={styles.application}>
      <SafetyHeader
        system={system.data}
        binance={binance.data}
        exchanges={exchanges.data}
        risk={risk.data}
        criticalAlerts={criticalIncidents}
        streamState={streamState}
      />
      <aside className={styles.sidebar}>
        <div className={styles.brand}>
          <span aria-hidden="true">A</span>
          <div>
            <strong>AXIOM</strong>
            <small>Research operations</small>
          </div>
        </div>
        <SidebarNavigation user={user} />
        <section
          className={styles.identity}
          aria-label="Owner and display preferences"
        >
          <dl className={styles.statusFacts}>
            <div>
              <dt>Account</dt>
              <dd>Owner</dd>
            </div>
            <div>
              <dt>Active run</dt>
              <dd>{system.data?.active_resource_id ?? "None"}</dd>
            </div>
            <div>
              <dt>Critical alerts</dt>
              <dd>{String(criticalIncidents)}</dd>
            </div>
            <div>
              <dt>Server time</dt>
              <dd>{serverTime}</dd>
            </div>
            <div>
              <dt>Clock drift</dt>
              <dd>
                {system.data?.clock_drift_ms ??
                  binance.data?.clock_drift_ms ??
                  "Unavailable"}
              </dd>
            </div>
          </dl>
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
        </section>
      </aside>
      <main className={styles.content}>{children}</main>
    </div>
  );
}
