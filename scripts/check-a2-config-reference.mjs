import fs from "node:fs";

const configurationPath = "deploy/config/platform-shadow.json";
const referencePath = "docs/configuration/v1a-product-configuration.md";
const configuration = JSON.parse(fs.readFileSync(configurationPath, "utf8"));
const reference = fs.readFileSync(referencePath, "utf8");

const rows = [
  [
    "risk.maximum_asset_allocation",
    "0.25",
    "decimal_fraction",
    "0",
    "1",
    true,
    true,
    8,
    "down",
  ],
  [
    "risk.maximum_order_notional",
    "1000",
    "USDT",
    "0",
    "1000000",
    false,
    true,
    8,
    "half_even",
  ],
  [
    "risk.maximum_daily_loss",
    "100",
    "USDT",
    "0",
    "1000000",
    false,
    true,
    8,
    "half_even",
  ],
  [
    "portfolio.starting_capital",
    "500",
    "USDT",
    "0",
    "1000000",
    false,
    true,
    8,
    "half_even",
  ],
];

function valueAt(path) {
  return path.split(".").reduce((value, key) => value[key], configuration);
}

function fail(message) {
  process.stderr.write(`ERROR [a2-config-reference] ${message}\n`);
  process.exitCode = 1;
}

if (configuration.schema_version !== "axiom.config.v1a.1") {
  fail("deployment schema version is not the documented V1A schema");
}

for (const [
  path,
  value,
  unit,
  minimum,
  maximum,
  minimumInclusive,
  maximumInclusive,
  scale,
  rounding,
] of rows) {
  const setting = valueAt(path);
  const expected = {
    value,
    unit,
    minimum,
    maximum,
    minimum_inclusive: minimumInclusive,
    maximum_inclusive: maximumInclusive,
    scale,
    rounding,
  };
  if (JSON.stringify(setting) !== JSON.stringify(expected)) {
    fail(`${path} does not match its expected exact numeric contract`);
  }
  const inclusivity = minimumInclusive
    ? "both inclusive"
    : "minimum exclusive, maximum inclusive";
  const row = `| \`${path}\` | \`${value}\` | \`${unit}\` | \`${minimum}..${maximum}\` | ${inclusivity} | ${scale} | \`${rounding}\` |`;
  if (!reference.includes(row)) {
    fail(`${path} is missing or stale in the configuration reference table`);
  }
}

if (!process.exitCode) {
  process.stdout.write("A2 configuration/reference consistency passed\n");
}
