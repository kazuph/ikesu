#!/usr/bin/env python3
"""Check per-repository Git-result work using real repos and Go coverage counts."""
import pathlib
import re
import subprocess
import tempfile

root = pathlib.Path(__file__).resolve().parent.parent
source = root / "internal/tui/sidebar_git.go"
lines = source.read_text().splitlines()
start = next(i for i, line in enumerate(lines, 1) if line.startswith("func (a *App) currentWorkspaceGitTarget("))
checks = []
with tempfile.TemporaryDirectory(prefix="ikesu-git-scale-") as tmp:
    for count in (4, 8):
        profile = pathlib.Path(tmp) / f"{count}.cover"
        command = ["go", "test", "./internal/tui", "-count=1", "-v", "-covermode=count", f"-coverprofile={profile}", "-run", f"^TestWorkspaceGitApplyWorkPerRepository/{count}$"]
        subprocess.run(command, cwd=root, check=True)
        matches = []
        for row in profile.read_text().splitlines():
            match = re.match(r".*/internal/tui/sidebar_git\.go:(\d+)\.\d+,\d+\.\d+ \d+ (\d+)$", row)
            if match and int(match.group(1)) == start + 1:
                matches.append(int(match.group(2)))
        if len(matches) != 1:
            raise SystemExit(f"could not identify target-validation count for {count} repositories: {matches}")
        checks.append((count, matches[0]))
        print(f"repositories={count} target_validations={matches[0]}", flush=True)
for count, actual in checks:
    if actual != count:
        raise SystemExit(f"each independent repository must validate its own workspace once: repositories={count} validations={actual}")
print("PASS: each Git result validated only its own workspace")
