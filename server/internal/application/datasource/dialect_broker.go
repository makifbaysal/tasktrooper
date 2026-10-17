package datasource

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// brokerDialect is the JSON query of a direct broker kind (domain.KafkaQuery,
// RabbitMQQuery, NATSQuery). A param is escaped for a JSON string and may sit
// only in a value naming the entity read (a group, a queue, a stream): in the
// metric it would choose the measure, in a key the shape. No built-in means
// anything: the kind reads the live value, at no step and no quantile.
type brokerDialect struct {
	label     string
	nameKeys  []string
	parse     func(string) error
	inventory *regexp.Regexp
	// noBuiltins says why no built-in applies; empty is a live read's reason.
	noBuiltins string
}

// The inventory entries each kind's Discover lists.
var (
	kafkaInventoryPattern    = regexp.MustCompile(`^(group|topic)/.+$`)
	rabbitMQInventoryPattern = regexp.MustCompile(`^queue/.+$`)
	natsInventoryPattern     = regexp.MustCompile(`^(stream/[^/]+|consumer/[^/]+/.+)$`)
)

var (
	kafkaDialect = brokerDialect{
		label: "Kafka", nameKeys: []string{"group", "topic"}, inventory: kafkaInventoryPattern,
		parse: func(q string) error { _, err := domain.ParseKafkaQuery(q); return err },
	}
	rabbitMQDialect = brokerDialect{
		label: "RabbitMQ", nameKeys: []string{"queue"}, inventory: rabbitMQInventoryPattern,
		parse: func(q string) error { _, err := domain.ParseRabbitMQQuery(q); return err },
	}
	natsDialect = brokerDialect{
		label: "NATS", nameKeys: []string{"stream", "consumer"}, inventory: natsInventoryPattern,
		parse: func(q string) error { _, err := domain.ParseNATSQuery(q); return err },
	}
)

func (d brokerDialect) validMetricName(name string) bool { return d.inventory.MatchString(name) }

func (d brokerDialect) check(subject, query string, declared map[string]bool) []string {
	var problems []string
	seen := map[string]bool{}
	add := func(format string, args ...any) {
		p := fmt.Sprintf(format, args...)
		if !seen[p] {
			seen[p] = true
			problems = append(problems, p)
		}
	}
	for _, name := range placeholdersOutsideJSONStrings(query) {
		add("%s: $%s must sit inside a JSON string", subject, name)
	}
	if len(problems) > 0 {
		return problems
	}
	var doc any
	dec := json.NewDecoder(strings.NewReader(query))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return []string{fmt.Sprintf("%s is not JSON: %v", subject, err)}
	}
	walkJSONStrings(doc, nil, func(path []string, s string, isKey bool) {
		for _, m := range placeholderRegex.FindAllStringSubmatch(s, -1) {
			name := m[1]
			switch {
			case isKey:
				add("%s: $%s may not sit in a JSON key", subject, name)
			case builtinPlaceholders[name]:
				add("%s: $%s has no meaning in a %s query; %s", subject, name, d.label, d.builtinsReason())
			case !declared[name]:
				add("%s uses $%s, which is neither a declared param nor a built-in", subject, name)
			case len(path) != 1 || !slices.Contains(d.nameKeys, path[0]):
				add("%s: $%s may appear only in the %s value", subject, name, strings.Join(d.nameKeys, " or "))
			}
		}
	})
	if len(problems) > 0 {
		return problems
	}
	sample := map[string]string{}
	for name := range declared {
		sample[name] = "sample"
	}
	rendered, err := d.render(query, sample, time.Minute, DefaultQuantile)
	if err == nil {
		err = d.parse(rendered)
	}
	if err != nil {
		return []string{fmt.Sprintf("%s: %v", subject, err)}
	}
	return nil
}

func (brokerDialect) render(query string, params map[string]string, _ time.Duration, _ float64) (string, error) {
	return expand(query, params, func(string) (string, bool) { return "", false }, escapeJSONString)
}

func (d brokerDialect) builtinsReason() string {
	if d.noBuiltins != "" {
		return d.noBuiltins
	}
	return "it reads the live value"
}
