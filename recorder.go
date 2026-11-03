//go:build windows

package main

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dvygolov/yasshoter/internal/win"
)

// Recorder captures a region with GDI (BitBlt) and encodes H.264/AAC into an
// MP4 with Media Foundation. Both loops run off the UI thread; the tray icon and
// the red frame are the only UI while recording.

const hnsPerSecond = 10000000

type Recorder struct {
	app    *App
	region win.RECT
	path   string
	fps    int
	audio  bool

	sw     *win.SinkWriter
	border *recBorder
	cursor bool
	start  time.Time
	stopCh chan struct{}
	wg     sync.WaitGroup

	frames  atomic.Int64
	audioOK atomic.Bool
	errOnce sync.Once
	err     error
}

// startRecording begins a recording of region (screen coordinates).
func (a *App) startRecording(region win.RECT, audioMode string) {
	audio := audioMode != ""
	if a.rec != nil {
		return
	}
	region = clampToScreen(region, a.screen)
	if region.W() < 16 || region.H() < 16 {
		a.notify("Запись", "Слишком маленькая область")
		return
	}
	dir := a.cfg.SaveDir
	if dir == "" {
		dir = defaultConfig().SaveDir
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		a.notify("Запись", "Не удалось создать папку: "+err.Error())
		return
	}
	base := expandTemplate(a.cfg.FileNameTemplate+"_video", time.Now())
	path := uniquePathExt(dir, base, ".mp4")

	fps := a.cfg.VideoFps
	if fps < 10 || fps > 60 {
		fps = 30
	}
	w := int(region.W())
	h := int(region.H())
	bitrate := int(float64(w*h*fps) * 0.12)
	if bitrate < 2_000_000 {
		bitrate = 2_000_000
	}
	if bitrate > 24_000_000 {
		bitrate = 24_000_000
	}

	cfg := win.MediaConfig{Width: w, Height: h, FPS: fps, Bitrate: bitrate, Audio: audio}
	// probe the loopback format before opening the file so the audio stream is
	// declared with the real device rate
	var caps []*win.AudioCapture
	if audio {
		caps = openAudioSources(a.cfg.AudioSource, a.cfg.AudioDevice, a.cfg.AudioDeviceMic)
		if len(caps) > 0 {
			cfg.AudioRate = canonRate
			cfg.AudioChannels = canonChannels
			for _, c := range caps {
				logf("record: audio source=%s device=%s %q (%d Hz, %d ch)", a.cfg.AudioSource, c.DeviceID, c.Name, c.Fmt.Rate, c.Fmt.Channels)
			}
		}
	}

	logf("record: cfg %dx%d fps=%d bitrate=%d audio=%v rate=%d ch=%d", w, h, fps, bitrate, cfg.Audio, cfg.AudioRate, cfg.AudioChannels)
	sw, err := win.NewSinkWriter(path, cfg)
	if err != nil && cfg.Audio {
		// the AAC path is the fragile one: retry without sound rather than lose
		// the recording entirely
		logf("record: audio sink failed (%v), retrying video only", err)
		cfg.Audio = false
		sw, err = win.NewSinkWriter(path, cfg)
	}
	if err != nil {
		closeCaptures(caps)
		logf("record: sink writer failed: %v", err)
		a.notify("Запись", "Кодек недоступен: "+err.Error())
		return
	}

	r := &Recorder{app: a, region: region, path: path, fps: fps, audio: sw.HasAudio(), sw: sw,
		cursor: a.cfg.VideoCursor, start: time.Now(), stopCh: make(chan struct{})}
	a.rec = r
	if len(caps) == 0 && audio {
		a.notify("Запись", "Звук недоступен, записываю без него")
	}
	logf("record: start %dx%d fps=%d audio=%v cursor=%v file=%s", w, h, fps, r.audio, r.cursor, path)

	r.border = newRecBorder(region, win.Scale(2, win.DpiForPoint(win.POINT{X: region.Left, Y: region.Top})))
	a.enterRecordingUI()

	r.wg.Add(1)
	go r.videoLoop(w, h)
	if r.audio && len(caps) > 0 {
		r.wg.Add(1)
		go r.audioLoop(caps)
	}
}

