package convert

import (
	"context"
	"io"
)

// Pool runs conversions on independent converters.
//
// A Converter owns a single PDFium instance, which is not safe to share
// between goroutines. A server that wants real concurrency needs several
// instances, not a bigger semaphore around one.
type Pool struct {
	free chan *Converter
	all  []*Converter
}

// NewPool starts n converters. n below one is treated as one.
func NewPool(n int, opts Options) (*Pool, error) {
	if n < 1 {
		n = 1
	}

	p := &Pool{free: make(chan *Converter, n)}
	for i := 0; i < n; i++ {
		c, err := New(opts)
		if err != nil {
			p.Close()
			return nil, err
		}
		p.all = append(p.all, c)
		p.free <- c
	}
	return p, nil
}

// Run waits for a free converter and converts on it.
func (p *Pool) Run(ctx context.Context, src io.ReaderAt, size int64, doc DocOptions) (Result, error) {
	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	case c := <-p.free:
		defer func() { p.free <- c }()
		return c.Run(ctx, src, size, doc)
	}
}

// Size is the number of converters in the pool.
func (p *Pool) Size() int { return len(p.all) }

// Close releases every converter. It must not run while conversions are in
// flight.
func (p *Pool) Close() error {
	var first error
	for _, c := range p.all {
		if err := c.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}
