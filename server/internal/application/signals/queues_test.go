package signals_test

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"

	"github.com/makifbaysal/tasktrooper/server/internal/application/signals"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

const (
	lagTable  = `sum by (topic, partition) (` + kafkaMetric + `{consumergroup="billing"})`
	rateTable = `sum by (topic, partition) (rate(svc_kafka_offset{consumergroup="billing"}[120s]))`
	direct    = `{"group": "billing"}`
)

func (s *SignalsSuite) confirmedOn(c domain.Component, ds domain.DataSource, signal, recipe string, params map[string]string, query string) domain.SignalBinding {
	b := s.binding(c, signal, domain.SignalBindingConfirmed, params)
	b.DataSourceID, b.RecipeID, b.Query = ds.ID, recipe, query
	return b
}

func (s *SignalsSuite) tableAnswers(querier *mocks.TableQuerier, query string, res domain.TableResult, err error) *mock.Call {
	return querier.On("Table", mock.Anything, mock.Anything, mock.Anything, mock.MatchedBy(func(q domain.TableQuery) bool {
		return q.Query == query && q.At.Equal(s.now)
	})).Return(res, err).Once()
}

func rows(r ...map[string]any) domain.TableResult { return domain.TableResult{Rows: r} }

func (s *SignalsSuite) TestQueuesReadsOneTablePerTargetAndJoinsItsColumnsOnTheKeyLabels() {
	worker := s.component(domain.ComponentRoleWorker, "billing")
	kafka := s.addSource(domain.DataSource{Kind: domain.DataSourceKindKafka, Name: "kafka prod"})
	backlog := s.confirmedOn(worker, s.promSource, "queue.backlog", kafkaRecipe, map[string]string{"group": "billing", "topic": "orders"}, "")
	publish := s.confirmedOn(worker, s.promSource, "queue.publish_rate", kafkaRecipe, map[string]string{"group": "billing", "topic": "invoices"}, "")
	consumers := s.confirmedOn(worker, s.promSource, "queue.consumers", "", nil, `sum(svc_members{group="billing"})`)
	consumed := s.confirmedOn(worker, kafka, "queue.consume_rate", "", nil, direct)
	requests := s.confirmedOn(worker, s.promSource, "http.requests", httpRecipe, map[string]string{"job": "billing"}, "")
	pending := s.binding(worker, "queue.dead_letter", domain.SignalBindingSuggested, map[string]string{"group": "billing"})
	pending.RecipeID = kafkaRecipe
	s.stored(worker.ID, domain.EnvironmentProduction, publish, consumers, consumed, requests, pending, backlog)
	s.tableAnswers(s.table, lagTable, rows(
		map[string]any{"topic": "orders", "partition": "10", "value": 5.0},
		map[string]any{"topic": "orders", "partition": "2", "value": 3.0},
	), nil)
	s.tableAnswers(s.table, rateTable, rows(
		map[string]any{"topic": "orders", "partition": "2", "value": 1.5},
		map[string]any{"topic": "audit", "partition": "0", "value": 0.0},
	), nil)
	brokerColumns := []domain.TableColumn{{Key: "partition", Label: "Partition"}, {Key: "lag", Label: "Lag", Unit: "messages"}}
	s.tableAnswers(s.brokerRead, direct, domain.TableResult{Columns: brokerColumns, Rows: []map[string]any{{"partition": 0, "lag": 4}}, Truncated: true}, nil)

	got, err := s.svc.Queues(s.ctx, worker.ID, domain.EnvironmentProduction)

	s.Require().NoError(err)
	s.Equal(worker.ID, got.ComponentID)
	s.Equal(domain.EnvironmentProduction, got.Environment)
	s.True(got.ReadAt.Equal(s.now))
	s.Require().Len(got.Queues, 2)
	recipeTable := got.Queues[0]
	s.Equal(backlog.ID, recipeTable.SignalBindingID, "the first binding in catalog order names the table")
	s.Equal("queue.backlog", recipeTable.Signal)
	s.Equal([]string{"topic", "partition"}, recipeTable.KeyColumns)
	s.Equal(s.promSource.ID, recipeTable.DataSourceID)
	s.Equal("local prom", recipeTable.DataSourceName)
	s.Equal(kafkaRecipe, recipeTable.RecipeID)
	s.Equal("kafka", recipeTable.MessagingSystem)
	s.Equal(map[string]string{"group": "billing"}, recipeTable.Params, "only the params the table reads")
	s.Nil(recipeTable.Error)
	s.Equal([]string{"topic", "partition", "lag", "rate"}, columnKeys(recipeTable.Columns))
	s.Equal([]map[string]any{
		{"topic": "audit", "partition": "0", "lag": nil, "rate": 0.0},
		{"topic": "orders", "partition": "2", "lag": 3.0, "rate": 1.5},
		{"topic": "orders", "partition": "10", "lag": 5.0, "rate": nil},
	}, recipeTable.Rows)
	s.False(recipeTable.Truncated)

	brokerTable := got.Queues[1]
	s.Equal(consumed.ID, brokerTable.SignalBindingID)
	s.Equal("kafka prod", brokerTable.DataSourceName)
	s.Equal("kafka", brokerTable.MessagingSystem, "a broker kind is its own messaging system")
	s.Equal("", brokerTable.RecipeID)
	s.Equal(brokerColumns, brokerTable.Columns)
	s.Equal([]map[string]any{{"partition": 0, "lag": 4}}, brokerTable.Rows)
	s.True(brokerTable.Truncated)
}

