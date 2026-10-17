package kafka

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// partition is one partition's place in a group: committed and end offsets
// and the lag between them, each -1 where the cluster had no answer.
type partition struct {
	topic     string
	partition int32
	committed int64
	end       int64
	lag       int64
	member    string
	client    string
}

type groupRead struct {
	exists     bool
	members    int
	consumers  int
	partitions []partition
}

func (r groupRead) totalLag() *float64 {
	var total int64
	read := 0
	for _, p := range r.partitions {
		if p.lag >= 0 {
			total += p.lag
			read++
		}
	}
	if read == 0 && len(r.partitions) > 0 {
		return nil
	}
	f := float64(total)
	return &f
}

func (r groupRead) warnings(q domain.KafkaQuery) []string {
	var w []string
	switch {
	case !r.exists:
		return []string{fmt.Sprintf("the cluster has no consumer group %q", q.Group)}
	case q.Topic != "" && len(r.partitions) == 0:
		w = append(w, fmt.Sprintf("the group %q has no partition of the topic %q", q.Group, q.Topic))
	}
	unknown := 0
	for _, p := range r.partitions {
		if p.lag < 0 {
			unknown++
		}
	}
	if unknown > 0 {
		w = append(w, fmt.Sprintf("the lag of %d of %d partitions could not be read", unknown, len(r.partitions)))
	}
	return w
}

// group reads a classic group through kadm's Lag. A group the classic
// describe does not know, or shows active with no member (no classic group
// is), may be a KIP-848 one (Kafka 4's consumer protocol), described through
// its own API instead.
func (s *session) group(ctx context.Context, group, topic string) (groupRead, error) {
	lags, err := s.adm.Lag(ctx, group)
	if err != nil {
		return groupRead{}, s.fail("consumer group", err)
	}
	l, ok := lags[group]
	missing := !ok || l.State == "Dead" || errors.Is(l.DescribeErr, kerr.GroupIDNotFound)
	if missing || (l.Error() == nil && len(l.Members) == 0 && l.State != "Empty") {
		r, err := s.nextGenGroup(ctx, group, topic)
		if err != nil || r.exists || missing {
			return r, err
		}
	}
	if err := l.Error(); err != nil {
		return groupRead{}, s.fail("consumer group", err)
	}
	r := groupRead{exists: true, members: len(l.Members)}
	for _, m := range l.Members {
		if c, ok := m.Assigned.AsConsumer(); ok && assigned(c, topic) {
			r.consumers++
		}
	}
	for _, pl := range l.Lag.Sorted() {
		if topic != "" && pl.Topic != topic {
			continue
		}
		p := partition{topic: pl.Topic, partition: pl.Partition, committed: -1, end: -1, lag: -1}
		if pl.Commit.At >= 0 {
			p.committed = pl.Commit.At
		}
		if pl.End.Err == nil && pl.End.Offset >= 0 {
			p.end = pl.End.Offset
		}
		if pl.Err == nil && pl.Lag >= 0 {
			p.lag = pl.Lag
		}
		if pl.Member != nil {
			p.member, p.client = pl.Member.MemberID, pl.Member.ClientID
		}
		r.partitions = append(r.partitions, p)
	}
	return r, nil
}

func assigned(c *kmsg.ConsumerMemberAssignment, topic string) bool {
	for _, t := range c.Topics {
		if (topic == "" || t.Topic == topic) && len(t.Partitions) > 0 {
			return true
		}
	}
	return false
}

