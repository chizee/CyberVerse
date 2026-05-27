package orchestrator

import (
	"context"
	"encoding/binary"
	"errors"
	"log"
	"math"
	"sync"
	"time"

	"github.com/cyberverse/server/internal/mediapeer"
	"github.com/cyberverse/server/internal/pb"
	"google.golang.org/protobuf/proto"
)

const (
	silentAvatarSampleRate       = 24000
	silentAvatarIdleChunk        = 100 * time.Millisecond
	silentAvatarDefaultMaxLead   = 1200 * time.Millisecond
	silentAvatarMaxLeadCap       = 5 * time.Second
	silentAvatarSpeechQueueDepth = 128
)

type silentAvatarSpanKind int

const (
	silentAvatarSpanIdle silentAvatarSpanKind = iota
	silentAvatarSpanSpeech
)

type silentAvatarPlaybackSpan struct {
	kind    silentAvatarSpanKind
	turnSeq uint64
	pcm     []byte
	isFinal bool
}

type silentAvatarSegmentMeta struct {
	hasSpeech     bool
	turnSeq       uint64
	finalTurnSeqs []uint64
}

type silentAvatarTimeline struct {
	mu              sync.Mutex
	spans           []silentAvatarPlaybackSpan
	modelInSamples  int64
	videoOutSamples int64
	carryNumer      int64
}

func (t *silentAvatarTimeline) append(kind silentAvatarSpanKind, turnSeq uint64, modelPCM, playbackPCM []byte, isFinal bool) {
	if len(playbackPCM)%2 != 0 {
		playbackPCM = playbackPCM[:len(playbackPCM)-1]
	}
	modelSamples := len(modelPCM) / 2
	if len(playbackPCM) == 0 && modelSamples == 0 {
		return
	}
	pcmCopy := append([]byte(nil), playbackPCM...)
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(pcmCopy) > 0 {
		t.spans = append(t.spans, silentAvatarPlaybackSpan{kind: kind, turnSeq: turnSeq, pcm: pcmCopy, isFinal: isFinal})
	}
	t.modelInSamples += int64(modelSamples)
}

func (t *silentAvatarTimeline) leadSamples() int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.modelInSamples - t.videoOutSamples
}

func (t *silentAvatarTimeline) take(frames, fps int) ([]byte, silentAvatarSegmentMeta) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if frames <= 0 || fps <= 0 {
		return nil, silentAvatarSegmentMeta{}
	}
	numer := int64(frames*silentAvatarSampleRate) + t.carryNumer
	wantSamples := int(numer / int64(fps))
	t.carryNumer = numer % int64(fps)
	if wantSamples <= 0 {
		wantSamples = desiredSamplesForVideo(frames, fps, silentAvatarSampleRate)
	}
	if wantSamples <= 0 {
		return nil, silentAvatarSegmentMeta{}
	}

	wantBytes := wantSamples * 2
	out := make([]byte, wantBytes)
	meta := silentAvatarSegmentMeta{}
	copied := 0
	for copied < wantBytes && len(t.spans) > 0 {
		span := &t.spans[0]
		takeBytes := wantBytes - copied
		if takeBytes > len(span.pcm) {
			takeBytes = len(span.pcm)
		}
		if takeBytes%2 != 0 {
			takeBytes--
		}
		if takeBytes <= 0 {
			t.spans = t.spans[1:]
			continue
		}
		copy(out[copied:], span.pcm[:takeBytes])
		if span.kind == silentAvatarSpanSpeech {
			meta.hasSpeech = true
			if span.turnSeq > meta.turnSeq {
				meta.turnSeq = span.turnSeq
			}
		}
		copied += takeBytes
		span.pcm = span.pcm[takeBytes:]
		if len(span.pcm) == 0 {
			if span.isFinal && span.turnSeq > 0 {
				meta.finalTurnSeqs = append(meta.finalTurnSeqs, span.turnSeq)
			}
			t.spans = t.spans[1:]
		}
	}
	t.videoOutSamples += int64(wantSamples)
	return out, meta
}