func (s *SignalsSuite) TestQueuesKeepsEveryOtherTableWhenOneCannotBeRead() {
	worker := s.component(domain.ComponentRoleWorker, "billing")
	kafka := s.addSource(domain.DataSource{Kind: domain.DataSourceKindKafka, Name: "kafka prod"})
	failing := s.confirmedOn(worker, s.promSource, "queue.backlog", kafkaRecipe, map[string]string{"group": "billing", "topic": "orders"}, "")
	unfilled := s.confirmedOn(worker, s.promSource, "queue.publish_rate", kafkaRecipe, map[string]string{"topic": "orders"}, "")
	fine := s.confirmedOn(worker, kafka, "queue.consume_rate", "", nil, direct)
	s.stored(worker.ID, domain.EnvironmentProduction, failing, unfilled, fine)
	s.tableAnswers(s.table, lagTable, rows(map[string]any{"topic": "orders", "partition": "0", "value": 1.0}), nil)
	s.tableAnswers(s.table, rateTable, domain.TableResult{}, errors.New("connection reset"))
	s.tableAnswers(s.brokerRead, direct, rows(map[string]any{"lag": 4}), nil)

	got, err := s.svc.Queues(s.ctx, worker.ID, domain.EnvironmentProduction)

	s.Require().NoError(err)
	s.Require().Len(got.Queues, 3)
	s.Require().NotNil(got.Queues[0].Error)
	s.Equal(domain.SignalErrUpstream, got.Queues[0].Error.Code)
	s.Empty(got.Queues[0].Rows, "a table that failed has no partial rows")
	s.Equal([]string{"topic", "partition", "lag", "rate"}, columnKeys(got.Queues[0].Columns))

	s.Equal(fine.ID, got.Queues[1].SignalBindingID)
	s.Nil(got.Queues[1].Error)
	s.Equal([]map[string]any{{"lag": 4}}, got.Queues[1].Rows)

	s.Equal(unfilled.ID, got.Queues[2].SignalBindingID)
	s.Require().NotNil(got.Queues[2].Error)
	s.Equal(domain.SignalErrNeedsParams, got.Queues[2].Error.Code)
	s.Equal("no value for group", got.Queues[2].Error.Message)
}

