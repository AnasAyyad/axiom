import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { describe, expect, it } from "vitest";

import { GettingStartedPage } from "./OwnerGuidancePages";

describe("GettingStartedPage", () => {
  it("guides an owner through the required safe-first checklist", () => {
    render(
      <MemoryRouter>
        <GettingStartedPage />
      </MemoryRouter>,
    );
    expect(screen.getByRole("heading", { name: "First-login checklist" })).toBeVisible();
    expect(screen.getByRole("link", { name: /Run a guided proof demonstration/ })).toHaveAttribute("href", "/guided-demonstrations");
    expect(screen.getByText(/does not predict returns or prove/i)).toBeInTheDocument();
    expect(screen.getByText(/only after the required strategy-session workflow is installed and armed/i)).toBeVisible();
  });
});
