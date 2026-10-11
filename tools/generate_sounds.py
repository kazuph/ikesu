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
# Pitches and the Hann-windowed sine motif are original, with no audio input.
SOUNDS = {
    "done": (48000, 1.080000, -23.9, (72, 76, 79)),
    "request": (44100, 1.462844, -23.2, (74, 69, 74)),
}


def generate(name, rate, duration, mean_db, notes):
    count = round(rate * duration)
    values = []
    for index in range(count):
        note_index = min(len(notes) - 1, index * len(notes) // count)
        start = math.ceil(note_index * count / len(notes))
        end = math.ceil((note_index + 1) * count / len(notes))
        phase = (index - start) / max(1, end - start - 1)
        envelope = (1 - math.cos(2 * math.pi * phase)) / 2
        frequency = 440 * 2 ** ((notes[note_index] - 69) / 12)
        values.append(envelope * math.sin(2 * math.pi * frequency * (index - start) / rate))
    rms = math.sqrt(sum(value * value for value in values) / count)
    gain = 10 ** (mean_db / 20) / rms
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
