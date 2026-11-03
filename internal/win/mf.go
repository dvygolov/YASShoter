//go:build windows

package win

import (
	"errors"
	"fmt"
	"sync"
	"syscall"
	"unsafe"
)

// Media Foundation sink writer: H.264 + AAC into MP4, encoded by the codecs
// that ship with Windows. No ffmpeg, no CGO.

var (
	dllMfplat      = syscall.NewLazyDLL("mfplat.dll")
	dllMfreadwrite = syscall.NewLazyDLL("mfreadwrite.dll")

	pMFStartup                 = dllMfplat.NewProc("MFStartup")
	pMFCreateMediaType         = dllMfplat.NewProc("MFCreateMediaType")
	pMFCreateMemoryBuffer      = dllMfplat.NewProc("MFCreateMemoryBuffer")
	pMFCreateSample            = dllMfplat.NewProc("MFCreateSample")
	pMFCreateAttributes        = dllMfplat.NewProc("MFCreateAttributes")
	pMFCreateSinkWriterFromURL = dllMfreadwrite.NewProc("MFCreateSinkWriterFromURL")
)

// GUIDs
var (
	MFMediaTypeVideo   = GUID{0x73646976, 0x0000, 0x0010, [8]byte{0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71}}
	MFMediaTypeAudio   = GUID{0x73647561, 0x0000, 0x0010, [8]byte{0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71}}
	MFVideoFormatRGB32 = GUID{0x00000016, 0x0000, 0x0010, [8]byte{0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71}}
	MFVideoFormatH264  = GUID{0x34363248, 0x0000, 0x0010, [8]byte{0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71}}
	MFAudioFormatPCM   = GUID{0x00000001, 0x0000, 0x0010, [8]byte{0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71}}
	MFAudioFormatAAC   = GUID{0x00001610, 0x0000, 0x0010, [8]byte{0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71}}

	MFMTMajorType                 = GUID{0x48EBA18E, 0xF8C9, 0x4687, [8]byte{0xBF, 0x11, 0x0A, 0x74, 0xC9, 0xF9, 0x6A, 0x8F}}
	MFMTSubtype                   = GUID{0xF7E34C9A, 0x42E8, 0x4714, [8]byte{0xB7, 0x4B, 0xCB, 0x29, 0xD7, 0x2C, 0x35, 0xE5}}
	MFMTAvgBitrate                = GUID{0x20332624, 0xFB0D, 0x4D9E, [8]byte{0xBD, 0x0D, 0xCB, 0xF6, 0x78, 0x6C, 0x10, 0x2E}}
	MFMTFrameSize                 = GUID{0x1652C33D, 0xD6B2, 0x4012, [8]byte{0xB8, 0x34, 0x72, 0x03, 0x08, 0x49, 0xA3, 0x7D}}
	MFMTFrameRate                 = GUID{0xC459A2E8, 0x3D2C, 0x4E44, [8]byte{0xB1, 0x32, 0xFE, 0xE5, 0x15, 0x6C, 0x7B, 0xB0}}
	MFMTPixelAspectRatio          = GUID{0xC6376A1E, 0x8D0A, 0x4027, [8]byte{0xBE, 0x45, 0x6D, 0x9A, 0x0A, 0xD3, 0x9B, 0xB6}}
	MFMTInterlaceMode             = GUID{0xE2724BB8, 0xE676, 0x4806, [8]byte{0xB4, 0xB2, 0xA8, 0xD6, 0xEF, 0xB4, 0x4C, 0xCD}}
	MFMTAllSamplesIndependent     = GUID{0xC9173739, 0x5E56, 0x461C, [8]byte{0xB7, 0x13, 0x46, 0xFB, 0x99, 0x5C, 0xB9, 0x5F}}
	MFMTAudioNumChannels          = GUID{0x37E48BF5, 0x645E, 0x4C5B, [8]byte{0x89, 0xDE, 0xAD, 0xA9, 0xE2, 0x9B, 0x69, 0x6A}}
	MFMTAudioSamplesPerSec        = GUID{0x5FAEEAE7, 0x0290, 0x4C31, [8]byte{0x9E, 0x8A, 0xC5, 0x34, 0xF6, 0x8D, 0x9D, 0xBA}}
	MFMTAudioBlockAlignment       = GUID{0x322DE230, 0x9EEB, 0x43BD, [8]byte{0xAB, 0x7A, 0xFF, 0x41, 0x22, 0x51, 0x54, 0x1D}}
	MFMTAudioAvgBytesPerSec       = GUID{0x1AAB75C8, 0xCFEF, 0x451C, [8]byte{0xAB, 0x95, 0xAC, 0x03, 0x4B, 0x8E, 0x17, 0x31}}
	MFMTAudioBitsPerSample        = GUID{0xF2DEB57F, 0x40FA, 0x4764, [8]byte{0xAA, 0x33, 0xED, 0x4F, 0x2D, 0x1F, 0xF6, 0x69}}
	MFMTAACPayloadType            = GUID{0xBFBABE79, 0x7434, 0x4D1C, [8]byte{0x94, 0xF0, 0x72, 0xA3, 0xB9, 0xE1, 0x71, 0x88}}
	MFMTAudioPreferWaveFormatEx   = GUID{0xA901A175, 0x48EA, 0x4A81, [8]byte{0x8C, 0x5F, 0x3A, 0x12, 0xAF, 0x9D, 0xB0, 0xA2}}
	MFMTAudioChannelMask          = GUID{0x55FB5765, 0x644A, 0x4CBD, [8]byte{0x84, 0x79, 0x93, 0x89, 0x83, 0xBB, 0x86, 0xBD}}
	MFMTAACProfileLevel           = GUID{0x7632F0E6, 0x9538, 0x4D61, [8]byte{0xAC, 0xDA, 0xEA, 0x29, 0xC8, 0xC1, 0x44, 0x56}}
	MFMTDefaultStride             = GUID{0x644B4E48, 0x1E02, 0x4516, [8]byte{0xB0, 0xEB, 0xC0, 0x1C, 0xA9, 0xD4, 0x9A, 0xC6}}
	MFReadWriteEnableHWTransforms = GUID{0xA634A91C, 0x822B, 0x41B9, [8]byte{0xA4, 0x94, 0x4D, 0xE4, 0x64, 0x36, 0x12, 0xB0}}
	MFSinkWriterDisableThrottling = GUID{0x08B845D8, 0x2B74, 0x4AFE, [8]byte{0x9D, 0x53, 0xBE, 0x16, 0xD2, 0xD5, 0xAE, 0x4F}}
)

