import { describe, expect, it } from "vitest";

import { navigationFor } from "./navigation";

describe("owner navigation", () => {
  it("shows the complete task-first product without a user-access destination", () => {
    const groups = navigationFor();
    expect(groups.map((group) => group.label)).toEqual([
      "Overview",
      "Explore",
      "Run",
      "Monitor",
      "Portfolio & Risk",
      "System",
    ]);
    const labels = groups.flatMap((group) => group.items.map((item) => item.label));
    expect(labels).toEqual(expect.arrayContaining(["Getting Started", "New Run", "Guided Demonstrations", "Exchange Sandbox", "Readiness"]));
    expect(labels).not.toContain("User access");
  });
});
