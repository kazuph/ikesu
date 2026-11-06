#!/usr/bin/env python3
"""Generate Android koi icons from the approved Mac dialog bitmap, without smoothing."""
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parent.parent
SOURCE = ROOT / 'docs/brand/mac-dialog-icon.png'
RES = ROOT / 'apps/android-decisions/android/app/src/main/res'
DENSITIES = {'mdpi': 1, 'hdpi': 1.5, 'xhdpi': 2, 'xxhdpi': 3, 'xxxhdpi': 4}


def magick(*args):
    subprocess.run(['magick', *map(str, args)], check=True)


def main():
    with tempfile.TemporaryDirectory(prefix='ikesu-android-icon-') as temp:
        silhouette = Path(temp) / 'silhouette.png'
        # Remove the paper and dotted background, retaining the fish's source pixels.
        magick(SOURCE, '-alpha', 'on', '-fill', 'none', '-opaque', '#F4F0E6',
               '-opaque', '#DDE4D3', '-channel', 'RGBA', '-fill', 'white',
               '-colorize', '100', '+channel', silhouette)
        for density, scale in DENSITIES.items():
            mipmap = RES / f'mipmap-{density}'
            drawable = RES / f'drawable-{density}'
            mipmap.mkdir(parents=True, exist_ok=True)
            drawable.mkdir(parents=True, exist_ok=True)
            pixels = int(48 * scale)
            magick(SOURCE, '-filter', 'point', '-resize', f'{pixels}x{pixels}!',
                   '-strip', mipmap / 'ic_launcher.png')
            inner, outer = int(72 * scale), int(108 * scale)
            magick(SOURCE, '-filter', 'point', '-resize', f'{inner}x{inner}!',
                   '-background', '#F4F0E6', '-gravity', 'center',
                   '-extent', f'{outer}x{outer}', '-strip', mipmap / 'ic_launcher_foreground.png')
            pixels = int(24 * scale)
            magick(silhouette, '-filter', 'point', '-resize', f'{pixels}x{pixels}!',
                   '-strip', drawable / 'ic_notification.png')


if __name__ == '__main__':
    main()