// stopRecording ends the capture, hides the frame and finalizes the file.
func (a *App) stopRecording() {
	r := a.rec
	if r == nil {
		return
	}
	a.rec = nil
	close(r.stopCh)
	if r.border != nil {
		r.border.Close()
		r.border = nil
	}
	a.leaveRecordingUI()
	logf("record: stopping after %d frames", r.frames.Load())

	dir := filepath.Dir(r.path)
	go func() {
		r.wg.Wait()
		err := r.sw.Finalize()
		if err == nil {
			if st, statErr := os.Stat(r.path); statErr == nil {
				logf("record: saved %s (%d KB, %d frames)", r.path, st.Size()/1024, r.frames.Load())
			} else {
				logf("record: saved %s", r.path)
			}
		} else {
			logf("record: finalize failed: %v", err)
		}
		debug.FreeOSMemory()
		a.recMu.Lock()
		a.recSavedPath = r.path
		if err != nil {
			a.recSavedErr = err.Error()
		}
		a.recMu.Unlock()
		a.hwnd.PostMessage(umRecordingSaved, 0, 0)
		_ = dir
	}()
}

// videoLoop grabs frames and pushes them to the encoder.
func (r *Recorder) videoLoop(w, h int) {
	defer r.wg.Done()
	// bottom-up DIB: Media Foundation expects RGB32 rows in this order, GDI
	// BitBlt does not care, so no extra flip copy is needed per frame
	dib, err := win.NewDibBottomUp(int32(w), int32(h))
	if err != nil {
		r.fail(err)
		return
	}
	defer dib.Close()

	frameDur := int64(hnsPerSecond / r.fps)
	frameNs := time.Second / time.Duration(r.fps)
	var last int64
	for {
		select {
		case <-r.stopCh:
			return
		default:
		}
		t0 := time.Now()
		sdc, release := win.ScreenDC()
		dib.BlitRaw(sdc, r.region.Left, r.region.Top, int32(w), int32(h))
		release()
		if r.cursor {
			// GDI capture never includes the pointer, so paint it in
			if ci, visible := win.GetCursorInfo(); visible {
				cx := ci.PtScreenPos.X - r.region.Left
				cy := ci.PtScreenPos.Y - r.region.Top
				if cx > -32 && cy > -32 && cx < int32(w)+32 && cy < int32(h)+32 {
					win.DrawCursor(dib.DC, cx, cy, ci.HCursor)
				}
			}
		}

		ts := time.Since(r.start).Nanoseconds() / 100
		if ts <= last {
			ts = last + 1
		}
		if err := r.sw.WriteVideo(dib.Bits, ts, frameDur); err != nil {
			r.fail(err)
			return
		}
		last = ts + frameDur
		r.frames.Add(1)

		if d := frameNs - time.Since(t0); d > 0 {
			// keep pacing responsive to Stop
			deadline := time.Now().Add(d)
			for time.Now().Before(deadline) {
				select {
				case <-r.stopCh:
					return
				case <-time.After(minDuration(deadline.Sub(time.Now()), 5*time.Millisecond)):
				}
			}
		}
	}
}

// audioLoop records one or two sources (system sound and/or microphone) and
// mixes them into a single PCM stream that stays glued to the video clock.
func (r *Recorder) audioLoop(caps []*win.AudioCapture) {
	defer r.wg.Done()
	defer closeCaptures(caps)
	if !win.ComInit() {
		logf("record: COM init failed on audio thread")
		return
	}
	defer win.ComUninit()

	// everything is converted to the canonical 48 kHz stereo PCM16 first
	conv := make([]*srcConverter, len(caps))
	for i, c := range caps {
		conv[i] = newSrcConverter(c.Fmt.Rate, c.Fmt.Channels)
	}
	rate := canonRate
	channels := canonChannels
	bytesPerFrame := channels * 2
	pend := make([][]byte, len(caps))
	out := make([]byte, rate*bytesPerFrame/2)
	var written int64
	loud, warned := false, false
	ticker := time.NewTicker(15 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
		}
		for i, c := range caps {
			pcm, err := c.ReadPCM()
			if err != nil {
				logf("record: audio read: %v", err)
				return
			}
			pend[i] = append(pend[i], conv[i].convert(pcm)...)
			if len(pend[i]) > rate*bytesPerFrame*30 {
				pend[i] = pend[i][len(pend[i])-rate*bytesPerFrame*30:]
			}
		}

		elapsedNs := time.Since(r.start).Nanoseconds()
		want := int64(float64(elapsedNs) / 1e9 * float64(rate))
		need := (want - written) * int64(bytesPerFrame)
		if need <= 0 {
			continue
		}
		if need > int64(rate*bytesPerFrame) { // never burst more than a second
			need = int64(rate * bytesPerFrame)
		}
		// pad short sources with silence so the mix stays aligned
		for i := range pend {
			if int64(len(pend[i])) < need {
				pend[i] = append(pend[i], make([]byte, need-int64(len(pend[i])))...)
			}
		}

		chunk := out[:need]
		if len(caps) == 1 {
			copy(chunk, pend[0][:need])
		} else {
			mixPCM(chunk, [][]byte{pend[0][:need], pend[1][:need]})
		}
		if !loud && hasSignal(chunk) {
			loud = true
			logf("record: audio signal detected (%d source(s))", len(caps))
		}
		ts := written * hnsPerSecond / int64(rate)
		dur := int64(len(chunk)/bytesPerFrame) * hnsPerSecond / int64(rate)
		if err := r.sw.WriteAudio(chunk, ts, dur); err != nil {
			logf("record: audio write: %v", err)
			return
		}
		written += int64(len(chunk) / bytesPerFrame)
		for i := range pend {
			pend[i] = append(pend[i][:0], pend[i][need:]...)
		}
		r.audioOK.Store(true)
		if !loud && !warned && time.Since(r.start) > 3*time.Second {
			warned = true
			logf("record: WARNING audio is silent so far (%d source(s)) — muted device or nothing playing?", len(caps))
			r.app.notify("Звук не пишется", "Похоже, на выбранном устройстве тишина. Проверьте громкость и устройство в меню трея.")
		}
	}
}

