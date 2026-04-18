package sounds

import "math"

/*
Biquad EQ — three-band equaliser (low shelf, peaking mid, high shelf).

# Difference equation (Direct Form I)

	y[n] = b0·x[n] + b1·x[n-1] + b2·x[n-2]
	                - a1·y[n-1] - a2·y[n-2]

x[n] is the current input sample (normalised to [−1, 1]). y[n] is the output.
x1/x2 and y1/y2 hold the one- and two-sample histories. State is per-BiquadFilter
instance so each audio track carries its own EQ state independently.

# Transfer function

Taking the Z-transform of the difference equation:

	H(z) = (b0 + b1·z⁻¹ + b2·z⁻²) / (1 + a1·z⁻¹ + a2·z⁻²)

A filter is stable when both poles of H(z) lie strictly inside the unit circle.
The cookbook coefficient formulas below guarantee this for all valid inputs.

# Coefficient derivation — bilinear transform

The coefficient formulas come from Robert Bristow-Johnson's Audio EQ Cookbook.
Each filter starts as an analog prototype in the s-domain (a second-order resonator
or shelving curve). The bilinear transform

	s = 2·fₛ · (1 − z⁻¹) / (1 + z⁻¹)

maps the entire left half s-plane into the unit circle, converting every stable
analog filter into a stable discrete one. The transform compresses high frequencies
toward Nyquist (frequency warping), which the cookbook corrects by pre-warping
ω₀ = 2π·f₀/fₛ before computing coefficients.

# The A = 10^(dB/40) convention

	Standard amplitude gain:      linear = 10^(dBgain/20)
	Cookbook A for peak/shelf:        A  = sqrt(10^(dBgain/20)) = 10^(dBgain/40)

The square-root form is used because the two-pole filter structure applies gain
symmetrically — once in the numerator, once reflected in the denominator — so the
physical amplitude gain at the target frequency is A², which equals 10^(dBgain/20)
as intended. cookbookA() encapsulates this. Do not substitute a plain 10^(dB/20)
conversion; the resulting filter gain would be double the requested value in dB.

When gainDB = 0, A = 1 and every coefficient formula collapses to b₀=a₀, b₁=a₁,
b₂=a₂, giving H(z) = 1 (identity). This can be verified algebraically for all
three filter types.

# Q and bandwidth (peaking filter)

Q controls the width of the boost/cut bell:

	BW_Hz ≈ f₀ / Q   (approximate at moderate gains)

Higher Q → narrower peak. At Q = 1 and f₀ = 1 kHz the affected band spans roughly
one octave (≈700 Hz–1.4 kHz). For sounds EQ, Q values of 0.7–2.0 are typical.
Very high Q (> 4) risks audible ringing artefacts on speech transients.

# Shelf slope S

The low/high shelf formulas use shelf slope S (hardcoded to 1.0). S = 1 gives the
steepest monotonically rising/falling transition without an overshoot bump at the
corner frequency. S < 1 produces a gentler slope; S > 1 is not meaningful for a
single second-order section and would require cascaded filter stages.
*/

type EQSettings struct {
	BassGain float32
	BassHz   float32 // low shelf corner frequency

	MidGain float32
	MidHz   float32 // peaking filter center frequency
	MidQ    float32 // peaking filter Q — higher = narrower band (typical range 0.7–2.0)

	TrebleGain float32
	TrebleHz   float32 // high shelf corner frequency

	PresenceGain float32
	PresenceHz   float32 // peaking filter center frequency (2–5 kHz)
	PresenceQ    float32 // bandwidth Q (same range as MidQ)
}

func (eq *EQSettings) SetDefaults() {
	eq.BassGain = 0.0
	eq.BassHz = 120.0

	eq.MidGain = 0.0
	eq.MidHz = 1000.0
	eq.MidQ = 1.0

	eq.TrebleGain = 0.0
	eq.TrebleHz = 5000.0

	eq.PresenceGain = 0.0
	eq.PresenceHz = 3000.0
	eq.PresenceQ = 1.0
}

type EQ struct {
	Settings EQSettings
	filters  [4]BiquadFilter
}

