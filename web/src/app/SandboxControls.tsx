import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";

import { newIdempotencyKey, postAPI, type APIModel } from "../api/client";
import { sessionQuery } from "../api/queries";
import { StatePanel } from "../components/StatePanel";
import {
  SandboxControlsView,
  type SandboxLowRiskAction,
} from "./SandboxControlsView";

type Account = APIModel<"SandboxAccount">;
type Order = APIModel<"SandboxOrder">;
type Reconciliation = APIModel<"SandboxReconciliation">;
type HighRiskAction = "arm" | "unlock";

interface Props {
  readonly accounts: Account[];
  readonly orders: Order[];
  readonly reconciliations: Reconciliation[];
}

export function SandboxControls({ accounts, orders, reconciliations }: Props) {
  const session = useQuery(sessionQuery);
  const queryClient = useQueryClient();
  const [accountID, setAccountID] = useState(accounts[0]?.id ?? "");
  const [highRiskAction, setHighRiskAction] = useState<HighRiskAction>("arm");
  const [password, setPassword] = useState("");
  const [totp, setTOTP] = useState("");
  const [highRiskReason, setHighRiskReason] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const [lowRiskReason, setLowRiskReason] = useState(
    "Operator requested safe recovery from C6 console",
  );
  const [instrument, setInstrument] = useState<"BTCUSDT" | "ETHUSDT">(
    "BTCUSDT",
  );
  const [style, setStyle] = useState<"LIMIT_GTC" | "LIMIT_IOC" | "POST_ONLY">(
    "LIMIT_GTC",
  );
  const [quantity, setQuantity] = useState("");
  const [limitPrice, setLimitPrice] = useState("");
  const [orderConfirmed, setOrderConfirmed] = useState(false);
  const account = accounts.find((item) => item.id === accountID);
  const cleanReconciliation = useMemo(
    () =>
      reconciliations.find(
        (item) => item.account_id === accountID && item.state === "clean",
      ),
    [accountID, reconciliations],
  );
  const invalidate = async () => {
    await queryClient.invalidateQueries({ queryKey: ["sandbox"] });
  };
  const highRisk = useMutation({
    mutationFn: async () => {
      if (!account || highRiskReason.trim().length < 8 || !confirmed)
        throw new Error("confirmation_required");
      const purpose = highRiskAction === "arm" ? "sandbox_arm" : "risk_unlock";
      const authorization: APIModel<"SandboxAuthorizationRequest"> = {
        purpose,
        password,
        totp,
        reason: highRiskReason.trim(),
      };
      let grant: APIModel<"SandboxAuthorizationGrant">;
      try {
        grant = await postAPI<"SandboxAuthorizationGrant">(
          "/api/v1/sandbox/authorizations",
          authorization,
        );
      } finally {
        setPassword("");
        setTOTP("");
      }
      if (highRiskAction === "arm") {
        if (!account.session_id || !account.session_revision)
          throw new Error("sandbox_session_unavailable");
        const body: APIModel<"SandboxArmRequest"> = {
          authorization_token: grant.token,
          expected_revision: account.session_revision,
          account_ids: [account.id],
          reason: highRiskReason.trim(),
        };
        return postAPI<"SandboxArm">(
          `/api/v1/sandbox/sessions/${encodeURIComponent(account.session_id)}/arms`,
          body,
          newIdempotencyKey("sandbox-arm"),
        );
      }
      if (!cleanReconciliation)
        throw new Error("clean_reconciliation_required");
      const body: APIModel<"SandboxUnlockRequest"> = {
        authorization_token: grant.token,
        expected_revision: account.revision,
        reconciliation_id: cleanReconciliation.id,
        reason: highRiskReason.trim(),
      };
      return postAPI<"CommandAccepted">(
        `/api/v1/sandbox/accounts/${encodeURIComponent(account.id)}/unlock`,
        body,
        newIdempotencyKey("sandbox-unlock"),
      );
    },
    onSuccess: async () => {
      setConfirmed(false);
      setHighRiskReason("");
      await invalidate();
    },
  });
  const order = useMutation({
    mutationFn: async () => {
      if (
        !account?.active_arm ||
        !account.session_id ||
        !account.session_revision ||
        !orderConfirmed ||
        lowRiskReason.trim().length < 8
      )
        throw new Error("active_arm_confirmation_required");
      const body: APIModel<"SandboxTestOrderRequest"> = {
        session_id: account.session_id,
        arm_id: account.active_arm.id,
        account_id: account.id,
        exchange: account.exchange,
        instrument,
        side: "buy",
        quantity,
        limit_price: limitPrice,
        style,
        expected_revision: account.session_revision,
        reason: lowRiskReason.trim(),
      };
      return postAPI<"CommandAccepted">(
        "/api/v1/sandbox/orders",
        body,
        newIdempotencyKey("sandbox-order"),
      );
    },
    onSuccess: async () => {
      setOrderConfirmed(false);
      setQuantity("");
      setLimitPrice("");
      await invalidate();
    },
  });
  const lowRisk = useMutation({
    mutationFn: async (action: SandboxLowRiskAction) => {
      if (lowRiskReason.trim().length < 8) throw new Error("reason_required");
      if (action.kind === "revoke") {
        return postAPI<"CommandAccepted">(
          `/api/v1/sandbox/arms/${encodeURIComponent(action.arm.id)}/revoke`,
          {
            expected_revision: action.arm.revision,
            reason: lowRiskReason.trim(),
          } satisfies APIModel<"RevisionCommandRequest">,
          newIdempotencyKey("sandbox-arm-revoke"),
        );
      }
      if (action.kind === "reconcile") {
        return postAPI<"CommandAccepted">(
          `/api/v1/sandbox/accounts/${encodeURIComponent(action.account.id)}/reconcile`,
          {
            expected_revision: action.account.revision,
            reason: lowRiskReason.trim(),
          } satisfies APIModel<"RevisionCommandRequest">,
          newIdempotencyKey("sandbox-reconcile"),
        );
      }
      return postAPI<"CommandAccepted">(
        `/api/v1/sandbox/orders/${encodeURIComponent(action.order.id)}/${action.kind}`,
        {
          expected_revision: action.order.revision,
          reason: lowRiskReason.trim(),
        } satisfies APIModel<"RevisionCommandRequest">,
        newIdempotencyKey(`sandbox-${action.kind}`),
      );
    },
    onSuccess: invalidate,
  });
  if (session.isLoading) return <StatePanel state="loading" />;
  if (session.isError || !session.data) return <StatePanel state="forbidden" />;
  return (
    <SandboxControlsView
      accounts={accounts}
      orders={orders}
      account={account}
      cleanReconciliation={cleanReconciliation}
      accountID={accountID}
      setAccountID={setAccountID}
      lowRiskReason={lowRiskReason}
      setLowRiskReason={setLowRiskReason}
      highRiskAction={highRiskAction}
      setHighRiskAction={setHighRiskAction}
      password={password}
      setPassword={setPassword}
      totp={totp}
      setTOTP={setTOTP}
      highRiskReason={highRiskReason}
      setHighRiskReason={setHighRiskReason}
      confirmed={confirmed}
      setConfirmed={setConfirmed}
      instrument={instrument}
      setInstrument={setInstrument}
      style={style}
      setStyle={setStyle}
      quantity={quantity}
      setQuantity={setQuantity}
      limitPrice={limitPrice}
      setLimitPrice={setLimitPrice}
      orderConfirmed={orderConfirmed}
      setOrderConfirmed={setOrderConfirmed}
      highRiskPending={highRisk.isPending}
      orderPending={order.isPending}
      onHighRisk={() => highRisk.mutate()}
      onOrder={() => order.mutate()}
      onLowRisk={(action) => lowRisk.mutate(action)}
      errors={[highRisk.error, order.error, lowRisk.error]}
      pending={highRisk.isPending || order.isPending || lowRisk.isPending}
      success={highRisk.isSuccess || order.isSuccess || lowRisk.isSuccess}
    />
  );
}
