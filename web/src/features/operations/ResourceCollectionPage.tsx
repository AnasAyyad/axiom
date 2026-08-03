import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router-dom";

import { APIError } from "../../api/client";
import { d1CollectionQuery } from "../../api/queries";
import { Page } from "../../app/OperationalShared";
import { StatePanel } from "../../components/StatePanel";
import { EvidenceDetails } from "../shared/EvidenceDetails";
import { StatusBadge } from "../shared/StatusBadge";
import styles from "../shared/D2.module.css";

export type D1Collection = Parameters<typeof d1CollectionQuery>[0];

interface ResourceCollectionPageProps {
  readonly resource: D1Collection;
  readonly title: string;
  readonly eyebrow: string;
  readonly description: string;
  readonly emptyDetail?: string;
}

export function ResourceCollectionPage({
  resource,
  title,
  eyebrow,
  description,
  emptyDetail,
}: ResourceCollectionPageProps) {
  const [state, setState] = useState("");
  const query = useQuery(d1CollectionQuery(resource, { state }));
  if (query.isLoading) return <StatePanel state="loading" />;
  if (query.error instanceof APIError && query.error.status === 403)
    return <StatePanel state="forbidden" />;
  if (query.isError || !query.data)
    return (
      <StatePanel
        state="error"
        detail={`${title} could not be loaded from authoritative state.`}
      />
    );
  return (
    <Page title={title} eyebrow={eyebrow} description={description}>
      <form
        className={styles.controlCard}
        onSubmit={(event) => event.preventDefault()}
      >
        <label className={styles.field}>
          State filter
          <input
            type="search"
            value={state}
            onChange={(event) => setState(event.target.value)}
          />
        </label>
      </form>
      {query.isFetching && (
        <StatePanel
          state="stale"
          detail="Showing the prior snapshot while authoritative state refreshes."
        />
      )}
      {query.data.items.length === 0 ? (
        <StatePanel state="empty" detail={emptyDetail} />
      ) : (
        <div className={styles.cardGrid}>
          {query.data.items.map((item) => (
            <article className={styles.card} key={item.id}>
              <div className={styles.cardHeader}>
                <div>
                  <h2>{humanLabel(item.attributes, item.id)}</h2>
                  <p>{item.kind.replaceAll("_", " ")}</p>
                </div>
                <StatusBadge value={item.state} />
              </div>
              {item.reason && (
                <div
                  className={styles.reasonCard}
                  data-severity={item.reason.severity}
                >
                  <h3>{item.reason.summary}</h3>
                  <p>{item.reason.explanation}</p>
                  <strong>
                    Recommended action: {item.reason.suggested_action}
                  </strong>
                </div>
              )}
              <dl className={styles.facts}>
                <div>
                  <dt>Revision</dt>
                  <dd>{item.revision}</dd>
                </div>
                <div>
                  <dt>Observed</dt>
                  <dd>{item.occurred_at ?? "Not recorded"}</dd>
                </div>
                <div>
                  <dt>Correlation</dt>
                  <dd>{item.correlation_id}</dd>
                </div>
              </dl>
              <div className={styles.actions}>
                <Link
                  className={styles.linkButton}
                  to={`/activity/decisions-orders?correlation_id=${encodeURIComponent(item.correlation_id)}`}
                >
                  Correlated activity
                </Link>
              </div>
              <EvidenceDetails
                summary="Technical fields are server allowlisted and redacted."
                value={{
                  id: item.id,
                  attributes: item.attributes,
                  links: item.links,
                }}
              />
            </article>
          ))}
        </div>
      )}
    </Page>
  );
}

function humanLabel(
  attributes: Readonly<Record<string, unknown>>,
  fallback: string,
) {
  for (const key of ["name", "symbol", "email", "instrument", "report_type"]) {
    if (typeof attributes[key] === "string") return attributes[key];
  }
  return fallback;
}
