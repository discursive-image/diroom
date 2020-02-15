# SPDX-FileCopyrightText: 2020 jecoz
#
# SPDX-License-Identifier: MIT

DIRS=bin
$(shell mkdir -p $(DIRS))

all: bin/sgtr bin/dic bin/dis
bin/sgtr:
	(cd sgtr && make && mv bin/sgtr ../bin/sgtr)
bin/dic:
	(cd dic && make && mv bin/dic ../bin/dic)
bin/dis:
	(cd dis && make && mv bin/dis ../bin/dis)

clean:
	rm -rf bin