/*
Copyright 2019 Google, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package pubsub

import (
	"context"
	"encoding/json"
	"sync"

	"cloud.google.com/go/pubsub"
	"github.com/GoogleCloudPlatform/compat/pkg/constants"
	gcb "google.golang.org/api/cloudbuild/v1"
)

const topicName = "cloud-builds"

type Publisher struct {
	t  *pubsub.Topic
	mu sync.Mutex
}

func New() *Publisher {
	return &Publisher{}
}

func (p *Publisher) Publish(b *gcb.Build) error {
	ctx := context.Background()
	p.mu.Lock()
	if p.t == nil {
		c, err := pubsub.NewClient(ctx, constants.ProjectID)
		if err != nil {
			return err
		}
		p.t = c.Topic(topicName)
	}
	p.mu.Unlock()

	bs, err := json.Marshal(b)
	if err != nil {
		return err
	}

	r := p.t.Publish(ctx, &pubsub.Message{
		Attributes: map[string]string{
			"gcb-compat": "true",
			"buildId":    b.Id,
			"status":     b.Status,
		},
		Data: bs,
	})
	<-r.Ready()
	_, err = r.Get(ctx)
	return err
}
