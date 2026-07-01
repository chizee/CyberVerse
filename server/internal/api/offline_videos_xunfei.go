package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cyberverse/server/internal/character"
	"github.com/cyberverse/server/internal/orchestrator"
)

const (
	xunfeiOfflineAudioSampleRate = 16000
	xunfeiOfflineCaptureWait     = 200 * time.Millisecond
	xunfeiOfflinePrerollWait     = 3 * time.Second
	xunfeiOfflineTailWait        = 2 * time.Second
	xunfeiOfflineOutputTailWait  = 2 * time.Second
	xunfeiOfflineRenderDrainWait = 1 * time.Second
	xunfeiOfflineDurationWait    = 8 * time.Second
	xunfeiOfflineDurationSlack   = 250 * time.Millisecond
	xunfeiOfflineStopTimeout     = 5 * time.Second
	xunfeiOfflineRemuxTimeout    = 2 * time.Minute
	xunfeiOfflineMaxAttempts     = 2
	xunfeiOfflineRetryWait       = 2 * time.Second
)

type xunfeiOfflineAttemptError struct {
	stage string
	err   error
}

func (e xunfeiOfflineAttemptError) Error() string {
	return e.err.Error()
}

func (e xunfeiOfflineAttemptError) Unwrap() error {
	return e.err
}