// COM vtable indices
const (
	idxIMFSinkWriterAddStream         = 3
	idxIMFSinkWriterSetInputMediaType = 4
	idxIMFSinkWriterBeginWriting      = 5
	idxIMFSinkWriterWriteSample       = 6
	idxIMFSinkWriterFinalize          = 11

	idxIMFSampleSetSampleTime     = 36
	idxIMFSampleSetSampleDuration = 38
	idxIMFSampleAddBuffer         = 42

	idxIMFMediaBufferLock             = 3
	idxIMFMediaBufferUnlock           = 4
	idxIMFMediaBufferSetCurrentLength = 6

	idxIMFAttributesSetUINT32 = 21
	idxIMFAttributesSetUINT64 = 22
	idxIMFAttributesSetGUID   = 24
)

const (
	MFVersion                   = 0x00020070
	MFSTARTUPFULL               = 0
	MFVideoInterlaceProgressive = 2
	HNSperSecond                = 10000000 // 100 ns units
)

var mfOnce sync.Once
var mfErr error

// MFInit starts Media Foundation once per process.
func MFInit() error {
	mfOnce.Do(func() {
		hr, _, _ := pMFStartup.Call(MFVersion, MFSTARTUPFULL)
		if HRFailed(int32(hr)) {
			mfErr = fmt.Errorf("MFStartup failed: 0x%08x", uint32(hr))
		}
	})
	return mfErr
}

