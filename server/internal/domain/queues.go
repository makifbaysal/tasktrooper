package domain

import (
	"time"

	"github.com/google/uuid"
)

// QueueTable is one table of a component's queues view: what one data source
// holds for the queue, partition or consumer a confirmed queue.* binding reads.
// SignalBindingID is the first such binding (in catalog order) of the ones
// that read this same table, Signal its signal; Params are the ones the table
// used. KeyColumns are the columns that name a row (a partition's topic and
// number, a queue's name); the others hold values. A table that could not be
// read carries Error and no rows; the others stand.
type QueueTable struct {
	SignalBindingID uuid.UUID         `json:"signal_binding_id"`
	Signal          string            `json:"signal"`
	DataSourceID    uuid.UUID         `json:"data_source_id"`
	DataSourceName  string            `json:"data_source_name"`
	RecipeID        string            `json:"recipe_id"`
	MessagingSystem string            `json:"messaging_system"`
	Params          map[string]string `json:"params"`
	Columns         []TableColumn     `json:"columns"`
	KeyColumns      []string          `json:"key_columns,omitempty"`
	Rows            []map[string]any  `json:"rows"`
	Truncated       bool              `json:"truncated"`
	Error           *SignalReadError  `json:"error"`
}

// ComponentQueues is GET /v1/components/:id/queues. ReadAt is when its
// tables were read: an answer is kept a while, so it may be older than the
// request.
type ComponentQueues struct {
	ComponentID uuid.UUID         `json:"component_id"`
	Environment DeployEnvironment `json:"environment"`
	ReadAt      time.Time         `json:"read_at"`
	Queues      []QueueTable      `json:"queues"`
}
