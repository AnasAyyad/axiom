# Backup image third-party notices

The final backup image contains only the Axiom backup executable, PostgreSQL
18.4 client tools, their required shared libraries, and the CA certificate
bundle. It does not contain the Alpine package database, a shell, a package
manager, `gosu`, or the PostgreSQL server.

The component identities and SPDX license identifiers are recorded in
`runtime-components.json`. PostgreSQL's complete license text is copied from
the checksum-verified source archive to
`/usr/share/axiom/licenses/PostgreSQL.txt` in the image. The remaining runtime
components use the MPL-2.0, BSD-2-Clause, MIT, Apache-2.0, Zlib, or
BSD-3-Clause licenses identified by that manifest.

The manifest is an inventory and provenance aid, not a replacement for the
corresponding upstream license terms.
