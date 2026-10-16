#!/usr/bin/env python3
"""Generate gtx's original MIT-licensed notification tones using stdlib only."""
import hashlib
import json
import math
from pathlib import Path
import struct
import wave

ROOT = Path(__file__).resolve().parents[1] / "internal/notify/sounds"
# Only duration/sample-rate/mean level were measured from the reference files.
# Pitches, mallet-like partials and envelopes are original, with no audio input.
SOUNDS = {
    # name: (rate, duration, mean_db, notes as (midi, start_seconds, velocity))
    # done: a soft two-note "pon-pin" (E5 then B5, a perfect fifth up).
    "done": (48000, 1.080000, -23.9, ((76, 0.00, 1.0), (83, 0.13, 0.85))),
    # request: two light taps then a brighter answer (G5, G5, D6) to ask for attention gently.
    "request": (44100, 1.462844, -23.2, ((79, 0.00, 0.8), (79, 0.17, 0.7), (86, 0.42, 1.0))),
}

# Mallet/glass-like timbre: harmonic and slightly inharmonic partials, each
# decaying exponentially; higher partials die out sooner so the tone stays soft.
PARTIALS = ((1.0, 1.00, 0.42), (2.0, 0.32, 0.22), (3.0, 0.12, 0.14), (4.18, 0.07, 0.09), (5.43, 0.03, 0.06))
ATTACK = 0.004
DETUNE_CENTS = 1.6
PEAK_LIMIT = 10 ** (-1.0 / 20)


def generate(name, rate, duration, mean_db, notes):
    count = round(rate * duration)
    values = [0.0] * count
    for midi, start, velocity in notes:
        base = 440 * 2 ** ((midi - 69) / 12)
        first = round(start * rate)
        for index in range(first, count):
            t = (index - first) / rate
            attack = min(1.0, t / ATTACK)
            sample = 0.0
            for ratio, amp, tau in PARTIALS:
                decay = math.exp(-t / tau)
                if decay < 1e-4:
                    continue
                for cents in (-DETUNE_CENTS, DETUNE_CENTS):
                    f = base * ratio * 2 ** (cents / 1200)
                    sample += 0.5 * amp * decay * math.sin(2 * math.pi * f * t)
            values[index] += velocity * attack * sample
    # A short feedback echo gives a little room without smearing the notes.
    delay = round(0.037 * rate)
    for index in range(delay, count):
        values[index] += 0.22 * values[index - delay]
    # Fade the very end so the file never stops on a click.
    fade = round(0.03 * rate)
    for index in range(count - fade, count):
        values[index] *= (count - index) / fade
    rms = math.sqrt(sum(value * value for value in values) / count)
    gain = 10 ** (mean_db / 20) / rms
    gain = min(gain, PEAK_LIMIT / max(abs(value) for value in values))
    scaled = [value * gain for value in values]
    assert max(abs(value) for value in scaled) < 1, "PCM clipping"
    pcm = [round(value * 32767) for value in scaled]
    ROOT.mkdir(parents=True, exist_ok=True)
    path = ROOT / f"{name}.wav"
    with wave.open(str(path), "wb") as output:
        output.setparams((1, 2, rate, count, "NONE", "not compressed"))
        output.writeframes(b"".join(struct.pack("<h", value) for value in pcm))
    return {
        "name": name, "sample_rate": rate, "frames": count,
        "duration": count / rate,
        "rms_dbfs": 20 * math.log10(math.sqrt(sum(value * value for value in pcm) / count) / 32768),
        "peak_dbfs": 20 * math.log10(max(abs(value) for value in pcm) / 32768),
        "sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
    }


if __name__ == "__main__":
    print(json.dumps([generate(name, *settings) for name, settings in SOUNDS.items()], indent=2))
