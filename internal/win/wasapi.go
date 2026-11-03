//go:build windows

package win

import (
	"errors"
	"fmt"
	"math"
	"unsafe"
)

// WASAPI loopback capture: records what the speakers are playing, which is what
// "со звуком" means for a screen recording. No drivers, no CGO.

var (
	CLSIDMMDeviceEnumerator    = GUID{0xBCDE0395, 0xE52F, 0x467C, [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}}
	IIDIMMDeviceEnumerator     = GUID{0xA95664D2, 0x9614, 0x4F35, [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}}
	IIDIAudioClient            = GUID{0x1CB9AD4C, 0xDBFA, 0x4C32, [8]byte{0xB1, 0x78, 0xC2, 0xF5, 0x68, 0xA7, 0x03, 0xB2}}
	IIDIAudioCaptureClient     = GUID{0xC8ADBD64, 0xE71E, 0x48A0, [8]byte{0xA4, 0xDE, 0x18, 0x5C, 0x39, 0x5C, 0xD3, 0x17}}
	KSDATAFORMAT_SUBTYPE_PCM   = GUID{0x00000001, 0x0000, 0x0010, [8]byte{0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71}}
	KSDATAFORMAT_SUBTYPE_FLOAT = GUID{0x00000003, 0x0000, 0x0010, [8]byte{0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71}}
)

const (
	AUDCLNT_STREAMFLAGS_LOOPBACK = 0x00020000
	AUDCLNT_BUFFERFLAGS_SILENT   = 0x00000002
	eRender                      = 0
	eConsole                     = 0
	WAVE_FORMAT_PCM              = 1
	WAVE_FORMAT_IEEE_FLOAT       = 3
	WAVE_FORMAT_EXTENSIBLE       = 0xFFFE
)

type waveFormatEx struct {
	FormatTag      uint16
	Channels       uint16
	SamplesPerSec  uint32
	AvgBytesPerSec uint32
	BlockAlign     uint16
	BitsPerSample  uint16
	CBSize         uint16
}

// AudioFormat describes the PCM we deliver to the encoder.
type AudioFormat struct {
	Rate     int
	Channels int
}

// AudioCapture is a started loopback stream.
type AudioCapture struct {
	enum    unsafe.Pointer
	dev     unsafe.Pointer
	client  unsafe.Pointer
	capture unsafe.Pointer
	Fmt     AudioFormat
	isFloat bool
	buf     []byte
}

