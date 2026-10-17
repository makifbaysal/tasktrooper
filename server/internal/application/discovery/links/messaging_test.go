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
		"streams/build.gradle":    mapFile("dependencies {\n  implementation 'org.apache.kafka:kafka-streams:3.7.0'\n}\n"),
		"binder/build.gradle":     mapFile("dependencies {\n  implementation(\"org.springframework.cloud:spring-cloud-stream-binder-rabbit:4.1.0\")\n}\n"),
		"quarkus/build.gradle":    mapFile("dependencies {\n  implementation 'io.quarkus:quarkus-smallrye-reactive-messaging-kafka'\n}\n"),
		"faust/requirements.txt":  mapFile("faust-streaming==0.11.0\n"),
		"dotnet/Worker.csproj": mapFile(`<Project Sdk="Microsoft.NET.Sdk.Worker">
  <ItemGroup>
    <PackageReference Include="confluent.kafka" Version="2.4.0" />
  </ItemGroup>
</Project>`),
		"bus/Bus.csproj":   mapFile(`<Project><ItemGroup><PackageReference Version="7.17.0" Include="Azure.Messaging.ServiceBus" /></ItemGroup></Project>`),
		"natsnet/N.csproj": mapFile(`<Project><ItemGroup><PackageReference Include="NATS.Net" Version="2.2.0" /></ItemGroup></Project>`),
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
		{"streams", "dep:kafka", "kafka"},
		{"binder", "dep:rabbitmq", "rabbitmq"},
		{"quarkus", "dep:kafka", "kafka"},
		{"faust", "dep:kafka", "kafka"},
		{"dotnet", "dep:kafka", "kafka"},
		{"bus", "dep:azure_servicebus", "servicebus"},
		{"natsnet", "dep:nats", "nats"},
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
