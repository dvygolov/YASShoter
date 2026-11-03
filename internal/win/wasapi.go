//go:build windows

package win

import (
	"errors"
	"fmt"
	"math"
	"strings"
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
	eCapture                     = 1
	eConsole                     = 0
	eMultimedia                  = 1
	eCommunications              = 2
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
	enum     unsafe.Pointer
	dev      unsafe.Pointer
	client   unsafe.Pointer
	capture  unsafe.Pointer
	Fmt      AudioFormat
	isFloat  bool
	DeviceID string
	buf      []byte
}

const DEVICE_STATE_ACTIVE = 0x00000001

// RenderDevice is one playback endpoint reported by the enumerator.
type RenderDevice struct {
	ID   string
	Name string
}

// ListCaptureDevices returns the active recording endpoints.
func ListCaptureDevices() ([]RenderDevice, error) {
	return listDevices(false)
}

// ListRenderDevices returns the active playback endpoints.
func ListRenderDevices() ([]RenderDevice, error) {
	return listDevices(true)
}

func listDevices(loopback bool) ([]RenderDevice, error) {
	enum, hr := ComCreateInstance(CLSIDMMDeviceEnumerator, IIDIMMDeviceEnumerator)
	if HRFailed(hr) || enum == nil {
		return nil, fmt.Errorf("device enumerator 0x%08x", uint32(hr))
	}
	defer ComRelease(enum)

	// IMMDeviceEnumerator::EnumAudioEndpoints (index 3)
	flow := uintptr(eRender)
	if !loopback {
		flow = uintptr(eCapture)
	}
	var coll unsafe.Pointer
	hr = comCall(enum, 3, flow, DEVICE_STATE_ACTIVE, uintptr(unsafe.Pointer(&coll)))
	if HRFailed(hr) || coll == nil {
		return nil, fmt.Errorf("EnumAudioEndpoints 0x%08x", uint32(hr))
	}
	defer ComRelease(coll)

	var count uint32
	comCall(coll, 3, uintptr(unsafe.Pointer(&count)))
	out := make([]RenderDevice, 0, count)
	for i := uint32(0); i < count; i++ {
		var dev unsafe.Pointer
		if hr := comCall(coll, 4, uintptr(i), uintptr(unsafe.Pointer(&dev))); HRFailed(hr) || dev == nil {
			continue
		}
		d := RenderDevice{}
		if id, err := deviceID(dev); err == nil {
			d.ID = id
		}
		if name, err := deviceName(dev); err == nil {
			d.Name = name
		}
		ComRelease(dev)
		out = append(out, d)
	}
	return out, nil
}

// deviceName reads the friendly name through the property store, best effort.
func deviceName(dev unsafe.Pointer) (string, error) {
	var store unsafe.Pointer
	// IMMDevice::OpenPropertyStore(dwStgmAccess, IPropertyStore**) — the access
	// mode parameter is easy to forget and then it writes through garbage
	if hr := comCall(dev, 4, 0 /* STGM_READ */, uintptr(unsafe.Pointer(&store))); HRFailed(hr) || store == nil {
		return "", fmt.Errorf("OpenPropertyStore 0x%08x", uint32(hr))
	}
	defer ComRelease(store)

	// PKEY_Device_FriendlyName = {a45c254e-df1c-4efd-8020-67d146a850e0}, 14
	key := propertyKey{FormatID: GUID{0xA45C254E, 0xDF1C, 0x4EFD, [8]byte{0x80, 0x20, 0x67, 0xD1, 0x46, 0xA8, 0x50, 0xE0}}, PID: 14}
	var pv propVariant
	if hr := comCall(store, 5, uintptr(unsafe.Pointer(&key)), uintptr(unsafe.Pointer(&pv))); HRFailed(hr) {
		return "", fmt.Errorf("GetValue 0x%08x", uint32(hr))
	}
	defer propVariantClear(&pv)
	if pv.VT != 31 || pv.P != 0 { // VT_LPWSTR
		return "", nil
	}
	return UTF16ToString((*[256]uint16)(unsafe.Pointer(pv.P))[:]), nil
}

