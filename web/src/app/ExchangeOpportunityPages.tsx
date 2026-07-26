import { useQuery } from "@tanstack/react-query";
import { Fragment, useState } from "react";

import { getAPI, type APIModel } from "../api/client";
import { exchangesQuery, opportunitiesQuery } from "../api/queries";
import { StatePanel } from "../components/StatePanel";
import { QualityBadge } from "./MultiExchangeShared";
import { Page } from "./OperationalShared";
import styles from "./Page.module.css";

export function ExchangesPage() {
  const query = useQuery(exchangesQuery);
  if (query.isLoading) return <StatePanel state="loading" />;
  if (query.isError) return <StatePanel state="degraded" />;
  const data = query.data!;
  return (
    <Page
      title="Exchange Operations"
      eyebrow="Versioned multi-exchange projection"
      description="Production-public connectivity and provenance. Every adapter remains public-only."
    >
      {data.items.length === 0 ? (
        <StatePanel state="empty" />
      ) : (
        <div className={styles.cardGrid}>
          {data.items.map((exchange) => (
            <article className={styles.card} key={exchange.id}>
              <div className={styles.cardHeading}>
                <div>
                  <span className={styles.eyebrow}>{exchange.environment}</span>
                  <h2>{exchange.name}</h2>
                </div>
                <QualityBadge quality={exchange.quality} />
              </div>
              <dl className={styles.facts}>
                <div>
                  <dt>WebSocket</dt>
                  <dd>{exchange.websocket_state}</dd>
                </div>
                <div>
                  <dt>Order book</dt>
                  <dd>{exchange.book_state}</dd>
                </div>
                <div>
                  <dt>Recorder</dt>
                  <dd>{exchange.recorder_state}</dd>
                </div>
                <div>
                  <dt>Instruments</dt>
                  <dd>{exchange.instruments}</dd>
                </div>
              </dl>
              <ul className={styles.tagList}>
                {exchange.capabilities.map((capability) => (
                  <li key={capability}>{capability}</li>
                ))}
              </ul>
            </article>
          ))}
        </div>
      )}
    </Page>
  );
}

export function OpportunityScanner() {
  const [kind, setKind] = useState("");
  const [selected, setSelected] = useState("");
  const query = useQuery(opportunitiesQuery(kind));
  const detail = useQuery({
    queryKey: ["opportunity", selected],
    queryFn: () =>
      getAPI<"OpportunityDetail">(`/api/v1/opportunities/${selected}`),
    enabled: selected !== "",
  });
  if (query.isLoading) return <StatePanel state="loading" />;
  if (query.isError) return <StatePanel state="degraded" />;
  const items = query.data!.items;
  return (
    <Page
      title="Opportunity Scanner"
      eyebrow="Triangular and cross-exchange evidence"
      description="Simulation-only candidates with worst-case economics, freshness, inventory impact, and recovery evidence."
    >
      <div className={styles.tabs} role="tablist" aria-label="Opportunity kind">
        {(
          [
            ["", "All"],
            ["triangular", "Triangular"],
            ["cross_exchange", "Cross-exchange"],
          ] as const
        ).map(([value, label]) => (
          <button
            key={value}
            type="button"
            role="tab"
            aria-selected={kind === value}
            onClick={() => {
              setKind(value);
              setSelected("");
            }}
          >
            {label}
          </button>
        ))}
      </div>
      {items.length === 0 ? (
        <StatePanel state="empty" />
      ) : (
        <div className={styles.operationsTable} tabIndex={0}>
          <table>
            <caption>Qualified simulation opportunities</caption>
            <thead>
              <tr>
                <th scope="col">Opportunity</th>
                <th scope="col">Route</th>
                <th scope="col">Expected</th>
                <th scope="col">Worst case</th>
                <th scope="col">Quality</th>
                <th scope="col">Status</th>
              </tr>
            </thead>
            <tbody>
              {items.map((item) => {
                const open = selected === item.id;
                return (
                  <Fragment key={item.id}>
                    <tr>
                      <td>
                        <button
                          type="button"
                          className={styles.rowButton}
                          aria-expanded={open}
                          onClick={() => setSelected(open ? "" : item.id)}
                        >
                          <strong>{item.kind.replace("_", " ")}</strong>
                          <small>{item.id}</small>
                        </button>
                      </td>
                      <td>{item.label}</td>
                      <td>{item.expected_profit}</td>
                      <td>{item.worst_case_profit}</td>
                      <td>
                        <QualityBadge quality={item.quality} />
                      </td>
                      <td>{item.status}</td>
                    </tr>
                    {open && (
                      <tr>
                        <td colSpan={6} className={styles.expandedCell}>
                          {detail.isLoading && <StatePanel state="loading" />}
                          {detail.isError && <StatePanel state="error" />}
                          {detail.data && (
                            <OpportunityEvidence detail={detail.data} />
                          )}
                        </td>
                      </tr>
                    )}
                  </Fragment>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </Page>
  );
}

function OpportunityEvidence({
  detail,
}: {
  readonly detail: APIModel<"OpportunityDetail">;
}) {
  return (
    <div className={styles.evidenceGrid}>
      <section>
        <h2>Leg evidence</h2>
        <ol className={styles.timeline}>
          {detail.legs.map((leg) => (
            <li key={`${leg.exchange}-${leg.index}`}>
              <strong>
                {leg.exchange} · {leg.side} {leg.instrument}
              </strong>
              <span>
                {leg.input_quantity} → {leg.net_output} · {leg.state}
              </span>
            </li>
          ))}
        </ol>
      </section>
      <section>
        <h2>Recovery</h2>
        <dl className={styles.facts}>
          <div>
            <dt>Disposition</dt>
            <dd>{detail.recovery.disposition}</dd>
          </div>
          <div>
            <dt>Attempted / succeeded</dt>
            <dd>
              {String(detail.recovery.attempted)} /{" "}
              {String(detail.recovery.succeeded)}
            </dd>
          </div>
          <div>
            <dt>Recovery loss</dt>
            <dd>{detail.recovery.recovery_loss}</dd>
          </div>
        </dl>
      </section>
      <section>
        <h2>Cost attribution</h2>
        <dl className={styles.facts}>
          {Object.entries(detail.cost_attribution).map(([label, value]) => (
            <div key={label}>
              <dt>{label.replaceAll("_", " ")}</dt>
              <dd>{value}</dd>
            </div>
          ))}
        </dl>
      </section>
      <section>
        <h2>Evidence timeline</h2>
        <ol className={styles.timeline}>
          {detail.timeline.map((event) => (
            <li key={`${event.index}-${event.event_type}`}>
              <strong>{event.label}</strong>
              <span>{event.occurred_at}</span>
            </li>
          ))}
        </ol>
      </section>
    </div>
  );
}
