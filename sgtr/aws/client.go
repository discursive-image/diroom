// SPDX-FileCopyrightText: 2020 KIM KeepInMind GmbH
//
// SPDX-License-Identifier: MIT

package aws

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
)

type Client struct {
	sess *session.Session
}

func NewClient(region string) (*Client, error) {
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(region),
	})
	if err != nil {
		return nil, fmt.Errorf("unable to create aws client: %w", err)
	}

	return &Client{sess}, nil
}
