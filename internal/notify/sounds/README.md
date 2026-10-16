# gtx original notification sounds (MIT)

店長の決定（2026-10-11）: ライセンスの都合で herdr の音は使わず gtx 独自の音。
These generated WAV files and their generator are covered by the repository's
MIT license. No herdr waveform, melody, decoded samples, or source code is used.

Regenerate from the repository root with `python3 tools/generate_sounds.py`.
The generator uses only Python's standard library and takes no audio input.
Done is a soft two-note chime (E5 then B5); request is two light taps on G5
followed by D6. Each note is a mallet-like sum of decaying partials with a
short attack, a light 37 ms echo and an end fade (replaced 2026-10-17 because
the earlier flat sine motif was unpleasant).
Pitches use twelve-tone equal temperament with A4 = 440 Hz.

Only the reference files' numeric duration, sample rate, and mean dBFS were
used as volume/length guidelines: done 1.080000 s, 48000 Hz, -23.9 dBFS;
request 1.462844 s, 44100 Hz, -23.2 dBFS. gtx's files are mono PCM16;
the request duration is rounded to the nearest sample. The motifs and envelopes
are independently synthesized. Peak levels follow from these new waveforms.

The binary embeds both files. Default playback materializes a content-hash
filename under the OS cache directory's binary namespace `sounds` directory,
with directories 0700 and files 0600. Existing hashes are reused. Custom paths
retain priority. Cache failures skip sound and warn once while notifications
and input continue. Verification uses numeric WAV checks and an injected
playback command; never play these files through the user's speakers.
