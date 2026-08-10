import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { Page } from "./OperationalShared";

describe("Page", () => {
  it("offers keyboard and click-accessible page help", () => {
    render(
      <Page
        title="Example"
        eyebrow="Research"
        description="A server-authoritative example projection."
      >
        <p>Content</p>
      </Page>,
    );

    const help = screen.getByRole("button", { name: "About this page" });
    fireEvent.focus(help);

    expect(
      screen.getByText(
        /most recently retrieved server-authoritative projection/i,
      ),
    ).toBeVisible();

    fireEvent.blur(help);
    fireEvent.mouseEnter(help);
    expect(
      screen.getByText(
        /most recently retrieved server-authoritative projection/i,
      ),
    ).toBeVisible();

    fireEvent.mouseLeave(help);
    fireEvent.click(help);
    expect(
      screen.getByText(
        /most recently retrieved server-authoritative projection/i,
      ),
    ).toBeVisible();
  });
});
