import { expect, test, type Page } from "@playwright/test";

const configuration = required("AXIOM_A11_E2E_CONFIGURATION_ID");
const dataset = required("AXIOM_A11_E2E_DATASET_ID");
const generation = required("AXIOM_A11_E2E_RESEARCH_GENERATION_ID");
const portfolio = required("AXIOM_A11_E2E_PORTFOLIO_ID");
const evidenceShadow = required("AXIOM_A11_E2E_EVIDENCE_SHADOW_ID");
const email = process.env.AXIOM_A11_E2E_EMAIL ?? "owner@example.test";
const password = required("AXIOM_A11_E2E_PASSWORD");
const seed = "8".repeat(64);

test.setTimeout(600_000);

test("real authenticated API drives the complete virtual research workflow", async ({
  page,
  context,
  isMobile,
}) => {
  await page.goto("/login");
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password").fill(password);
  const loginResponse = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      new URL(response.url()).pathname === "/api/v1/session/login",
  );
  await page.getByRole("button", { name: "Enter console" }).click();
  expect((await loginResponse).status()).toBe(201);
  expect((await context.cookies()).map((cookie) => cookie.name)).toEqual(
    expect.arrayContaining(["axiom_session", "axiom_csrf"]),
  );
  const sessionResponse = await page.request.get("/api/v1/session/me");
  expect(sessionResponse.status()).toBe(200);
  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "Command Center" }),
  ).toBeVisible();
  await expect(page.getByText("REAL TRADING DISABLED")).toBeVisible();

  await page.getByRole("link", { name: "Binance" }).click();
  await expect(page.getByText("Production-public only")).toBeVisible();
  await expect(
    page.getByText("Private routes and credentials are absent.", {
      exact: false,
    }),
  ).toBeVisible();

  await page.getByRole("link", { name: "Backtest Lab" }).click();
  await fillRun(page, dataset);
  await page.getByRole("button", { name: "Launch backtest" }).click();
  await expect(page.getByText("SUCCEEDED")).toBeVisible({ timeout: 120_000 });
  await expect(page.getByText("single_run_incomplete")).toBeVisible();
  await expect(page.getByText("canonical_pipeline_completed")).toBeVisible();
  await expect(
    page
      .getByRole("note")
      .filter({
        hasText: "not evidence or a guarantee of production profitability",
      })
      .first(),
  ).toBeVisible();

  await page.goto("/replays");
  await fillRun(page, dataset);
  const replayResponse = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      new URL(response.url()).pathname === "/api/v1/replays",
  );
  await page.getByRole("button", { name: "Create replay" }).click();
  const replayID = ((await (await replayResponse).json()) as { id: string }).id;
  await expect(page.getByText("RUNNING")).toBeVisible({ timeout: 15_000 });
  await confirm(page, "pause", "pause");
  await expect(
    page.getByRole("main").getByText("PAUSED", { exact: true }),
  ).toBeVisible({ timeout: 15_000 });
  await page.goto(`/replays/${replayID}`);
  const pausedOrdinal = Number((await replaySnapshot(page, replayID)).ordinal);
  expect(pausedOrdinal).toBeGreaterThan(0);
  await confirm(page, "step", "step");
  await expect
    .poll(
      async () => {
        const snapshot = await replaySnapshot(page, replayID);
        return `${snapshot.state}:${snapshot.ordinal}`;
      },
      { timeout: 15_000 },
    )
    .toBe(`PAUSED:${pausedOrdinal + 1}`);
  await page.goto(`/replays/${replayID}`);
  await expect(
    page.getByRole("main").getByText("PAUSED", { exact: true }),
  ).toBeVisible({ timeout: 15_000 });
  await confirm(page, "resume", "resume");
  await expect(page.getByText("SUCCEEDED")).toBeVisible({ timeout: 120_000 });
  await expect(
    page.getByRole("heading", { name: "Exact event and decision inspection" }),
  ).toBeVisible();
  await expect(page.getByText("Canonical event hash")).toBeVisible();
  await page.getByText("Canonical decision", { exact: true }).click();
  await expect(
    page
      .getByRole("group")
      .filter({
        has: page.getByText("Canonical decision", { exact: true }),
      })
      .locator("pre")
      .filter({ hasText: "reason_code" }),
  ).toBeVisible();

  await page.goto(`/shadow/${evidenceShadow}`);
  await expect(page.getByText("CANCELED")).toBeVisible({ timeout: 15_000 });
  await expect(
    page.getByRole("table", { name: "Simulated orders and fills" }),
  ).toBeVisible();
  await expect(page.getByText(/1 accepted/)).toBeVisible();
  await expect(page.getByText(/transactions/)).toBeVisible();
  await expect(page.getByText("namespace-a11")).toBeVisible();

  await page.goto("/strategies/trend");
  await expect(
    page.getByRole("table", { name: "Decision and rejection evidence" }),
  ).toBeVisible();
  await page.goto("/risk");
  await expect(
    page.getByRole("heading", { name: "Risk Center" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Resume risk" }).click();
  await expect(page.getByRole("alertdialog")).toBeVisible();
  await page.getByRole("button", { name: "Request resume" }).click();
  await expect(
    page.getByRole("main").getByText("NORMAL", { exact: true }),
  ).toBeVisible({
    timeout: 15_000,
  });
  await page.goto("/portfolios");
  await expect(
    page.getByRole("table", { name: "Virtual balances" }),
  ).toBeVisible();
  await expect(
    page.getByRole("table", { name: "Immutable journal lines" }),
  ).toBeVisible();

  await page.goto("/incidents");
  await page
    .getByRole("link", { name: "Open latest incident evidence" })
    .click();
  await page
    .getByRole("button", { name: "Show authorized evidence hashes" })
    .click();
  await expect(page.getByText(/event_hash.*[a-f0-9]{64}/)).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Prepare incident replay" }),
  ).toBeVisible({ timeout: 15_000 });
  await page.getByRole("link", { name: "Prepare incident replay" }).click();
  await fillRun(page, dataset);
  await page.getByRole("button", { name: "Create replay" }).click();
  await expect(page.getByText("SUCCEEDED")).toBeVisible({ timeout: 120_000 });

  await context.setOffline(true);
  await expect(page.getByText("reconnecting")).toBeVisible({ timeout: 20_000 });
  await context.setOffline(false);
  await expect(page.getByText("live")).toBeVisible({ timeout: 20_000 });

  await page.goto("/shadow");
  await page.getByLabel("Configuration ID").fill(configuration);
  await page.getByLabel("Portfolio ID").fill(portfolio);
  const liveShadowResponse = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      new URL(response.url()).pathname === "/api/v1/shadow-sessions",
  );
  await page.getByRole("button", { name: "Start virtual shadow" }).click();
  const liveShadowID = (
    (await (await liveShadowResponse).json()) as { id: string }
  ).id;
  await expect(page.getByText("RUNNING")).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText("yes").first()).toBeVisible();
  await page.goto("/shadow");
  await page.getByLabel("Configuration ID").fill(configuration);
  await page.getByLabel("Portfolio ID").fill(portfolio);
  await page.getByRole("button", { name: "Start virtual shadow" }).click();
  await expect(page.getByText(/one-session quota/i)).toBeVisible();
  await page.goto(`/shadow/${liveShadowID}`);
  const stopShadow = page.getByRole("button", {
    name: "Stop shadow session",
  });
  await stopShadow.click();
  await page.getByRole("button", { name: "Stop session" }).click();
  await expect(page.getByText("CANCELED")).toBeVisible({ timeout: 30_000 });
  await expect(stopShadow).toBeFocused();

  if (!isMobile) {
    // The invoking control is the final tab stop, so forward traversal exits
    // the document in Chromium. Reverse traversal proves keyboard continuity.
    await page.keyboard.press("Shift+Tab");
    expect(await page.evaluate(() => document.activeElement?.tagName)).not.toBe(
      "BODY",
    );
  }
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  await page.getByRole("button", { name: "Log out" }).click();
  await expect(
    page.getByRole("heading", { name: "Owner access" }),
  ).toBeVisible();
});

async function fillRun(page: Page, datasetID: string) {
  await page.getByLabel("Configuration ID").fill(configuration);
  await page.getByLabel("Dataset ID").fill(datasetID);
  await page.getByLabel("Research generation ID").fill(generation);
  await page.getByLabel("Root seed hash").fill(seed);
}

async function confirm(page: Page, trigger: string, confirmation: string) {
  await page.getByRole("button", { name: trigger, exact: true }).click();
  await expect(page.getByRole("alertdialog")).toBeVisible();
  await page
    .getByRole("button", { name: confirmation, exact: true })
    .last()
    .click();
}

async function replaySnapshot(page: Page, replayID: string) {
  const response = await page.request.get(`/api/v1/replays/${replayID}`);
  expect(response.status()).toBe(200);
  const resource = (await response.json()) as {
    state: string;
    replay_inspection?: { ordinal: string };
  };
  return {
    state: resource.state,
    ordinal: resource.replay_inspection?.ordinal ?? "0",
  };
}

function required(name: string): string {
  const value = process.env[name];
  if (!value) throw new Error(`${name} is required for integrated A11 E2E`);
  return value;
}
