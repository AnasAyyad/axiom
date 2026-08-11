import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo } from "react";
import { useNavigate, useParams } from "react-router";

import {
  apiErrorDetail,
  newIdempotencyKey,
  postAPI,
  type APIModel,
} from "../../api/client";
import {
  evaluationCampaignEventsQuery,
  evaluationCampaignQuery,
  evaluationCampaignReportQuery,
  evaluationCampaignsQuery,
} from "../../api/queries";
import { Page } from "../../app/OperationalShared";
import { StatePanel } from "../../components/StatePanel";
import shared from "../shared/ConsoleSurface.module.css";
import { Coverage, FeedHealth, Matrix } from "./StrategyEvaluationData";
import {
  CampaignHistory,
  CampaignOverview,
  ImportProgress,
  StageTimeline,
} from "./StrategyEvaluationOverview";
import { ReportPanel } from "./StrategyEvaluationReport";
import { EventTimeline, Shadow } from "./StrategyEvaluationShadow";
import styles from "./StrategyEvaluationPage.module.css";
import type { Campaign } from "./StrategyEvaluationTypes";
import { summarizeMembers } from "./StrategyEvaluationView";

const terminalStates = new Set<Campaign["state"]>([
  "COMPLETED",
  "PARTIAL",
  "BLOCKED",
  "CANCELED",
]);

export function StrategyEvaluationPage() {
  const { id: routeID = "" } = useParams();
  const navigate = useNavigate();
  const client = useQueryClient();
  const campaigns = useQuery(evaluationCampaignsQuery);
  const selectedID = routeID || campaigns.data?.items[0]?.id || "";
  const campaign = useQuery(evaluationCampaignQuery(selectedID));
  const events = useQuery(evaluationCampaignEventsQuery(selectedID));
  const report = useQuery(evaluationCampaignReportQuery(selectedID));
  const active = campaigns.data?.items.find(
    (item) => !terminalStates.has(item.state),
  );
  const start = useMutation({
    mutationFn: () =>
      postAPI<"EvaluationCampaign">(
        "/api/v1/evaluation-campaigns",
        {
          preset: "balanced_full_v1",
        } satisfies APIModel<"EvaluationCampaignCreateRequest">,
        newIdempotencyKey("evaluation-start"),
      ),
    onSuccess: async (created) => {
      await client.invalidateQueries({ queryKey: ["evaluation-campaigns"] });
      navigate(`/strategy-evaluation/${encodeURIComponent(created.id)}`);
    },
  });
  const cancel = useMutation({
    mutationFn: (value: Campaign) =>
      postAPI<"CommandAccepted">(
        `/api/v1/evaluation-campaigns/${encodeURIComponent(value.id)}/cancel`,
        {
          expected_revision: value.revision,
          reason:
            "Owner emergency cancellation from Strategy Evaluation console",
        } satisfies APIModel<"RevisionCommandRequest">,
        newIdempotencyKey("evaluation-cancel"),
      ),
    onSuccess: async () => {
      await Promise.all([
        client.invalidateQueries({ queryKey: ["evaluation-campaigns"] }),
        client.invalidateQueries({
          queryKey: ["evaluation-campaign", selectedID],
        }),
      ]);
    },
  });

  const current = campaign.data;
  const matrixSummary = useMemo(
    () => summarizeMembers(current?.matrix ?? []),
    [current?.matrix],
  );

  return (
    <Page
      title="Strategy Evaluation"
      eyebrow="One unattended, evidence-preserving workflow"
      description="Import and audit public data, qualify a fresh two-exchange recording, execute the full offline matrix, run eligible strategies together in simulated shadow, and preserve a final or partial report."
    >
      <section className={styles.hero} aria-labelledby="evaluation-action">
        <div>
          <p className={styles.safety}>Spot only · simulated orders only</p>
          <h2 id="evaluation-action">Full balanced evaluation</h2>
          <p>
            The server owns every dataset, configuration, seed, model,
            portfolio, and evidence identifier. Starting sends only the reviewed
            preset name.
          </p>
        </div>
        <div className={shared.actions}>
          <button
            className={shared.button}
            type="button"
            disabled={
              campaigns.isLoading ||
              campaigns.isError ||
              !!active ||
              start.isPending
            }
            onClick={() => start.mutate()}
          >
            {start.isPending ? "Starting…" : "Start Full Evaluation"}
          </button>
          {current && !terminalStates.has(current.state) && (
            <button
              className={shared.danger}
              type="button"
              disabled={cancel.isPending}
              onClick={() => cancel.mutate(current)}
            >
              {cancel.isPending ? "Canceling safely…" : "Emergency Cancel"}
            </button>
          )}
        </div>
      </section>

      {campaigns.isLoading && <StatePanel state="loading" />}
      {campaigns.isError && (
        <StatePanel
          state="error"
          detail={apiErrorDetail(
            campaigns.error,
            "Campaign history is temporarily unavailable; start is disabled until single-campaign state can be verified.",
          )}
        />
      )}
      {start.isError && (
        <StatePanel
          state="error"
          detail={apiErrorDetail(start.error, "The campaign was not started.")}
        />
      )}
      {cancel.isError && (
        <StatePanel
          state="error"
          detail={apiErrorDetail(
            cancel.error,
            "Emergency cancellation was not accepted; the campaign remains unchanged.",
          )}
        />
      )}

      {campaigns.data && campaigns.data.items.length > 0 && (
        <CampaignHistory
          campaigns={campaigns.data.items}
          selectedID={selectedID}
        />
      )}
      {campaigns.data?.items.length === 0 && !start.isPending && (
        <StatePanel
          state="empty"
          detail="No evaluation campaign has been started. The button above creates the complete server-owned plan."
        />
      )}

      {selectedID !== "" && campaign.isLoading && (
        <StatePanel state="loading" />
      )}
      {campaign.isError && (
        <StatePanel
          state="error"
          detail={apiErrorDetail(
            campaign.error,
            "This campaign's detailed progress is temporarily unavailable.",
          )}
        />
      )}
      {current && (
        <>
          <CampaignOverview campaign={current} />
          <StageTimeline campaign={current} />
          <ImportProgress campaign={current} />
          <Coverage campaign={current} />
          <FeedHealth campaign={current} />
          <Matrix campaign={current} summary={matrixSummary} />
          <Shadow campaign={current} />
        </>
      )}

      {selectedID !== "" && (
        <section className={shared.section} aria-labelledby="evaluation-events">
          <h2 id="evaluation-events">Durable timeline</h2>
          {events.isLoading && <StatePanel state="loading" />}
          {events.isError && (
            <StatePanel
              state="partial"
              detail={apiErrorDetail(
                events.error,
                "Campaign progress remains visible, but timeline events could not be refreshed.",
              )}
            />
          )}
          {events.data && <EventTimeline items={events.data.items} />}
        </section>
      )}

      {selectedID !== "" && (
        <ReportPanel
          report={report.data}
          error={report.error}
          loading={report.isLoading}
        />
      )}
    </Page>
  );
}
