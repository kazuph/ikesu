# unicode-width scalar tables

The Go table in `internal/cellwidth/width.go` and Swift table in
`apps/mac-dialog-decisions/unicode-width.swift` are generated from
[unicode-width 0.2.2](https://crates.io/crates/unicode-width/0.2.2), using
`UnicodeWidthChar::width` for every valid Unicode scalar. `None` becomes zero;
width one is the implicit default. The generator emits 522 non-default ranges.

The upstream crate offers MIT OR Apache-2.0. These generated tables are used
under MIT. `LICENSE-MIT` preserves the upstream copyright and permission notice.
The Mac-dialog release includes this directory and the same notice.

The development-only Rust oracle is in `tools/unicode-width/src/main.rs`, with
the exact crate version and checksum pinned by Cargo.toml and Cargo.lock.
From the repository root, with Cargo and Python 3 installed:

```sh
python3 tools/unicode-width/generate.py
python3 tools/unicode-width/generate.py --check
```

This regenerates both source tables and the JSON oracle used by the exhaustive
Go and Swift scalar-width checks. It does not change the width lookup functions.
No Rust dependency is added to the shipped applications.
