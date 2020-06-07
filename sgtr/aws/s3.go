// SPDX-FileCopyrightText: 2020 KIM KeepInMind GmbH
//
// SPDX-License-Identifier: MIT

package aws

import (
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

type Bkt struct {
	client *s3.S3
	name   string
}

func (c *Client) NewBkt(name string) *Bkt {
	return &Bkt{
		client: s3.New(c.sess),
		name:   name,
	}
}

func (b *Bkt) UploadObj(ctx context.Context, r io.Reader, key string) (string, error) {
	uploader := s3manager.NewUploaderWithClient(b.client)
	resp, err := uploader.UploadWithContext(ctx, &s3manager.UploadInput{
		Bucket: aws.String(b.name),
		Key:    aws.String(key),
		Body:   r,
	})
	if err != nil {
		return "", fmt.Errorf("unable to upload obj to s3 bkt: %w", err)
	}
	return resp.Location, nil
}

func (b *Bkt) Trash(key string) error {
	if _, err := b.client.DeleteObject(&s3.DeleteObjectInput{
		Bucket: aws.String(b.name),
		Key:    aws.String(key),
	}); err != nil {
		return fmt.Errorf("unable to delete obj from s3 bkt: %w", err)
	}
	return nil
}