func mfCreateMediaType() (unsafe.Pointer, error) {
	var out unsafe.Pointer
	hr, _, _ := pMFCreateMediaType.Call(uintptr(unsafe.Pointer(&out)))
	if HRFailed(int32(hr)) {
		return nil, fmt.Errorf("MFCreateMediaType 0x%08x", uint32(hr))
	}
	return out, nil
}

func mfSetGUID(p unsafe.Pointer, key GUID, value GUID) {
	g := value
	comCall(p, idxIMFAttributesSetGUID, key.Ptr(), uintptr(unsafe.Pointer(&g)))
}

func mfSetU32(p unsafe.Pointer, key GUID, value uint32) {
	comCall(p, idxIMFAttributesSetUINT32, key.Ptr(), uintptr(value))
}

func mfSetU64(p unsafe.Pointer, key GUID, value uint64) {
	comCall(p, idxIMFAttributesSetUINT64, key.Ptr(), uintptr(value))
}

func packRatio(a, b uint32) uint64 { return uint64(a)<<32 | uint64(b) }

// SinkWriter writes H.264 video (and optional AAC audio) into an MP4 file.
type SinkWriter struct {
	mu        sync.Mutex
	writer    unsafe.Pointer
	attrs     unsafe.Pointer
	video     uint32
	audio     uint32
	hasVideo  bool
	hasAudio  bool
	finalized bool
}

// MediaConfig describes the stream layout of a recording.
type MediaConfig struct {
	Width, Height int
	FPS           int
	Bitrate       int
	Audio         bool
	AudioRate     int
	AudioChannels int
}

// NewSinkWriter creates <path> and configures the encoder.
func NewSinkWriter(path string, cfg MediaConfig) (*SinkWriter, error) {
	if err := MFInit(); err != nil {
		return nil, err
	}
	sw := &SinkWriter{}

	var attrs unsafe.Pointer
	if r, _, _ := pMFCreateAttributes.Call(uintptr(unsafe.Pointer(&attrs)), 3); HRFailed(int32(r)) {
		return nil, errors.New("MFCreateAttributes failed")
	}
	sw.attrs = attrs
	mfSetU32(attrs, MFReadWriteEnableHWTransforms, 1)
	mfSetU32(attrs, MFSinkWriterDisableThrottling, 1)

	var writer unsafe.Pointer
	if r, _, _ := pMFCreateSinkWriterFromURL.Call(
		uintptr(unsafe.Pointer(UTF16Ptr(path))), 0, uintptr(attrs),
		uintptr(unsafe.Pointer(&writer))); HRFailed(int32(r)) {
		ComRelease(attrs)
		return nil, fmt.Errorf("MFCreateSinkWriterFromURL 0x%08x", uint32(r))
	}
	sw.writer = writer

	out, err := mfCreateMediaType()
	if err != nil {
		sw.Close()
		return nil, err
	}
	mfSetGUID(out, MFMTMajorType, MFMediaTypeVideo)
	mfSetGUID(out, MFMTSubtype, MFVideoFormatH264)
	mfSetU32(out, MFMTAvgBitrate, uint32(cfg.Bitrate))
	mfSetU64(out, MFMTFrameSize, packRatio(uint32(cfg.Width), uint32(cfg.Height)))
	mfSetU64(out, MFMTFrameRate, packRatio(uint32(cfg.FPS), 1))
	mfSetU32(out, MFMTInterlaceMode, MFVideoInterlaceProgressive)
	mfSetU64(out, MFMTPixelAspectRatio, packRatio(1, 1))

	var stream uint32
	hrAdd := comCall(writer, idxIMFSinkWriterAddStream, uintptr(unsafe.Pointer(out)), uintptr(unsafe.Pointer(&stream)))
	ComRelease(out)
	if HRFailed(hrAdd) {
		sw.Close()
		return nil, fmt.Errorf("AddStream(video) 0x%08x", uint32(hrAdd))
	}
	sw.video = stream
	sw.hasVideo = true

	in, err := mfCreateMediaType()
	if err != nil {
		sw.Close()
		return nil, err
	}
	mfSetGUID(in, MFMTMajorType, MFMediaTypeVideo)
	mfSetGUID(in, MFMTSubtype, MFVideoFormatRGB32)
	mfSetU64(in, MFMTFrameSize, packRatio(uint32(cfg.Width), uint32(cfg.Height)))
	mfSetU64(in, MFMTFrameRate, packRatio(uint32(cfg.FPS), 1))
	mfSetU64(in, MFMTPixelAspectRatio, packRatio(1, 1))
	mfSetU32(in, MFMTInterlaceMode, MFVideoInterlaceProgressive)
	mfSetU32(in, MFMTAllSamplesIndependent, 1)
	hrIn := comCall(writer, idxIMFSinkWriterSetInputMediaType, uintptr(stream), uintptr(unsafe.Pointer(in)), 0)
	ComRelease(in)
	if HRFailed(hrIn) {
		sw.Close()
		return nil, fmt.Errorf("SetInputMediaType(video) 0x%08x", uint32(hrIn))
	}

	if cfg.Audio {
		if err := sw.addAudio(cfg); err != nil {
			logMF("audio stream disabled: %v", err)
			sw.hasAudio = false
		}
	}

	if hrBegin := comCall(writer, idxIMFSinkWriterBeginWriting); HRFailed(hrBegin) {
		sw.Close()
		return nil, fmt.Errorf("BeginWriting 0x%08x", uint32(hrBegin))
	}
	return sw, nil
}

