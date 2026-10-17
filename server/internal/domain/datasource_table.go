package domain

import "time"

// TableQuery is one instant read that answers rows instead of a series: Query
// is the kind's own query text (rendered from a recipe table or a binding's
// query), At the instant it is read at.
type TableQuery struct {
	Query string
	At    time.Time
}

type TableColumn struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Unit  string `json:"unit"`
}

// TableResult is rows keyed by TableColumn.Key; a row may lack a key its
// source had no value for, and a value is a string, a finite number or nil.
// KeyColumns, when the kind knows them, are the columns that name a row.
// Truncated says the source held more rows than were returned.
type TableResult struct {
	Columns    []TableColumn
	KeyColumns []string
	Rows       []map[string]any
	Truncated  bool
}

// TableValueKey is the column a kind that reads one value per row (an
// instant PromQL vector, a CloudWatch metric) puts it under; every other
// column of such a row is one of its labels.
const TableValueKey = "value"

// QueueTarget is one consumer a broker kind found on its broker (a Kafka
// consumer group, a RabbitMQ queue, a NATS durable consumer). Name is what
// names it, matched against a component's names; Params name it as the
// params of the kind's direct recipe (group; queue; stream and consumer), so
// a suggestion reads it through that recipe, whose rendering escapes them.
type QueueTarget struct {
	Name   string
	Params map[string]string
}