func (r *Router) handleCreateXunfeiOfflineVideo(w http.ResponseWriter, req *http.Request, ch *character.Character, inputType, text string) {
	if ch == nil || ch.Xunfei == nil || strings.TrimSpace(ch.Xunfei.AvatarID) == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Xunfei avatar_id is required"})
		return
	}
	root, err := r.offlineVideoRoot(ch.ID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: err.Error()})
		return
	}
	jobID := uuid.NewString()
	ttsConfig, err := r.offlineVideoTTSConfig(req, ch, inputType, "offline-"+jobID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	jobDir := filepath.Join(root, jobID)
	if err := os.MkdirAll(jobDir, 0755); err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	var audio *orchestrator.OfflineAudioInput
	if inputType == "audio" {
		audio, err = readOfflineVideoAudio(req, jobDir)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	title := strings.TrimSpace(req.FormValue("title"))
	if title == "" {
		title = defaultOfflineVideoTitle()
	}
	cfg := xunfeiOfflineCharacter(ch)
	job := &offlineVideoJob{
		ID:          jobID,
		CharacterID: ch.ID,
		Title:       title,
		Provider:    character.AvatarBackendXunfei,
		InputType:   inputType,
		Text:        text,
		Status:      "queued",
		Stage:       "queued",
		Message:     "Queued for Xunfei stream recording",
		Progress:    0,
		Width:       cfg.Xunfei.Width,
		Height:      cfg.Xunfei.Height,
		FPS:         cfg.Xunfei.FPS,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := writeOfflineVideoJob(jobDir, job); err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	crf := 23
	if r.cfg != nil {
		crf = r.cfg.Recording.CRF
	}
	runInput := xunfeiOfflineVideoRunInput{
		JobID:       jobID,
		CharacterID: ch.ID,
		Text:        text,
		Audio:       audio,
		TTSConfig:   ttsConfig,
		OutputPath:  filepath.Join(jobDir, "output.mp4"),
		CRF:         crf,
	}
	go r.runXunfeiOfflineVideoJob(runInput)

	writeJSON(w, http.StatusCreated, r.offlineVideoResponse(job))
}

func (r *Router) runXunfeiOfflineVideoJob(in xunfeiOfflineVideoRunInput) {
	update := func(stage string, progress int, message string) {
		_ = r.updateOfflineVideoJob(in.CharacterID, in.JobID, func(job *offlineVideoJob) {
			job.Status = "running"
			job.Stage = stage
			job.Progress = progress
			job.Message = message
		})
	}
	fail := func(stage string, err error) {
		_ = r.updateOfflineVideoJob(in.CharacterID, in.JobID, func(job *offlineVideoJob) {
			job.Status = "failed"
			job.Stage = stage
			job.Message = "Xunfei offline video generation failed"
			job.Error = err.Error()
			job.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()

	if err := ensureXunfeiOfflinePrerequisites(); err != nil {
		fail("start", err)
		return
	}

	jobDir := filepath.Dir(in.OutputPath)
	update("audio", 10, "Preparing driving audio")
	pcmPath := filepath.Join(jobDir, "driver.pcm")
	audioPath, sampleRate, err := r.prepareXunfeiOfflineAudio(ctx, in, pcmPath)
	if err != nil {
		fail("audio", err)
		return
	}
	if audioPath != "" {
		_ = r.updateOfflineVideoJob(in.CharacterID, in.JobID, func(job *offlineVideoJob) {
			job.AudioFilename = filepath.Base(audioPath)
			job.AudioSampleRate = sampleRate
		})
	}
	pcm, err := os.ReadFile(pcmPath)
	if err != nil {
		fail("audio", err)
		return
	}
	targetOutputDuration := xunfeiOfflineOutputTargetDuration(pcm)

	ch, err := r.charStore.Get(in.CharacterID)
	if err != nil {
		fail("start", err)
		return
	}
	sessionCharacter := xunfeiOfflineCharacter(ch)
	cfg, err := r.runXunfeiOfflineVideoAttempt(ctx, in, sessionCharacter, pcm, targetOutputDuration, 1, update)
	if err != nil && isRetryableXunfeiOfflineAttemptError(err) {
		update("retrying", 60, "Retrying Xunfei avatar stream")
		if waitErr := waitForXunfeiOfflineRetry(ctx); waitErr != nil {
			fail("drive", waitErr)
			return
		}
		cfg, err = r.runXunfeiOfflineVideoAttempt(ctx, in, sessionCharacter, pcm, targetOutputDuration, 2, update)
	}
	if err != nil {
		var attemptErr xunfeiOfflineAttemptError
		if errors.As(err, &attemptErr) && attemptErr.stage != "" {
			fail(attemptErr.stage, attemptErr.err)
			return
		}
		fail("drive", err)
		return
	}

	update("encoding", 88, "Finalizing Xunfei video")
	_ = r.updateOfflineVideoJob(in.CharacterID, in.JobID, func(job *offlineVideoJob) {
		job.Status = "completed"
		job.Stage = "completed"
		job.Message = "Offline video is ready"
		job.Progress = 100
		job.VideoFilename = filepath.Base(in.OutputPath)
		job.Width = cfg.Width
		job.Height = cfg.Height
		job.FPS = cfg.FPS
		job.AudioSampleRate = xunfeiOfflineAudioSampleRate
		job.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	})
}

func (r *Router) runXunfeiOfflineVideoAttempt(ctx context.Context, in xunfeiOfflineVideoRunInput, sessionCharacter *character.Character, pcm []byte, targetOutputDuration time.Duration, attempt int, update func(stage string, progress int, message string)) (*xunfeiAvatarSessionConfig, error) {
	jobDir := filepath.Dir(in.OutputPath)
	capturePath := filepath.Join(jobDir, fmt.Sprintf("capture-attempt-%d.flv", attempt))
	outputPath := filepath.Join(jobDir, fmt.Sprintf("output-attempt-%d.mp4", attempt))

	update("start", 22, "Starting Xunfei avatar stream")
	startCtx, startCancel := context.WithTimeout(ctx, 30*time.Second)
	runtime, cfg, err := startXunfeiAvatarSession(startCtx, sessionCharacter)
	startCancel()
	if err != nil {
		return nil, xunfeiOfflineAttemptError{stage: "start", err: err}
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = runtime.Stop(stopCtx)
		stopCancel()
	}()
	if err := ensureXunfeiOfflineStreamSupported(cfg.StreamURL); err != nil {
		return nil, xunfeiOfflineAttemptError{stage: "start", err: err}
	}

	update("recording", 34, "Recording Xunfei avatar stream")
	recordCtx, stopRecording := context.WithCancel(context.Background())
	recordDone := make(chan error, 1)
	go func() {
		recordDone <- recordXunfeiOfflineStream(recordCtx, cfg.StreamURL, capturePath, outputPath, in.CRF, targetOutputDuration)
	}()
	defer stopRecording()
	if err := waitForXunfeiOfflineCapture(ctx, capturePath, recordDone); err != nil {
		return nil, xunfeiOfflineAttemptError{stage: "recording", err: err}
	}

	update("drive", 58, "Sending audio to Xunfei avatar")
	drivingPCM := buildXunfeiOfflineDrivingPCM(pcm)
	driveStartedAt := time.Now()
	if err := sendXunfeiOfflinePCM(ctx, runtime, drivingPCM); err != nil {
		stopRecording()
		waitForXunfeiOfflineRecordDone(recordDone)
		return nil, xunfeiOfflineAttemptError{stage: "drive", err: err}
	}

	update("rendering", 76, "Waiting for Xunfei avatar render")
	if err := waitForXunfeiOfflineRender(ctx, drivingPCM, driveStartedAt); err != nil {
		return nil, xunfeiOfflineAttemptError{stage: "rendering", err: err}
	}
	if err := waitForXunfeiOfflineCaptureDuration(ctx, capturePath, targetOutputDuration); err != nil {
		return nil, xunfeiOfflineAttemptError{stage: "rendering", err: err}
	}

	stopRecording()
	if err := <-recordDone; err != nil {
		return nil, xunfeiOfflineAttemptError{stage: "encoding", err: err}
	}
	if outputPath != in.OutputPath {
		_ = os.Remove(in.OutputPath)
		if err := os.Rename(outputPath, in.OutputPath); err != nil {
			return nil, xunfeiOfflineAttemptError{stage: "encoding", err: err}
		}
	}
	finalCapturePath := filepath.Join(jobDir, "capture.flv")
	if capturePath != finalCapturePath {
		_ = os.Remove(finalCapturePath)
		_ = os.Rename(capturePath, finalCapturePath)
	}
	return cfg, nil
}

func waitForXunfeiOfflineRetry(ctx context.Context) error {
	timer := time.NewTimer(xunfeiOfflineRetryWait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func waitForXunfeiOfflineRecordDone(recordDone <-chan error) {
	timer := time.NewTimer(xunfeiOfflineStopTimeout)
	defer timer.Stop()
	select {
	case <-recordDone:
	case <-timer.C:
	}
}

func isRetryableXunfeiOfflineAttemptError(err error) bool {
	var attemptErr xunfeiOfflineAttemptError
	if !errors.As(err, &attemptErr) || attemptErr.stage != "drive" {
		return false
	}
	message := strings.ToLower(attemptErr.err.Error())
	return strings.Contains(message, "websocket: close sent") ||
		strings.Contains(message, "connection closed") ||
		strings.Contains(message, "close 1000") ||
		strings.Contains(message, "close 1001")
}

func (r *Router) prepareXunfeiOfflineAudio(ctx context.Context, in xunfeiOfflineVideoRunInput, outputPCMPath string) (string, int, error) {
	if strings.TrimSpace(in.Text) != "" {
		wavPath := filepath.Join(filepath.Dir(outputPCMPath), "driver.wav")
		_, err := r.orch.GenerateOfflineAudio(ctx, orchestrator.OfflineAudioGenerateInput{
			Text:       in.Text,
			TTSConfig:  in.TTSConfig,
			OutputPath: wavPath,
		})
		if err != nil {
			return "", 0, err
		}
		if err := convertAudioFileToXunfeiPCM(ctx, wavPath, outputPCMPath); err != nil {
			return "", 0, err
		}
		return wavPath, xunfeiOfflineAudioSampleRate, nil
	}
	if in.Audio == nil || len(in.Audio.PCM16) == 0 {
		return "", 0, errors.New("text or audio input is required")
	}
	if err := convertPCM16ToXunfeiPCM(ctx, in.Audio, outputPCMPath); err != nil {
		return "", 0, err
	}
	return outputPCMPath, xunfeiOfflineAudioSampleRate, nil
}

func xunfeiOfflineCharacter(ch *character.Character) *character.Character {
	if ch == nil || ch.Xunfei == nil {
		return ch
	}
	copyCharacter := *ch
	copyXunfei := *ch.Xunfei
	protocol := strings.ToLower(strings.TrimSpace(os.Getenv("XUNFEI_AVATAR_OFFLINE_PROTOCOL")))
	if protocol != "flv" && protocol != "rtmp" {
		protocol = strings.ToLower(strings.TrimSpace(copyXunfei.Protocol))
	}
	if protocol != "flv" && protocol != "rtmp" {
		protocol = "flv"
	}
	copyXunfei.Protocol = protocol
	copyCharacter.Xunfei = character.NormalizeXunfeiAvatarConfig(&copyXunfei)
	copyCharacter.AvatarBackend = character.AvatarBackendXunfei
	return &copyCharacter
}

func ensureXunfeiOfflineStreamSupported(streamURL string) error {
	switch detectXunfeiStreamTransport(streamURL) {
	case xunfeiStreamTransportRTMP, xunfeiStreamTransportHTTPFLV:
		return nil
	default:
		parsed, _ := url.Parse(strings.TrimSpace(streamURL))
		scheme := strings.TrimSpace(parsed.Scheme)
		if scheme == "" {
			scheme = "unknown"
		}
		return fmt.Errorf("Xunfei offline recording only supports RTMP or HTTP-FLV streams, got %s", scheme)
	}
}

func ensureXunfeiOfflinePrerequisites() error {
	appID := strings.TrimSpace(os.Getenv("XUNFEI_AVATAR_APP_ID"))
	apiKey := strings.TrimSpace(os.Getenv("XUNFEI_AVATAR_API_KEY"))
	apiSecret := strings.TrimSpace(os.Getenv("XUNFEI_AVATAR_API_SECRET"))
	if appID == "" || apiKey == "" || apiSecret == "" {
		return errors.New("Xunfei avatar credentials are not configured")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return errors.New("ffmpeg is required for Xunfei offline video generation")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		return errors.New("ffprobe is required for Xunfei offline video generation")
	}
	return nil
}

func convertAudioFileToXunfeiPCM(ctx context.Context, inputPath, outputPCMPath string) error {
	if strings.TrimSpace(inputPath) == "" {
		return errors.New("input audio path is required")
	}
	return runFFmpeg(ctx, []string{
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-i", inputPath,
		"-f", "s16le",
		"-ac", "1",
		"-ar", strconv.Itoa(xunfeiOfflineAudioSampleRate),
		outputPCMPath,
	})
}

func convertPCM16ToXunfeiPCM(ctx context.Context, audio *orchestrator.OfflineAudioInput, outputPCMPath string) error {
	if audio == nil || len(audio.PCM16) == 0 {
		return errors.New("pcm audio is required")
	}
	sampleRate := audio.SampleRate
	if sampleRate <= 0 {
		sampleRate = xunfeiOfflineAudioSampleRate
	}
	inputPCMPath := outputPCMPath + ".input"
	data := audio.PCM16
	if len(data)%2 != 0 {
		data = data[:len(data)-1]
	}
	if err := os.WriteFile(inputPCMPath, data, 0644); err != nil {
		return err
	}
	defer os.Remove(inputPCMPath)
	return runFFmpeg(ctx, []string{
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-f", "s16le",
		"-ac", "1",
		"-ar", strconv.Itoa(sampleRate),
		"-i", inputPCMPath,
		"-f", "s16le",
		"-ac", "1",
		"-ar", strconv.Itoa(xunfeiOfflineAudioSampleRate),
		outputPCMPath,
	})
}

func sendXunfeiOfflinePCM(ctx context.Context, runtime interface {
	SendPCMStream(context.Context, <-chan []byte) error
}, pcm []byte) error {
	if len(pcm) == 0 {
		return errors.New("driving pcm is empty")
	}
	sendCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	ch := make(chan []byte, 8)
	go func() {
		defer close(ch)
		const chunkBytes = 10 * 1024
		for len(pcm) > 0 {
			take := chunkBytes
			if len(pcm) < take {
				take = len(pcm)
			}
			part := append([]byte(nil), pcm[:take]...)
			pcm = pcm[take:]
			select {
			case ch <- part:
			case <-sendCtx.Done():
				return
			}
		}
	}()
	return runtime.SendPCMStream(sendCtx, ch)
}

func prependXunfeiOfflinePreroll(pcm []byte) []byte {
	prerollBytes := xunfeiOfflineSilenceBytes(xunfeiOfflinePrerollDuration())
	if prerollBytes <= 0 {
		return pcm
	}
	drivingPCM := make([]byte, prerollBytes+len(pcm))
	copy(drivingPCM[prerollBytes:], pcm)
	return drivingPCM
}

func buildXunfeiOfflineDrivingPCM(pcm []byte) []byte {
	drivingPCM := prependXunfeiOfflinePreroll(pcm)
	tailBytes := xunfeiOfflineSilenceBytes(xunfeiOfflineTailDuration())
	if tailBytes <= 0 {
		return drivingPCM
	}
	padded := make([]byte, len(drivingPCM)+tailBytes)
	copy(padded, drivingPCM)
	return padded
}

func xunfeiOfflineSilenceBytes(duration time.Duration) int {
	if duration <= 0 {
		return 0
	}
	bytesPerSecond := xunfeiOfflineAudioSampleRate * 2
	bytes := int(duration * time.Duration(bytesPerSecond) / time.Second)
	if bytes%2 != 0 {
		bytes--
	}
	if bytes < 0 {
		return 0
	}
	return bytes
}

func waitForXunfeiOfflineRender(ctx context.Context, pcm []byte, startedAt time.Time) error {
	wait := xunfeiOfflineRemainingRenderDuration(pcm, startedAt)
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func xunfeiOfflineRemainingRenderDuration(pcm []byte, startedAt time.Time) time.Duration {
	wait := xunfeiOfflineAudioDuration(pcm) + xunfeiOfflineRenderDrainWait - time.Since(startedAt)
	if wait < 0 {
		return 0
	}
	return wait
}

func xunfeiOfflineAudioDuration(pcm []byte) time.Duration {
	if len(pcm) <= 0 {
		return 0
	}
	bytesPerSecond := xunfeiOfflineAudioSampleRate * 2
	return time.Duration(len(pcm)) * time.Second / time.Duration(bytesPerSecond)
}

func xunfeiOfflineOutputTargetDuration(pcm []byte) time.Duration {
	audioDuration := xunfeiOfflineAudioDuration(pcm)
	if audioDuration <= 0 {
		return 0
	}
	return audioDuration + xunfeiOfflineOutputTailDuration()
}

func recordXunfeiOfflineStream(ctx context.Context, streamURL, capturePath, outputPath string, crf int, minOutputDuration time.Duration) error {
	if strings.TrimSpace(streamURL) == "" {
		return errors.New("Xunfei stream URL is required")
	}
	if err := os.MkdirAll(filepath.Dir(capturePath), 0755); err != nil {
		return err
	}
	_ = os.Remove(capturePath)
	_ = os.Remove(outputPath)
	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-i", streamURL,
		"-c", "copy",
		"-f", "flv",
		capturePath,
	}
	cmd, stderr, err := startFFmpeg(args)
	if err != nil {
		return err
	}
	waitErr := waitForCommandStop(ctx, cmd)
	if waitErr != nil && ctx.Err() == nil {
		return fmt.Errorf("Xunfei stream recording failed: %w: %s", waitErr, strings.TrimSpace(stderr.String()))
	}
	if err := requireNonEmptyFile(capturePath, "Xunfei stream recording produced no data"); err != nil {
		return err
	}
	return remuxXunfeiCapture(capturePath, outputPath, crf, minOutputDuration)
}

func waitForXunfeiOfflineCapture(ctx context.Context, capturePath string, recordDone <-chan error) error {
	wait := xunfeiOfflineCaptureWaitDuration()
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-recordDone:
			if err == nil {
				return errors.New("Xunfei stream recording stopped before audio was sent")
			}
			return err
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			select {
			case err := <-recordDone:
				if err == nil {
					return errors.New("Xunfei stream recording stopped before audio was sent")
				}
				return err
			default:
			}
			return nil
		case <-ticker.C:
			info, err := os.Stat(capturePath)
			if err == nil && info.Size() > 0 {
				return nil
			}
		}
	}
}

func waitForXunfeiOfflineCaptureDuration(ctx context.Context, capturePath string, target time.Duration) error {
	if target <= 0 {
		return nil
	}
	deadline := time.NewTimer(xunfeiOfflineDurationWait)
	defer deadline.Stop()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return nil
		case <-ticker.C:
			duration, err := probeMediaDurationWithTimeout(ctx, capturePath)
			if err == nil && duration+xunfeiOfflineDurationSlack >= target {
				return nil
			}
		}
	}
}

func xunfeiOfflineCaptureWaitDuration() time.Duration {
	value := strings.TrimSpace(os.Getenv("XUNFEI_AVATAR_OFFLINE_CAPTURE_WAIT_MS"))
	if value == "" {
		return xunfeiOfflineCaptureWait
	}
	ms, err := strconv.Atoi(value)
	if err != nil || ms < 0 {
		return xunfeiOfflineCaptureWait
	}
	return time.Duration(ms) * time.Millisecond
}

func xunfeiOfflinePrerollDuration() time.Duration {
	value := strings.TrimSpace(os.Getenv("XUNFEI_AVATAR_OFFLINE_PREROLL_MS"))
	if value == "" {
		return xunfeiOfflinePrerollWait
	}
	ms, err := strconv.Atoi(value)
	if err != nil || ms < 0 {
		return xunfeiOfflinePrerollWait
	}
	return time.Duration(ms) * time.Millisecond
}

func remuxXunfeiCapture(capturePath, outputPath string, crf int, minOutputDuration time.Duration) error {
	remuxCtx, cancel := context.WithTimeout(context.Background(), xunfeiOfflineRemuxTimeout)
	defer cancel()
	copyArgs := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-i", capturePath,
		"-c", "copy",
		"-movflags", "+faststart",
		outputPath,
	}
	if err := runFFmpeg(remuxCtx, copyArgs); err == nil {
		if err := requireNonEmptyFile(outputPath, "Xunfei MP4 output is empty"); err != nil {
			return err
		}
		return finalizeXunfeiOfflineOutput(outputPath, minOutputDuration, crf)
	}
	if crf <= 0 {
		crf = 23
	}
	reencodeArgs := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-i", capturePath,
		"-c:v", "libx264",
		"-preset", "fast",
		"-crf", strconv.Itoa(crf),
		"-pix_fmt", "yuv420p",
		"-c:a", "aac",
		"-b:a", "96k",
		"-movflags", "+faststart",
		outputPath,
	}
	if err := runFFmpeg(remuxCtx, reencodeArgs); err != nil {
		return err
	}
	if err := requireNonEmptyFile(outputPath, "Xunfei MP4 output is empty"); err != nil {
		return err
	}
	return finalizeXunfeiOfflineOutput(outputPath, minOutputDuration, crf)
}

func finalizeXunfeiOfflineOutput(outputPath string, minDuration time.Duration, crf int) error {
	if err := ensureXunfeiOfflineOutputDuration(outputPath, minDuration, crf); err != nil {
		return err
	}
	return trimXunfeiOfflineOutputTailSilence(outputPath, xunfeiOfflineOutputTailDuration(), crf)
}

func ensureXunfeiOfflineOutputDuration(outputPath string, minDuration time.Duration, crf int) error {
	if minDuration <= 0 {
		return nil
	}
	duration, err := probeMediaDurationWithTimeout(context.Background(), outputPath)
	if err != nil {
		return err
	}
	if duration+xunfeiOfflineDurationSlack >= minDuration {
		return nil
	}
	pad := minDuration - duration
	if crf <= 0 {
		crf = 23
	}
	tmpPath := outputPath + ".pad.mp4"
	_ = os.Remove(tmpPath)
	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-i", outputPath,
		"-vf", fmt.Sprintf("tpad=stop_mode=clone:stop_duration=%.3f", pad.Seconds()),
		"-af", fmt.Sprintf("apad=pad_dur=%.3f", pad.Seconds()),
		"-t", fmt.Sprintf("%.3f", minDuration.Seconds()),
		"-c:v", "libx264",
		"-preset", "fast",
		"-crf", strconv.Itoa(crf),
		"-pix_fmt", "yuv420p",
		"-c:a", "aac",
		"-b:a", "96k",
		"-movflags", "+faststart",
		tmpPath,
	}
	padCtx, cancel := context.WithTimeout(context.Background(), xunfeiOfflineRemuxTimeout)
	defer cancel()
	if err := runFFmpeg(padCtx, args); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := requireNonEmptyFile(tmpPath, "Xunfei MP4 output is empty"); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, outputPath)
}

