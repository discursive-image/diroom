// SPDX-FileCopyrightText: 2019 KIM KeepInMind GmbH
// SPDX-FileCopyrightText: 2020 KIM KeepInMind GmbH
//
// SPDX-License-Identifier: MIT

package google

import (
	"context"
	"fmt"

	"cloud.google.com/go/storage"
)

type Bkt struct {
	name string
	h    *storage.BucketHandle
}

func (c *Client) NewBkt(ctx context.Context, name string) (*Bkt, error) {
	gsc, err := storage.NewClient(ctx, c.Opts...)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize storage client: %w", err)
	}

	return &Bkt{h: gsc.Bucket(name), name: name}, nil
}

func (b *Bkt) Object(key string) *Obj {
	return &Obj{ObjectHandle: b.h.Object(key), key: key, bkt: b.name}
}

type Obj struct {
	bkt string
	key string
	*storage.ObjectHandle
}

func (o *Obj) URI() string { return "gs://" + o.bkt + "/" + o.key }

func (o *Obj) Trash() error {
	return o.Delete(context.Background())
}