type silentAvatarSpeechCallbacks struct {
	TraceLabel   func(segSeq int64) string
	UserFinalAt  time.Time
	OnFirstVideo func(width, height, fps int)
	OnSegment    func(rgb []byte, pcm []byte, sampleRate int)
	OnFinished   func()
}

type silentAvatarSpeechState struct {
	turnSeq    uint64
	callbacks  silentAvatarSpeechCallbacks
	doneCh     chan error
	segSeq     int64
	mediaStart int64
	firstVideo bool
	cancelOnce sync.Once
	finishOnce sync.Once
}

type silentAvatarAudioItem struct {
	kind        silentAvatarSpanKind
	turnSeq     uint64
	modelPCM    []byte
	playbackPCM []byte
	isFinal     bool
	speech      *silentAvatarSpeechState
}

type silentAvatarRuntime struct {
	orchestrator *Orchestrator
	sessionID    string
	ctx          context.Context
	cancel       context.CancelFunc
	doneCh       chan struct{}
	modelAudioCh chan *pb.AudioChunk
	speechCh     chan silentAvatarAudioItem
	timeline     silentAvatarTimeline
	maxLead      time.Duration

	stateMu      sync.Mutex
	speeches     map[uint64]*silentAvatarSpeechState
	activeSpeech *silentAvatarSpeechState
	finalSpeech  *silentAvatarSpeechState
	idleSegSeq   int64
	idleMediaMS  int64
}

func newSilentAvatarRuntime(parent context.Context, o *Orchestrator, sessionID string, maxLead time.Duration) *silentAvatarRuntime {
	if maxLead <= 0 {
		maxLead = silentAvatarDefaultMaxLead
	}
	ctx, cancel := context.WithCancel(parent)
	return &silentAvatarRuntime{
		orchestrator: o,
		sessionID:    sessionID,
		ctx:          ctx,
		cancel:       cancel,
		doneCh:       make(chan struct{}),
		modelAudioCh: make(chan *pb.AudioChunk),
		speechCh:     make(chan silentAvatarAudioItem, silentAvatarSpeechQueueDepth),
		maxLead:      maxLead,
		speeches:     make(map[uint64]*silentAvatarSpeechState),
	}
}

func (r *silentAvatarRuntime) start() {
	go r.run()
}

func (r *silentAvatarRuntime) stop() {
	r.cancel()
}

func (r *silentAvatarRuntime) wait(timeout time.Duration) {
	if timeout <= 0 {
		<-r.doneCh
		return
	}
	select {
	case <-r.doneCh:
	case <-time.After(timeout):
	}
}

func (r *silentAvatarRuntime) beginSpeech(turnSeq uint64, callbacks silentAvatarSpeechCallbacks) (*silentAvatarSpeechState, error) {
	if turnSeq == 0 {
		return nil, errors.New("silent avatar speech turnSeq is zero")
	}
	state := &silentAvatarSpeechState{
		turnSeq:   turnSeq,
		callbacks: callbacks,
		doneCh:    make(chan error, 1),
	}
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	if r.ctx.Err() != nil {
		return nil, r.ctx.Err()
	}
	r.speeches[turnSeq] = state
	return state, nil
}

func (r *silentAvatarRuntime) submitSpeech(ctx context.Context, speech *silentAvatarSpeechState, chunk *pb.AudioChunk) error {
	if speech == nil {
		return errors.New("silent avatar speech is nil")
	}
	modelPCM, _ := normalizedPCM16Mono(chunk, silentAvatarSampleRate)
	if len(modelPCM) == 0 {
		return nil
	}
	playbackPCM := append([]byte(nil), modelPCM...)
	item := silentAvatarAudioItem{
		kind:        silentAvatarSpanSpeech,
		turnSeq:     speech.turnSeq,
		modelPCM:    modelPCM,
		playbackPCM: playbackPCM,
		speech:      speech,
	}
	r.markSpeechActive(speech)
	if err := r.enqueueSpeech(ctx, item); err != nil {
		r.cancelSpeech(speech, err)
		return err
	}
	return nil
}