func (s *SignalsSuite) TestQueuesNeverAnswersOneCacheScopeFromAnothersEntry() {
	s.scopeByTenant()
	worker := s.component(domain.ComponentRoleWorker, "billing")
	backlog := s.confirmedOn(worker, s.promSource, "queue.backlog", kafkaRecipe, map[string]string{"group": "billing", "topic": "orders"}, "")
	s.stored(worker.ID, domain.EnvironmentProduction, backlog)
	for _, lag := range []float64{1, 2} {
		s.tableAnswers(s.table, lagTable, rows(map[string]any{"topic": "orders", "partition": "0", "value": lag}), nil)
		s.tableAnswers(s.table, rateTable, rows(), nil)
	}

	a, err := s.svc.Queues(s.scoped("tenant-a"), worker.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)
	b, err := s.svc.Queues(s.scoped("tenant-b"), worker.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)
	again, err := s.svc.Queues(s.scoped("tenant-a"), worker.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)

	s.Equal(1.0, a.Queues[0].Rows[0]["lag"])
	s.Equal(2.0, b.Queues[0].Rows[0]["lag"])
	s.Equal(a, again, "read once per scope within the cache window")
}

func (s *SignalsSuite) TestQueuesIsReadAgainOnceTheBindingChanges() {
	worker := s.component(domain.ComponentRoleWorker, "billing")
	backlog := s.confirmedOn(worker, s.promSource, "queue.backlog", kafkaRecipe, map[string]string{"group": "billing", "topic": "orders"}, "")
	moved := backlog
	moved.Params = map[string]string{"group": "invoicing", "topic": "orders"}
	moved.UpdatedAt = s.now.Add(time.Minute)
	s.bindings.On("ListSignalBindings", mock.Anything, worker.ID, domain.EnvironmentProduction).Return([]domain.SignalBinding{backlog}, nil).Once()
	s.bindings.On("ListSignalBindings", mock.Anything, worker.ID, domain.EnvironmentProduction).Return([]domain.SignalBinding{moved}, nil).Once()
	s.tableAnswers(s.table, lagTable, rows(), nil)
	s.tableAnswers(s.table, rateTable, rows(), nil)
	s.tableAnswers(s.table, `sum by (topic, partition) (`+kafkaMetric+`{consumergroup="invoicing"})`, rows(), nil)
	s.tableAnswers(s.table, `sum by (topic, partition) (rate(svc_kafka_offset{consumergroup="invoicing"}[120s]))`, rows(), nil)

	for range 2 {
		_, err := s.svc.Queues(s.ctx, worker.ID, domain.EnvironmentProduction)
		s.Require().NoError(err)
	}
}

func (s *SignalsSuite) TestQueuesGroupsABrokersQueriesByWhatTheyReadNotWhatTheyMeasure() {
	worker := s.component(domain.ComponentRoleWorker, "billing")
	kafka := s.addSource(domain.DataSource{Kind: domain.DataSourceKindKafka, Name: "kafka prod"})
	lag := s.confirmedOn(worker, kafka, "queue.backlog", "", nil, `{"group":"billing","metric":"lag"}`)
	members := s.confirmedOn(worker, kafka, "queue.consumers", "", nil, `{"group":"billing","metric":"consumers"}`)
	other := s.confirmedOn(worker, kafka, "queue.consume_rate", "", nil, `{"group":"shipping"}`)
	s.stored(worker.ID, domain.EnvironmentProduction, members, other, lag)
	s.tableAnswers(s.brokerRead, `{"group":"billing","metric":"lag"}`, domain.TableResult{
		KeyColumns: []string{"topic", "partition"}, Rows: []map[string]any{{"topic": "orders", "partition": 0.0, "lag": 4.0}},
	}, nil)
	s.tableAnswers(s.brokerRead, `{"group":"shipping"}`, rows(), nil)

	got, err := s.svc.Queues(s.ctx, worker.ID, domain.EnvironmentProduction)

	s.Require().NoError(err)
	s.Require().Len(got.Queues, 2, "one table per group, however many measures are bound")
	s.Equal(lag.ID, got.Queues[0].SignalBindingID)
	s.Equal("queue.backlog", got.Queues[0].Signal)
	s.Equal([]string{"topic", "partition"}, got.Queues[0].KeyColumns, "a broker kind names its own key columns")
	s.Equal(other.ID, got.Queues[1].SignalBindingID)
}

