---
name: Audio mono left-ear bug
description: oto initialized with 1 channel causes audio to only play in left ear on headphones
type: feedback
---

Audio plays only in left ear when oto context is initialized with numChannels=1 (mono). OS does not upmix mono to stereo on headphones. Fix by initializing oto with 2 channels and duplicating mono samples to L+R before writing to the player.

**Why:** Discovered during debugging of voice/sounds playback.
**How to apply:** Any time oto context channel count is touched, keep it at 2. Opus encode/decode stays mono (numChannels=1) — the stereo upmix only happens at the oto write step.
