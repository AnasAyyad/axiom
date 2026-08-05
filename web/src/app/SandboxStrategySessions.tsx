import type { APIModel } from "../api/client";
import styles from "./SandboxOperationsPage.module.css";

type StrategySession = APIModel<"SandboxStrategySession">;

interface Props {
  readonly sessions: StrategySession[];
}

export function SandboxStrategySessions({ sessions }: Props) {
  return (
    <section
      className={styles.card}
      aria-labelledby="strategy-sessions-heading"
    >
      <header>
        <div>
          <span>Automatic Testnet and Demo sessions</span>
          <h2 id="strategy-sessions-heading">Strategy sessions</h2>
        </div>
      </header>
      <p className={styles.disclaimer}>
        These are separate from the advanced connection check. A session never
        enables real-money trading, and it cannot submit an automatic order
        until its strategy worker is installed.
      </p>
      {sessions.length === 0 ? (
        <p>No strategy sessions have been prepared.</p>
      ) : (
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
              </tr>
            </thead>
            <tbody>
              {sessions.map((session) => (
                <tr key={session.id}>
                  <td>{session.display_name}</td>
                  <td>{presentationState(session.state)}</td>
                  <td>{session.waiting_reason ?? "Status is not recorded."}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
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
