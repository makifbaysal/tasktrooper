package indexer

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

func TestCleanPassCommit(t *testing.T) {
	paths := []string{"a.go", "dir/b.go"}
	clean := &gitTreeState{head: "h1"}

	tests := []struct {
		name  string
		paths []string
		start *gitTreeState
		end   *gitTreeState
		want  string
	}{
		{name: "clean at start and end", paths: paths, start: clean, end: clean, want: "h1"},
		{name: "indexable file dirty at start", paths: paths, start: &gitTreeState{head: "h1", dirty: []string{"dir/b.go"}}, end: clean, want: ""},
		{name: "indexable file dirty at end", paths: paths, start: clean, end: &gitTreeState{head: "h1", dirty: []string{"a.go"}}, want: ""},
		{name: "only non-indexable files dirty", paths: paths, start: &gitTreeState{head: "h1", dirty: []string{"README.md"}}, end: &gitTreeState{head: "h1", dirty: []string{"notes.txt"}}, want: "h1"},
		{name: "HEAD moved during the pass", paths: paths, start: clean, end: &gitTreeState{head: "h2"}, want: ""},
		{name: "no git snapshot at start", paths: paths, start: nil, end: clean, want: ""},
		{name: "no git snapshot at end", paths: paths, start: clean, end: nil, want: ""},
		{name: "no indexable files", paths: nil, start: clean, end: clean, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, cleanPassCommit(tt.paths, tt.start, tt.end))
		})
	}
}

type CleanPassLedgerSuite struct {
	suite.Suite
	ledger  *cleanPassLedger
	indexID uuid.UUID
}

func (s *CleanPassLedgerSuite) SetupTest() {
	s.ledger = &cleanPassLedger{}
	s.indexID = uuid.New()
}

func (s *CleanPassLedgerSuite) recordClean(commit string) {
	s.ledger.end(s.ledger.begin(s.indexID), commit)
}

func (s *CleanPassLedgerSuite) TestCleanPassIsTrustedByTheNextPassOnly() {
	first := s.ledger.begin(s.indexID)
	s.Empty(first.trusted)
	s.ledger.end(first, "h1")

	s.Empty(s.ledger.begin(uuid.New()).trusted, "another index trusts nothing")
	second := s.ledger.begin(s.indexID)
	s.Equal("h1", second.trusted)
}

func (s *CleanPassLedgerSuite) TestFailedPassLeavesNothingToTrust() {
	s.recordClean("h1")

	s.ledger.end(s.ledger.begin(s.indexID), "")

	s.Empty(s.ledger.begin(s.indexID).trusted)
}

func (s *CleanPassLedgerSuite) TestOverlappingPassesNeitherTrustNorRecord() {
	s.recordClean("h1")

	older := s.ledger.begin(s.indexID)
	newer := s.ledger.begin(s.indexID)
	s.Equal("h1", older.trusted)
	s.Empty(newer.trusted, "a pass that starts while another runs trusts nothing")

	s.ledger.end(older, "h1")
	s.ledger.end(newer, "h1")

	s.Empty(s.ledger.begin(s.indexID).trusted)
}

func TestCleanPassLedgerSuite(t *testing.T) {
	suite.Run(t, new(CleanPassLedgerSuite))
}
