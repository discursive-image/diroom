// SPDX-FileCopyrightText: 2019 KIM KeepInMind GmbH
// SPDX-FileCopyrightText: 2020 KIM KeepInMind GmbH
//
// SPDX-License-Identifier: MIT

package google

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"time"

	speech "cloud.google.com/go/speech/apiv1"
	"git.keepinmind.info/subgensdk/sgenc"
	"git.keepinmind.info/subgensdk/sgtr/ffmpeg"
	"git.keepinmind.info/subgensdk/sgtr/tr"
	"github.com/golang/protobuf/ptypes/duration"
	"github.com/kim-company/pmux/pwrap"
	speechpb "google.golang.org/genproto/googleapis/cloud/speech/v1"
)

const progressPollInterval = time.Millisecond * 2500
const progressDescription = "transcribing"

// inBitrate returns the expected audio input bitrate. This value
// is used after each transcript session (usually of 5 minutes) to compute
// the duration of the audio data that has been sent up to the reset
// point, so that it is later possible to shift the matched content
// found by adding that value to its `From` and `To` fields.
var inBitrate int = inSampleRate * inBitDepth * 1 /* # of channels */

const (
	// There is a transcoding limit of 305 seconds (5 mins and 5 secs) per session.
	// We have then to reset the session to keep the stream alive.
	maxSessTimeout = time.Minute * 5
	// inSampleRate is expressed in samples per second (in Hz)
	// and defines the number of times in second that the analog
	// input audio signal has been measured.
	inSampleRate = 16000
	// inBitDepth defines the digital “word length” used to
	// represent a given input sample.
	inBitDepth = 16
	// txBuffSize is the size of the transmitter buffer. Buffering the tx channel
	// will give the session loop a little bit more time to wait for the
	// results before sending data again, allowing the producer (which could be a
	// UDP connection that we do not want to block) to keep on sending data.
	txBuffSize = 100
)

func mapDuration(d *duration.Duration) time.Duration {
	var ns int64 = d.Seconds*1000*1000*1000 + int64(d.Nanos)
	return time.Duration(ns)
}

func mapSpeechResults(alts []*speechpb.SpeechRecognitionAlternative) []*sgenc.TrRec {
	if len(alts) == 0 {
		return []*sgenc.TrRec{}
	}

	// First alternative is the most probable one.
	alt := alts[0]
	acc := make([]*sgenc.TrRec, 0, len(alt.Words))
	for _, v := range alt.Words {
		if v.Word == "" {
			continue
		}
		acc = append(acc, &sgenc.TrRec{
			Start:   mapDuration(v.StartTime),
			End:     mapDuration(v.EndTime),
			TextRaw: v.Word,
		})
	}
	return acc
}

func openProgressLoop(ctx context.Context, task *speech.LongRunningRecognizeOperation, pf pwrap.WriteProgressUpdateFunc) {
	pwf := func(desc string, part int) {
		if err := pf(desc, 2, 2, part, 100); err != nil {
			log.Printf("[ERROR] unable to publish progress update: %v", err)
		}
	}
	lastP := -1
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(progressPollInterval):
			if _, err := task.Poll(ctx); err != nil {
				pwf(err.Error(), -1)
				return
			}
			meta, err := task.Metadata()
			if err != nil {
				pwf(err.Error(), -1)
				return
			}
			if p := int(meta.GetProgressPercent()); p != lastP {
				pwf(progressDescription, p)
			}
		}
	}
}

