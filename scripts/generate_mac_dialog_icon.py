#!/usr/bin/env python3
"""Build the approved koi icon's standard macOS representations without smoothing."""
import argparse
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parent.parent
SOURCE = ROOT / "docs/brand/mac-dialog-icon.png"
OUTPUT = ROOT / "apps/mac-dialog-decisions/resources/IkesuDecisionDialog.icns"
# Standard iconset names: 64px is the 32pt @2x representation.
REPRESENTATIONS = (
    ("icon_16x16.png", 16), ("icon_16x16@2x.png", 32),
    ("icon_32x32.png", 32), ("icon_32x32@2x.png", 64),
    ("icon_128x128.png", 128), ("icon_128x128@2x.png", 256),
    ("icon_256x256.png", 256), ("icon_256x256@2x.png", 512),
    ("icon_512x512.png", 512), ("icon_512x512@2x.png", 1024),
)


def generate(output):
    output = Path(output)
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="ikesu-icon-") as temp:
        iconset = Path(temp) / "IkesuDecisionDialog.iconset"
        iconset.mkdir()
        for name, size in REPRESENTATIONS:
            subprocess.run(["magick", str(SOURCE), "-filter", "point", "-resize",
                            f"{size}x{size}", "-strip", str(iconset / name)], check=True)
        subprocess.run(["iconutil", "-c", "icns", str(iconset), "-o", str(output)], check=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=OUTPUT)
    generate(parser.parse_args().output)
