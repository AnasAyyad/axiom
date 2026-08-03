import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import {
  APIError,
  newIdempotencyKey,
  postAPI,
  type APIModel,
} from "../../api/client";
import { d1CollectionQuery, sessionQuery } from "../../api/queries";
import { Page } from "../../app/OperationalShared";
import { StatePanel } from "../../components/StatePanel";
import { HighRiskAuthorizationForm } from "../shared/HighRiskAuthorizationForm";
import { StatusBadge } from "../shared/StatusBadge";
import {
  stringAttribute,
  stringListAttribute,
} from "../strategies/strategyModel";
import styles from "../shared/D2.module.css";

const roles = ["researcher", "operator", "auditor", "owner"] as const;

export function UserAccessPage() {
  const session = useQuery(sessionQuery);
  const query = useQuery(d1CollectionQuery("users"));
  if (session.isLoading || query.isLoading)
    return <StatePanel state="loading" />;
  if (
    (session.error instanceof APIError && session.error.status === 403) ||
    (query.error instanceof APIError && query.error.status === 403)
  )
    return <StatePanel state="forbidden" />;
  if (session.isError || query.isError || !session.data || !query.data)
    return (
      <StatePanel state="error" detail="User access records are unavailable." />
    );
  return (
    <Page
      title="User Access"
      eyebrow="Owner-administered roles"
      description="Assign explicit Researcher, Operator, Auditor, or Owner roles. Deprecated Viewer remains read-only and cannot be newly assigned."
    >
      <p className={styles.notice} role="note">
        Every change requires exact-revision Owner password/TOTP
        reauthentication. The server prevents removal of the final active Owner.
      </p>
      <div className={styles.cardGrid}>
        {query.data.items.map((user) => (
          <UserRoleCard key={user.id} user={user} />
        ))}
      </div>
    </Page>
  );
}

function UserRoleCard({ user }: { readonly user: APIModel<"D1Resource"> }) {
  const initialRoles = stringListAttribute(user.attributes, "roles").filter(
    (role): role is (typeof roles)[number] =>
      roles.includes(role as (typeof roles)[number]),
  );
  const [selectedRoles, setSelectedRoles] =
    useState<Array<(typeof roles)[number]>>(initialRoles);
  const queryClient = useQueryClient();
  const toggle = (role: (typeof roles)[number]) =>
    setSelectedRoles((current) =>
      current.includes(role)
        ? current.filter((item) => item !== role)
        : [...current, role],
    );
  return (
    <article className={styles.card}>
      <div className={styles.cardHeader}>
        <div>
          <h2>{stringAttribute(user.attributes, "email", user.id)}</h2>
          <p>{user.id}</p>
        </div>
        <StatusBadge value={user.state} />
      </div>
      <fieldset className={styles.controlCard}>
        <legend>Assigned roles</legend>
        {roles.map((role) => (
          <label className={styles.checkbox} key={role}>
            <input
              type="checkbox"
              checked={selectedRoles.includes(role)}
              onChange={() => toggle(role)}
            />
            <span>{role}</span>
          </label>
        ))}
      </fieldset>
      <HighRiskAuthorizationForm
        title="Apply role revision"
        purpose="role_change"
        expectedRevision={user.revision}
        confirmLabel="Change roles"
        disabled={selectedRoles.length === 0}
        onAuthorized={async (authorization_token, reason) => {
          await postAPI<"CommandAccepted">(
            `/api/v1/users/${encodeURIComponent(user.id)}/roles`,
            {
              authorization_token,
              expected_revision: user.revision,
              reason,
              roles: selectedRoles,
            } satisfies APIModel<"RoleChangeRequest">,
            newIdempotencyKey("role-change"),
          );
          await queryClient.invalidateQueries({ queryKey: ["d1", "users"] });
        }}
      />
    </article>
  );
}