// TranscribeFile can be used to transcribe a input audio file into a list
// of transcript raw records. Input path is taken from `req`, and it does not
// matter in which encoding format the file is saved in: this function takes
// care of transcoding the audio first.
func (c *Client) TranscribeFile(ctx context.Context, req *tr.Req, pf pwrap.WriteProgressUpdateFunc) ([]*sgenc.TrRec, error) {
	// Prepare s3 object writer.
	bkt, err := c.NewBkt(ctx, req.Bkt)
	if err != nil {
		return nil, err
	}
	obj := bkt.Object(req.ID)
	defer obj.Trash()

	objw := obj.NewWriter(ctx)
	defer objw.Close()

	// Input needs to be transcoded to Linear16 first.
	t := ffmpeg.New(ffmpeg.FormatL16(), ffmpeg.Input(req.Input))
	if err := t.Start(); err != nil {
		return nil, fmt.Errorf("unable to transcode input to linear 16: %w", err)
	}
	defer t.Close()

	// Upload.
	pf("uploading", 1, 2, 0, 1)
	_, err = io.Copy(objw, t)
	if err != nil {
		return nil, fmt.Errorf("unable to upload input to google storage: %w", err)
	}

	objw.Close()
	pf("uploading", 1, 2, 1, 1)

	// Build speech context.
	sctx, err := req.ReadSpeechContext()
	if err != nil {
		return nil, fmt.Errorf("unable to build speech context: %w", err)
	}

	// Transcribe!
	gsc, err := speech.NewClient(ctx, c.Opts...)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize google speech client: %w", err)
	}
	task, err := gsc.LongRunningRecognize(ctx, &speechpb.LongRunningRecognizeRequest{
		Config: &speechpb.RecognitionConfig{
			Encoding:                   speechpb.RecognitionConfig_LINEAR16,
			SampleRateHertz:            16000,
			LanguageCode:               req.Lang,
			EnableWordTimeOffsets:      true,
			EnableAutomaticPunctuation: true,
			SpeechContexts: []*speechpb.SpeechContext{
				&speechpb.SpeechContext{
					Phrases: sctx,
				},
			},
		},
		Audio: &speechpb.RecognitionAudio{
			AudioSource: &speechpb.RecognitionAudio_Uri{Uri: obj.URI()},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("unable to start transcript task: %w", err)
	}

	// Progress updates.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go openProgressLoop(ctx, task, pf)

	resp, err := task.Wait(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to finish transcript task: %w", err)
	}

	// Map results to trascript records.
	records := []*sgenc.TrRec{}
	for _, v := range resp.Results {
		records = append(records, mapSpeechResults(v.Alternatives)...)
	}
	return records, nil
}

// stream is an higher interface to the Google Speech stream: while
// the latter does not allow to make audio streams longer that 305
// seconds, and it does not allow to open a stream and keep it open
// without sending data do it, `stream` keeps on opening the internal
// streams to google, while providing an `Rx` channels that stays
// open until something goes really wrong or the context is canceled.
// stream is also an `io.Writer` implementation.
type stream struct {
	// err contains the last error produced by
	// the stream. It is not safe to read by multiple
	// gorountines, hence should be read only after `Rx`
	// is closed.
	err error

	closeOnError   bool
	sessionTimeout time.Duration
	speechContext  []string
	lang           string
	interim        bool

	rx     chan *sgenc.StrTrRec
	tx     chan []byte
	client *speech.Client

	timeshiftOffset time.Duration
}

// Rx returns a channel that produces matched content returned
// by Google Speech. It is closed only when the stream can no
// longer be used, either because its context was canceled or
// becuase a fatal error occurred.
// Has to be called after `Open`.
func (s *stream) Rx() <-chan *sgenc.StrTrRec {
	return s.rx
}

// Err if filled with an error only with the stream encountered
// one during execution.
func (s *stream) Err() error {
	return s.err
}

// Open starts the channel's session loop, which will try to keep
// a transcription stream open with the Google Speech API. Opening
// a stream that is already open will produce unexpected (and bad)
// side effects.
// After the stream has been opened, `Rx` will be ready to produce
// data.
func (s *stream) Open(ctx context.Context) {
	s.rx = make(chan *sgenc.StrTrRec)
	s.tx = make(chan []byte, txBuffSize)
	go s.openSessionLoop(ctx)
}

// Write expects `p` to contain audio data encoded into Linear16 format
// (see `s16le` package for a useful codec). The data is sent directly
// to Google Speech. The results will be available through the `Rx`
// channel.
func (s *stream) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	s.tx <- p
	return len(p), nil
}

func (s *stream) openSessionLoop(ctx context.Context) {
	for {
		if err := s.sessLoop(ctx); err != nil {
			s.err = err
			log.Printf("[INFO] session loop returned with error: %v", err)

			var ferr *fatalError
			if errors.As(err, &ferr) || ctx.Err() == context.Canceled {
				log.Printf("[INFO] closing stream with reason: %v", err)
				close(s.rx)
				return
			}
		}
	}
}

