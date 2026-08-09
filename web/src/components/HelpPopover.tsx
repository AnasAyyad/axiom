import { useId, useState, type ReactNode } from "react";

import styles from "./HelpPopover.module.css";

interface HelpPopoverProps {
  readonly label: string;
  readonly children: ReactNode;
}

/** HelpPopover keeps explanatory content available to mouse, keyboard, and touch users. */
export function HelpPopover({ label, children }: HelpPopoverProps) {
  const contentID = useId();
  const [hovered, setHovered] = useState(false);
  const [focused, setFocused] = useState(false);
  const [pinned, setPinned] = useState(false);
  const open = hovered || focused || pinned;
  return (
    <span
      className={styles.root}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
    >
      <button
        aria-describedby={open ? contentID : undefined}
        aria-expanded={open}
        className={styles.trigger}
        type="button"
        onBlur={() => setFocused(false)}
        onClick={() => setPinned((value) => !value)}
        onFocus={() => setFocused(true)}
      >
        {label}
      </button>
      {open && (
        <span className={styles.content} id={contentID} role="tooltip">
          {children}
        </span>
      )}
    </span>
  );
}
