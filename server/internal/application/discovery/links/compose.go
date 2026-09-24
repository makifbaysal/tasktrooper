package links

import (
	"path"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// composeExtractor reads docker-compose service topology: a built service
// depending on an infra image becomes a resource link, depending on another
// built service becomes a component link, and an infra image seen anywhere
// upgrades the engine on database links the repo already has.
type composeExtractor struct{}

type composeFile struct {
	Services map[string]composeService `yaml:"services"`
}

type composeService struct {
	Image       string    `yaml:"image"`
	Build       yaml.Node `yaml:"build"`
	DependsOn   yaml.Node `yaml:"depends_on"`
	Links       []string  `yaml:"links"`
	Environment yaml.Node `yaml:"environment"`
}

// infraImage is a docker-compose "image:" prefix that names infrastructure
// rather than something this repo built.
type infraImage struct {
	prefix   string
	kind     domain.ResourceKind
	vendor   string
	name     string
	protocol domain.LinkProtocol
}

var infraImages = []infraImage{
	{"postgres", domain.ResourceDatabase, "postgres", "PostgreSQL", domain.LinkSQL},
	{"mysql", domain.ResourceDatabase, "mysql", "MySQL", domain.LinkSQL},
	{"mariadb", domain.ResourceDatabase, "mariadb", "MariaDB", domain.LinkSQL},
	{"mongo", domain.ResourceDatabase, "mongodb", "MongoDB", domain.LinkOther},
	{"redis", domain.ResourceCache, "redis", "Redis", domain.LinkRedis},
	{"rabbitmq", domain.ResourceQueue, "rabbitmq", "RabbitMQ", domain.LinkQueue},
	{"confluentinc/", domain.ResourceQueue, "kafka", "Kafka", domain.LinkQueue},
	{"kafka", domain.ResourceQueue, "kafka", "Kafka", domain.LinkQueue},
	{"elasticsearch", domain.ResourceSearch, "elasticsearch", "Elasticsearch", domain.LinkSDK},
	{"opensearch", domain.ResourceSearch, "opensearch", "OpenSearch", domain.LinkSDK},
	{"minio", domain.ResourceStorage, "minio", "MinIO", domain.LinkSDK},
	{"localstack", domain.ResourceAPI, "aws", "AWS (LocalStack)", domain.LinkSDK},
}

func matchInfraImage(image string) (infraImage, bool) {
	image = strings.ToLower(image)
	if idx := strings.IndexByte(image, ':'); idx >= 0 {
		image = image[:idx]
	}
	base := image
	if idx := strings.LastIndexByte(image, '/'); idx >= 0 {
		base = image[idx+1:]
	}
	for _, m := range infraImages {
		if strings.HasPrefix(image, m.prefix) || strings.HasPrefix(base, m.prefix) {
			return m, true
		}
	}
	return infraImage{}, false
}

func (composeExtractor) extract(ctx *scanCtx) {
	for _, file := range ctx.tree.Files {
		base := baseName(file)
		if !isComposeFile(base) {
			continue
		}
		content := ctx.tree.ReadString(file)
		if content == "" {
			continue
		}
		var doc composeFile
		if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
			ctx.collector.warn(file + " is not parseable YAML")
			continue
		}
		processCompose(ctx, file, doc)
	}
}

func isComposeFile(base string) bool {
	lower := strings.ToLower(base)
	if !strings.HasSuffix(lower, ".yml") && !strings.HasSuffix(lower, ".yaml") {
		return false
	}
	return strings.HasPrefix(lower, "docker-compose") || strings.HasPrefix(lower, "compose")
}

func processCompose(ctx *scanCtx, file string, doc composeFile) {
	dir := path.Dir(file)
	if dir == "." {
		dir = ""
	}

	infraServices := map[string]infraImage{}
	builtServices := map[string]string{}

	for name, svc := range doc.Services {
		if svc.Image != "" {
			if m, ok := matchInfraImage(svc.Image); ok {
				infraServices[name] = m
			}
		}
		if buildCtx, ok := composeBuildContext(svc.Build); ok {
			resolved := joinRel(dir, buildCtx)
			if comp, ok := ctx.component(resolved); ok {
				builtServices[name] = comp.Path
			}
		}
	}

	upgradeEngineFromInfra(ctx, infraServices, file)

	for name, svc := range doc.Services {
		componentPath, isBuilt := builtServices[name]
		if !isBuilt {
			continue
		}
		deps := composeDependencies(svc.DependsOn, svc.Links)
		for _, dep := range deps {
			if m, ok := infraServices[dep]; ok {
				ctx.collector.addLink(rawSignal{
					componentPath: componentPath,
					signalKey:     "compose:" + dep,
					protocol:      m.protocol,
					target: domain.LinkTarget{
						Kind:         domain.LinkTargetResource,
						ResourceKind: m.kind,
						Vendor:       m.vendor,
						Name:         m.name,
					},
					evidence:   []domain.SourceEvidence{{Path: file}},
					confidence: domain.ConfidenceHigh,
					family:     familyOf(m.kind, m.vendor),
				})
				continue
			}
			if targetComponent, ok := builtServices[dep]; ok && targetComponent != componentPath {
				ctx.collector.addLink(rawSignal{
					componentPath: componentPath,
					signalKey:     "compose:" + dep,
					protocol:      domain.LinkHTTP,
					target: domain.LinkTarget{
						Kind:          domain.LinkTargetComponent,
						ComponentPath: targetComponent,
					},
					evidence:   []domain.SourceEvidence{{Path: file}},
					confidence: domain.ConfidenceHigh,
				})
			}
		}
	}
}

// upgradeEngineFromInfra sets the vendor/name on every generic database link
// already collected anywhere in the repo when exactly the kind of database
// infra images name is running, adding the compose file as evidence.
func upgradeEngineFromInfra(ctx *scanCtx, infraServices map[string]infraImage, file string) {
	for _, m := range infraServices {
		if m.kind != domain.ResourceDatabase {
			continue
		}
		for _, sig := range ctx.collector.links {
			if sig.target.Kind != domain.LinkTargetResource || sig.target.ResourceKind != domain.ResourceDatabase {
				continue
			}
			if sig.target.Vendor != "" && sig.target.Vendor != "database" {
				continue
			}
			sig.target.Vendor = m.vendor
			sig.target.Name = m.name
			sig.protocol = m.protocol
			sig.mergeEvidence([]domain.SourceEvidence{{Path: file}})
		}
	}
}

func joinRel(dir, rel string) string {
	rel = strings.TrimPrefix(rel, "./")
	if dir == "" {
		return path.Clean(rel)
	}
	return path.Clean(dir + "/" + rel)
}

func composeBuildContext(node yaml.Node) (string, bool) {
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Value == "" {
			return "", false
		}
		return node.Value, true
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value == "context" {
				return node.Content[i+1].Value, true
			}
		}
	}
	return "", false
}

func composeDependencies(dependsOn yaml.Node, links []string) []string {
	var out []string
	switch dependsOn.Kind {
	case yaml.SequenceNode:
		for _, c := range dependsOn.Content {
			out = append(out, c.Value)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(dependsOn.Content); i += 2 {
			out = append(out, dependsOn.Content[i].Value)
		}
	}
	for _, l := range links {
		if idx := strings.IndexByte(l, ':'); idx >= 0 {
			l = l[:idx]
		}
		out = append(out, l)
	}
	return out
}
