// SPDX-FileCopyrightText: 2019 KIM KeepInMind GmbH
// SPDX-FileCopyrightText: 2020 KIM KeepInMind GmbH
//
// SPDX-License-Identifier: MIT

package ffmpeg

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

// ffmpeg -i /Users/danielmorandini/Downloads/testinput.mp4 -f segment -acodec pcm_s16le -vn -ac 1 -ar 16k -segment_time 30 -segment_list test-output/output.csv test-output/output%03d.wav

// Transcoder is a pcm_s16le audio transcoder.
type Transcoder struct {
	format string
	ext    string
	wd     string // work directory path.
	segd   int
	segn   string
	in     string
	out    string
	args   []string

	stdoutPipe io.ReadCloser
}

func FormatL16() func(*Transcoder) {
	return func(t *Transcoder) {
		t.format = "s16le"
		t.ext = ".raw"
	}
}

func FormatWav() func(*Transcoder) {
	return func(t *Transcoder) {
		t.format = "wav"
		t.ext = ".wav"
	}
}

// If enabled, the output produced will be written into files.
// `segn` is the base segment name.
func Segment(d time.Duration) func(*Transcoder) {
	return func(t *Transcoder) {
		t.format = "segment" // No worries, ext will work.
		t.segd = int(d.Seconds())
	}
}

func Input(path string) func(*Transcoder) {
	return func(t *Transcoder) {
		t.in = path
	}
}

func Wd(path string) func(*Transcoder) {
	return func(t *Transcoder) {
		t.wd = path
	}
}

func New(opts ...func(*Transcoder)) *Transcoder {
	t := &Transcoder{
		format: "s16le",
		ext:    ".raw",
		wd:     ".",
		in:     "-",
		out:    "-",
	}
	for _, f := range opts {
		f(t)
	}

	args := []string{
		"-i", t.in,
		"-f", t.format,
		"-acodec", "pcm_s16le",
		"-vn",
		"-ac", "1",
		"-ar", "16k",
	}
	if t.segd > 0 {
		args = append(args, []string{
			"-segment_time", strconv.Itoa(t.segd),
			"-segment_list", t.segmentListName(),
		}...)
	}
	args = append(args, t.outputName())
	t.args = args

	return t
}

func (t *Transcoder) Start() error {
	if t.args == nil || len(t.args) == 0 {
		return fmt.Errorf("transcoder has not been properly initialized")
	}

	cmd := exec.Command("ffmpeg", t.args...)
	cmd.Stdin = os.Stdin
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("unable to open ffmpeg's stdout pipe: %w", err)
	}
	t.stdoutPipe = pipe

	return cmd.Start()
}

func (t *Transcoder) Run() error {
	log.Printf("[INFO] running ffmpeg with args: %v", t.args)
	if t.args == nil || len(t.args) == 0 {
		return fmt.Errorf("transcoder has not been properly initialized")
	}

	cmd := exec.Command("ffmpeg", t.args...)
	return cmd.Run()
}

func (t *Transcoder) Read(p []byte) (int, error) {
	if t.stdoutPipe == nil || t.out != "-" {
		return 0, fmt.Errorf("transcoder did not open its stdout pipe")
	}
	return t.stdoutPipe.Read(p)
}

func (t *Transcoder) Close() error {
	if t.stdoutPipe == nil {
		return nil
	}
	return t.stdoutPipe.Close()
}

func (t *Transcoder) segmentListName() string {
	n := "segment"
	if t.wd != "" {
		n = filepath.Join(t.wd, n)
	}

	return n + ".csv"
}

func (t *Transcoder) outputName() string {
	if t.segd == 0 {
		return "-"
	}

	n := "segment"
	if t.wd != "" {
		n = filepath.Join(t.wd, n)
	}

	return n + "%02d" + t.ext
}

type Seg struct {
	Index int
	Name  string
	Start time.Duration
	End   time.Duration
}

func (s *Seg) Duration() time.Duration {
	return s.End - s.Start
}

func (t *Transcoder) GetSegmentList() ([]*Seg, error) {
	segf, err := os.Open(t.segmentListName())
	if err != nil {
		return nil, fmt.Errorf("unable to open segment list file: %w", err)
	}
	defer segf.Close()

	dir := filepath.Dir(t.segmentListName())
	return parseSegmentList(segf, dir)
}

func parseSegmentList(src io.Reader, segDir string) ([]*Seg, error) {
	r := csv.NewReader(src)
	r.FieldsPerRecord = 3
	recs, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("unable to read segment records: %w", err)
	}

	acc := make([]*Seg, len(recs))
	for i, v := range recs {
		start, err := time.ParseDuration(v[1] + "s")
		if err != nil {
			return nil, fmt.Errorf("unable to parse start duration of record %d: %w", i, err)
		}
		end, err := time.ParseDuration(v[2] + "s")
		if err != nil {
			return nil, fmt.Errorf("unable to parse end duration of record %d: %w", i, err)
		}

		acc[i] = &Seg{
			Index: i,
			Name:  filepath.Join(segDir, v[0]),
			Start: start,
			End:   end,
		}
	}

	return acc, nil
}
