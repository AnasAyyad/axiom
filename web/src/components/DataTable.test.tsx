import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { expect, it } from "vitest";

import { DataTable } from "./DataTable";

it("remains interactive when its parent replaces table rows", () => {
  render(<InteractiveTable />);

  fireEvent.click(screen.getByRole("button", { name: "Authorize evidence" }));

  expect(screen.getByRole("cell", { name: "authorized" })).toBeInTheDocument();
  expect(
    screen.getByRole("button", { name: "Use redacted evidence" }),
  ).toBeEnabled();
});

function InteractiveTable() {
  const [authorized, setAuthorized] = useState(false);
  return (
    <>
      <button type="button" onClick={() => setAuthorized((value) => !value)}>
        {authorized ? "Use redacted evidence" : "Authorize evidence"}
      </button>
      <DataTable
        caption="Evidence"
        rows={[
          { id: "event-a11", detail: authorized ? "authorized" : "redacted" },
        ]}
        columns={[{ key: "detail", label: "Detail" }]}
      />
    </>
  );
}
