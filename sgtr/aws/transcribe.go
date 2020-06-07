// SPDX-FileCopyrightText: 2020 KIM KeepInMind GmbH
//
// SPDX-License-Identifier: MIT

package aws

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"git.keepinmind.info/subgensdk/sgenc"
	"git.keepinmind.info/subgensdk/sgtr/ffmpeg"
	"git.keepinmind.info/subgensdk/sgtr/tr"
	"github.com/aws/aws-sdk-go/aws"
	ts "github.com/aws/aws-sdk-go/service/transcribeservice"
	"github.com/kim-company/pmux/pwrap"
)

const progressPollInterval = time.Millisecond * 2500
const maxSegDuration = time.Hour * 1 // https://docs.aws.amazon.com/transcribe/latest/dg/limits-guidelines.html

type trAlt struct {
	Confidence float64
	Content    string
}

func (t *trAlt) UnmarshalJSON(b []byte) error {
	var raw struct {
		Conf    string `json:"confidence"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}

	c, err := strconv.ParseFloat(raw.Conf, 64)
	if err != nil {
		// This alternative is invalid, but it does not matter: we'll
		// just ignore its contents, as its confidence is 0.
		return nil
	}

	t.Confidence = c
	t.Content = raw.Content

	return nil
}

type trItem struct {
	Start        time.Duration
	End          time.Duration
	Type         string
	Alternatives []trAlt
}

func (t *trItem) UnmarshalJSON(b []byte) error {
	var raw struct {
		Start string   `json:"start_time"`
		End   string   `json:"end_time"`
		Type  string   `json:"type"`
		Alts  []*trAlt `json:"alternatives"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}

	// At least initialize the list and type, even though the
	// resulting item is invalid.
	t.Type = raw.Type
	t.Alternatives = make([]trAlt, len(raw.Alts))
	for i, v := range raw.Alts {
		t.Alternatives[i] = *v
	}

	str, err := time.ParseDuration(raw.Start + "s")
	if err != nil {
		return nil
	}
	end, err := time.ParseDuration(raw.End + "s")
	if err != nil {
		return nil
	}
	t.Start = str
	t.End = end

	return nil
}

type transcript struct {
	// Duration of the transcribe. Has to be set manually, used
	// for merge tasks.
	Duration time.Duration
	// Fields populated by the decoder.
	Name      string
	AccountID string
	Status    string
	Items     []trItem
}

