# TLS and secrets

Public ports bind to `127.0.0.1` by default. External access requires the
profile-gated Caddy edge or an equivalently reviewed TLS proxy, a valid hostname,
trusted certificate, HTTPS public base URL/origin, strict forwarding policy, and
firewall rules that expose only the edge. Direct API, database, metrics,
dashboard, proxy, and engine ports remain private.

Secrets are never committed, placed in `.env`, arguments, generated Compose,
fixtures, screenshots, logs, traces, metrics, exports, or evidence bundles.
Provision independent random files with the service-specific UID/GID and mode
documented in `deploy/README.md`, or use an external secret manager. Each
service receives only required files. Exchange engines never receive user/TOTP
or other-exchange secrets; API, browser, public roles, proxy, and observers
receive no exchange credential.

Session cookies are host-only, `HttpOnly`, `SameSite=Strict`, and `Secure`
outside local deployment. High-risk sandbox controls require password/TOTP,
purpose-bound one-use authorization, role checks, exact revision, expiry, and
audit. There is no recovery-code or environment bypass.

Startup rejects missing, empty, placeholder, broad-permission, raw environment,
or incompatible secrets and any endpoint/proxy override. Redaction tests,
secret scans, API projection tests, image scans, and request-capture tests prove
that values, signatures, authorization headers, and unrestricted private
payloads do not leave their boundary. Rotation invalidates affected sessions or
credentials and requires reconciliation before readiness.
