import type { APIModel } from "../../api/client";
import { HelpDetails } from "../../components/HelpDetails";
import styles from "../shared/ConsoleSurface.module.css";
import {
  exchangeLabel,
  readableMachineValue,
  type EvidenceTab,
} from "./runEvidencePresentation";

interface RunPortfolioRiskTabsProps {
  readonly activeTab: EvidenceTab;
  readonly portfolio: APIModel<"RunPortfolioProjection"> | undefined;
  readonly risk: APIModel<"RunRiskProjection"> | undefined;
}

export function RunPortfolioRiskTabs({
  activeTab,
  portfolio,
  risk,
}: RunPortfolioRiskTabsProps) {
  return (
    <>
      {activeTab === "portfolio" && (
        <section
          aria-labelledby="run-tab-control-portfolio"
          className={styles.card}
          id="run-tab-portfolio"
          role="tabpanel"
        >
          <h2>Portfolio &amp; P&amp;L</h2>
          {portfolio?.state === "recorded" ? (
            <>
              <p>
                {portfolio.summary ??
                  `Latest reducer-owned portfolio snapshot recorded at event ${portfolio.ordinal}.`}
              </p>
              <dl className={styles.facts}>
                {portfolio.realized_pnl !== undefined && (
                  <div>
                    <dt>Realized P&amp;L</dt>
                    <dd>{portfolio.realized_pnl}</dd>
                  </div>
                )}
                {portfolio.unrealized_pnl !== undefined && (
                  <div>
                    <dt>Unrealized P&amp;L</dt>
                    <dd>{portfolio.unrealized_pnl}</dd>
                  </div>
                )}
                {portfolio.total_pnl !== undefined && (
                  <div>
                    <dt>Total P&amp;L</dt>
                    <dd>{portfolio.total_pnl}</dd>
                  </div>
                )}
                {portfolio.account_drawdown !== undefined && (
                  <div>
                    <dt>Account drawdown</dt>
                    <dd>{portfolio.account_drawdown}</dd>
                  </div>
                )}
              </dl>
              {portfolio.positions && portfolio.positions.length > 0 && (
                <div className={styles.tableFrame}>
                  <table>
                    <caption>Run-owned sandbox positions</caption>
                    <thead>
                      <tr>
                        <th scope="col">Exchange</th>
                        <th scope="col">Instrument</th>
                        <th scope="col">Quantity</th>
                        <th scope="col">Average cost</th>
                        <th scope="col">Realized P&amp;L</th>
                        <th scope="col">Valuation</th>
                      </tr>
                    </thead>
                    <tbody>
                      {portfolio.positions.map((position) => (
                        <tr key={`${position.exchange}-${position.instrument}`}>
                          <td>{exchangeLabel(position.exchange)}</td>
                          <td>{position.instrument}</td>
                          <td>{position.quantity}</td>
                          <td>{position.weighted_average_cost}</td>
                          <td>{position.realized_pnl}</td>
                          <td>
                            {readableMachineValue(position.valuation_state)}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
              <HelpDetails title="How to interpret these values">
                <p>
                  These values come only from this run’s immutable sandbox
                  journal and central-risk valuation. They are integration
                  evidence, not proof of forward-looking or real-money
                  profitability.
                </p>
              </HelpDetails>
            </>
          ) : (
            <p>
              {portfolio?.waiting_reason ?? "Loading portfolio projection…"}
            </p>
          )}
        </section>
      )}
      {activeTab === "risk" && (
        <section
          aria-labelledby="run-tab-control-risk"
          className={styles.card}
          id="run-tab-risk"
          role="tabpanel"
        >
          <h2>Risk</h2>
          <p>{risk?.summary ?? "Loading risk evidence…"}</p>
          {risk?.status && (
            <p className={styles.notice}>
              Current run risk status: {readableMachineValue(risk.status)}
            </p>
          )}
          {risk?.blockers && risk.blockers.length > 0 && (
            <ul>
              {risk.blockers.map((blocker) => (
                <li key={blocker}>{blocker}</li>
              ))}
            </ul>
          )}
          {risk?.observations && risk.observations.length > 0 && (
            <div className={styles.tableFrame}>
              <table>
                <caption>
                  Latest immutable central-risk input per exchange
                </caption>
                <thead>
                  <tr>
                    <th scope="col">Exchange</th>
                    <th scope="col">Instrument</th>
                    <th scope="col">Drawdown</th>
                    <th scope="col">Strategy loss</th>
                    <th scope="col">Exposure</th>
                    <th scope="col">Open orders</th>
                    <th scope="col">Quality</th>
                  </tr>
                </thead>
                <tbody>
                  {risk.observations.map((observation) => (
                    <tr
                      key={`${observation.exchange}-${observation.instrument}`}
                    >
                      <td>{exchangeLabel(observation.exchange)}</td>
                      <td>{observation.instrument}</td>
                      <td>{observation.account_drawdown}</td>
                      <td>{observation.strategy_loss}</td>
                      <td>{observation.exchange_exposure}</td>
                      <td>{observation.open_orders}</td>
                      <td>{observation.quality_score}/100</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          <HelpDetails title="What central risk proves">
            <p>
              This view shows the exact run-scoped facts presented to central
              risk. A normal observation does not guarantee that the next
              candidate will be approved; every candidate is evaluated again.
            </p>
          </HelpDetails>
        </section>
      )}
    </>
  );
}