func (s *SinkWriter) addAudio(cfg MediaConfig) error {
	out, err := mfCreateMediaType()
	if err != nil {
		return err
	}
	defer ComRelease(out)
	mfSetGUID(out, MFMTMajorType, MFMediaTypeAudio)
	mfSetGUID(out, MFMTSubtype, MFAudioFormatAAC)
	mfSetU32(out, MFMTAudioAvgBytesPerSec, 24000) // 192 kbit/s
	mfSetU32(out, MFMTAudioSamplesPerSec, uint32(cfg.AudioRate))
	mfSetU32(out, MFMTAudioNumChannels, uint32(cfg.AudioChannels))
	mfSetU32(out, MFMTAudioBitsPerSample, 16)
	// raw AAC (payload type 0) is what the MP4 muxer expects; block alignment and
	// profile hints are left to the encoder, setting them made BeginWriting fail
	// with MF_E_INVALIDMEDIATYPE
	mfSetU32(out, MFMTAACPayloadType, 0)

	var stream uint32
	if hrAdd := comCall(s.writer, idxIMFSinkWriterAddStream, uintptr(unsafe.Pointer(out)), uintptr(unsafe.Pointer(&stream))); HRFailed(hrAdd) {
		return fmt.Errorf("AddStream(audio) 0x%08x", uint32(hrAdd))
	}

	in, err := mfCreateMediaType()
	if err != nil {
		return err
	}
	defer ComRelease(in)
	mfSetGUID(in, MFMTMajorType, MFMediaTypeAudio)
	mfSetGUID(in, MFMTSubtype, MFAudioFormatPCM)
	mfSetU32(in, MFMTAudioNumChannels, uint32(cfg.AudioChannels))
	mfSetU32(in, MFMTAudioSamplesPerSec, uint32(cfg.AudioRate))
	mfSetU32(in, MFMTAudioBitsPerSample, 16)
	block := cfg.AudioChannels * 2
	mfSetU32(in, MFMTAudioBlockAlignment, uint32(block))
	mfSetU32(in, MFMTAudioAvgBytesPerSec, uint32(cfg.AudioRate*block))
	mfSetU32(in, MFMTAudioPreferWaveFormatEx, 1)
	mfSetU32(in, MFMTAllSamplesIndependent, 1)

	if hrIn := comCall(s.writer, idxIMFSinkWriterSetInputMediaType, uintptr(stream), uintptr(unsafe.Pointer(in)), 0); HRFailed(hrIn) {
		return fmt.Errorf("SetInputMediaType(audio) 0x%08x", uint32(hrIn))
	}
	s.audio = stream
	s.hasAudio = true
	return nil
}