type propertyKey struct {
	FormatID GUID
	PID      uint32
	_        uint32
}

type propVariant struct {
	VT   uint16
	R1   uint16
	R2   uint16
	R3   uint16
	P    uintptr
	Rest [2]uintptr
}

func propVariantClear(pv *propVariant) {
	dllOle32.NewProc("PropVariantClear").Call(uintptr(unsafe.Pointer(pv)))
}

// OpenLoopbackAudio captures a playback device (system sound). deviceFilter is a
// substring of the endpoint id; an empty string means "the system default".
func OpenLoopbackAudio(deviceFilter string) (*AudioCapture, error) {
	return openAudio(deviceFilter, true)
}

// OpenMicrophoneAudio captures the default recording device (microphone).
func OpenMicrophoneAudio(deviceFilter string) (*AudioCapture, error) {
	return openAudio(deviceFilter, false)
}

func openAudio(deviceFilter string, loopback bool) (*AudioCapture, error) {
	a := &AudioCapture{}
	enum, hr := ComCreateInstance(CLSIDMMDeviceEnumerator, IIDIMMDeviceEnumerator)
	if HRFailed(hr) || enum == nil {
		return nil, fmt.Errorf("device enumerator 0x%08x", uint32(hr))
	}
	a.enum = enum

	var dev unsafe.Pointer
	if deviceFilter != "" {
		dev = findDevice(enum, deviceFilter, loopback)
	}
	if dev == nil {
		// IMMDeviceEnumerator::GetDefaultAudioEndpoint (index 4)
		flow := uintptr(eRender)
		if !loopback {
			flow = uintptr(eCapture)
		}
		hr = comCall(enum, 4, flow, eMultimedia, uintptr(unsafe.Pointer(&dev)))
		if HRFailed(hr) || dev == nil {
			a.Close()
			return nil, fmt.Errorf("no default playback device 0x%08x", uint32(hr))
		}
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
	var flags uintptr
	if loopback {
		flags = AUDCLNT_STREAMFLAGS_LOOPBACK
	}
	hr = comCall(client, 3, 0 /*shared*/, flags, 2*HNSperSecond, 0, uintptr(wfx), 0)
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

	// record which endpoint we are listening to, for the log
	if id, err := deviceID(dev); err == nil {
		a.DeviceID = id
	}

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

// findDevice looks for an endpoint whose id contains filter.
func findDevice(enum unsafe.Pointer, filter string, loopback bool) unsafe.Pointer {
	var coll unsafe.Pointer
	flow := uintptr(eRender)
	if !loopback {
		flow = uintptr(eCapture)
	}
	if hr := comCall(enum, 3, flow, DEVICE_STATE_ACTIVE, uintptr(unsafe.Pointer(&coll))); HRFailed(hr) || coll == nil {
		return nil
	}
	defer ComRelease(coll)
	var count uint32
	comCall(coll, 3, uintptr(unsafe.Pointer(&count)))
	for i := uint32(0); i < count; i++ {
		var dev unsafe.Pointer
		if hr := comCall(coll, 4, uintptr(i), uintptr(unsafe.Pointer(&dev))); HRFailed(hr) || dev == nil {
			continue
		}
		id, _ := deviceID(dev)
		if id != "" && strings.Contains(strings.ToLower(id), strings.ToLower(filter)) {
			return dev
		}
		ComRelease(dev)
	}
	return nil
}

// deviceID reads IMMDevice::GetId (index 5), which is a CoTaskMem string.
func deviceID(dev unsafe.Pointer) (string, error) {
	var p *uint16
	if hr := comCall(dev, 5, uintptr(unsafe.Pointer(&p))); HRFailed(hr) || p == nil {
		return "", fmt.Errorf("GetId 0x%08x", uint32(hr))
	}
	s := UTF16ToString((*[512]uint16)(unsafe.Pointer(p))[:])
	pCoTaskMemFree.Call(uintptr(unsafe.Pointer(p)))
	return s, nil
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