func trimXunfeiOfflineOutputTailSilence(outputPath string, keepSilence time.Duration, crf int) error {
	if keepSilence < 0 {
		return nil
	}
	duration, err := probeMediaDurationWithTimeout(context.Background(), outputPath)
	if err != nil {
		return err
	}
	silenceStart, ok, err := detectXunfeiOfflineTrailingSilence(outputPath, duration)
	if err != nil || !ok {
		return err
	}
	trimDuration := silenceStart + keepSilence
	if trimDuration <= xunfeiOfflineDurationSlack {
		return nil
	}
	if trimDuration+xunfeiOfflineDurationSlack >= duration {
		return nil
	}
	if crf <= 0 {
		crf = 23
	}
	tmpPath := outputPath + ".trim.mp4"
	_ = os.Remove(tmpPath)
	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-i", outputPath,
		"-t", fmt.Sprintf("%.3f", trimDuration.Seconds()),
		"-c:v", "libx264",
		"-preset", "fast",
		"-crf", strconv.Itoa(crf),
		"-pix_fmt", "yuv420p",
		"-c:a", "aac",
		"-b:a", "96k",
		"-movflags", "+faststart",
		tmpPath,
	}
	trimCtx, cancel := context.WithTimeout(context.Background(), xunfeiOfflineRemuxTimeout)
	defer cancel()
	if err := runFFmpeg(trimCtx, args); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := requireNonEmptyFile(tmpPath, "Xunfei MP4 output is empty"); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, outputPath)
}

