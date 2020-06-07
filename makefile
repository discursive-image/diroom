# SPDX-FileCopyrightText: 2020 jecoz
#
# SPDX-License-Identifier: MIT

GO111MODULE := on

.PHONY: trnscr dic dis
all: trnscr dic dis
trnscr: bin/trnscr
bin/trnscr:
	go build -o $@ trnscr/cmd/trnscr/main.go
bin/dic:
	go build -o $@ dic/cmd/main.go
dis: bin/dis
bin/dis:
	go build -o $@ dis/cmd/dis/main.go

format:
	go fmt ./...
clean:
	rm -rf bin
