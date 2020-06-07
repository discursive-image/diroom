# SPDX-FileCopyrightText: 2019 KIM KeepInMind GmbH
#
# SPDX-License-Identifier: MIT

sgtr: cmd/*.go google/*.go ffmpeg/*.go
	go build -v -o bin/sgtr cmd/main.go
clean:
	rm -rf bin/
test:
	go test ./...
format:
	go fmt ./...
