# ikesu original notification sounds (MIT)

店長の決定（2026-10-11）: ライセンスの都合で herdr の音は使わず ikesu 独自の音。
These generated WAV and MIDI files and their generator are covered by the
repository's MIT license. No herdr waveform, melody, decoded samples, or source
code is used.

Regenerate from the repository root with `python3 tools/generate_sounds.py`.
The generator uses only Python's standard library and takes no audio input; it
writes `done.wav`/`request.wav` (embedded in the binary) and `done.mid`/
`request.mid` (the same notes as Standard MIDI files).

Replaced on 2026-10-17 at 店長's request. Meaning is carried by how the notes
are played, not only by the pitches:

- done (affirmative, 0.8 s): Cmaj9 (C3 G3 B3 D4 E4) bouncing upward 45 ms apart
  and growing louder, the last note ringing.
- request (asking, 0.76 s): a damped low tap on A3, then an open sus2
  (D4 E4 A4) rolled upward and fading out, left unresolved.

Each note is a mallet-like sum of decaying partials; harder strikes brighten the
upper partials, and damped notes are cut with an 18 ms release. A light 37 ms
echo and an end fade follow. Pitches use twelve-tone equal temperament with
A4 = 440 Hz.

Provenance of the phrases: candidate phrases and chord colors came from symbolic
scores (ABC notation) that YuE2 (multimodal-art-projection, code Apache-2.0,
weights CC BY-NC 4.0 with the authors' added permission for individuals) generated
locally on 2026-10-17 for short notification-jingle prompts. Only the note choices
were taken, re-voiced and articulated by hand; no YuE2 audio is included.

Only the reference files' sample rate and mean dBFS are kept as level
guidelines: done 48000 Hz, -23.9 dBFS; request 44100 Hz, -23.2 dBFS. ikesu's
files are mono PCM16. Peak levels follow from these waveforms.

The binary embeds both WAV files. Default playback materializes a content-hash
filename under the OS cache directory's binary namespace `sounds` directory,
with directories 0700 and files 0600. Existing hashes are reused. Custom paths
retain priority. Cache failures skip sound and warn once while notifications
and input continue. Verification uses numeric WAV checks and an injected
playback command; never play these files through the user's speakers.
