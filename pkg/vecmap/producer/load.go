package producer

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"
)

type job struct {
	key    Key
	ctx    context.Context
	cancel context.CancelFunc
}

type result struct {
	job      job
	data     []byte
	err      error
	retry    bool
	duration time.Duration
}

// Allocate once per job, never grow. Returning the result transfers its buffer;
// the owner retains the job slot until consuming that result, even on cancellation.
type responseWriter struct {
	data  []byte
	limit int
	err   error
}

func (w *responseWriter) Write(data []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	if len(data) > w.limit-len(w.data) {
		w.err = ErrLimit
		return 0, w.err
	}
	w.data = append(w.data, data...)
	return len(data), nil
}

func (p *Producer) load(wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case <-p.ctx.Done():
			return
		case j := <-p.jobs:
			started := time.Now()
			w := responseWriter{data: make([]byte, 0, p.limits.RawBytes), limit: p.limits.RawBytes}
			err := p.loader(j.ctx, j.key, &w)
			if w.err != nil {
				err = w.err
			} // a loader cannot swallow an overflow
			if err == nil {
				err = j.ctx.Err()
			}
			r := result{job: j, data: w.data, retry: retryable(err), duration: time.Since(started)}
			if err != nil {
				r.err = errors.New(errorMessage(err))
			}
			// Queued results retain only bounded error text, never an arbitrary
			// loader error object that could own another response or large buffer.
			select {
			case p.results <- r:
			case <-p.ctx.Done():
				return
			}
		}
	}
}

func retryable(err error) bool {
	return !errors.Is(err, ErrMissing) && !errors.Is(err, ErrPermanent) && !errors.Is(err, ErrLimit) &&
		!errors.Is(err, context.Canceled)
}

var _ io.Writer = (*responseWriter)(nil)
