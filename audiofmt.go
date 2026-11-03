//go:build windows

package main

// Canonical audio format for recordings: 48 kHz, stereo, PCM16. Endpoints may
// deliver anything (44.1 kHz mono electret mics are common), so every source is
// converted before mixing.

const (
	canonRate     = 48000
	canonChannels = 2
)

type srcConverter struct {
	rate int
	ch   int
	step float64 // input frames per output frame
	pos  float64 // fractional read position carried between chunks
	tail [canonChannels]int16
	has  bool
}

func newSrcConverter(rate, ch int) *srcConverter {
	c := &srcConverter{rate: rate, ch: ch, step: float64(rate) / canonRate}
	return c
}

// convert turns one packet of PCM16 in the device format into canonical PCM16.
func (c *srcConverter) convert(pcm []byte) []byte {
	if len(pcm) < 2 || c.ch <= 0 {
		return nil
	}
	frames := len(pcm) / (2 * c.ch)
	if frames == 0 {
		return nil
	}
	// 1) channel mapping -> stereo
	stereo := make([]int16, frames*canonChannels)
	for i := 0; i < frames; i++ {
		base := i * c.ch
		switch {
		case c.ch == 1:
			v := readS16(pcm, base)
			stereo[i*2] = v
			stereo[i*2+1] = v
		default:
			stereo[i*2] = readS16(pcm, base)
			stereo[i*2+1] = readS16(pcm, base+1)
		}
	}

	// 2) rate conversion -> 48 kHz
	if c.rate == canonRate {
		return s16ToBytes(stereo)
	}
	if c.has {
		stereo = append([]int16{c.tail[0], c.tail[1]}, stereo...)
		frames++
	}
	out := make([]int16, 0, frames*canonChannels+2)
	pos := c.pos
	for {
		i := int(pos)
		if i+1 >= frames {
			break
		}
		frac := pos - float64(i)
		for ch := 0; ch < canonChannels; ch++ {
			a := float64(stereo[i*canonChannels+ch])
			b := float64(stereo[(i+1)*canonChannels+ch])
			out = append(out, int16(a+(b-a)*frac))
		}
		pos += c.step
	}
	c.tail = [canonChannels]int16{stereo[(frames-1)*canonChannels], stereo[(frames-1)*canonChannels+1]}
	c.has = true
	c.pos = pos - float64(frames-1)
	if c.pos < 0 {
		c.pos = 0
	}
	return s16ToBytes(out)
}

func readS16(b []byte, sample int) int16 {
	i := sample * 2
	if i+1 >= len(b) {
		return 0
	}
	return int16(uint16(b[i]) | uint16(b[i+1])<<8)
}

func s16ToBytes(s []int16) []byte {
	out := make([]byte, len(s)*2)
	for i, v := range s {
		out[i*2] = byte(uint16(v))
		out[i*2+1] = byte(uint16(v) >> 8)
	}
	return out
}