func detectXunfeiOfflineTrailingSilence(path string, duration time.Duration) (time.Duration, bool, error) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return 0, false, errors.New("ffmpeg is required for Xunfei offline video generation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), xunfeiOfflineRemuxTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-hide_banner",
		"-nostats",
		"-i", path,
		"-af", "silencedetect=noise=-35dB:d=0.5",
		"-f", "null",
		"-",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return 0, false, fmt.Errorf("ffmpeg silencedetect failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return parseXunfeiOfflineTrailingSilence(stderr.String(), duration)
}

func parseXunfeiOfflineTrailingSilence(output string, duration time.Duration) (time.Duration, bool, error) {
	var currentStart time.Duration
	haveCurrentStart := false
	var lastStart time.Duration
	var lastEnd time.Duration
	haveLast := false
	for _, line := range strings.Split(output, "\n") {
		if _, rest, ok := strings.Cut(line, "silence_start:"); ok {
			start, ok := parseXunfeiOfflineSilenceSeconds(rest)
			if !ok {
				return 0, false, fmt.Errorf("parse silence_start from %q", line)
			}
			currentStart = start
			haveCurrentStart = true
			continue
		}
		if _, rest, ok := strings.Cut(line, "silence_end:"); ok {
			end, ok := parseXunfeiOfflineSilenceSeconds(rest)
			if !ok {
				return 0, false, fmt.Errorf("parse silence_end from %q", line)
			}
			if haveCurrentStart {
				lastStart = currentStart
				lastEnd = end
				haveLast = true
				haveCurrentStart = false
			}
		}
	}
	if haveCurrentStart {
		return currentStart, true, nil
	}
	if !haveLast {
		return 0, false, nil
	}
	if lastEnd+xunfeiOfflineDurationSlack < duration {
		return 0, false, nil
	}
	return lastStart, true, nil
}

func parseXunfeiOfflineSilenceSeconds(value string) (time.Duration, bool) {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) == 0 {
		return 0, false
	}
	seconds, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || seconds < 0 {
		return 0, false
	}
	return time.Duration(seconds * float64(time.Second)), true
}

