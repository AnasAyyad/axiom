import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { newIdempotencyKey, postAPI, type APIModel } from "../api/client";
import styles from "./SandboxOperationsPage.module.css";
import {
  SandboxSessionPreparationForm,
  SandboxSessionStartForm,
  SandboxStrategySessionTable,
} from "./SandboxStrategySessionControls";

type StrategySession = APIModel<"SandboxStrategySession">;

interface Props {
  readonly sessions: StrategySession[];
}

export function SandboxStrategySessions({ sessions }: Props) {
  const queryClient = useQueryClient();
  const [strategy, setStrategy] =
    useState<APIModel<"SandboxStrategySessionCreateRequest">["strategy_id"]>(
      "trend-following",
    );
  const [exchange, setExchange] = useState<"binance" | "bybit">("binance");
  const [instrument, setInstrument] = useState<"BTCUSDT" | "ETHUSDT">(
    "BTCUSDT",
  );
  const [startTarget, setStartTarget] = useState<StrategySession>();
  const [password, setPassword] = useState("");
  const [totp, setTOTP] = useState("");
  const [reason, setReason] = useState(
    "Owner reviewed this sandbox strategy session",
  );
  const [confirmed, setConfirmed] = useState(false);
  const prepare = useMutation({
    mutationFn: () =>
      postAPI<"CommandAccepted">(
        "/api/v1/sandbox/strategy-sessions",
        {
          strategy_id: strategy,
          exchanges:
            strategy === "cross-exchange-arbitrage"
              ? ["binance", "bybit"]
              : [exchange],
          instrument,
          preset: "latest-qualified-inputs",
          reason: `Prepare ${strategy} automatic sandbox session`,
        } satisfies APIModel<"SandboxStrategySessionCreateRequest">,
        newIdempotencyKey("sandbox-strategy-prepare"),
      ),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["sandbox"] }),
  });
  const start = useMutation({
    mutationFn: async () => {
      if (
        !startTarget ||
        !confirmed ||
        reason.trim().length < 8 ||
        password.length === 0 ||
        !/^\d{6}$/.test(totp)
      ) {
        throw new Error("strategy_session_reauthentication_required");
      }
      let grant: APIModel<"SandboxAuthorizationGrant">;
      try {
        grant = await postAPI<"SandboxAuthorizationGrant">(
          "/api/v1/sandbox/authorizations",
          {
            purpose: "sandbox_arm",
            password,
            totp,
            reason: reason.trim(),
          } satisfies APIModel<"SandboxAuthorizationRequest">,
        );
      } finally {
        setPassword("");
        setTOTP("");
      }
      return postAPI<"CommandAccepted">(
        `/api/v1/sandbox/strategy-sessions/${encodeURIComponent(startTarget.id)}/start`,
        {
          authorization_token: grant.token,
          expected_revision: startTarget.revision,
          reason: reason.trim(),
        } satisfies APIModel<"SandboxStrategySessionStartRequest">,
        newIdempotencyKey("sandbox-strategy-start"),
      );
    },
    onSuccess: async () => {
      setConfirmed(false);
      setStartTarget(undefined);
      await queryClient.invalidateQueries({ queryKey: ["sandbox"] });
    },
  });
  const stop = useMutation({
    mutationFn: (session: StrategySession) => {
      if (reason.trim().length < 8)
        throw new Error("strategy_session_reason_required");
      return postAPI<"CommandAccepted">(
        `/api/v1/sandbox/strategy-sessions/${encodeURIComponent(session.id)}/stop`,
        {
          expected_revision: session.revision,
          reason: reason.trim(),
        } satisfies APIModel<"RevisionCommandRequest">,
        newIdempotencyKey("sandbox-strategy-stop"),
      );
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["sandbox"] }),
  });
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
        enables real-money trading. After explicit arming and reauthentication,
        each installed strategy evaluates through allocation, central risk,
        capped spot execution, accounting, and reconciliation.
      </p>
      <ol
        className={styles.sessionWorkflow}
        aria-label="Strategy session steps"
      >
        <li>
          Prepare a strategy session with a supported exchange and instrument.
        </li>
        <li>
          Use the account controls above to arm every selected Testnet or Demo
          account.
        </li>
        <li>
          Reauthenticate here, confirm the armed accounts, and start evaluation.
        </li>
        <li>
          Review decisions and evidence; stop the session or revoke an arm at
          any time.
        </li>
      </ol>
      <SandboxSessionPreparationForm
        strategy={strategy}
        exchange={exchange}
        instrument={instrument}
        pending={prepare.isPending}
        onStrategy={setStrategy}
        onExchange={setExchange}
        onInstrument={setInstrument}
        onSubmit={() => prepare.mutate()}
      />
      {strategy === "cross-exchange-arbitrage" ? (
        <p className={styles.disclaimer}>
          This paired session requires both separate armed accounts. Binance is
          the evaluation coordinator; each credential-owning engine may submit
          only its own approved, fenced leg.
        </p>
      ) : null}
      {prepare.isError ? (
        <p className={styles.sessionError} role="status">
          The session was not prepared. Confirm the selected engine is ready,
          eligible, and not already assigned; no session or order was created.
        </p>
      ) : null}
      {startTarget ? (
        <SandboxSessionStartForm
          password={password}
          totp={totp}
          reason={reason}
          confirmed={confirmed}
          pending={start.isPending}
          onPassword={setPassword}
          onTOTP={setTOTP}
          onReason={setReason}
          onConfirmed={setConfirmed}
          onSubmit={() => start.mutate()}
          onCancel={() => setStartTarget(undefined)}
        />
      ) : null}
      {start.isError ? (
        <p className={styles.sessionError} role="status">
          The session did not start. Confirm every selected account has a
          current owner arm, its engine is ready, and the session revision has
          not changed; no order was created.
        </p>
      ) : null}
      {stop.isError ? (
        <p className={styles.sessionError} role="status">
          The session was not stopped. Refresh its current state and try again;
          no recovery or cancellation action was changed.
        </p>
      ) : null}
      <SandboxStrategySessionTable
        sessions={sessions}
        stopPending={stop.isPending}
        onStart={setStartTarget}
        onStop={(session) => stop.mutate(session)}
      />
    </section>
  );
}