func (r *silentAvatarRuntime) finishSpeech(ctx context.Context, speech *silentAvatarSpeechState) error {
	if speech == nil {
		return errors.New("silent avatar speech is nil")
	}
	silence := buildTrailingSilence(silentAvatarSampleRate)
	modelPCM := append([]byte(nil), silence.GetData()...)
	item := silentAvatarAudioItem{
		kind:        silentAvatarSpanSpeech,
		turnSeq:     speech.turnSeq,
		modelPCM:    modelPCM,
		playbackPCM: modelPCM,
		isFinal:     true,
		speech:      speech,
	}
	if err := r.enqueueSpeech(ctx, item); err != nil {
		return err
	}
	select {
	case err := <-speech.doneCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-r.ctx.Done():
		return r.ctx.Err()
	}
}

func (r *silentAvatarRuntime) cancelSpeech(speech *silentAvatarSpeechState, err error) {
	if speech == nil {
		return
	}
	if err == nil {
		err = context.Canceled
	}
	speech.cancelOnce.Do(func() {
		r.stateMu.Lock()
		delete(r.speeches, speech.turnSeq)
		if r.activeSpeech == speech {
			r.activeSpeech = nil
		}
		if r.finalSpeech == speech {
			r.finalSpeech = nil
		}
		r.stateMu.Unlock()
		select {
		case speech.doneCh <- err:
		default:
		}
	})
}

func (r *silentAvatarRuntime) enqueueSpeech(ctx context.Context, item silentAvatarAudioItem) error {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case r.speechCh <- item:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-r.ctx.Done():
		return r.ctx.Err()
	}
}

func (r *silentAvatarRuntime) run() {
	defer close(r.doneCh)
	defer close(r.modelAudioCh)
	r.orchestrator.avatarMu.Lock()
	defer r.orchestrator.avatarMu.Unlock()

	videoCh, errCh := r.orchestrator.inference.GenerateAvatarStream(r.ctx, r.modelAudioCh)
	feedDone := make(chan struct{})
	go func() {
		defer close(feedDone)
		r.feedLoop()
	}()
	r.receiveLoop(videoCh, errCh)
	r.cancel()
	<-feedDone
	r.finishAllSpeeches(context.Canceled)
}

func (r *silentAvatarRuntime) feedLoop() {
	ticker := time.NewTicker(silentAvatarIdleChunk)
	defer ticker.Stop()
	r.fillIdleLead()
	for {
		select {
		case <-r.ctx.Done():
			return
		case item := <-r.speechCh:
			r.sendSpeechItem(item)
		case <-ticker.C:
			for {
				select {
				case item := <-r.speechCh:
					r.sendSpeechItem(item)
					continue
				default:
				}
				break
			}
			r.fillIdleLead()
		}
	}
}

func (r *silentAvatarRuntime) sendSpeechItem(item silentAvatarAudioItem) {
	if item.speech == nil || len(item.modelPCM) == 0 {
		return
	}
	r.stateMu.Lock()
	if _, ok := r.speeches[item.turnSeq]; !ok {
		r.stateMu.Unlock()
		return
	}
	if r.activeSpeech == nil {
		r.activeSpeech = item.speech
	}
	if item.isFinal {
		r.finalSpeech = item.speech
	}
	r.stateMu.Unlock()
	r.sendAudioItem(item)
}

func (r *silentAvatarRuntime) markSpeechActive(speech *silentAvatarSpeechState) {
	if speech == nil {
		return
	}
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	if _, ok := r.speeches[speech.turnSeq]; !ok {
		return
	}
	if r.activeSpeech == nil {
		r.activeSpeech = speech
	}
}

func (r *silentAvatarRuntime) shouldSendIdle() bool {
	r.stateMu.Lock()
	activeSpeech := r.activeSpeech
	blockedBySpeech := activeSpeech != nil && r.finalSpeech == nil && activeSpeech.firstVideo
	r.stateMu.Unlock()
	if blockedBySpeech {
		return false
	}
	maxLeadSamples := int64(math.Round(r.maxLead.Seconds() * silentAvatarSampleRate))
	return r.timeline.leadSamples() < maxLeadSamples
}