func runFFmpeg(ctx context.Context, args []string) error {
	cmd, stderr, err := startFFmpeg(args)
	if err != nil {
		return err
	}
	if err := waitForCommandStop(ctx, cmd); err != nil {
		return fmt.Errorf("ffmpeg failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func startFFmpeg(args []string) (*exec.Cmd, *bytes.Buffer, error) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, nil, errors.New("ffmpeg is required for Xunfei offline video generation")
	}
	cmd := exec.Command(ffmpegPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("start ffmpeg: %w", err)
	}
	return cmd, &stderr, nil
}

func probeMediaDurationWithTimeout(ctx context.Context, path string) (time.Duration, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return probeMediaDuration(probeCtx, path)
}

func probeMediaDuration(ctx context.Context, path string) (time.Duration, error) {
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		return 0, errors.New("ffprobe is required for Xunfei offline video generation")
	}
	cmd := exec.CommandContext(ctx, ffprobePath,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe duration failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	seconds, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		return 0, fmt.Errorf("parse media duration: %w", err)
	}
	if seconds <= 0 {
		return 0, nil
	}
	return time.Duration(seconds * float64(time.Second)), nil
}

func waitForCommandStop(ctx context.Context, cmd *exec.Cmd) error {
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		if cmd.Process != nil {
			_ = cmd.Process.Signal(os.Interrupt)
		}
		select {
		case err := <-done:
			return err
		case <-time.After(xunfeiOfflineStopTimeout):
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			<-done
			return ctx.Err()
		}
	}
}

func requireNonEmptyFile(path, message string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.New(message)
		}
		return err
	}
	if info.Size() <= 0 {
		return errors.New(message)
	}
	return nil
}

func xunfeiOfflineTailDuration() time.Duration {
	value := strings.TrimSpace(os.Getenv("XUNFEI_AVATAR_OFFLINE_TAIL_MS"))
	if value == "" {
		return xunfeiOfflineTailWait
	}
	ms, err := strconv.Atoi(value)
	if err != nil || ms < 0 {
		return xunfeiOfflineTailWait
	}
	return time.Duration(ms) * time.Millisecond
}

func xunfeiOfflineOutputTailDuration() time.Duration {
	value := strings.TrimSpace(os.Getenv("XUNFEI_AVATAR_OFFLINE_OUTPUT_TAIL_MS"))
	if value == "" {
		return xunfeiOfflineOutputTailWait
	}
	ms, err := strconv.Atoi(value)
	if err != nil || ms < 0 {
		return xunfeiOfflineOutputTailWait
	}
	return time.Duration(ms) * time.Millisecond
}
