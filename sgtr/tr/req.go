// SPDX-FileCopyrightText: 2020 KIM KeepInMind GmbH
//
// SPDX-License-Identifier: MIT

package tr

import (
	"bufio"
	"fmt"
	"os"

	"git.keepinmind.info/subgensdk/sgenc"
)

type TrStreamer interface {
	Rx() <-chan *sgenc.StrTrRec
	Err() error
}

type Req struct {
	Input         string
	Lang          string
	Bkt           string
	ID            string
	SpeechContext string
	Interim       bool
}

func Interim(ok bool) func(*Req) {
	return func(t *Req) {
		t.Interim = ok
	}
}

func Language(code string) func(*Req) {
	return func(t *Req) {
		t.Lang = code
	}
}

func Bucket(bkt string) func(*Req) {
	return func(t *Req) {
		t.Bkt = bkt
	}
}

func ID(id string) func(*Req) {
	return func(t *Req) {
		t.ID = id
	}
}

func Input(path string) func(*Req) {
	return func(t *Req) {
		t.Input = path
	}
}

func SpeechContext(s string) func(*Req) {
	return func(t *Req) {
		t.SpeechContext = s
	}
}

func NewReq(opts ...func(t *Req)) *Req {
	t := &Req{
		// Set defaults here
		Lang:  "en-US",
		Input: "-",
	}
	// User defined conf.
	for _, f := range opts {
		f(t)
	}
	return t
}

func (r *Req) HasSpeechContext() bool {
	return r.SpeechContext != ""
}

func (r *Req) ReadSpeechContext() ([]string, error) {
	if !r.HasSpeechContext() {
		return []string{}, nil
	}

	file, err := os.Open(r.SpeechContext)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	s := bufio.NewScanner(file)
	acc := []string{}
	for s.Scan() {
		acc = append(acc, s.Text())

	}
	if err := s.Err(); err != nil {
		return nil, fmt.Errorf("unable to scan speech context input file: %w", err)
	}
	return acc, nil
}