func computeTimeshiftOffset(bytesSent int) time.Duration {
	var byteRate float64 = float64(inBitrate) / 8
	if byteRate == 0 {
		return 0
	}
	var secs float64 = float64(bytesSent) / byteRate
	if secs == 0 {
		return 0
	}
	durationString := fmt.Sprintf("%fs", secs)
	d, err := time.ParseDuration(durationString)
	if err != nil {
		return 0
	}
	return d
}

type fatalError struct {
	Err error
}

func (e *fatalError) Error() string {
	return fmt.Errorf("fatal error: %w", e.Err).Error()
}

func (s *stream) sessLoop(ctx context.Context) error {
	log.Printf("[INFO] opening transcript session for lang: %v", s.lang)

	// Open a transcript session with a context that will not expire
	// because of us. Instead, call CloseSend when the dealine expires:
	// this way we'll not loose any content sent to google.
	sess, err := s.openTrSession(context.Background())
	if err != nil {
		return &fatalError{err}
	}
	defer func() {
		s.timeshiftOffset += computeTimeshiftOffset(sess.bytesSent)
		log.Printf("[INFO] bytes transferred during session: %v", sess.bytesSent)
		log.Printf("[INFO] new timeshift offset: %v", s.timeshiftOffset)
	}()

	// There is a transcoding limit of 305 seconds per session.
	// Restart the session either if we hit the limit in terms of audio duration
	// or time elapsed.
	_ctx, cancel := context.WithTimeout(ctx, s.sessionTimeout)
	defer cancel()

	for {
		select {
		case <-_ctx.Done():
			log.Printf("[INFO] closing transcript session after: %v", _ctx.Err())
			// Time to reset the session.
			if err := sess.sstream.CloseSend(); err != nil {
				return &fatalError{fmt.Errorf("unable to close speech-to-text session: %w", err)}
			}

			log.Printf("[INFO] consuming last session content...")
			for rr := range sess.rx {
				// Keep on looping until the channel is closed, otherwise we would
				// miss items computed between last send and stream close.
				s.txRecognitionResults(rr)
			}
			// Return outer context error: if the inner one (_ctx) fails, it is
			// because of us, and it also means that we do not want to stop the
			// session loop in that case.
			return ctx.Err()
		case rr, ok := <-sess.rx:
			if !ok {
				return fmt.Errorf("session rx was closed: %w", sess.err)
			}
			s.txRecognitionResults(rr)
		case p, ok := <-s.tx:
			if !ok {
				return fmt.Errorf("session tx was closed: %w", sess.err)
			}
			log.Printf("[INFO] sending audio buffer (size %d) to google --->", len(p))
			if err := sess.sendAudio(p); err != nil {
				return err
			}
			toff := computeTimeshiftOffset(sess.bytesSent)
			if toff >= s.sessionTimeout {
				// We've sent enough audio data for this session!
				cancel()
			}
		}
	}
}

func (s *stream) txRecognitionResults(rr ...*sgenc.StrTrRec) {
	for _, v := range rr {
		v.ShiftTime(s.timeshiftOffset)
		s.rx <- v
	}
}

func sendConfig(stream speechpb.Speech_StreamingRecognizeClient, lang string, context []string, interim bool) error {
	speechContexts := []*speechpb.SpeechContext{
		&speechpb.SpeechContext{
			Phrases: context,
		},
	}
	if err := stream.Send(&speechpb.StreamingRecognizeRequest{
		StreamingRequest: &speechpb.StreamingRecognizeRequest_StreamingConfig{
			StreamingConfig: &speechpb.StreamingRecognitionConfig{
				Config: &speechpb.RecognitionConfig{
					Encoding:                   speechpb.RecognitionConfig_LINEAR16,
					SampleRateHertz:            int32(inSampleRate),
					LanguageCode:               lang,
					EnableWordTimeOffsets:      true,
					EnableAutomaticPunctuation: true,
					SpeechContexts:             speechContexts,
				},
				InterimResults: interim,
			},
		},
	}); err != nil {
		return fmt.Errorf("unable to send initial stream configuration message: %w", err)
	}
	return nil
}

