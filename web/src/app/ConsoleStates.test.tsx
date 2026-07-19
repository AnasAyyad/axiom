import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import axe from "axe-core";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

import { setCSRFToken } from "../api/client";
import { StatePanel } from "../components/StatePanel";
import { AppShell } from "./AppShell";
import { LoginPage } from "./LoginPage";

const states = [
  "loading",
  "empty",
  "degraded",
  "stale",
  "paused",
  "locked",
  "reconnecting",
  "forbidden",
  "error",
] as const;

afterEach(() => {
  vi.unstubAllGlobals();
  setCSRFToken("");
  sessionStorage.clear();
});

describe("A11 console states", () => {
  it("announces every required non-happy state accessibly", async () => {
    const view = render(
      <main>
        {states.map((state) => (
          <StatePanel key={state} state={state} detail={`${state} detail`} />
        ))}
      </main>,
    );
    expect(
      screen.getByText("Loading authoritative state…"),
    ).toBeInTheDocument();
    expect(screen.getByText("No durable records yet")).toBeInTheDocument();
    expect(screen.getByText("Data is stale")).toBeInTheDocument();
    expect(screen.getByText("Operations are paused")).toBeInTheDocument();
    expect(screen.getByText("Safety lock is active")).toBeInTheDocument();
    expect(
      screen.getByText("Reconnecting to live updates…"),
    ).toBeInTheDocument();
    expect(
      screen.getByText("You do not have permission to view this evidence"),
    ).toBeInTheDocument();
    const result = await axe.run(view.container);
    expect(result.violations).toHaveLength(0);
  });

  it("keeps the execution lock visible on the authenticated shell", async () => {
    vi.stubGlobal("fetch", vi.fn(a11FetchFixture));
    vi.stubGlobal("EventSource", FakeEventSource);
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const view = render(
      <QueryClientProvider client={client}>
        <MemoryRouter>
          <AppShell
            user={{
              id: "user-a11",
              email: "owner@example.test",
              roles: ["owner"],
              permissions: ["operations.read"],
            }}
          >
            <h1>Evidence workspace</h1>
          </AppShell>
        </MemoryRouter>
      </QueryClientProvider>,
    );
    expect(screen.getByText("REAL TRADING DISABLED")).toBeInTheDocument();
    expect(screen.getByText("SHADOW · VIRTUAL")).toBeInTheDocument();
    expect(screen.getByText("production_public · shadow")).toBeInTheDocument();
    await screen.findByText("PAUSED");
    await waitFor(async () => {
      const result = await axe.run(view.container);
      expect(result.violations).toHaveLength(0);
    });
  });

  it("presents an accessible credential form without accepting exchange credentials", async () => {
    const client = new QueryClient({
      defaultOptions: { mutations: { retry: false } },
    });
    const view = render(
      <QueryClientProvider client={client}>
        <MemoryRouter>
          <LoginPage />
        </MemoryRouter>
      </QueryClientProvider>,
    );
    expect(screen.getByLabelText("Email")).toHaveAttribute(
      "autocomplete",
      "username",
    );
    expect(screen.getByLabelText("Password")).toHaveAttribute(
      "autocomplete",
      "current-password",
    );
    expect(screen.getByText("REAL TRADING DISABLED")).toBeInTheDocument();
    expect(
      screen.getByText(/No exchange credentials are accepted/),
    ).toBeInTheDocument();
    const result = await axe.run(view.container);
    expect(result.violations).toHaveLength(0);
  });
});

function a11FetchFixture(input: RequestInfo | URL) {
  const path = String(input);
  const body = path.includes("system/status")
    ? {
        release: "V1A",
        phase: "A11",
        role: "api",
        lifecycle_state: "READY_PAUSED",
        strategy_activation: "trend.v1a.1",
        real_trading_enabled: false,
        execution_mode: "shadow",
        environment: "production_public",
        risk_state: "PAUSED",
        critical_incidents: 0,
        server_time: "2026-07-16T12:00:00Z",
        revision: "0",
      }
    : path.includes("binance/health")
      ? {
          environment: "production_public",
          public_only: true,
          websocket_state: "stale",
          book_state: "stale",
          recorder_state: "degraded",
          observed_at: "2026-07-16T12:00:00Z",
          revision: "0",
        }
      : path.includes("risk/status")
        ? {
            state: "PAUSED",
            policy_version: "1",
            recovery_ready: false,
            contributors: [],
            revision: "1",
            updated_at: "2026-07-16T12:00:00Z",
          }
        : { items: [], revision: "0", has_more: false };
  return Promise.resolve(
    new Response(JSON.stringify(body), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    }),
  );
}

class FakeEventSource {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSED = 2;
  readonly CONNECTING = 0;
  readonly OPEN = 1;
  readonly CLOSED = 2;
  readonly url: string;
  readonly withCredentials = false;
  readyState = FakeEventSource.CONNECTING;
  onopen: ((event: Event) => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;
  constructor(url: string | URL) {
    this.url = String(url);
  }
  close() {
    this.readyState = FakeEventSource.CLOSED;
  }
  addEventListener() {}
  removeEventListener() {}
  dispatchEvent() {
    return true;
  }
}
