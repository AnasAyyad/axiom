import { useQuery } from "@tanstack/react-query";
import { useState } from "react";

import { inventoryQuery, strategiesQuery } from "../api/queries";
import { StatePanel } from "../components/StatePanel";
import { QualityBadge } from "./MultiExchangeShared";
import { Page } from "./OperationalShared";
import styles from "./Page.module.css";

export function StrategyCenter() {
  const query = useQuery(strategiesQuery);
  if (query.isLoading) return <StatePanel state="loading" />;
  if (query.isError) return <StatePanel state="degraded" />;
  return (
    <Page
      title="Strategy Center"
      eyebrow="Version and evidence registry"
      description="Compare supported research modes, maturity, champion/challenger role, and evidence posture."
    >
      {query.data!.items.length === 0 ? (
        <StatePanel state="empty" />
      ) : (
        <div className={styles.cardGrid}>
          {query.data!.items.map((strategy) => (
            <article className={styles.card} key={strategy.id}>
              <div className={styles.cardHeading}>
                <div>
                  <span className={styles.eyebrow}>{strategy.family}</span>
                  <h2>{strategy.name}</h2>
                </div>
                <span className={styles.quality}>{strategy.evidence_role}</span>
              </div>
              <dl className={styles.facts}>
                <div>
                  <dt>Version</dt>
                  <dd>{strategy.version}</dd>
                </div>
                <div>
                  <dt>Maturity</dt>
                  <dd>{strategy.maturity}</dd>
                </div>
                <div>
                  <dt>Confidence</dt>
                  <dd>{strategy.confidence}</dd>
                </div>
                <div>
                  <dt>Viability</dt>
                  <dd>{strategy.viability}</dd>
                </div>
              </dl>
              <ul className={styles.tagList}>
                {strategy.supported_modes.map((mode) => (
                  <li key={mode}>{mode}</li>
                ))}
              </ul>
              <p className={styles.disclaimer}>{strategy.disclaimer}</p>
            </article>
          ))}
        </div>
      )}
    </Page>
  );
}

const emptyFilters = {
  exchange: "",
  asset: "",
  strategy: "",
  portfolio: "",
};

export function InventoryPage() {
  const [filters, setFilters] = useState(emptyFilters);
  const query = useQuery(inventoryQuery(filters));
  if (query.isLoading) return <StatePanel state="loading" />;
  if (query.isError) return <StatePanel state="degraded" />;
  const data = query.data!;
  return (
    <Page
      title="Virtual Inventory"
      eyebrow="Explicit isolation dimensions"
      description={data.isolation_notice}
    >
      <form
        className={styles.filterBar}
        onSubmit={(event) => event.preventDefault()}
      >
        {Object.keys(filters).map((key) => (
          <label key={key}>
            {key}
            <input
              value={filters[key as keyof typeof filters]}
              onChange={(event) =>
                setFilters({ ...filters, [key]: event.target.value })
              }
              placeholder={`Filter ${key}`}
            />
          </label>
        ))}
        <button type="button" onClick={() => setFilters(emptyFilters)}>
          Clear
        </button>
      </form>
      <div className={styles.notice} role="note">
        Combined balance: <strong>DISABLED</strong>. Values are never netted
        across exchanges, strategies, experiments, or portfolios.
      </div>
      {data.items.length === 0 ? (
        <StatePanel state="empty" />
      ) : (
        <div className={styles.operationsTable} tabIndex={0}>
          <table>
            <caption>Isolated virtual inventory positions</caption>
            <thead>
              <tr>
                <th scope="col">Exchange / asset</th>
                <th scope="col">Strategy</th>
                <th scope="col">Portfolio</th>
                <th scope="col">Before → after</th>
                <th scope="col">State</th>
                <th scope="col">Quality</th>
              </tr>
            </thead>
            <tbody>
              {data.items.map((item) => (
                <tr key={item.id}>
                  <td>
                    {item.exchange} / {item.asset}
                  </td>
                  <td>{item.strategy_version}</td>
                  <td>{item.portfolio_id}</td>
                  <td>
                    {item.before} → {item.after}
                  </td>
                  <td>{item.status}</td>
                  <td>
                    <QualityBadge quality={item.quality} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Page>
  );
}