func (eq *EQ) UpdateFilters(sampleRate float32) {
	eq.filters[0].SetLowShelf(sampleRate, eq.Settings.BassHz, eq.Settings.BassGain)
	eq.filters[1].SetPeaking(sampleRate, eq.Settings.MidHz, eq.Settings.MidQ, eq.Settings.MidGain)
	eq.filters[2].SetHighShelf(sampleRate, eq.Settings.TrebleHz, eq.Settings.TrebleGain)
	eq.filters[3].SetPeaking(sampleRate, eq.Settings.PresenceHz, eq.Settings.PresenceQ, eq.Settings.PresenceGain)
}

func (eq *EQ) Apply(frame []float32) {
	eq.filters[0].Apply(frame)
	eq.filters[1].Apply(frame)
	eq.filters[2].Apply(frame)
	eq.filters[3].Apply(frame)
}

// BiquadFilter is a single second-order IIR section (Direct Form I).
// b0–b2 are feed-forward (numerator) coefficients; a1–a2 are feed-back
// (denominator) coefficients pre-divided by a0 at coefficient-set time.
// x1/x2 hold the previous two input samples; y1/y2 the previous two outputs.
type BiquadFilter struct {
	b0, b1, b2, a1, a2 float32
	x1, x2, y1, y2     float32
}

// MagResponse returns the magnitude of H(e^jω) at the normalized angular
// frequency omega (0 = DC, π = Nyquist). Used for plotting the EQ curve.
func (bq *BiquadFilter) MagResponse(omega float64) float64 {
	cosW := math.Cos(omega)
	cos2W := math.Cos(2 * omega)
	sinW := math.Sin(omega)
	sin2W := math.Sin(2 * omega)

	numR := float64(bq.b0) + float64(bq.b1)*cosW + float64(bq.b2)*cos2W
	numI := -float64(bq.b1)*sinW - float64(bq.b2)*sin2W
	denR := 1.0 + float64(bq.a1)*cosW + float64(bq.a2)*cos2W
	denI := -float64(bq.a1)*sinW - float64(bq.a2)*sin2W

	numMag := math.Sqrt(numR*numR + numI*numI)
	denMag := math.Sqrt(denR*denR + denI*denI)
	if denMag < 1e-10 {
		return 1.0
	}
	return numMag / denMag
}

// MagResponseDB returns the combined magnitude response in dB of all four EQ
// filters at freqHz, using the given sample rate. Call UpdateFilters before use.
func (eq *EQ) MagResponseDB(freqHz, sampleRate float64) float64 {
	omega := 2.0 * math.Pi * freqHz / sampleRate
	mag := eq.filters[0].MagResponse(omega) *
		eq.filters[1].MagResponse(omega) *
		eq.filters[2].MagResponse(omega) *
		eq.filters[3].MagResponse(omega)
	if mag <= 0 {
		return -96.0
	}
	return 20.0 * math.Log10(mag)
}

func (bq *BiquadFilter) Apply(frame []float32) {
	for i, x0 := range frame {
		y0 := bq.b0*x0 + bq.b1*bq.x1 + bq.b2*bq.x2 - bq.a1*bq.y1 - bq.a2*bq.y2
		bq.x2 = bq.x1
		bq.x1 = x0
		bq.y2 = bq.y1
		bq.y1 = y0
		frame[i] = y0
	}
}

// ── RBJ Audio EQ Cookbook coefficient formulas ───────────────────────────────

// clampFreq keeps freqHz inside [20 Hz, Nyquist − 1 Hz] so ω₀ never reaches
// 0 or π, which would make the coefficient formulas ill-conditioned.
func clampFreq(freqHz, sampleRate float32) float32 {
	minHz := float32(20.0)
	maxHz := sampleRate*0.5 - 1.0
	if freqHz < minHz {
		return minHz
	}
	if freqHz > maxHz {
		return maxHz
	}
	return freqHz
}

// cookbookA returns the A coefficient used by the RBJ peaking and shelving
// formulas: A = 10^(dBgain/40) = sqrt(linearAmplitudeGain).
// See the theory block above for why this square-root form is used.
// Do not replace with a standard dB→amplitude conversion (÷20); that would
// produce twice the intended gain in dB.
func cookbookA(db float32) float64 {
	return math.Pow(10.0, float64(db)/40.0)
}

