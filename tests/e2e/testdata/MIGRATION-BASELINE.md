# Migration baseline

`migration-baseline-49f0363.tar.gz` contains the actual pre-migration source
from commit `49f0363a3bcd74aa96c7daa3059b0c56dcc6cf0d`, with its LICENSE,
Go module files, command, internal packages and local third-party modules.
The migration tests build that source as `gtx-2e01183`; they do not emulate
the old server or change the current product's migration behavior.

Reproduce the fixture from a checkout containing that commit:

```sh
git archive --format=tar.gz --output=tests/e2e/testdata/migration-baseline-49f0363.tar.gz 49f0363a3bcd74aa96c7daa3059b0c56dcc6cf0d go.mod go.sum cmd internal third_party LICENSE
```

SHA-256: `01d9713a735b3e6b9dde1f53bc4df5c933bb74213bf35729aebbbb9a07812fe5`.

The fixture is embedded in the E2E test binary. Running from a source archive
therefore needs neither Git history nor an external baseline executable.
The build lives in TestMain's directory until the entire E2E suite exits,
so one test's cleanup cannot remove another test's baseline. An explicitly
supplied `IKESU_MIGRATION_BASE_BIN` still selects the caller's baseline.
