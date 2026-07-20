import {
  cloneElement,
  isValidElement,
  useId,
  type ButtonHTMLAttributes,
  type MouseEvent,
  type ReactElement,
} from "react";

import styles from "./UI.module.css";

interface ConfirmActionProps {
  readonly trigger: ReactElement<ButtonHTMLAttributes<HTMLButtonElement>>;
  readonly title: string;
  readonly description: string;
  readonly confirmLabel: string;
  readonly onConfirm: () => void;
}

/** ConfirmAction uses the browser modal primitive for consequential commands. */
export function ConfirmAction(props: ConfirmActionProps) {
  const titleID = useId();
  const descriptionID = useId();

  if (!isValidElement(props.trigger)) return null;

  const trigger = cloneElement(props.trigger, {
    "aria-haspopup": "dialog",
    onClick: (event: MouseEvent<HTMLButtonElement>) => {
      props.trigger.props.onClick?.(event);
      if (!event.defaultPrevented) {
        const dialog = event.currentTarget.nextElementSibling;
        openModal(dialog instanceof HTMLDialogElement ? dialog : null);
      }
    },
  });

  return (
    <>
      {trigger}
      <dialog
        className={styles.dialog}
        role="alertdialog"
        aria-modal="true"
        aria-labelledby={titleID}
        aria-describedby={descriptionID}
      >
        <h2 id={titleID}>{props.title}</h2>
        <p id={descriptionID}>{props.description}</p>
        <div className={styles.dialogActions}>
          <button
            type="button"
            className={styles.secondaryButton}
            onClick={(event) =>
              closeModal(event.currentTarget.closest("dialog"))
            }
          >
            Cancel
          </button>
          <button
            type="button"
            className={styles.dangerButton}
            onClick={(event) => {
              closeModal(event.currentTarget.closest("dialog"));
              props.onConfirm();
            }}
          >
            {props.confirmLabel}
          </button>
        </div>
      </dialog>
    </>
  );
}

function openModal(dialog: HTMLDialogElement | null) {
  if (!dialog || dialog.open) return;
  if (typeof dialog.showModal === "function") {
    dialog.showModal();
    return;
  }
  dialog.setAttribute("open", "");
}

function closeModal(dialog: HTMLDialogElement | null) {
  if (!dialog?.open) return;
  const invokingButton = dialog.previousElementSibling;
  if (typeof dialog.close === "function") {
    dialog.close();
  } else {
    dialog.removeAttribute("open");
  }
  if (invokingButton instanceof HTMLButtonElement) invokingButton.focus();
}