func (r *silentAvatarRuntime) fillIdleLead() {
	for r.shouldSendIdle() {
		if !r.sendIdle() {
			return
		}
	}
}

func (r *silentAvatarRuntime) sendIdle() bool {
	modelPCM := buildIdleBreathingPCM(silentAvatarIdleChunk, silentAvatarSampleRate)
	if len(modelPCM) == 0 {
		return false
	}
	item := silentAvatarAudioItem{
		kind:        silentAvatarSpanIdle,
		modelPCM:    modelPCM,
		playbackPCM: make([]byte, len(modelPCM)),
	}
	return r.sendAudioItem(item)
}

func (r *silentAvatarRuntime) sendAudioItem(item silentAvatarAudioItem) bool {
	chunk := &pb.AudioChunk{
		Data:       append([]byte(nil), item.modelPCM...),
		SampleRate: silentAvatarSampleRate,
		Channels:   1,
		Format:     "pcm_s16le",
		IsFinal:    false,
	}
	select {
	case r.modelAudioCh <- chunk:
		r.timeline.append(item.kind, item.turnSeq, item.modelPCM, item.playbackPCM, item.isFinal)
		return true
	case <-r.ctx.Done():
		return false
	}
}

func (r *silentAvatarRuntime) receiveLoop(videoCh <-chan *pb.VideoChunk, errCh <-chan error) {
	for {
		select {
		case <-r.ctx.Done():
			return
		case chunk, ok := <-videoCh:
			if !ok {
				select {
				case err, ok := <-errCh:
					if ok && err != nil && !errors.Is(err, context.Canceled) {
						log.Printf("silent avatar stream error session=%s: %v", r.sessionID, err)
					}
				default:
				}
				return
			}
			r.publishVideoChunk(chunk)
		}
	}
}

func (r *silentAvatarRuntime) publishVideoChunk(chunk *pb.VideoChunk) {
	if chunk == nil || len(chunk.GetData()) == 0 {
		return
	}
	nf := int(chunk.GetNumFrames())
	width := int(chunk.GetWidth())
	height := int(chunk.GetHeight())
	if nf <= 0 && width*height*3 > 0 {
		nf = len(chunk.GetData()) / (width * height * 3)
	}
	if nf <= 0 {
		return
	}
	fps := int(chunk.GetFps())
	if fps <= 0 {
		fps = 25
	}

	pcm, meta := r.timeline.take(nf, fps)
	segDurationMS := durationMSForVideo(nf, fps)
	peer := r.lookupPeer()
	if peer == nil {
		r.finishFinalSpeechIfNeeded(chunk.GetIsFinal(), nil)
		return
	}

	var speech *silentAvatarSpeechState
	var speechCallbacks silentAvatarSpeechCallbacks
	var traceLabel string
	var epoch uint64
	var segSeq int64
	var mediaStartMS int64
	var userFinalAt time.Time
	var firstSpeechVideo bool
	if meta.hasSpeech && meta.turnSeq > 0 {
		epoch = meta.turnSeq
		r.stateMu.Lock()
		speech = r.speeches[meta.turnSeq]
		if speech != nil {
			speech.segSeq++
			segSeq = speech.segSeq
			mediaStartMS = speech.mediaStart
			speech.mediaStart += segDurationMS
			if !speech.firstVideo {
				speech.firstVideo = true
				firstSpeechVideo = true
			}
			speechCallbacks = speech.callbacks
		}
		r.stateMu.Unlock()
		userFinalAt = speechCallbacks.UserFinalAt
		if speechCallbacks.TraceLabel != nil {
			traceLabel = speechCallbacks.TraceLabel(segSeq)
		}
		if firstSpeechVideo && speechCallbacks.OnFirstVideo != nil {
			speechCallbacks.OnFirstVideo(width, height, fps)
		}
	} else {
		r.idleSegSeq++
		segSeq = r.idleSegSeq
		mediaStartMS = r.idleMediaMS
		r.idleMediaMS += segDurationMS
	}

	raw := &mediapeer.RawAVSegment{
		TraceLabel:   traceLabel,
		Epoch:        epoch,
		SegmentSeq:   segSeq,
		MediaStartMS: mediaStartMS,
		DurationMS:   segDurationMS,
		RGB:          chunk.GetData(),
		PCM:          pcm,
		UserFinalAt:  userFinalAt,
		SampleRate:   silentAvatarSampleRate,
		Width:        width,
		Height:       height,
		FPS:          fps,
		NumFrames:    nf,
		Supersedable: !meta.hasSpeech,
	}
	if err := peer.SendAVSegment(raw); err != nil {
		log.Printf("silent avatar SendAVSegment failed session=%s turn=%d: %v", r.sessionID, epoch, err)
	}
	if speech != nil && speechCallbacks.OnSegment != nil {
		speechCallbacks.OnSegment(chunk.GetData(), pcm, silentAvatarSampleRate)
	}
	if len(meta.finalTurnSeqs) > 0 {
		if peer != nil {
			peer.WaitAVDrain(10 * time.Second)
		}
		for _, turnSeq := range meta.finalTurnSeqs {
			r.finishSpeechTurnIfFinalConsumed(turnSeq, nil)
		}
	} else if chunk.GetIsFinal() {
		r.finishFinalSpeechIfNeeded(true, nil)
	}
}

