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
  process.stderr.write(`ERROR [configuration-reference] ${message}\n`);
  process.exitCode = 1;
}

if (configuration.schema_version !== "axiom.configuration@1.0.0") {
  fail("deployment schema version is not the documented shadow schema");
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

if (
  configuration.trend.strategy_version !== "trend-following@1.0.0" ||
  configuration.trend.timeframe !== "4h" ||
  configuration.trend.parameters.length !== 16
) {
  fail("Trend version, timeframe, or complete parameter count is stale");
} else {
  for (const parameter of configuration.trend.parameters) {
    const inclusivity = parameter.minimum_inclusive
      ? "both inclusive"
      : "minimum exclusive, maximum inclusive";
    const dependencies = parameter.model_dependencies
      .map((dependency) => `\`${dependency}\``)
      .join(", ");
    const row = `| \`${parameter.id}\` | \`${parameter.value}\` | \`${parameter.unit}\` | \`${parameter.minimum}..${parameter.maximum}\` | ${inclusivity} | ${parameter.scale} | \`${parameter.rounding}\` | \`${parameter.warm_up}\` | ${dependencies} |`;
    if (!reference.includes(row)) {
      fail(`${parameter.id} is missing or stale in the Trend parameter table`);
    }
  }
}

if (!process.exitCode) {
  process.stdout.write("Configuration/reference consistency passed\n");
}
