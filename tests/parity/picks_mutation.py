#!/usr/bin/env python3
"""Prove that release-registration rollback is protected by a failing test."""
import json
from pathlib import Path
import subprocess
import tempfile

source = Path("internal/distribution/install.go").resolve()
original = source.read_text()
needle = "i.Store.RestoreRegistration(expected, previous)"
assert original.count(needle) == 1
with tempfile.TemporaryDirectory(prefix="ikesu-rollback-mutation-") as work:
    work = Path(work)
    mutant = work / "install.go"
    mutant.write_text(original.replace(
        needle,
        "func(_ plugin.Installed, _ *plugin.Installed) error { return nil }(expected, previous)",
    ))
    overlay = work / "overlay.json"
    overlay.write_text(json.dumps({"Replace": {str(source): str(mutant)}}))
    result = subprocess.run([
        "go", "test", "-overlay", str(overlay), "./internal/distribution",
        "-run", "^TestRegistrationFailureRestoresFilesAndPreservesConcurrentOperations$/^saved-response-lost$",
        "-count=1", "-v",
    ], text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    print(result.stdout, end="")
    assert result.returncode != 0, "rollback deletion survived the regression test"
    assert "registration rollback did not remove own write" in result.stdout, "mutation did not fail for the expected behavioral reason"
    print("PASS: deleting registration restoration fails the saved-response-loss assertion")