func (r *silentAvatarRuntime) speechForTurn(turnSeq uint64) *silentAvatarSpeechState {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	return r.speeches[turnSeq]
}

func (r *silentAvatarRuntime) finishFinalSpeechIfNeeded(isFinal bool, err error) {
	if !isFinal {
		return
	}
	r.stateMu.Lock()
	speech := r.finalSpeech
	r.stateMu.Unlock()
	if speech == nil {
		return
	}
	r.finishSpeechTurnIfFinalConsumed(speech.turnSeq, err)
}

func (r *silentAvatarRuntime) finishSpeechTurnIfFinalConsumed(turnSeq uint64, err error) {
	if turnSeq == 0 {
		return
	}
	r.stateMu.Lock()
	speech := r.speeches[turnSeq]
	if speech != nil {
		delete(r.speeches, speech.turnSeq)
		if r.activeSpeech == speech {
			r.activeSpeech = nil
		}
		if r.finalSpeech == speech {
			r.finalSpeech = nil
		}
	}
	r.stateMu.Unlock()
	if speech == nil {
		return
	}
	speech.finishOnce.Do(func() {
		if speech.callbacks.OnFinished != nil {
			speech.callbacks.OnFinished()
		}
		select {
		case speech.doneCh <- err:
		default:
		}
	})
}

func (r *silentAvatarRuntime) finishAllSpeeches(err error) {
	r.stateMu.Lock()
	speeches := make([]*silentAvatarSpeechState, 0, len(r.speeches))
	for _, speech := range r.speeches {
		speeches = append(speeches, speech)
	}
	r.speeches = make(map[uint64]*silentAvatarSpeechState)
	r.activeSpeech = nil
	r.finalSpeech = nil
	r.stateMu.Unlock()
	for _, speech := range speeches {
		select {
		case speech.doneCh <- err:
		default:
		}
	}
}

func (r *silentAvatarRuntime) lookupPeer() mediapeer.MediaPeer {
	r.orchestrator.mu.RLock()
	defer r.orchestrator.mu.RUnlock()
	return r.orchestrator.peers[r.sessionID]
}

func (o *Orchestrator) silentRuntime(sessionID string) *silentAvatarRuntime {
	if o == nil {
		return nil
	}
	o.silentMu.Lock()
	defer o.silentMu.Unlock()
	return o.silentRuntimes[sessionID]
}

func (o *Orchestrator) hasOtherSilentAvatarRuntime(sessionID string) bool {
	if o == nil {
		return false
	}
	o.silentMu.Lock()
	defer o.silentMu.Unlock()
	for id := range o.silentRuntimes {
		if id != sessionID {
			return true
		}
	}
	return false
}