// mixPCM averages several PCM16 chunks of equal length into dst.
func mixPCM(dst []byte, streams [][]byte) {
	n := len(streams)
	for i := 0; i+1 < len(dst); i += 2 {
		sum := 0
		for _, s := range streams {
			sum += int(int16(uint16(s[i]) | uint16(s[i+1])<<8))
		}
		v := sum / n
		if v > 32767 {
			v = 32767
		} else if v < -32768 {
			v = -32768
		}
		dst[i] = byte(uint16(v))
		dst[i+1] = byte(uint16(v) >> 8)
	}
}

func closeCaptures(caps []*win.AudioCapture) {
	for _, c := range caps {
		if c != nil {
			c.Close()
		}
	}
}

func (r *Recorder) fail(err error) {
	r.errOnce.Do(func() {
		r.err = err
		logf("record: %v", err)
	})
}

// openAudioSources opens the endpoints for the configured mode. "mix" records
// system sound and the microphone together, converted to the same 48k stereo
// format so they can be averaged.
func openAudioSources(source, device, deviceMic string) []*win.AudioCapture {
	ok := make(chan []*win.AudioCapture, 1)
	go func() {
		if !win.ComInit() {
			ok <- nil
			return
		}
		var out []*win.AudioCapture
		open := func(loopback bool, filter string) {
			var cap *win.AudioCapture
			var err error
			if loopback {
				cap, err = win.OpenLoopbackAudio48(filter)
			} else {
				cap, err = win.OpenMicrophoneAudio48(filter)
			}
			if err != nil {
				logf("record: %s unavailable (filter %q): %v", map[bool]string{true: "loopback", false: "microphone"}[loopback], filter, err)
				return
			}
			out = append(out, cap)
		}
		switch source {
		case "mic":
			open(false, deviceMic)
		case "mix":
			open(true, device)
			open(false, deviceMic)
		default:
			open(true, device)
		}
		ok <- out
	}()
	select {
	case c := <-ok:
		return c
	case <-time.After(3 * time.Second):
		return nil
	}
}

// hasSignal reports whether a PCM16 chunk is louder than noise.
func hasSignal(pcm []byte) bool {
	for i := 0; i+1 < len(pcm); i += 2 {
		v := int16(uint16(pcm[i]) | uint16(pcm[i+1])<<8)
		if v > 400 || v < -400 {
			return true
		}
	}
	return false
}

func clampToScreen(r win.RECT, screen win.RECT) win.RECT {
	if r.Left < screen.Left {
		r.Left = screen.Left
	}
	if r.Top < screen.Top {
		r.Top = screen.Top
	}
	if r.Right > screen.Right {
		r.Right = screen.Right
	}
	if r.Bottom > screen.Bottom {
		r.Bottom = screen.Bottom
	}
	if r.Right < r.Left {
		r.Right = r.Left
	}
	if r.Bottom < r.Top {
		r.Bottom = r.Top
	}
	return r
}

func uniquePathExt(dir, base, ext string) string {
	p := filepath.Join(dir, base+ext)
	if !exists(p) {
		return p
	}
	for i := 2; i < 10000; i++ {
		p = filepath.Join(dir, base+"_"+itoa(int64(i))+ext)
		if !exists(p) {
			return p
		}
	}
	return filepath.Join(dir, base+"_"+itoa(time.Now().UnixNano())+ext)
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// recordingElapsed is used for the tray tooltip.
func (r *Recorder) elapsed() time.Duration { return time.Since(r.start) }