// writeSample copies data into a Media Foundation buffer and hands it over.
func (s *SinkWriter) writeSample(stream uint32, data []byte, ts, dur int64) error {
	if len(data) == 0 {
		return nil
	}
	var buf unsafe.Pointer
	if r, _, _ := pMFCreateMemoryBuffer.Call(uintptr(len(data)), uintptr(unsafe.Pointer(&buf))); HRFailed(int32(r)) {
		return fmt.Errorf("MFCreateMemoryBuffer 0x%08x", uint32(r))
	}
	defer ComRelease(buf)

	var ptr unsafe.Pointer
	if hrLock := comCall(buf, idxIMFMediaBufferLock, uintptr(unsafe.Pointer(&ptr)), 0, 0); HRFailed(hrLock) {
		return errors.New("IMFMediaBuffer::Lock failed")
	}
	copy(unsafe.Slice((*byte)(ptr), len(data)), data)
	comCall(buf, idxIMFMediaBufferUnlock)
	comCall(buf, idxIMFMediaBufferSetCurrentLength, uintptr(len(data)))

	var sample unsafe.Pointer
	if r, _, _ := pMFCreateSample.Call(uintptr(unsafe.Pointer(&sample))); HRFailed(int32(r)) {
		return fmt.Errorf("MFCreateSample 0x%08x", uint32(r))
	}
	defer ComRelease(sample)

	comCall(sample, idxIMFSampleAddBuffer, uintptr(buf))
	comCall(sample, idxIMFSampleSetSampleTime, uintptr(ts))
	comCall(sample, idxIMFSampleSetSampleDuration, uintptr(dur))

	s.mu.Lock()
	hrWrite := comCall(s.writer, idxIMFSinkWriterWriteSample, uintptr(stream), uintptr(sample))
	s.mu.Unlock()
	if HRFailed(hrWrite) {
		return fmt.Errorf("WriteSample 0x%08x", uint32(hrWrite))
	}
	return nil
}

// WriteVideo pushes one RGB32 frame (row stride = width*4).
func (s *SinkWriter) WriteVideo(frame []byte, ts, dur int64) error {
	if !s.hasVideo {
		return errors.New("no video stream")
	}
	return s.writeSample(s.video, frame, ts, dur)
}

// WriteAudio pushes interleaved PCM16 samples.
func (s *SinkWriter) WriteAudio(pcm []byte, ts, dur int64) error {
	if !s.hasAudio {
		return nil
	}
	return s.writeSample(s.audio, pcm, ts, dur)
}

func (s *SinkWriter) HasAudio() bool { return s.hasAudio }

// Finalize flushes the encoder and closes the file. Safe to call twice.
func (s *SinkWriter) Finalize() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writer == nil || s.finalized {
		return nil
	}
	s.finalized = true
	hr := comCall(s.writer, idxIMFSinkWriterFinalize)
	ComRelease(s.writer)
	s.writer = nil
	ComRelease(s.attrs)
	s.attrs = nil
	if HRFailed(hr) {
		return fmt.Errorf("Finalize 0x%08x", uint32(hr))
	}
	return nil
}

func (s *SinkWriter) Close() {
	_ = s.Finalize()
}

// logMF lets the win package report non fatal encoder issues.
var logMF = func(string, ...any) {}

// SetMFLogger routes win package diagnostics into the application log.
func SetMFLogger(fn func(string, ...any)) { logMF = fn }