func (o *Orchestrator) startSilentAvatarRuntime(ctx context.Context, session *Session) {
	if !o.useSilentInference() || session == nil || o.inference == nil {
		return
	}
	maxLead := silentAvatarDefaultMaxLead
	if info, err := o.inference.AvatarInfo(ctx); err == nil && info != nil {
		maxLead = silentAvatarMaxLead(info)
	}
	o.silentMu.Lock()
	if existing := o.silentRuntimes[session.ID]; existing != nil {
		o.silentMu.Unlock()
		return
	}
	runtime := newSilentAvatarRuntime(context.Background(), o, session.ID, maxLead)
	o.silentRuntimes[session.ID] = runtime
	o.silentMu.Unlock()
	runtime.start()
}

func silentAvatarMaxLead(info *pb.AvatarInfo) time.Duration {
	chunkDuration := silentAvatarDefaultMaxLead
	if info != nil {
		if d := time.Duration(float64(info.GetChunkDurationS()) * float64(time.Second)); d > 0 {
			chunkDuration = d
		}
		frames := int(info.GetFramesPerChunk())
		fps := int(info.GetOutputFps())
		if frames > 0 && fps > 0 {
			frameDuration := time.Duration(math.Round(float64(frames) * float64(time.Second) / float64(fps)))
			if frameDuration > chunkDuration {
				chunkDuration = frameDuration
			}
		}
	}
	if chunkDuration > silentAvatarMaxLeadCap {
		return silentAvatarMaxLeadCap
	}
	return chunkDuration
}

func (o *Orchestrator) stopSilentAvatarRuntime(sessionID string) *silentAvatarRuntime {
	if o == nil {
		return nil
	}
	o.silentMu.Lock()
	runtime := o.silentRuntimes[sessionID]
	if runtime != nil {
		delete(o.silentRuntimes, sessionID)
	}
	o.silentMu.Unlock()
	if runtime != nil {
		runtime.stop()
	}
	return runtime
}

func (o *Orchestrator) restartSilentAvatarRuntime(ctx context.Context, session *Session) {
	if !o.useSilentInference() || session == nil {
		return
	}
	if runtime := o.stopSilentAvatarRuntime(session.ID); runtime != nil {
		runtime.wait(3 * time.Second)
	}
	if session.CharacterID != "" {
		_, imageFilename, err := o.activeCharacterImage(session.CharacterID)
		if err == nil && imageFilename != "" {
			if setErr := o.setAvatarFromCharacterImage(ctx, session.ID, session.CharacterID, imageFilename); setErr != nil {
				log.Printf("silent avatar restart SetAvatar failed session=%s character=%s: %v", session.ID, session.CharacterID, setErr)
			}
		}
	}
	o.startSilentAvatarRuntime(ctx, session)
}

func (o *Orchestrator) stopAllSilentAvatarRuntimes() {
	if o == nil {
		return
	}
	o.silentMu.Lock()
	runtimes := make([]*silentAvatarRuntime, 0, len(o.silentRuntimes))
	for _, runtime := range o.silentRuntimes {
		runtimes = append(runtimes, runtime)
	}
	o.silentRuntimes = make(map[string]*silentAvatarRuntime)
	o.silentMu.Unlock()
	for _, runtime := range runtimes {
		runtime.stop()
	}
	for _, runtime := range runtimes {
		runtime.wait(3 * time.Second)
	}
}

