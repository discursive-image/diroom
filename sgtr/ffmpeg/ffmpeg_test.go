// SPDX-FileCopyrightText: 2020 KIM KeepInMind GmbH
//
// SPDX-License-Identifier: MIT

package ffmpeg

import (
	"strings"
	"testing"
	"time"
)

func TestParseSegmentList(t *testing.T) {
	in := `output000.wav,0.000000,30.022375
output001.wav,30.022375,60.022562
output002.wav,60.022562,90.022750
output003.wav,90.022750,120.022937
output004.wav,120.022937,150.023125
output005.wav,150.023125,180.000063
output006.wav,180.000063,210.000250
output007.wav,210.000250,240.000438
output008.wav,240.000438,270.000625
output009.wav,270.000625,300.000812
output010.wav,300.000812,328.864250`

	buf := strings.NewReader(in)
	segs, err := parseSegmentList(buf, "")
	if err != nil {
		t.Fatal(err)
	}

	// Let's pick a random entry and check that one.
	// e.g. output006.wav,180.000063,210.000250
	seg := segs[6]
	if seg.Name != "output006.wav" {
		t.Fatalf("Unexpected seg name: %+v", seg)
	}
	if seg.Start != mustParseDuration(t, "180.000063s") {
		t.Fatalf("Unexpected seg start: %+v", seg)
	}
	if seg.End != mustParseDuration(t, "210.000250s") {
		t.Fatalf("Unexpected seg end: %+v", seg)
	}
}

func mustParseDuration(t *testing.T, draw string) time.Duration {
	d, err := time.ParseDuration(draw)
	if err != nil {
		t.Fatalf("Unable to parse duration: %v", err)
	}
	return d
}
