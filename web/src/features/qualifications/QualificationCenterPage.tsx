import { useQuery } from "@tanstack/react-query";

import { APIError } from "../../api/client";
import { d1CollectionQuery, sessionQuery } from "../../api/queries";
import { Page } from "../../app/OperationalShared";
import { StatePanel } from "../../components/StatePanel";
import styles from "../shared/ConsoleSurface.module.css";
import { QualificationCard } from "./QualificationCard";

export function QualificationCenterPage() {
  const session = useQuery(sessionQuery);
  const query = useQuery(d1CollectionQuery("qualifications"));
  if (session.isLoading || query.isLoading)
    return <StatePanel state="loading" />;
  if (
    (session.error instanceof APIError && session.error.status === 403) ||
    (query.error instanceof APIError && query.error.status === 403)
  )
    return <StatePanel state="forbidden" />;
  if (session.isError || query.isError || !session.data || !query.data)
    return (
      <StatePanel
        state="error"
        detail="Approved qualification state is unavailable."
      />
    );
  const canStart = true;
  const canAbort = true;
  return (
    <Page
      title="Qualification Center"
      eyebrow="Approved tests and operational drills"
      description="Start only registered qualifications, inspect fail-closed preflight and progress, abort safely, and preserve exact-source evidence and terminal verdicts."
    >
      <p className={styles.notice} role="note">
        C6 remains its own exact 72-hour sandbox qualification. D5 remains a
        separate seven-day readiness soak, and B2 keeps its market-data verdict.
        A smoke pass cannot become a formal pass.
      </p>
      {query.data.items.length === 0 ? (
        <StatePanel
          state="empty"
          detail="No active qualification definition is registered."
        />
      ) : (
        <div className={styles.cardGrid}>
          {query.data.items.map((qualification) => (
            <QualificationCard
              key={qualification.id}
              qualification={qualification}
              canStart={canStart}
              canAbort={canAbort}
            />
          ))}
        </div>
      )}
    </Page>
  );
}
