# ADR 0020: closed authenticated sandbox boundary

Status: accepted for V1C PR1.

Authenticated requests are high-level typed operations over compile-time
Testnet/Demo policies. Signed request types and serializers stay unexported.
Production constructors load only approved secret files and use one fixed
exchange-specific CONNECT proxy. Test constructors are package-private and
accept deterministic in-memory connectors.

This prevents an otherwise safe signer from becoming a generic private
request primitive. Request evidence is written before network I/O and retains
only the host, method, path, sorted field names, enumerated policy values, and
request hash.
