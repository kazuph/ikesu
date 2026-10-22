#!/usr/bin/env python3
"""Regenerate scalar-width ranges from the pinned Rust development oracle."""
import argparse
import json
from pathlib import Path
import re
import subprocess

ROOT = Path(__file__).resolve().parents[2]
NOTICE = "// License: third_party/unicode-width/LICENSE-MIT; generator: tools/unicode-width/generate.py"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    output = subprocess.check_output([
        "cargo", "run", "--quiet", "--locked", "--manifest-path",
        str(Path(__file__).with_name("Cargo.toml")),
    ], text=True)
    ranges = json.loads(output)
    updates = {"internal/cellwidth/testdata/unicode-width-0.2.2.json": output}
    for name, start, end, format_row in (
        ("internal/cellwidth/width.go", "var ranges = [...]widthRange{", "}",
         lambda r: "\t{%d, %d, %d}," % tuple(r)),
        ("apps/mac-dialog-decisions/unicode-width.swift", "let scalarWidthRanges: [(UInt32, UInt32, Int)] = [", "]",
         lambda r: "  (%d, %d, %d)," % tuple(r)),
    ):
        text = (ROOT / name).read_text()
        lines = text.splitlines()
        if NOTICE not in lines:
            lines.insert(1, NOTICE)
        text = "\n".join(lines) + "\n"
        pattern = re.escape(start) + r"\n.*?\n" + re.escape(end)
        replacement = start + "\n" + "\n".join(format_row(row) for row in ranges) + "\n" + end
        text, count = re.subn(pattern, lambda _: replacement, text, count=1, flags=re.S)
        if count != 1:
            raise RuntimeError("generated range boundaries not found: " + name)
        updates[name] = text
    for name, expected in updates.items():
        target = ROOT / name
        if args.check:
            if target.read_text() != expected:
                raise RuntimeError("generated file differs: " + name)
        else:
            target.write_text(expected)
    print(f"unicode-width 0.2.2: {len(ranges)} ranges; " + ("verified" if args.check else "generated"))


if __name__ == "__main__":
    main()
