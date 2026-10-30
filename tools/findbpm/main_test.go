package main

import (
	"math"
	"testing"
)

// clicks returns a click track with a short noise burst on every beat.
func clicks(bpm, offset, seconds float64) []float32 {
	audio := make([]float32, int(seconds*sampleRate))
	seed := uint32(1)
	for t := offset; t < seconds; t += 60 / bpm {
		start := int(t * sampleRate)
		for i := 0; i < 200 && start+i < len(audio); i++ {
			seed = seed*1664525 + 1013904223
			noise := float32(seed>>8)/(1<<24)*2 - 1
			audio[start+i] = noise * float32(math.Exp(-float64(i)/50))
		}
	}
	return audio
}

func TestTempoAndFirstBeat(t *testing.T) {
	for _, tc := range []struct{ bpm, offset float64 }{
		{120, 0.25},
		{98, 0.4},
		{140, 0.1},
	} {
		flux := onsets(clicks(tc.bpm, tc.offset, 30))
		bpm, offset := fit(flux, tempo(flux))
		if math.Abs(bpm-tc.bpm) > 0.1 {
			t.Errorf("bpm %g: got %.2f", tc.bpm, bpm)
		}
		if math.Abs(offset-tc.offset) > 0.02 {
			t.Errorf("bpm %g offset %g: got %.3f", tc.bpm, tc.offset, offset)
		}
	}
}

func TestFFT(t *testing.T) {
	x := make([]complex128, 8)
	for i := range x {
		x[i] = complex(math.Cos(2*math.Pi*float64(i)/8), 0)
	}
	fft(x)
	for i, v := range x {
		want := 0.0
		if i == 1 || i == 7 {
			want = 4
		}
		if math.Abs(real(v)-want) > 1e-9 || math.Abs(imag(v)) > 1e-9 {
			t.Errorf("bin %d = %v, want %g", i, v, want)
		}
	}
}
