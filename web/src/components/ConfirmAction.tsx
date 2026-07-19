import * as AlertDialog from "@radix-ui/react-alert-dialog";
import type { ReactNode } from "react";

import styles from "./UI.module.css";

interface ConfirmActionProps {
  readonly trigger: ReactNode;
  readonly title: string;
  readonly description: string;
  readonly confirmLabel: string;
  readonly onConfirm: () => void;
}

/** ConfirmAction wraps the accessible Radix primitive for consequential commands. */
export function ConfirmAction(props: ConfirmActionProps) {
  return (
    <AlertDialog.Root>
      <AlertDialog.Trigger asChild>{props.trigger}</AlertDialog.Trigger>
      <AlertDialog.Portal>
        <AlertDialog.Overlay className={styles.overlay} />
        <AlertDialog.Content className={styles.dialog}>
          <AlertDialog.Title>{props.title}</AlertDialog.Title>
          <AlertDialog.Description>{props.description}</AlertDialog.Description>
          <div className={styles.dialogActions}>
            <AlertDialog.Cancel className={styles.secondaryButton}>
              Cancel
            </AlertDialog.Cancel>
            <AlertDialog.Action
              className={styles.dangerButton}
              onClick={props.onConfirm}
            >
              {props.confirmLabel}
            </AlertDialog.Action>
          </div>
        </AlertDialog.Content>
      </AlertDialog.Portal>
    </AlertDialog.Root>
  );
}
