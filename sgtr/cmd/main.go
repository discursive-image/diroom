// SPDX-FileCopyrightText: 2019 KIM KeepInMind GmbH
// SPDX-FileCopyrightText: 2020 KIM KeepInMind GmbH
//
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"strings"
	"time"

	"git.keepinmind.info/subgensdk/sgenc"
	"git.keepinmind.info/subgensdk/sgenc/strraw"
	"git.keepinmind.info/subgensdk/sgenc/trraw"
	"git.keepinmind.info/subgensdk/sgtr/aws"
	"git.keepinmind.info/subgensdk/sgtr/google"
	"git.keepinmind.info/subgensdk/sgtr/tr"
	"github.com/google/uuid"
	"github.com/kim-company/pmux/pwrap"
)

func errorf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, os.Args[0]+" error: "+format+"\n", args...)
}

var silentProgressWriter = func(d string, stage, stages, part, tot int) error {
	return nil
}

var stderrProgressWriter = func(d string, stage, stages, part, tot int) error {
	fmt.Fprintf(os.Stderr, "%s update: state %d/%d, progress %d/%d\n", os.Args[0], stage, stages, part, tot)
	return nil
}

func makeProgressWriter(ctx context.Context, path string) (pwrap.WriteProgressUpdateFunc, context.CancelFunc, error) {
	ctx, cancel := context.WithCancel(ctx)
	switch path {
	case "-":
		return stderrProgressWriter, cancel, nil
	case "", "/dev/null", "null", "discard":
		return silentProgressWriter, cancel, nil
	default:
	}

	br, err := pwrap.NewUnixCommBridge(ctx, path, makeOnCommandOption(cancel))
	if err != nil {
		return nil, nil, fmt.Errorf("unable to make progress writer: %w", err)
	}
	go br.Open(ctx)
	return br.WriteProgressUpdate, func() {
		cancel()
		br.Close()
	}, nil
}

func makeOnCommandOption(cancel context.CancelFunc) func(*pwrap.UnixCommBridge) {
	return pwrap.OnCommand(func(u *pwrap.UnixCommBridge, cmd string) error {
		if strings.Contains(cmd, "cancel") {
			cancel()
			return u.Close()
		}
		return nil
	})
}

func setLogOutput(path string) error {
	switch path {
	case "", "/dev/null", "null", "discard":
		log.SetOutput(ioutil.Discard)
		return nil
	case "-":
		log.SetOutput(os.Stderr)
		return nil
	default:
		f, err := os.Create(path)
		if err != nil {
			return fmt.Errorf("unable to open log file: %w", err)
		}
		log.SetOutput(f)
		return nil
	}
}

type Transcriber interface {
	TranscribeFile(context.Context, *tr.Req, pwrap.WriteProgressUpdateFunc) ([]*sgenc.TrRec, error)
	TranscribeStream(context.Context, *tr.Req, time.Duration) (tr.TrStreamer, error)
}

func newTranscriber(ctx context.Context, engine, region string) (Transcriber, error) {
	switch engine {
	case "google":
		return google.NewClient(ctx), nil
	case "aws":
		return aws.NewClient(region)
	default:
		return nil, fmt.Errorf("unsupported transcription engine %s", engine)
	}
}

func main() {
	in := flag.String("in", "-", "Input file path. Use - for stdin.")
	lang := flag.String("lang", "en-US", "Expected input spoken language code, formatted as a BCP-47 identifier (RFC5646).")
	bkt := flag.String("bkt", "tv1-speech", "Google storage bucket, used to save temporarly the file that has to be transcribed.")
	id := flag.String("id", uuid.New().String(), "Identifier for this transcription task. It is also used as reference when storing data.")
	sp := flag.String("sp", "", "Path to the unix socket file. Use - to print progress to stdout.")
	lp := flag.String("lp", "", "Log file path. Use - for stderr.")
	e := flag.String("e", "google", "Transcription engine to use. Choose either aws or google and bkt accordingly.")
	r := flag.String("r", "eu-west-1", "AWS region. Used only if engine is aws, ignored otherwise.")
	c := flag.String("c", "", "Context file path. Provide a list of phrases/words that the audio is supposed to contain for improved recognition.")
	s := flag.Bool("s", false, "Enable streaming mode. The input is expected to be an audio stream, useful with microphones and audio live streaming in general.")
	i := flag.Int("i", 15, "Session interval duration, expressed in seconds.")
	interim := flag.Bool("interim", false, "Produce also intermediate results. Applied only in streaming mode.")
	flag.Parse()

	// Configure log.
	if err := setLogOutput(*lp); err != nil {
		errorf(err.Error())
		os.Exit(-1)
	}

	// Setup transcribe engine.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var err error
	var eng Transcriber
	if eng, err = newTranscriber(ctx, *e, *r); err != nil {
		errorf("unable to initiate transcript engine: %v", err)
		os.Exit(-1)
	}

	// Build transcribe request.
	req := tr.NewReq(
		tr.Input(*in),
		tr.Language(*lang),
		tr.Bucket(*bkt),
		tr.ID(*id),
		tr.SpeechContext(*c),
		tr.Interim(*interim),
	)

	// Handle signals.
	sigch := make(chan os.Signal, 1)
	signal.Notify(sigch, os.Interrupt)
	go func() {
		log.Printf("signal %v received. Canceling", <-sigch)
		cancel()
	}()

	if *s {
		interval := time.Second * time.Duration(*i)
		transcribeStream(ctx, eng, req, os.Stdout, interval)
	} else {
		// Prepare progress writer.
		f, cancel, err := makeProgressWriter(ctx, *sp)
		if err != nil {
			errorf(err.Error())
			os.Exit(-1)
		}
		defer cancel()

		transcribeFile(ctx, eng, req, os.Stdout, f)
	}
}

func transcribeStream(ctx context.Context, eng Transcriber, req *tr.Req, out io.Writer, interval time.Duration) {
	w := strraw.NewWriter(out)
	stream, err := eng.TranscribeStream(ctx, req, interval)
	if err != nil {
		errorf(err.Error())
		os.Exit(-1)
	}
	for rec := range stream.Rx() {
		if err := w.Write(rec); err != nil {
			errorf("unable to write record: %v", err)
			os.Exit(-1)
		}
		w.Flush()
	}

	if err := stream.Err(); err != nil {
		errorf("stream exited with error: %v", err)
		os.Exit(-1)
	}
}

func transcribeFile(ctx context.Context, eng Transcriber, req *tr.Req, out io.Writer, f pwrap.WriteProgressUpdateFunc) {
	// This might take a while.
	var records []*sgenc.TrRec
	var err error
	if records, err = eng.TranscribeFile(ctx, req, f); err != nil {
		errorf(err.Error())
		os.Exit(-1)
	}

	// Encode results.
	w := trraw.NewWriter(out)
	if err := w.WriteAll(records); err != nil {
		errorf(err.Error())
		os.Exit(-1)
	}
	w.Flush()
}
