The unchanged signed snapshot originals in snapshots.json come from
`github.com/relux-works/swarma-envelope`, version
`v0.0.0-20261026200550-5636986d0de5`, under
`vectors/swarm-envelope-2/{0-negative,1-positive}`. The fixture field retains the
first source vector name for each distinct exact original. The wire field is
standard base64 carriage of those original bytes. The root, issued and entries
fields are descriptive metadata; the tests verify the original wire itself.

Canonical module ZIP h1:
`h1:Dy8UwCgPb9Ak8VTh4q9fprGzWd7XH1o/7p7xN2VnxQM=`.

These are public, synthetic conformance vectors licensed under Apache-2.0;
see LICENSE. No private key material is present. The local tests only verify
existing signatures; they create no signing key or live grant.
