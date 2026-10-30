// Command findbpm finds the tempo and the first beat of an mp3 or wav file.
// It needs ffmpeg to decode the audio.
package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/cmplx"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	sampleRate = 11025
	hop        = 128
	frameSize  = 1024
	fps        = float64(sampleRate) / hop
	minBPM     = 60
	maxBPM     = 200
	multiples  = 8
	// latency is how far an onset lags its frame start. A hit shows up
	// between the ends of the two frames compared.
	latency = (frameSize + hop/2) / float64(sampleRate)
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: findbpm <song.mp3|song.wav>")
		os.Exit(1)
	}
	path := os.Args[1]
	audio, err := decode(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(audio) < frameSize*2 {
		fmt.Fprintln(os.Stderr, "song is too short")
		os.Exit(1)
	}

	flux := onsets(audio)
	bpm, offset := fit(flux, tempo(flux))

	fmt.Printf("bpm:        %.1f\n", bpm)
	fmt.Printf("first beat: %.2fs\n", offset)
	fmt.Printf("dance %s %.1f %.2f\n", filepath.Base(path), bpm, offset)
}

// decode returns the song as mono samples at sampleRate.
func decode(path string) ([]float32, error) {
	out, err := exec.Command("ffmpeg", "-v", "error", "-i", path,
		"-ac", "1", "-ar", fmt.Sprint(sampleRate), "-f", "f32le", "-").Output()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg: %w", err)
	}
	audio := make([]float32, len(out)/4)
	for i := range audio {
		audio[i] = math.Float32frombits(binary.LittleEndian.Uint32(out[i*4:]))
	}
	return audio, nil
}

// onsets returns how much the spectrum grows in each frame, with the local
// average removed so that only sudden changes such as drum hits remain.
func onsets(audio []float32) []float64 {
	window := make([]float64, frameSize)
	for i := range window {
		window[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/(frameSize-1))
	}

	frames := (len(audio)-frameSize)/hop + 1
	bins := frameSize/2 + 1
	prev := make([]float64, bins)
	buf := make([]complex128, frameSize)
	flux := make([]float64, 0, frames-1)
	for f := range frames {
		for i := range buf {
			buf[i] = complex(float64(audio[f*hop+i])*window[i], 0)
		}
		fft(buf)
		var sum float64
		for b := range bins {
			mag := math.Log1p(100 * cmplx.Abs(buf[b]))
			if f > 0 {
				sum += max(mag-prev[b], 0)
			}
			prev[b] = mag
		}
		if f > 0 {
			flux = append(flux, sum)
		}
	}

	const avg = 32
	out := make([]float64, len(flux))
	for i := range flux {
		var sum float64
		for j := i - avg/2; j < i+avg/2; j++ {
			if j >= 0 && j < len(flux) {
				sum += flux[j]
			}
		}
		out[i] = max(flux[i]-sum/avg, 0)
	}
	return out
}

// tempo returns the rough bpm whose beat and its multiples best match how
// the onsets repeat.
func tempo(flux []float64) float64 {
	ac := make([]float64, int(math.Ceil(60*fps/minBPM*multiples))+2)
	for lag := range ac {
		for i := 0; i+lag < len(flux); i++ {
			ac[lag] += flux[i] * flux[i+lag]
		}
	}
	score := func(bpm float64) float64 {
		lag := 60 * fps / bpm
		var s float64
		for k := 1; k <= multiples; k++ {
			s += interp(ac, lag*float64(k)) / math.Sqrt(float64(k))
		}
		return s
	}

	bpm, _ := best(minBPM, maxBPM, 0.5, score)
	return bpm
}

// fit returns the bpm near rough and the time of the first beat where a beat
// grid lines up best with the onsets over the whole song.
func fit(flux []float64, rough float64) (float64, float64) {
	bpm, _ := best(rough-0.5, rough+0.5, 0.01, func(bpm float64) float64 {
		_, s := firstBeat(flux, bpm)
		return s
	})
	offset, _ := firstBeat(flux, bpm)
	return bpm, offset
}

// firstBeat returns the time of the first beat for a beat grid at bpm, and
// how well that grid lines up with the onsets.
func firstBeat(flux []float64, bpm float64) (float64, float64) {
	period := 60 / bpm
	end := float64(len(flux))/fps + latency
	return best(0, period, period/200, func(offset float64) float64 {
		var s float64
		for t := offset; t < end; t += period {
			s += interp(flux, (t-latency)*fps)
		}
		return s
	})
}

// best returns the value from start to end by step with the highest score,
// and that score.
func best(start, end, step float64, score func(float64) float64) (float64, float64) {
	top, topScore := start, math.Inf(-1)
	for v := start; v < end; v += step {
		if s := score(v); s > topScore {
			top, topScore = v, s
		}
	}
	return top, topScore
}

// interp reads xs at a fractional index with linear interpolation.
func interp(xs []float64, x float64) float64 {
	i := int(x)
	if i < 0 || i+1 >= len(xs) {
		return 0
	}
	frac := x - float64(i)
	return xs[i]*(1-frac) + xs[i+1]*frac
}

// fft is an in place radix 2 FFT. len(x) must be a power of 2.
func fft(x []complex128) {
	n := len(x)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			x[i], x[j] = x[j], x[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		w := cmplx.Exp(complex(0, -2*math.Pi/float64(size)))
		for start := 0; start < n; start += size {
			wk := complex(1, 0)
			for k := range size / 2 {
				a, b := x[start+k], x[start+k+size/2]*wk
				x[start+k], x[start+k+size/2] = a+b, a-b
				wk *= w
			}
		}
	}
}
