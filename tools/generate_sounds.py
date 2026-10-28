#!/usr/bin/env python3
"""Generate ikesu's original MIT-licensed notification tones and MIDI using stdlib only."""
import hashlib
import json
import math
from pathlib import Path
import struct
import wave

ROOT = Path(__file__).resolve().parents[1] / "internal/notify/sounds"
# Only duration/sample-rate/mean level guidelines came from the reference files.
# The phrases were picked from symbolic scores (ABC) that YuE2 generated for us
# (2026-10-17) and re-voiced by hand; the waveform below is synthesized here
# with no audio input.
SOUNDS = {
    # name: (rate, mean_db, notes as (midi, start_seconds, velocity, length_seconds or None))
    # done (affirmative): a Cmaj9 that bounces upward and grows louder, last note ringing.
    "done": (48000, -23.9, (
        (48, 0.000, 0.45, None), (55, 0.045, 0.55, None), (59, 0.090, 0.68, None),
        (62, 0.135, 0.82, None), (64, 0.180, 1.00, None))),
    # request (asking): a damped low tap, then an open sus2 rolled upward and fading out.
    "request": (44100, -23.2, (
        (57, 0.000, 0.90, 0.05), (62, 0.100, 0.65, None), (64, 0.120, 0.55, None),
        (69, 0.140, 0.45, None))),
}

# Mallet/glass-like timbre: harmonic and slightly inharmonic partials, each
# decaying exponentially. Harder strikes (higher velocity) brighten the upper
# partials, like a real mallet, so dynamics carry meaning.
PARTIALS = ((1.0, 1.00, 0.42), (2.0, 0.32, 0.22), (3.0, 0.12, 0.14), (4.18, 0.07, 0.09), (5.43, 0.03, 0.06))
ATTACK = 0.003
DAMP = 0.018  # release time constant after a damped (staccato) note's length
DETUNE_CENTS = 1.6
RING = 0.5
TAIL = 0.12
PEAK_LIMIT = 10 ** (-1.0 / 20)


def generate(name, rate, mean_db, notes):
    duration = max(start + (length or RING) for _, start, _, length in notes) + TAIL
    count = round(rate * duration)
    values = [0.0] * count
    for midi, start, velocity, length in notes:
        base = 440 * 2 ** ((midi - 69) / 12)
        bright = 0.35 + 0.9 * velocity
        first = round(start * rate)
        for index in range(first, count):
            t = (index - first) / rate
            envelope = min(1.0, t / ATTACK)
            if length is not None and t > length:
                envelope *= math.exp(-(t - length) / DAMP)
            sample = 0.0
            for order, (ratio, amp, tau) in enumerate(PARTIALS):
                decay = math.exp(-t / tau)
                if decay < 1e-4:
                    continue
                for cents in (-DETUNE_CENTS, DETUNE_CENTS):
                    f = base * ratio * 2 ** (cents / 1200)
                    sample += 0.5 * amp * bright ** order * decay * math.sin(2 * math.pi * f * t)
            values[index] += velocity * envelope * sample
    # A short feedback echo gives a little room without smearing the notes.
    delay = round(0.037 * rate)
    for index in range(delay, count):
        values[index] += 0.18 * values[index - delay]
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
    write_midi(ROOT / f"{name}.mid", notes, duration)
    return {
        "name": name, "sample_rate": rate, "frames": count,
        "duration": count / rate,
        "rms_dbfs": 20 * math.log10(math.sqrt(sum(value * value for value in pcm) / count) / 32768),
        "peak_dbfs": 20 * math.log10(max(abs(value) for value in pcm) / 32768),
        "sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
    }


def vlq(number):
    out = [number & 0x7F]
    number >>= 7
    while number:
        out.append(0x80 | (number & 0x7F))
        number >>= 7
    return bytes(reversed(out))


def write_midi(path, notes, duration):
    """Standard MIDI file (format 0, 480 ticks per quarter at 120 BPM: 960 ticks per second)."""
    events = []
    for midi, start, velocity, length in notes:
        end = start + length if length is not None else duration
        events.append((round(start * 960), 0x90, midi, max(1, round(127 * velocity))))
        events.append((round(end * 960), 0x80, midi, 0))
    events.sort(key=lambda event: (event[0], event[1] == 0x90))
    track = bytearray(vlq(0) + b"\xff\x51\x03" + (500000).to_bytes(3, "big"))
    last = 0
    for tick, status, midi, velocity in events:
        track += vlq(tick - last) + bytes((status, midi, velocity))
        last = tick
    track += vlq(0) + b"\xff\x2f\x00"
    path.write_bytes(b"MThd" + struct.pack(">IHHH", 6, 0, 1, 480) + b"MTrk" + struct.pack(">I", len(track)) + track)


if __name__ == "__main__":
    print(json.dumps([generate(name, *settings) for name, settings in SOUNDS.items()], indent=2))