func (s *SignalsSuite) TestQueuesAskedAtOnceAreReadOnce() {
	worker := s.component(domain.ComponentRoleWorker, "billing")
	backlog := s.confirmedOn(worker, s.promSource, "queue.backlog", kafkaRecipe, map[string]string{"group": "billing", "topic": "orders"}, "")
	s.stored(worker.ID, domain.EnvironmentProduction, backlog)
	release := make(chan struct{})
	s.tableAnswers(s.table, lagTable, rows(), nil).Run(func(mock.Arguments) { <-release })
	s.tableAnswers(s.table, rateTable, rows(), nil).Run(func(mock.Arguments) { <-release })

	const callers = 5
	done := make(chan error, callers)
	for range callers {
		go func() {
			_, err := s.svc.Queues(s.ctx, worker.ID, domain.EnvironmentProduction)
			done <- err
		}()
	}
	time.Sleep(100 * time.Millisecond)
	close(release)
	for range callers {
		s.Require().NoError(<-done)
	}
}

func (s *SignalsSuite) TestABindingChangeDropsOnlyItsOwnScopesAnswers() {
	s.scopeByTenant()
	worker := s.component(domain.ComponentRoleWorker, "billing")
	backlog := s.confirmedOn(worker, s.promSource, "queue.backlog", kafkaRecipe, map[string]string{"group": "billing", "topic": "orders"}, "")
	s.stored(worker.ID, domain.EnvironmentProduction, backlog)
	for _, lag := range []float64{1, 2, 3} {
		s.tableAnswers(s.table, lagTable, rows(map[string]any{"topic": "orders", "partition": "0", "value": lag}), nil)
		s.tableAnswers(s.table, rateTable, rows(), nil)
	}
	gone := uuid.New()
	s.bindings.On("DeleteSignalBinding", mock.Anything, gone).Return(nil).Once()

	a, err := s.svc.Queues(s.scoped("tenant-a"), worker.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)
	b, err := s.svc.Queues(s.scoped("tenant-b"), worker.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)
	s.Require().NoError(s.svc.Delete(s.scoped("tenant-a"), gone))
	stillB, err := s.svc.Queues(s.scoped("tenant-b"), worker.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)
	againA, err := s.svc.Queues(s.scoped("tenant-a"), worker.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)

	s.Equal(1.0, a.Queues[0].Rows[0]["lag"])
	s.Equal(b, stillB, "another scope's answer outlives this one's change")
	s.Equal(3.0, againA.Queues[0].Rows[0]["lag"], "the scope that changed reads again")
}

func (s *SignalsSuite) TestAJoinedTableOverTheRowCapKeepsTheLargestInKeyOrder() {
	worker := s.component(domain.ComponentRoleWorker, "billing")
	backlog := s.confirmedOn(worker, s.promSource, "queue.backlog", kafkaRecipe, map[string]string{"group": "billing", "topic": "orders"}, "")
	s.stored(worker.ID, domain.EnvironmentProduction, backlog)
	var lags, rates []map[string]any
	for p := range 300 {
		lags = append(lags, map[string]any{"topic": "orders", "partition": fmt.Sprint(p), "value": float64(p)})
		rates = append(rates, map[string]any{"topic": "orders", "partition": fmt.Sprint(300 + p), "value": 1.0})
	}
	s.tableAnswers(s.table, lagTable, rows(lags...), nil)
	s.tableAnswers(s.table, rateTable, rows(rates...), nil)

	got, err := s.svc.Queues(s.ctx, worker.ID, domain.EnvironmentProduction)

	s.Require().NoError(err)
	q := got.Queues[0]
	s.True(q.Truncated)
	s.Require().Len(q.Rows, 500)
	s.Equal("0", q.Rows[0]["partition"], "key order survives the cut")
	s.Equal("299", q.Rows[299]["partition"], "every row with a backlog is kept")
	s.Equal("499", q.Rows[499]["partition"], "rows with none are what goes")
}

func (s *SignalsSuite) TestQueuesRefusesAnUnknownEnvironment() {
	_, err := s.svc.Queues(s.ctx, uuid.New(), domain.DeployEnvironment("qa"))
	s.ErrorIs(err, signals.ErrInvalidInput)
}

func columnKeys(cols []domain.TableColumn) []string {
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		out = append(out, c.Key)
	}
	return out
}