func (t *transcript) UnmarshalJSON(b []byte) error {
	var raw struct {
		ID      string `json:"accountId"`
		Name    string `json:"jobName"`
		Status  string `json:"status"`
		Results struct {
			Items []*trItem `json:"items"`
		} `json:"results"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	t.Name = raw.Name
	t.AccountID = raw.ID
	t.Status = raw.Status

	t.Items = make([]trItem, len(raw.Results.Items))
	for i, v := range raw.Results.Items {
		t.Items[i] = *v
	}
	return nil
}

type decoder struct {
	r io.Reader
}

func newDecoder(r io.Reader) *decoder {
	return &decoder{r: r}
}

func (d *decoder) decode(i *transcript) error {
	if err := json.NewDecoder(d.r).Decode(&i); err != nil {
		return fmt.Errorf("unable to decode transcribe: %v", err)
	}

	return nil
}

func traceTr(ctx context.Context, c *ts.TranscribeService, jobid string) (string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, progressPollInterval)
	defer cancel()

	resp, err := c.GetTranscriptionJobWithContext(ctx, &ts.GetTranscriptionJobInput{
		TranscriptionJobName: aws.String(jobid),
	})
	if err != nil {
		return "", false, err
	}
	job := resp.TranscriptionJob

	// QUEUED | IN_PROGRESS | FAILED | COMPLETED
	switch *job.TranscriptionJobStatus {
	case "QUEUED", "IN_PROGRESS":
		return "", false, nil
	case "FAILED":
		return "", false, fmt.Errorf("transcription failure: %v", *job.FailureReason)
	case "COMPLETED":
		return *job.Transcript.TranscriptFileUri, true, nil
	default:
		return "", false, fmt.Errorf("unrecognised trascription job status %s", *job.TranscriptionJobStatus)
	}
}

func openTraceTrLoop(ctx context.Context, c *ts.TranscribeService, jobid string) (string, error) {
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(progressPollInterval):
			uri, done, err := traceTr(ctx, c, jobid)
			if err != nil {
				return "", err
			}
			if done && uri != "" {
				return uri, nil
			}
		}
	}
}

type segReq struct {
	*tr.Req
	vname string
	seg   *ffmpeg.Seg
}

func (c *Client) transcribeSeg(ctx context.Context, tsc *ts.TranscribeService, bkt *Bkt, req *segReq) (*trSeg, error) {
	// Open the file.
	file, err := os.Open(req.seg.Name)
	if err != nil {
		return nil, fmt.Errorf("unable to open segment file %s: %w", req.seg.Name, err)
	}
	defer file.Close()

	// Upload the file to S3.
	filen := filepath.Base(req.seg.Name)
	key := filepath.Join(req.ID, filen)

	// TODO: Add progress here.
	uri, err := bkt.UploadObj(ctx, file, key)
	if err != nil {
		return nil, fmt.Errorf("unable to start transcription: %w", err)
	}
	defer bkt.Trash(key)

	// Start transcription job.
	jobid := req.ID + "-" + strconv.Itoa(req.seg.Index)
	settings := new(ts.Settings)
	if name := req.vname; len(name) > 0 {
		// Validation will fail if we set an empty vocabulary.
		settings.SetVocabularyName(name)
	}

	config := &ts.StartTranscriptionJobInput{
		LanguageCode:         aws.String(req.Lang),
		TranscriptionJobName: aws.String(jobid),
		MediaSampleRateHertz: aws.Int64(16000),
		MediaFormat:          aws.String("wav"),
		Media: &ts.Media{
			MediaFileUri: aws.String(uri),
		},
		Settings: settings,
	}
	if _, err := tsc.StartTranscriptionJobWithContext(ctx, config); err != nil {
		return nil, fmt.Errorf("unable to start transcription: %w", err)
	}

	// Trace progress.
	uri, err = openTraceTrLoop(ctx, tsc, jobid)
	if err != nil {
		return nil, fmt.Errorf("unable to complete transcription: %w", err)
	}

	// Download transcript.
	resp, err := http.Get(uri)
	if err != nil {
		return nil, fmt.Errorf("unable to download file: %w", err)
	}
	defer resp.Body.Close()

	var tr transcript
	if err := newDecoder(resp.Body).decode(&tr); err != nil {
		return nil, fmt.Errorf("unable to decode transcript file: %w", err)
	}

	// Remove transcribe job only when everything ended up well.
	tsc.DeleteTranscriptionJobWithContext(ctx, &ts.DeleteTranscriptionJobInput{
		TranscriptionJobName: aws.String(jobid),
	})
	return &trSeg{
		tr:  &tr,
		seg: req.seg,
	}, nil
}

type trSeg struct {
	tr  *transcript
	seg *ffmpeg.Seg
}

func (t *trSeg) makeTrRecs() []*sgenc.TrRec {
	items := t.tr.Items
	seg := t.seg
	acc := make([]*sgenc.TrRec, 0, len(items))

	for _, v := range items {
		if v.Type == "pronunciation" {
			acc = append(acc, &sgenc.TrRec{
				Start:   v.Start + seg.Start,
				End:     v.End + seg.Start,
				TextRaw: v.Alternatives[0].Content,
			})
		}
	}
	return acc
}

func mkvl(w ...string) string {
	return strings.Join(w, "\t") + "\n"
}

// If successfull, returns the vocabulary name to be used fot the transcribe job, the bucket key
// that holds the vocabulary input table file. This function waits until the vocabulary its ready
// to be used, hence it is pretty slow (roughly 4 minutes!).
// In case of error, the s3 file and the vocabulary are removed.
func makeVocabulary(ctx context.Context, tsc *ts.TranscribeService, bkt *Bkt, req *tr.Req) (string, string, error) {
	// Build Phrases first.
	sctx, err := req.ReadSpeechContext()
	if err != nil {
		return "", "", fmt.Errorf("unable to read speech context: %w", err)
	}

	// https://docs.aws.amazon.com/transcribe/latest/dg/how-vocabulary.html#create-vocabulary-list
	// Upload vocabulary to s3. Needs to be removed later on.
	buf := new(bytes.Buffer)
	buf.WriteString(mkvl("Phrase", "DisplayAs", "IPA", "SoundsLike"))
	for _, v := range sctx {
		buf.WriteString(mkvl(strings.ReplaceAll(v, " ", "-"), v))
	}
	key := filepath.Join(req.ID) + "-vocabulary"
	loc, err := bkt.UploadObj(ctx, buf, key)
	if err != nil {
		return "", "", fmt.Errorf("unable to upload vocabulary: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			bkt.Trash(key)
		}
	}()

	resp, err := tsc.CreateVocabulary(&ts.CreateVocabularyInput{
		VocabularyName:    aws.String(req.ID),
		LanguageCode:      aws.String(req.Lang),
		VocabularyFileUri: aws.String(loc),
	})
	if err != nil {
		if reason := resp.FailureReason; reason != nil {
			return "", key, fmt.Errorf("%w, %s", err, *reason)
		}
		return "", key, err
	}

	name := *resp.VocabularyName
	defer func() {
		if cleanup {
			removeVocabulary(tsc, name)
		}
	}()

	// Wait till vocabulary is in READY state, otherwise we'll not be
	// able to use it.
	for {
		select {
		case <-ctx.Done():
			return "", key, fmt.Errorf("vocabulary creation is taking too long: %w", ctx.Err())
		case <-time.After(time.Second * 2):
			voc, err := tsc.GetVocabulary(&ts.GetVocabularyInput{
				VocabularyName: &name,
			})
			if err != nil {
				return "", key, fmt.Errorf("unable to retrieve vocabulary state: %w", err)
			}
			switch *voc.VocabularyState {
			case ts.VocabularyStateReady:
				cleanup = false
				return name, key, nil
			case ts.VocabularyStateFailed:
				return "", key, fmt.Errorf("%v", *voc.FailureReason)
			default:
				// Try again.
			}
		}
	}
}

func removeVocabulary(tsc *ts.TranscribeService, name string) error {
	_, err := tsc.DeleteVocabulary(&ts.DeleteVocabularyInput{
		VocabularyName: aws.String(name),
	})
	if err != nil {
		return fmt.Errorf("unable to delete vocabulary: %w", err)
	}

	return nil
}

func (c *Client) TranscribeFile(ctx context.Context, req *tr.Req, pf pwrap.WriteProgressUpdateFunc) ([]*sgenc.TrRec, error) {
	// Prepare work space.
	wdp := filepath.Join(os.TempDir(), filepath.Base(os.Args[0]), req.ID)
	os.MkdirAll(wdp, os.ModePerm)
	defer os.RemoveAll(wdp)

	// Input needs to be transcoded to Linear16 first.
	t := ffmpeg.New(
		ffmpeg.Input(req.Input),
		ffmpeg.FormatWav(),
		ffmpeg.Segment(maxSegDuration),
		ffmpeg.Wd(wdp),
	)
	// Transcode the input.
	pf("transcoding", 1, 2, 0, 1)
	if err := t.Run(); err != nil {
		return nil, fmt.Errorf("unable to transcode input to wav: %w", err)
	}
	pf("transcoding", 1, 2, 1, 1)
	// Each segment will be an input to be transcoded alone.
	segs, err := t.GetSegmentList()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Open transcribing session.
	tsc := ts.New(c.sess)
	bkt := c.NewBkt(req.Bkt)

	// Build vocabulary if needed.
	var vname string
	var key string
	if req.HasSpeechContext() {
		ctx, cancel := context.WithTimeout(ctx, time.Minute*5)
		defer cancel()
		if vname, key, err = makeVocabulary(ctx, tsc, bkt, req); err != nil {
			return nil, fmt.Errorf("unable to build vocabulary: %w", err)
		}
		// TODO: watch out, error is silently discarded.
		defer removeVocabulary(tsc, vname)
		defer bkt.Trash(key)
	}

	// Transcode in parallel, then join the results back.

	ch := make(chan *trSeg)
	errc := make(chan error)
	for _, v := range segs {
		go func(seg *ffmpeg.Seg) {
			trseg, err := c.transcribeSeg(ctx, tsc, bkt, &segReq{
				Req:   req,
				seg:   seg,
				vname: vname,
			})
			if err != nil {
				errc <- err
			}
			ch <- trseg
		}(v)
	}

	// TODO: Add deadlines.
	trsegs := make([]*trSeg, 0, len(segs))
	pf("transcribing", 2, 2, 0, len(segs))
	for i := 0; i < len(segs); i++ {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("stopped waiting for transcript records: %w", ctx.Err())
		case err := <-errc:
			cancel()
			return nil, fmt.Errorf("could complete transcription: %w", err)
		case trseg := <-ch:
			pf("transcribing", 2, 2, i+1, len(segs))
			trsegs = append(trsegs, trseg)
		}
	}

	// Ensure the order is preserved.
	sort.SliceStable(trsegs, func(i, j int) bool {
		return trsegs[i].seg.Index < trsegs[j].seg.Index
	})

	recs := []*sgenc.TrRec{}
	for _, v := range trsegs {
		recs = append(recs, v.makeTrRecs()...)
	}
	return recs, nil
}

func (c *Client) TranscribeStream(context.Context, *tr.Req, time.Duration) (tr.TrStreamer, error) {
	return nil, fmt.Errorf("stream is not supported yet with aws engine")
}