// OpenLoopbackAudio captures the default playback device.
func OpenLoopbackAudio() (*AudioCapture, error) {
	a := &AudioCapture{}
	enum, hr := ComCreateInstance(CLSIDMMDeviceEnumerator, IIDIMMDeviceEnumerator)
	if HRFailed(hr) || enum == nil {
		return nil, fmt.Errorf("device enumerator 0x%08x", uint32(hr))
	}
	a.enum = enum

	// IMMDeviceEnumerator::GetDefaultAudioEndpoint (index 4)
	var dev unsafe.Pointer
	hr = comCall(enum, 4, eRender, eConsole, uintptr(unsafe.Pointer(&dev)))
	if HRFailed(hr) || dev == nil {
		a.Close()
		return nil, fmt.Errorf("no default playback device 0x%08x", uint32(hr))
	}
	a.dev = dev

	// IMMDevice::Activate (index 3)
	var client unsafe.Pointer
	iid := IIDIAudioClient
	hr = comCall(dev, 3, uintptr(unsafe.Pointer(&iid)), CLSCTX_ALL, 0, uintptr(unsafe.Pointer(&client)))
	if HRFailed(hr) || client == nil {
		a.Close()
		return nil, fmt.Errorf("IAudioClient 0x%08x", uint32(hr))
	}
	a.client = client

	// IAudioClient::GetMixFormat (index 8)
	var wfx unsafe.Pointer
	hr = comCall(client, 8, uintptr(unsafe.Pointer(&wfx)))
	if HRFailed(hr) || wfx == nil {
		a.Close()
		return nil, fmt.Errorf("GetMixFormat 0x%08x", uint32(hr))
	}
	format := (*waveFormatEx)(wfx)
	a.Fmt.Rate = int(format.SamplesPerSec)
	a.Fmt.Channels = int(format.Channels)

	tag := format.FormatTag
	a.isFloat = tag == WAVE_FORMAT_IEEE_FLOAT
	if tag == WAVE_FORMAT_EXTENSIBLE && format.CBSize >= 22 {
		// WAVEFORMATEXTENSIBLE: SubFormat GUID sits after the base struct
		sub := (*GUID)(unsafe.Add(wfx, unsafe.Sizeof(waveFormatEx{})))
		a.isFloat = *sub == KSDATAFORMAT_SUBTYPE_FLOAT
	}
	if a.Fmt.Rate <= 0 || a.Fmt.Channels <= 0 {
		pCoTaskMemFree.Call(uintptr(wfx))
		a.Close()
		return nil, errors.New("bogus mix format")
	}

	// IAudioClient::Initialize (index 3), 1 s shared buffer
	hr = comCall(client, 3, 0 /*shared*/, AUDCLNT_STREAMFLAGS_LOOPBACK,
		2*HNSperSecond, 0, uintptr(wfx), 0)
	pCoTaskMemFree.Call(uintptr(wfx))
	if HRFailed(hr) {
		a.Close()
		return nil, fmt.Errorf("Initialize(loopback) 0x%08x", uint32(hr))
	}

	// IAudioClient::GetService (index 14)
	var cap unsafe.Pointer
	iidCap := IIDIAudioCaptureClient
	hr = comCall(client, 14, uintptr(unsafe.Pointer(&iidCap)), uintptr(unsafe.Pointer(&cap)))
	if HRFailed(hr) || cap == nil {
		a.Close()
		return nil, fmt.Errorf("IAudioCaptureClient 0x%08x", uint32(hr))
	}
	a.capture = cap

	// IAudioClient::Start (index 10)
	if hr = comCall(client, 10); HRFailed(hr) {
		a.Close()
		return nil, fmt.Errorf("audio Start 0x%08x", uint32(hr))
	}
	return a, nil
}

// ReadPCM drains everything the loopback buffer holds and returns interleaved
// 16 bit samples. A silent packet yields silence of the same length, so the
// audio timeline stays aligned with the video.
func (a *AudioCapture) ReadPCM() ([]byte, error) {
	if a.capture == nil {
		return nil, errors.New("audio closed")
	}
	out := a.buf[:0]
	for {
		var next uint32
		if hr := comCall(a.capture, 5, uintptr(unsafe.Pointer(&next))); HRFailed(hr) {
			return out, fmt.Errorf("GetNextPacketSize 0x%08x", uint32(hr))
		}
		if next == 0 {
			break
		}
		var data unsafe.Pointer
		var frames, flags uint32
		var devPos, qpc uint64
		hr := comCall(a.capture, 3, uintptr(unsafe.Pointer(&data)),
			uintptr(unsafe.Pointer(&frames)), uintptr(unsafe.Pointer(&flags)),
			uintptr(unsafe.Pointer(&devPos)), uintptr(unsafe.Pointer(&qpc)))
		if HRFailed(hr) {
			return out, fmt.Errorf("GetBuffer 0x%08x", uint32(hr))
		}
		if frames > 0 {
			samples := int(frames) * a.Fmt.Channels
			dst := make([]byte, samples*2)
			if flags&AUDCLNT_BUFFERFLAGS_SILENT != 0 || data == nil {
				// leave zeros
			} else if a.isFloat {
				src := unsafe.Slice((*float32)(data), samples)
				for i, f := range src {
					v := f * 32767
					if v > 32767 {
						v = 32767
					} else if v < -32768 {
						v = -32768
					}
					s := int16(math.Round(float64(v)))
					dst[i*2] = byte(uint16(s))
					dst[i*2+1] = byte(uint16(s) >> 8)
				}
			} else {
				src := unsafe.Slice((*byte)(data), samples*2)
				copy(dst, src)
			}
			out = append(out, dst...)
		}
		comCall(a.capture, 4, uintptr(frames)) // ReleaseBuffer
	}
	a.buf = out
	return out, nil
}

func (a *AudioCapture) Close() {
	if a.client != nil {
		comCall(a.client, 11) // Stop
	}
	ComRelease(a.capture)
	ComRelease(a.client)
	ComRelease(a.dev)
	ComRelease(a.enum)
	a.capture, a.client, a.dev, a.enum = nil, nil, nil, nil
}
