import { fireEvent, render, screen } from "@testing-library/react";
import axe from "axe-core";
import { expect, it, vi } from "vitest";

import { ConfirmAction } from "./ConfirmAction";

it("requires an accessible modal confirmation before running an action", async () => {
  const confirm = vi.fn();
  const view = render(
    <ConfirmAction
      trigger={<button type="button">pause</button>}
      title="pause deterministic replay?"
      description="The command is durable and audited."
      confirmLabel="pause"
      onConfirm={confirm}
    />,
  );

  const trigger = screen.getByRole("button", { name: "pause" });
  fireEvent.click(trigger);
  const dialog = screen.getByRole("alertdialog");
  expect(dialog).toHaveAttribute("open");
  expect(dialog).toHaveAccessibleName("pause deterministic replay?");
  expect(dialog).toHaveAccessibleDescription(
    "The command is durable and audited.",
  );
  expect(confirm).not.toHaveBeenCalled();
  expect((await axe.run(view.container)).violations).toHaveLength(0);

  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  expect(dialog).not.toHaveAttribute("open");
  expect(trigger).toHaveFocus();
  expect(confirm).not.toHaveBeenCalled();

  fireEvent.click(trigger);
  fireEvent.click(screen.getAllByRole("button", { name: "pause" }).at(-1)!);
  expect(dialog).not.toHaveAttribute("open");
  expect(trigger).toHaveFocus();
  expect(confirm).toHaveBeenCalledOnce();
});