func (o *Orchestrator) runSilentAvatarSpeechStream(
	ctx context.Context,
	sessionID string,
	turnSeq uint64,
	audioCh <-chan *pb.AudioChunk,
	errCh <-chan error,
	callbacks silentAvatarSpeechCallbacks,
	onAudio func(pcm []byte, sampleRate int),
) error {
	runtime := o.silentRuntime(sessionID)
	if runtime == nil {
		return errors.New("silent avatar runtime is not running")
	}
	speech, err := runtime.beginSpeech(turnSeq, callbacks)
	if err != nil {
		return err
	}
	sentAudio := false
	completed := false
	defer func() {
		if !completed {
			runtime.cancelSpeech(speech, context.Canceled)
		}
	}()

	for audioCh != nil || errCh != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case chunk, ok := <-audioCh:
			if !ok {
				audioCh = nil
				continue
			}
			pcm, sampleRate := audioChunkToPCM16(chunk)
			if len(pcm) > 0 && onAudio != nil {
				onAudio(pcm, sampleRate)
			}
			if len(pcm) == 0 {
				continue
			}
			if err := runtime.submitSpeech(ctx, speech, proto.Clone(chunk).(*pb.AudioChunk)); err != nil {
				return err
			}
			sentAudio = true
		case err, ok := <-errCh:
			if !ok {
				errCh = nil
				continue
			}
			if err != nil {
				return err
			}
		}
	}
	if !sentAudio {
		completed = true
		runtime.cancelSpeech(speech, nil)
		return nil
	}
	if err := runtime.finishSpeech(ctx, speech); err != nil {
		return err
	}
	completed = true
	return nil
}

func normalizedPCM16Mono(chunk *pb.AudioChunk, targetSampleRate int) ([]byte, int) {
	if targetSampleRate <= 0 {
		targetSampleRate = silentAvatarSampleRate
	}
	pcm, sampleRate := audioChunkToPCM16(chunk)
	if len(pcm) == 0 {
		return nil, sampleRate
	}
	channels := 1
	if chunk != nil && chunk.GetChannels() > 1 {
		channels = int(chunk.GetChannels())
	}
	if channels > 1 {
		pcm = downmixPCM16ToMono(pcm, channels)
	}
	if sampleRate <= 0 {
		sampleRate = targetSampleRate
	}
	return resamplePCM16Mono(pcm, sampleRate, targetSampleRate), sampleRate
}

func downmixPCM16ToMono(pcm []byte, channels int) []byte {
	if channels <= 1 || len(pcm) < channels*2 {
		return pcm
	}
	frameBytes := channels * 2
	frameCount := len(pcm) / frameBytes
	out := make([]byte, frameCount*2)
	for frame := 0; frame < frameCount; frame++ {
		sum := 0
		base := frame * frameBytes
		for ch := 0; ch < channels; ch++ {
			sum += int(int16(binary.LittleEndian.Uint16(pcm[base+ch*2:])))
		}
		sample := int16(sum / channels)
		binary.LittleEndian.PutUint16(out[frame*2:], uint16(sample))
	}
	return out
}

func resamplePCM16Mono(pcm []byte, srcRate, dstRate int) []byte {
	if len(pcm)%2 != 0 {
		pcm = pcm[:len(pcm)-1]
	}
	if len(pcm) == 0 || srcRate <= 0 || dstRate <= 0 {
		return nil
	}
	if srcRate == dstRate {
		return append([]byte(nil), pcm...)
	}
	inSamples := len(pcm) / 2
	if inSamples <= 0 {
		return nil
	}
	outSamples := int(math.Round(float64(inSamples) * float64(dstRate) / float64(srcRate)))
	if outSamples <= 0 {
		return nil
	}
	out := make([]byte, outSamples*2)
	for i := 0; i < outSamples; i++ {
		pos := float64(i) * float64(srcRate) / float64(dstRate)
		idx := int(pos)
		if idx >= inSamples-1 {
			sample := int16(binary.LittleEndian.Uint16(pcm[(inSamples-1)*2:]))
			binary.LittleEndian.PutUint16(out[i*2:], uint16(sample))
			continue
		}
		frac := pos - float64(idx)
		s0 := float64(int16(binary.LittleEndian.Uint16(pcm[idx*2:])))
		s1 := float64(int16(binary.LittleEndian.Uint16(pcm[(idx+1)*2:])))
		sample := int16(math.Round(s0 + (s1-s0)*frac))
		binary.LittleEndian.PutUint16(out[i*2:], uint16(sample))
	}
	return out
}
