# Deployment environment reference

`.env.example` is the complete non-secret deployment surface and the source of
truth for variable names and safe defaults. Copy it to an untracked `.env`,
replace every applicable `CHANGE_ME`, and validate the rendered Compose model.

## Configuration layers

- Compiled policy fixes execution modes, spot products, authenticated
  host/route/method allowlists, prohibited fields, and absence of a production
  broker. Environment variables cannot weaken it.
- `APP_CONFIG_FILE` selects one complete immutable research/risk graph. Assets,
  instruments, strategies, allocation, fees, latency, valuation, datasets, and
  reporting cannot be partially overridden through the environment.
- `.env` supplies deployment wiring: immutable images, profiles, ports, paths,
  resource bounds, public URLs, timeouts, retention, and secret-file references.
- Secret contents are file-mounted or externally provisioned. Raw credential,
  private endpoint, proxy override, signer, production mode, withdrawal,
  transfer, or leverage variables are rejected or absent.

## Safe operation

`EXECUTION_MODE` permits only the compiled backtest, replay, paper, and shadow
states in the public application. Authenticated Binance Spot Testnet and Bybit
Demo engines remain separate, default-off, credential-isolated profiles with
exact compiled destinations. Keep `APP_FAIL_CLOSED=true`, UTC time, explicit
resource limits, and loopback host binding unless TLS is configured.

Missing, placeholder, malformed, duplicated, unknown, unsafe, or partial
configuration fails before listeners, database mutation, or outbound network
use. The configuration identity is a canonical hash and is part of every run,
artifact, safety manifest, and final candidate. Tests cover strict decode,
older-schema compatibility, prohibited modes/products, endpoint policy,
environment override rejection, and all supported Compose profile renders.
