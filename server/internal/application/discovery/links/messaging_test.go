package links

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/inventory"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestDetect_MessagingLibrariesLinkToTheirBrokersQueue(t *testing.T) {
	fsys := fstest.MapFS{
		"franz/go.mod":            mapFile("module example.com/franz\n\ngo 1.22\n\nrequire github.com/twmb/franz-go/pkg/kadm v1.12.0\n"),
		"sarama/go.mod":           mapFile("module example.com/sarama\n\ngo 1.22\n\nrequire github.com/IBM/sarama v1.43.0\n"),
		"spring/build.gradle":     mapFile("dependencies {\n  implementation 'org.springframework.kafka:spring-kafka:3.2.0'\n}\n"),
		"rabbit/requirements.txt": mapFile("aio-pika==9.4.0\n"),
		"nats/package.json":       mapFile(`{"dependencies": {"@nats-io/jetstream": "3.0.0"}}`),
		"pubsub/requirements.txt": mapFile("google-cloud-pubsub>=2.21\n"),
		"sqs/Gemfile":             mapFile("gem 'shoryuken'\n"),
		"jobs/Gemfile":            mapFile("gem \"sidekiq\"\n"),
		"asynq/go.mod":            mapFile("module example.com/asynq\n\ngo 1.22\n\nrequire github.com/hibiken/asynq v0.25.0\n"),
	}
	cases := []struct {
		path, signalKey, vendor string
	}{
		{"franz", "dep:kafka", "kafka"},
		{"sarama", "dep:kafka", "kafka"},
		{"spring", "dep:kafka", "kafka"},
		{"rabbit", "dep:rabbitmq", "rabbitmq"},
		{"nats", "dep:nats", "nats"},
		{"pubsub", "dep:pubsub", "pubsub"},
		{"sqs", "dep:aws_sqs", "sqs"},
		{"jobs", "dep:bullmq", "redis"},
		{"asynq", "dep:bullmq", "redis"},
	}
	var components []domain.DetectedComponent
	for _, tc := range cases {
		components = append(components, domain.DetectedComponent{Path: tc.path})
	}

	result := Detect(inventory.FromFS(fsys), components)

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			l, ok := linkBySignal(result.Links, tc.path, tc.signalKey)
			require.True(t, ok, "%+v", result.Links)
			assert.Equal(t, domain.ResourceQueue, l.Target.ResourceKind)
			assert.Equal(t, tc.vendor, l.Target.Vendor)
			assert.Equal(t, domain.LinkQueue, l.Protocol)
		})
	}
}
