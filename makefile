# SPDX-FileCopyrightText: 2020 jecoz
#
# SPDX-License-Identifier: MIT

GO111MODULE := on

.PHONY: trnscr dic dis
all: trnscr dic dis
trnscr: bin/trnscr
bin/trnscr:
	(cd trnscr && go build -o ../$@ cmd/trnscr/main.go)
dic: bin/dic
bin/dic:
	(cd dic && go build -o ../$@ cmd/main.go)
dis: bin/dis
bin/dis:
	(cd dis && go build -o ../$@ cmd/dis/main.go)

format:
	go fmt ./...
clean:
	rm -rf bin
