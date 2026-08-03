import { useMutation } from "@tanstack/react-query";
import { useState } from "react";

import { postAPI, type APIModel } from "../../api/client";
import styles from "./D2.module.css";

type Purpose = APIModel<"HighRiskAuthorizationRequest">["purpose"];

interface HighRiskAuthorizationFormProps {
  readonly title: string;
  readonly purpose: Purpose;
  readonly expectedRevision: string;
  readonly confirmLabel: string;
  readonly disabled?: boolean;
  readonly onAuthorized: (token: string, reason: string) => Promise<unknown>;
}

export function HighRiskAuthorizationForm({
  title,
  purpose,
  expectedRevision,
  confirmLabel,
  disabled = false,
  onAuthorized,
}: HighRiskAuthorizationFormProps) {
  const [password, setPassword] = useState("");
  const [totp, setTOTP] = useState("");
  const [reason, setReason] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const mutation = useMutation({
    mutationFn: async () => {
      const cleanReason = reason.trim();
      if (!confirmed || cleanReason.length < 8)
        throw new Error("confirmation_and_reason_required");
      let grant: APIModel<"HighRiskAuthorizationGrant">;
      try {
        grant = await postAPI<"HighRiskAuthorizationGrant">(
          "/api/v1/authorizations",
          {
            purpose,
            expected_revision: expectedRevision,
            password,
            totp,
            reason: cleanReason,
          } satisfies APIModel<"HighRiskAuthorizationRequest">,
        );
      } finally {
        setPassword("");
        setTOTP("");
      }
      return onAuthorized(grant.token, cleanReason);
    },
    onSuccess: () => {
      setConfirmed(false);
      setReason("");
    },
  });
  return (
    <section className={styles.controlCard} aria-label={title}>
      <h3>{title}</h3>
      <p>
        Owner password and time-based one-time password (TOTP) create a
        single-use authorization bound to revision {expectedRevision}.
      </p>
      <div className={styles.form}>
        <label className={styles.field}>
          Reason
          <textarea
            value={reason}
            onChange={(event) => setReason(event.target.value)}
            minLength={8}
            required
          />
        </label>
        <label className={styles.field}>
          Password
          <input
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            required
          />
        </label>
        <label className={styles.field}>
          TOTP
          <input
            inputMode="numeric"
            autoComplete="one-time-code"
            value={totp}
            onChange={(event) => setTOTP(event.target.value)}
            required
          />
        </label>
        <label className={`${styles.checkbox} ${styles.spanAll}`}>
          <input
            type="checkbox"
            checked={confirmed}
            onChange={(event) => setConfirmed(event.target.checked)}
          />
          <span>
            I confirm the exact target revision and understand this command is
            audited. It cannot enable real-money production trading.
          </span>
        </label>
      </div>
      <button
        className={styles.danger}
        type="button"
        disabled={
          disabled ||
          mutation.isPending ||
          !confirmed ||
          reason.trim().length < 8 ||
          password === "" ||
          totp === ""
        }
        onClick={() => mutation.mutate()}
      >
        {mutation.isPending ? "Authorizing…" : confirmLabel}
      </button>
      {mutation.isError && (
        <p className={styles.error} role="alert">
          The command was not applied. Verify permission, credentials, TOTP,
          reason, and exact revision before retrying.
        </p>
      )}
      {mutation.isSuccess && (
        <p className={styles.success} role="status">
          Command accepted. Follow its durable command state for the final
          outcome.
        </p>
      )}
    </section>
  );
}
