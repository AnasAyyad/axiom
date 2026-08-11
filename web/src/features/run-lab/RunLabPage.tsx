import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link, useNavigate } from "react-router";

import {
  APIError,
  apiErrorDetail,
  newIdempotencyKey,
  postAPI,
  type APIModel,
} from "../../api/client";
import { runCatalogQuery, runsQuery } from "../../api/queries";
import { Page } from "../../app/OperationalShared";
import { StatePanel } from "../../components/StatePanel";
import { WhyNothingPanel } from "../../components/WhyNothingPanel";
import styles from "../shared/ConsoleSurface.module.css";
import { RunChoiceWizard } from "./RunChoiceWizard";

type RunMode = APIModel<"RunChoice">["mode"];

function runFailureDetail(error: unknown) {
  if (!(error instanceof APIError))
    return "The server did not accept this reviewed run. No partial run was created; review the workflow prerequisites and try again.";
  const parts = [
    error.details?.summary,
    error.details?.detail,
    error.details?.impact,
    error.details?.suggestedAction,
    error.details?.blockingPrerequisites?.length
      ? `Required before retrying: ${error.details.blockingPrerequisites.join(", ")}.`
      : undefined,
  ].filter((part): part is string => part !== undefined);
  return parts.length > 0
    ? parts.join(" ")
    : "The server did not accept this reviewed run. No partial run was created; review the workflow prerequisites and try again.";
}

export function RunLabPage() {
  const catalog = useQuery(runCatalogQuery);
  const history = useQuery(runsQuery);
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [purpose, setPurpose] = useState<RunMode | "">("");
  const [strategyID, setStrategyID] = useState("");
  const [selectedChoiceKey, setSelectedChoiceKey] = useState("");
  const createRun = useMutation({
    mutationFn: (choice: APIModel<"RunChoice">) =>
      postAPI<"RunResource">(
        "/api/v1/runs",
        {
          strategy_id: choice.strategy_id,
          strategy_version: choice.strategy_version,
          mode: choice.mode,
          exchanges: choice.exchanges,
          instrument: choice.instrument,
          preset: "latest-qualified-inputs",
        } satisfies APIModel<"RunCreateRequest">,
        newIdempotencyKey("run-create"),
      ),
    onSuccess: async (run) => {
      await queryClient.invalidateQueries({ queryKey: ["runs"] });
      navigate(`/runs/${encodeURIComponent(run.id)}`);
    },
  });
  function choosePurpose(mode: RunMode) {
    setPurpose(mode);
    setStrategyID("");
    setSelectedChoiceKey("");
  }

  function chooseStrategy(id: string) {
    setStrategyID(id);
    setSelectedChoiceKey("");
  }

  return (
    <Page
      title="New Run"
      eyebrow="Choose a reviewed workflow"
      description="Every choice comes from the server. You never need to copy a dataset, configuration, portfolio, or model ID."
    >
      <details className={styles.notice}>
        <summary>About this page</summary>
        <p>
          A run follows the shared strategy, allocation, risk, execution,
          accounting, and reconciliation path. Historical, replay, shadow,
          Testnet, and Demo outcomes are research or integration evidence; they
          do not prove profitability.
        </p>
      </details>
      {catalog.isLoading && <StatePanel state="loading" />}
      {catalog.isError && (
        <StatePanel
          state="error"
          detail={apiErrorDetail(
            catalog.error,
            "Approved run choices are temporarily unavailable.",
          )}
        />
      )}
      {catalog.data?.blocker && (
        <StatePanel
          state="blocked"
          detail={`${catalog.data.blocker.summary} ${catalog.data.blocker.suggested_action}`}
        />
      )}
      {createRun.isError && (
        <StatePanel state="error" detail={runFailureDetail(createRun.error)} />
      )}
      {catalog.data && (
        <RunChoiceWizard
          catalog={catalog.data}
          purpose={purpose}
          strategyID={strategyID}
          selectedChoiceKey={selectedChoiceKey}
          createPending={createRun.isPending}
          onPurpose={choosePurpose}
          onStrategy={chooseStrategy}
          onChoice={setSelectedChoiceKey}
          onCreate={(choice) => createRun.mutate(choice)}
        />
      )}
      <section className={styles.section} aria-labelledby="recent-runs-heading">
        <h2 id="recent-runs-heading">Recent runs</h2>
        <p>
          This is the durable history of recorded-data, public-data shadow, and
          explicitly armed exchange-sandbox work. A run can wait safely when no
          worker, fresh input, reconciliation, or owner arm is ready.
        </p>
        {history.isLoading && <StatePanel state="loading" />}
        {history.isError && (
          <StatePanel
            state="error"
            detail={apiErrorDetail(
              history.error,
              "Run history is temporarily unavailable.",
            )}
          />
        )}
        {history.data?.items.length === 0 && (
          <>
            <StatePanel
              state="empty"
              detail="No durable runs have been created yet."
            />
            <WhyNothingPanel
              reason="Nothing is wrong: a run appears here only after the server has accepted approved immutable inputs. A strategy may also wait safely for fresh data or its next evaluation point."
              nextAction="Choose a reviewed workflow"
              to="/run-lab"
            />
          </>
        )}
        {history.data?.items.map((run) => (
          <article className={styles.card} key={run.id}>
            <div className={styles.cardHeader}>
              <h3>{run.friendly_name}</h3>
              <span>{run.state}</span>
            </div>
            <p>
              {run.strategy_version} · {run.environment.replace("_", " ")}
            </p>
            {run.waiting_reason && (
              <p className={styles.notice}>{run.waiting_reason}</p>
            )}
            <Link
              className={styles.linkButton}
              to={`/runs/${encodeURIComponent(run.id)}`}
            >
              View run evidence
            </Link>
          </article>
        ))}
      </section>
    </Page>
  );
}
