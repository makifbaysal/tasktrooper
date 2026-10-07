package agent

import (
	"context"
	"strings"
)

type segmentBreakKey struct{}

func WithSegmentBreak(ctx context.Context, fn func()) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, segmentBreakKey{}, fn)
}

func segmentBreak(ctx context.Context) {
	if fn, ok := ctx.Value(segmentBreakKey{}).(func()); ok && fn != nil {
		fn()
	}
}

func SegmentBreak(ctx context.Context) { segmentBreak(ctx) }

// withoutShownPrefix streams a retried turn without the opening the user
// already watched arrive in the cut-off attempt: while the retry repeats that
// text nothing is forwarded, and once it runs past it only the new tail is.
// A retry that departs from it is streamed whole after a segment break, so
// the user reads it in full rather than a splice of two answers. It holds no
// text, only how far into shown the retry has got.
func withoutShownPrefix(ctx context.Context, shown string, onToken func(string)) func(string) {
	if shown == "" || onToken == nil {
		return onToken
	}
	pos := 0
	return func(tok string) {
		if pos < 0 {
			onToken(tok)
			return
		}
		rest := shown[pos:]
		switch {
		case strings.HasPrefix(rest, tok):
			pos += len(tok)
		case strings.HasPrefix(tok, rest):
			pos = -1
			if tail := tok[len(rest):]; tail != "" {
				onToken(tail)
			}
		default:
			repeated := shown[:pos]
			pos = -1
			segmentBreak(ctx)
			onToken(repeated + tok)
		}
	}
}
