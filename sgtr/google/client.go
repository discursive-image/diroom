// SPDX-FileCopyrightText: 2019 KIM KeepInMind GmbH
// SPDX-FileCopyrightText: 2020 KIM KeepInMind GmbH
//
// SPDX-License-Identifier: MIT

package google

import (
	"context"

	"google.golang.org/api/option"
)

// Client is responsible for retriving the credentials and using them to
// authenticate with the Google services.
type Client struct {
	Opts []option.ClientOption
}

// NewClient returns a new client instance. If no option is provided, it will
// try to authenticate (when needed, for example at stream initialization) using
// the GOOGLE_APPLICATION_CREDENTIALS environment variable.
func NewClient(ctx context.Context, opts ...func(*Client)) *Client {
	c := &Client{Opts: []option.ClientOption{}}
	for _, f := range opts {
		f(c)
	}
	return c
}
