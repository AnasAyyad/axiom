# B1 formal qualification

**Decision date:** 2026-07-26

**Acceptance state:** Formally qualified

The immutable formal run is `/srv/axiom-data/qualification/b1-da47d14-r1`
from exact source `da47d143ac26806eb8f318d8e141f396d5576fea`.
Its terminal machine result is `qualified: true`, with no terminal failures.

## Qualification result

- Continuous duration: 72 hours.
- Verified records: 4,212,483 across 864 segment pairs.
- BTCUSDT resynchronization maximum: 2.057899018 seconds.
- ETHUSDT resynchronization maximum: 2.090901820 seconds.
- Over-15-second recovery samples: zero for both instruments.
- p95 resynchronization bucket: 5 seconds for both instruments.
- Final BTCUSDT and ETHUSDT books: healthy and eligible.

## Immutable artifact identity

- Terminal evidence SHA-256:
  `658d363c56d358f614db827c783bd2b91e83dcaf01b8251851191a855c628709`
- Rolling status SHA-256:
  `a5ca3c6dd158d6e8c08d353492b218aa5b885a310466c64d34a4a015e1adf563`
- Event journal SHA-256:
  `33c2e3c3e71e08e083dde4433bf9a9e0f857c85037e19f2d0f7c65b21967417d`
- Manifest hash:
  `3b465bfe0502987616bdd8e5f634876356949755a1d2bf992cc84437db64cd50`
- Replay checksum:
  `ab6ac00982f72c14577a97832aadff873ef8a27948bedfb25c626b1429817310`
- Journal terminal hash:
  `1000bdf3f4fe57f2e88641e21f830202dad5978729172710bb2a2861df4d211e`

Terminal JSON, status, journal, service logs, manifests, Parquet data, and
checksums remain unchanged in the retained qualification directory.

B1 formal qualification does not promote B2 or B3-B8. B2 retains its own open
72-hour qualification, and B3-B8 retain their own formal acceptance and
approver gates.