// nextGenGroup answers a group that does not exist (exists false) when the
// cluster has no KIP-848 group of that name, or cannot describe one (a broker
// before Kafka 4). It describes the group itself rather than through kadm so
// that an assignment naming a topic only by its id still reads per topic.
func (s *session) nextGenGroup(ctx context.Context, group, topic string) (groupRead, error) {
	req := kmsg.NewPtrConsumerGroupDescribeRequest()
	req.Groups = []string{group}
	var g *kmsg.ConsumerGroupDescribeResponseGroup
	for _, shard := range s.cl.RequestSharded(ctx, req) {
		if shard.Err != nil {
			if errors.Is(shard.Err, context.DeadlineExceeded) || errors.Is(shard.Err, context.Canceled) {
				return groupRead{}, s.fail("consumer group", shard.Err)
			}
			continue
		}
		resp, ok := shard.Resp.(*kmsg.ConsumerGroupDescribeResponse)
		if !ok {
			continue
		}
		for i := range resp.Groups {
			if resp.Groups[i].Group == group {
				g = &resp.Groups[i]
			}
		}
	}
	if g == nil {
		return groupRead{}, nil
	}
	switch err := kerr.ErrorForCode(g.ErrorCode); {
	case errors.Is(err, kerr.GroupAuthorizationFailed):
		return groupRead{}, s.fail("consumer group", &kadm.AuthError{Err: err})
	case err != nil || g.State == "Dead":
		return groupRead{}, nil
	}
	commits, err := s.adm.FetchOffsets(ctx, group)
	if err != nil {
		return groupRead{}, s.fail("committed offsets", err)
	}
	names, err := s.topicNames(ctx, g)
	if err != nil {
		return groupRead{}, err
	}

	set := make(kadm.TopicsSet)
	owners := map[string]map[int32]kmsg.ConsumerGroupDescribeResponseGroupMember{}
	r := groupRead{exists: true, members: len(g.Members)}
	for _, m := range g.Members {
		counted := false
		for _, tp := range m.Assignment.TopicPartitions {
			name := tp.Topic
			if name == "" {
				name = names[tp.TopicID]
			}
			if name == "" || (topic != "" && name != topic) || len(tp.Partitions) == 0 {
				continue
			}
			set.Add(name, tp.Partitions...)
			if owners[name] == nil {
				owners[name] = map[int32]kmsg.ConsumerGroupDescribeResponseGroupMember{}
			}
			for _, p := range tp.Partitions {
				owners[name][p] = m
			}
			if !counted {
				counted = true
				r.consumers++
			}
		}
		for _, t := range m.SubscribedTopics {
			if topic == "" || t == topic {
				set.MergeTopics([]string{t})
			}
		}
	}
	commits.Each(func(o kadm.OffsetResponse) {
		if o.Err == nil && (topic == "" || o.Topic == topic) {
			set.Add(o.Topic, o.Partition)
		}
	})
	topics := set.Topics()
	if len(topics) == 0 {
		return r, nil
	}
	ends, err := s.adm.ListEndOffsets(ctx, topics...)
	if err := partialOK(err); err != nil {
		return groupRead{}, s.fail("end offsets", err)
	}
	starts, err := s.adm.ListStartOffsets(ctx, topics...)
	if err := partialOK(err); err != nil {
		return groupRead{}, s.fail("start offsets", err)
	}
	ends.Each(func(o kadm.ListedOffset) {
		if o.Partition >= 0 && set[o.Topic] != nil {
			set.Add(o.Topic, o.Partition)
		}
	})

	set.Each(func(t string, p int32) {
		row := partition{topic: t, partition: p, committed: -1, end: -1, lag: -1}
		if c, ok := commits.Lookup(t, p); ok && c.Err == nil && c.At >= 0 {
			row.committed = c.At
		}
		if e, ok := ends.Lookup(t, p); ok && e.Err == nil && e.Offset >= 0 {
			row.end = e.Offset
		}
		switch {
		case row.end < 0:
		case row.committed >= 0:
			row.lag = max(row.end-row.committed, 0)
		default:
			start := int64(0)
			if st, ok := starts.Lookup(t, p); ok && st.Err == nil && st.Offset >= 0 {
				start = st.Offset
			}
			row.lag = max(row.end-start, 0)
		}
		if m, ok := owners[t][p]; ok {
			row.member, row.client = m.MemberID, m.ClientID
		}
		r.partitions = append(r.partitions, row)
	})
	sort.Slice(r.partitions, func(i, j int) bool {
		if r.partitions[i].topic != r.partitions[j].topic {
			return r.partitions[i].topic < r.partitions[j].topic
		}
		return r.partitions[i].partition < r.partitions[j].partition
	})
	return r, nil
}

// topicNames maps topic ids to names, read from the metadata only when an
// assignment of the group leaves a name out.
func (s *session) topicNames(ctx context.Context, g *kmsg.ConsumerGroupDescribeResponseGroup) (map[[16]byte]string, error) {
	needed := false
	for _, m := range g.Members {
		for _, tp := range m.Assignment.TopicPartitions {
			needed = needed || tp.Topic == ""
		}
	}
	if !needed {
		return nil, nil
	}
	topics, err := s.adm.ListTopics(ctx)
	if err != nil {
		return nil, s.fail("topics", err)
	}
	names := make(map[[16]byte]string, len(topics))
	for _, t := range topics {
		names[t.ID] = t.Topic
	}
	return names, nil
}

// partialOK lets a listing with some failed shards through: the partitions
// it could not list read as unknown.
func partialOK(err error) error {
	var shard *kadm.ShardErrors
	if err == nil || (errors.As(err, &shard) && !shard.AllFailed) {
		return nil
	}
	return err
}
