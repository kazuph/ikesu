#!/usr/bin/env python3
"""Verify the gtx-specific left/right layout in real-terminal captures."""
import json
import sys
from pathlib import Path

root = Path(sys.argv[1])
proof = []
widths = (26, 2, 0)
for mode in ("server", "no-session"):
    for left, lw in enumerate(widths):
        for right, rw in enumerate(widths):
            name = f"{mode}-left{left}-right{right}"
            frame = json.loads((root / f"{name}.json").read_text())
            cols, rows, cells = frame["Cols"], frame["Rows"], frame["Cells"]
            assert len(cells) == rows and all(len(row) == cols for row in cells), name
            if lw:
                assert all(row[lw-1]["Content"] == "│" for row in cells), (name, "left divider")
            if rw:
                assert all(row[cols-rw]["Content"] == "│" for row in cells), (name, "right divider")
            title = "".join(c["Content"] for c in cells[0][cols-rw+1:]) if rw else ""
            if rw > 2:
                assert "[agents]" in title and "[jobs]" in title, (name, title)
            if lw > 2:
                assert "[agents]" not in "".join(c["Content"] for row in cells for c in row[:lw]), name
            marker = f"SIZE_{left}{right}_{rows-4}_{cols-lw-rw-3}"
            assert marker in (root / f"{name}.txt").read_text(), (name, "kernel PTY width")
            proof.append({"capture": name, "left": lw, "right": rw, "pty_cols": cols-lw-rw-3, "status": "PASS"})
(root / "matrix-proof.json").write_text(json.dumps(proof, indent=2)+"\n")
print(f"PASS: {len(proof)} real-terminal captures, divider columns, headers, and kernel PTY dimensions")
