// SPDX-FileCopyrightText: 2020 Jecoz
//
// SPDX-License-Identifier: MIT

package main

import (
	"flag"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/google/uuid"
	"gopkg.in/pipe.v2"
	"gopkg.in/yaml.v2"
)

var logger = log.New(os.Stderr, "", log.LstdFlags)
var arg0 = filepath.Base(os.Args[0])

func logf(format string, args ...interface{}) {
	logger.Printf(arg0+" * "+format, args...)
}

func errorf(format string, args ...interface{}) {
	logf("error: "+format, args...)
}

func fatalf(format string, args ...interface{}) {
	errorf(format, args...)
	os.Exit(1)
}

type config struct {
	AppCreds string `yaml:"google_application_credentials"`
	Key      string `yaml:"google_speech_key"`
	CX       string `yaml:"google_speech_cx"`
}

func main() {
	cp := flag.String("config", "config.yaml", "Path to configuration file. One is required.")
	lang := flag.String("lang", "it-IT", "Expected input spoken language code, formatted as a BCP-47 identifier (RFC5646).")
	id := flag.String("id", uuid.New().String(), "Room identifier. It is also used as reference when storing data.")
	p := flag.Int("p", 7745, "Discorsive Image server listening port.")
	cache := flag.Bool("cache", false, "Enable caching results with REDIS.")
	flag.Parse()

	// Read configuration.
	cf, err := os.Open(*cp)
	if err != nil {
		fatalf("unable to open configuration file: %v", err)
	}
	defer cf.Close()

	var config config
	if err = yaml.NewDecoder(cf).Decode(&config); err != nil {
		fatalf("unable to decode configuration file: %v", err)
	}

	logf("parsed configuration: %+v", config)

	// Prepare base directory for storing data.
	root := *id
	if err := os.MkdirAll(root, os.ModePerm); err != nil {
		fatalf("unable to prepare storage directory: %v", err)
	}

	// Start cache if needed.
	dicArgs := []string{"-c", "2", "-cx", config.CX, "-k", config.Key}
	if *cache {
		rp := strconv.Itoa(*p + 1)
		ra := "localhost:" + rp

		logf("redis server listening on %d", rp)
		redis := exec.Command("redis-server", "--port", rp)
		if err := redis.Start(); err != nil {
			fatalf("unable to start redis server: %v", err)
		}
		dicArgs = append(dicArgs, "ra", ra)
		defer func() {
			if err := redis.Wait(); err != nil {
				errorf("redis exited with error: %v", err)
			}
		}()
	}


	// Prepare environment.
	os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", config.AppCreds)
	os.Setenv("PATH", filepath.Dir(os.Args[0]))

	logf("server is listening on %d", *p)
	l := pipe.Line(
		pipe.Read(os.Stdin),
		pipe.Exec("sgtr", "-s", "-lang", *lang, "-id", *id, "-lp", filepath.Join(root, "sgtr.log")),
		pipe.TeeWriteFile(filepath.Join(root, "transcript.trr"), os.ModePerm),
		pipe.Exec("dic", dicArgs...),
		pipe.TeeWriteFile(filepath.Join(root, "transcript+images.csv"), os.ModePerm),
		pipe.Exec("dis", "-p", strconv.Itoa(*p) /*, "-lp", filepath.Join(root, "dis.log")*/),
	)
	if err := pipe.Run(l); err != nil {
		fatalf("unable to run pipe: %v", err)
	}
}