// SetPeaking configures a bell-shaped boost or cut centred at freqHz.
// q controls bandwidth: BW_Hz ≈ freqHz/q (typical range 0.7–2.0).
// gainDB > 0 boosts; gainDB < 0 cuts; gainDB = 0 is identity.
func (bq *BiquadFilter) SetPeaking(sampleRate, freqHz, q, gainDB float32) {
	if sampleRate <= 0 {
		return
	}
	freqHz = clampFreq(freqHz, sampleRate)
	if q <= 0 {
		q = 1.0
	}

	A := cookbookA(gainDB)
	w0 := 2.0 * math.Pi * float64(freqHz) / float64(sampleRate)
	cosw0 := math.Cos(w0)
	sinw0 := math.Sin(w0)
	alpha := sinw0 / (2.0 * float64(q))

	b0 := 1.0 + alpha*A
	b1 := -2.0 * cosw0
	b2 := 1.0 - alpha*A
	a0 := 1.0 + alpha/A
	a1 := -2.0 * cosw0
	a2 := 1.0 - alpha/A

	bq.b0 = float32(b0 / a0)
	bq.b1 = float32(b1 / a0)
	bq.b2 = float32(b2 / a0)
	bq.a1 = float32(a1 / a0)
	bq.a2 = float32(a2 / a0)
}

// SetLowShelf boosts or cuts all frequencies below freqHz by gainDB.
// Above the corner frequency the filter is unity gain. gainDB = 0 is identity.
func (bq *BiquadFilter) SetLowShelf(sampleRate, freqHz, gainDB float32) {
	if sampleRate <= 0 {
		return
	}
	freqHz = clampFreq(freqHz, sampleRate)

	A := cookbookA(gainDB)
	w0 := 2.0 * math.Pi * float64(freqHz) / float64(sampleRate)
	cosw0 := math.Cos(w0)
	sinw0 := math.Sin(w0)

	// Shelf slope S = 1.0 for a sensible default.
	S := 1.0
	alpha := sinw0 / 2.0 * math.Sqrt((A+1.0/A)*(1.0/S-1.0)+2.0)
	twoSqrtAAlpha := 2.0 * math.Sqrt(A) * alpha

	b0 := A * ((A + 1.0) - (A-1.0)*cosw0 + twoSqrtAAlpha)
	b1 := 2.0 * A * ((A - 1.0) - (A+1.0)*cosw0)
	b2 := A * ((A + 1.0) - (A-1.0)*cosw0 - twoSqrtAAlpha)
	a0 := (A + 1.0) + (A-1.0)*cosw0 + twoSqrtAAlpha
	a1 := -2.0 * ((A - 1.0) + (A+1.0)*cosw0)
	a2 := (A + 1.0) + (A-1.0)*cosw0 - twoSqrtAAlpha

	bq.b0 = float32(b0 / a0)
	bq.b1 = float32(b1 / a0)
	bq.b2 = float32(b2 / a0)
	bq.a1 = float32(a1 / a0)
	bq.a2 = float32(a2 / a0)
}

// SetHighShelf boosts or cuts all frequencies above freqHz by gainDB.
// Below the corner frequency the filter is unity gain. gainDB = 0 is identity.
func (bq *BiquadFilter) SetHighShelf(sampleRate, freqHz, gainDB float32) {
	if sampleRate <= 0 {
		return
	}
	freqHz = clampFreq(freqHz, sampleRate)

	A := cookbookA(gainDB)
	w0 := 2.0 * math.Pi * float64(freqHz) / float64(sampleRate)
	cosw0 := math.Cos(w0)
	sinw0 := math.Sin(w0)

	// Shelf slope S = 1.0 for a sensible default.
	S := 1.0
	alpha := sinw0 / 2.0 * math.Sqrt((A+1.0/A)*(1.0/S-1.0)+2.0)
	twoSqrtAAlpha := 2.0 * math.Sqrt(A) * alpha

	b0 := A * ((A + 1.0) + (A-1.0)*cosw0 + twoSqrtAAlpha)
	b1 := -2.0 * A * ((A - 1.0) + (A+1.0)*cosw0)
	b2 := A * ((A + 1.0) + (A-1.0)*cosw0 - twoSqrtAAlpha)
	a0 := (A + 1.0) - (A-1.0)*cosw0 + twoSqrtAAlpha
	a1 := 2.0 * ((A - 1.0) - (A+1.0)*cosw0)
	a2 := (A + 1.0) - (A-1.0)*cosw0 - twoSqrtAAlpha

	bq.b0 = float32(b0 / a0)
	bq.b1 = float32(b1 / a0)
	bq.b2 = float32(b2 / a0)
	bq.a1 = float32(a1 / a0)
	bq.a2 = float32(a2 / a0)
}
