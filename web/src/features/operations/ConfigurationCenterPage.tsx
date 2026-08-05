import { useQuery, useQueryClient } from "@tanstack/react-query";

import {
  APIError,
  newIdempotencyKey,
  postAPI,
  type APIModel,
} from "../../api/client";
import { d1CollectionQuery, sessionQuery } from "../../api/queries";
import { Page } from "../../app/OperationalShared";
import { StatePanel } from "../../components/StatePanel";
import { EvidenceDetails } from "../shared/EvidenceDetails";
import { HighRiskAuthorizationForm } from "../shared/HighRiskAuthorizationForm";
import { StatusBadge } from "../shared/StatusBadge";
import { stringAttribute } from "../strategies/strategyModel";
import styles from "../shared/ConsoleSurface.module.css";

export function ConfigurationCenterPage() {
  const session = useQuery(sessionQuery);
  const query = useQuery(d1CollectionQuery("configuration-revisions"));
  const queryClient = useQueryClient();
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
        detail="Configuration revisions are unavailable."
      />
    );
  const isOwner = true;
  return (
    <Page
      title="Configuration Center"
      eyebrow="Immutable revision review and activation"
      description="Compare recorded configuration identity and activate only an exact reviewed revision with Owner reauthentication."
    >
      <p className={styles.notice} role="note">
        Activation remains fail-closed. It cannot add production order routes or
        override any prohibited V1 capability.
      </p>
      {query.data.items.length === 0 ? (
        <StatePanel state="empty" />
      ) : (
        <div className={styles.cardGrid}>
          {query.data.items.map((configuration) => (
            <article className={styles.card} key={configuration.id}>
              <div className={styles.cardHeader}>
                <h2>{configuration.id}</h2>
                <StatusBadge value={configuration.state} />
              </div>
              <dl className={styles.facts}>
                <div>
                  <dt>Version</dt>
                  <dd>{configuration.revision}</dd>
                </div>
                <div>
                  <dt>Hash</dt>
                  <dd className={styles.inlineCode}>
                    {stringAttribute(
                      configuration.attributes,
                      "configuration_hash",
                    )}
                  </dd>
                </div>
                <div>
                  <dt>Recorded by</dt>
                  <dd>{stringAttribute(configuration.attributes, "actor")}</dd>
                </div>
              </dl>
              {isOwner && configuration.state !== "active" && (
                <HighRiskAuthorizationForm
                  title="Activate this exact revision"
                  purpose="configuration_activation"
                  expectedRevision={configuration.revision}
                  confirmLabel="Activate configuration"
                  onAuthorized={async (authorization_token, reason) => {
                    await postAPI<"CommandAccepted">(
                      "/api/v1/configuration-revisions",
                      {
                        authorization_token,
                        configuration_id: configuration.id,
                        expected_revision: configuration.revision,
                        reason,
                      } satisfies APIModel<"ConfigurationActivationRequest">,
                      newIdempotencyKey("configuration-activation"),
                    );
                    await queryClient.invalidateQueries({
                      queryKey: ["d1", "configuration-revisions"],
                    });
                  }}
                />
              )}
              <EvidenceDetails
                summary="Immutable server-redacted configuration provenance."
                value={configuration}
              />
            </article>
          ))}
        </div>
      )}
    </Page>
  );
}
