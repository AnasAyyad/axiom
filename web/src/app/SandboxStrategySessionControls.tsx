import type { APIModel } from "../api/client";
import styles from "./SandboxOperationsPage.module.css";

type StrategySession = APIModel<"SandboxStrategySession">;
type StrategyID =
  APIModel<"SandboxStrategySessionCreateRequest">["strategy_id"];
type Exchange = "binance" | "bybit";
type Instrument = "BTCUSDT" | "ETHUSDT";

interface PreparationProps {
  readonly strategy: StrategyID;
  readonly exchange: Exchange;
  readonly instrument: Instrument;
  readonly pending: boolean;
  readonly onStrategy: (value: StrategyID) => void;
  readonly onExchange: (value: Exchange) => void;
  readonly onInstrument: (value: Instrument) => void;
  readonly onSubmit: () => void;
}

export function SandboxSessionPreparationForm(props: PreparationProps) {
  return (
    <form
      className={styles.sessionForm}
      onSubmit={(event) => {
        event.preventDefault();
        props.onSubmit();
      }}
    >
      <label>
        Strategy
        <select
          value={props.strategy}
          onChange={(event) =>
            props.onStrategy(event.target.value as StrategyID)
          }
        >
          <option value="trend-following">Trend Following</option>
          <option value="mean-reversion">Mean Reversion</option>
          <option value="triangular-arbitrage">Triangular Arbitrage</option>
          <option value="cross-exchange-arbitrage">
            Cross-Exchange Arbitrage
          </option>
        </select>
      </label>
      <label>
        Exchange
        <select
          value={props.exchange}
          disabled={props.strategy === "cross-exchange-arbitrage"}
          onChange={(event) => props.onExchange(event.target.value as Exchange)}
        >
          <option value="binance">Binance Spot Testnet</option>
          <option value="bybit">Bybit Demo</option>
        </select>
      </label>
      <label>
        Instrument
        <select
          value={props.instrument}
          onChange={(event) =>
            props.onInstrument(event.target.value as Instrument)
          }
        >
          <option value="BTCUSDT">BTC/USDT</option>
          <option value="ETHUSDT">ETH/USDT</option>
        </select>
      </label>
      <button type="submit" disabled={props.pending}>
        {props.pending ? "Preparing…" : "Prepare strategy session"}
      </button>
    </form>
  );
}

interface StartProps {
  readonly password: string;
  readonly totp: string;
  readonly reason: string;
  readonly confirmed: boolean;
  readonly pending: boolean;
  readonly onPassword: (value: string) => void;
  readonly onTOTP: (value: string) => void;
  readonly onReason: (value: string) => void;
  readonly onConfirmed: (value: boolean) => void;
  readonly onSubmit: () => void;
  readonly onCancel: () => void;
}

export function SandboxSessionStartForm(props: StartProps) {
  return (
    <form
      className={styles.sessionForm}
      onSubmit={(event) => {
        event.preventDefault();
        props.onSubmit();
      }}
    >
      <label>
        Owner password
        <input
          type="password"
          autoComplete="current-password"
          value={props.password}
          onChange={(event) => props.onPassword(event.target.value)}
        />
      </label>
      <label>
        One-time code
        <input
          inputMode="numeric"
          autoComplete="one-time-code"
          value={props.totp}
          onChange={(event) => props.onTOTP(event.target.value)}
        />
      </label>
      <label>
        Audit reason
        <input
          minLength={8}
          maxLength={500}
          value={props.reason}
          onChange={(event) => props.onReason(event.target.value)}
        />
      </label>
      <label>
        <input
          type="checkbox"
          checked={props.confirmed}
          onChange={(event) => props.onConfirmed(event.target.checked)}
        />
        I confirmed every selected session account is armed. Starting evaluates
        the strategy; it does not create an order by itself.
      </label>
      <button type="submit" disabled={props.pending}>
        {props.pending ? "Starting…" : "Reauthenticate and start"}
      </button>
      <button type="button" onClick={props.onCancel}>
        Cancel
      </button>
    </form>
  );
}

interface TableProps {
  readonly sessions: readonly StrategySession[];
  readonly stopPending: boolean;
  readonly onStart: (session: StrategySession) => void;
  readonly onStop: (session: StrategySession) => void;
}

export function SandboxStrategySessionTable({
  sessions,
  stopPending,
  onStart,
  onStop,
}: TableProps) {
  if (sessions.length === 0)
    return <p>No strategy sessions have been prepared.</p>;
  return (
    <div className={styles.table}>
      <table>
        <caption>
          Current session state and the exact reason automatic activity is
          waiting or blocked.
        </caption>
        <thead>
          <tr>
            <th scope="col">Strategy</th>
            <th scope="col">State</th>
            <th scope="col">Why</th>
            <th scope="col">Action</th>
          </tr>
        </thead>
        <tbody>
          {sessions.map((session) => (
            <tr key={session.id}>
              <td>{session.display_name}</td>
              <td>{presentationState(session.state)}</td>
              <td>{session.waiting_reason ?? "Status is not recorded."}</td>
              <td>
                {session.state === "prepared" ? (
                  <button type="button" onClick={() => onStart(session)}>
                    Start after reauthentication
                  </button>
                ) : null}
                {session.state === "prepared" ||
                session.state === "running" ||
                session.state === "blocked" ? (
                  <button
                    type="button"
                    disabled={stopPending}
                    onClick={() => onStop(session)}
                  >
                    {stopPending ? "Stopping…" : "Stop session"}
                  </button>
                ) : null}
                {session.state === "stopped" ? "No action available" : null}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function presentationState(state: StrategySession["state"]) {
  switch (state) {
    case "prepared":
      return "Prepared — waiting";
    case "running":
      return "Running";
    case "blocked":
      return "Blocked";
    case "stopped":
      return "Stopped";
  }
}
