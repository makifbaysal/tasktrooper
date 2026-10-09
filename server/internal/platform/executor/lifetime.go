package executor

import "io"

// StdinClosed closes once the parent's end of stdin does: the parent holds it
// open for exactly as long as it wants the executor alive, so a parent that
// dies without a word still takes the executor with it.
func StdinClosed(r io.Reader) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, r)
		close(done)
	}()
	return done
}