func (s *stream) openTrSession(ctx context.Context) (*trSess, error) {
	stream, err := s.client.StreamingRecognize(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize text-to-speech stream: %w", err)
	}
	if err := sendConfig(stream, s.lang, s.speechContext, s.interim); err != nil {
		return nil, err
	}

	sess := &trSess{
		sstream: stream,
		rx:      make(chan *sgenc.StrTrRec),
	}
	go sess.listenTr()

	return sess, nil
}

type trSess struct {
	sstream   speechpb.Speech_StreamingRecognizeClient
	bytesSent int
	err       error
	rx        chan *sgenc.StrTrRec
}

func (s *trSess) sendAudio(p []byte) error {
	if err := s.sstream.Send(&speechpb.StreamingRecognizeRequest{
		StreamingRequest: &speechpb.StreamingRecognizeRequest_AudioContent{
			AudioContent: p,
		},
	}); err != nil {
		return fmt.Errorf("unable to send audio buffer: %v", err)
	}
	s.bytesSent += len(p)
	return nil
}

func mapStreamSpeechResults(results []*speechpb.StreamingRecognitionResult) []*sgenc.StrTrRec {
	acc := []*sgenc.StrTrRec{}
	for _, r := range results {
		acc = append(acc, mapStreamSpeechResult(r)...)
	}
	return acc
}

func mapStreamSpeechResult(r *speechpb.StreamingRecognitionResult) []*sgenc.StrTrRec {
	alt := r.Alternatives[0]
	recs := make([]*sgenc.StrTrRec, len(alt.Words))
	for i, w := range alt.Words {
		recs[i] = &sgenc.StrTrRec{
			TrRec: &sgenc.TrRec{
				Start:   mapDuration(w.StartTime),
				End:     mapDuration(w.EndTime),
				TextRaw: w.Word,
			},
			IsFinal:    r.IsFinal,
			Confidence: float64(alt.Confidence),
		}
	}
	return recs
}

func (s *trSess) listenTr() {
	for {
		resp, err := s.sstream.Recv()
		if err != nil {
			log.Printf("[INFO] closing session transmitter: %v", err)
			s.err = err
			close(s.rx)
			return
		}
		if resp.Error != nil {
			// Just acknoledge that a non-fatal error occurred.
			log.Printf("[INFO] session transmitter returned a status error: %v", resp.Error.Message)
		}

		results := mapStreamSpeechResults(resp.Results)
		if len(results) == 0 {
			log.Printf("[INFO] no transcript items received in response from google")
			continue
		}
		log.Printf("[INFO] <--- received transcript items (%d) from google!", len(results))
		for _, v := range results {
			s.rx <- v
		}
	}
}

func (c *Client) TranscribeStream(ctx context.Context, req *tr.Req, sessionTimeout time.Duration) (tr.TrStreamer, error) {
	// Input needs to be transcoded to Linear16 first.
	t := ffmpeg.New(ffmpeg.FormatL16(), ffmpeg.Input(req.Input))
	if err := t.Start(); err != nil {
		return nil, fmt.Errorf("unable to transcode input to linear 16: %w", err)
	}

	// Create a stream to Google Speech API and open it.
	gsc, err := speech.NewClient(ctx, c.Opts...)
	if err != nil {
		t.Close()
		return nil, fmt.Errorf("unable to open google speech client: %w", err)
	}
	stream := &stream{
		client:         gsc,
		lang:           req.Lang,
		sessionTimeout: sessionTimeout,
		interim:        req.Interim,
	}

	ctx, cancel := context.WithCancel(ctx)
	stream.Open(ctx)

	// Now copy data from input to stream till something gets closed.
	w := bufio.NewWriterSize(stream, 1024*32)

	// Copy data from source indefinetely.
	go func() {
		defer t.Close()
		defer cancel()

		// Copy is expected to exit with an error only when something
		// very unexpected happens.
		n, err := io.Copy(w, t)
		if err != nil {
			log.Printf("[ERROR] copy exited with error: %v", err)
		}
		log.Printf("[INFO] copy: %d bytes transferred", n)
		cancel()
	}()

	return stream, nil
}
