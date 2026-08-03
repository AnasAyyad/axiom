import { describe, expect, it } from "vitest";

import type { APIModel } from "../api/client";
import { navigationFor } from "./navigation";

function user(roles: string[], permissions: string[]): APIModel<"SessionUser"> {
  return {
    id: roles.join("-") || "viewer",
    email: "role@example.test",
    roles,
    permissions,
  };
}

function links(value: APIModel<"SessionUser">) {
  return navigationFor(value).flatMap((group) =>
    group.items.map((item) => item.to),
  );
}

describe("D2 role-aware navigation", () => {
  it("gives Owner/Admin the six product groups and all high-risk destinations", () => {
    const groups = navigationFor(user(["owner"], []));
    expect(groups.map((group) => group.label)).toEqual([
      "Home",
      "Activity",
      "Strategies",
      "Run Lab",
      "Risk & Controls",
      "Operations",
    ]);
    expect(links(user(["owner"], []))).toEqual(
      expect.arrayContaining([
        "/operations/qualifications",
        "/operations/configuration",
        "/operations/users",
      ]),
    );
  });

  it("keeps deprecated viewer read-only and hides operational destinations", () => {
    expect(links(user(["viewer"], []))).toEqual(["/"]);
  });

  it("shows explicit researcher and operator destinations from permissions", () => {
    const researcher = links(
      user(["researcher"], ["research.control", "activity.read"]),
    );
    expect(researcher).toEqual(
      expect.arrayContaining([
        "/activity/decisions-orders",
        "/backtests",
        "/replays",
      ]),
    );
    expect(researcher).not.toContain("/operations/users");
    const operator = links(
      user(["operator"], ["operations.read", "sandbox.read"]),
    );
    expect(operator).toEqual(
      expect.arrayContaining([
        "/risk/controls",
        "/operations/qualifications",
        "/operations/sandbox",
      ]),
    );
  });
});
