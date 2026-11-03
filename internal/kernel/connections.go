package kernel

import (
	"context"
	"time"

	"gopurr/internal/core"
)

// CachedConnections shares an immutable two-second snapshot between desktop
// and web readers. A canceled reader does not cancel another reader's fetch.
func (r *Remote) CachedConnections(ctx context.Context) ([]core.Conn, error) {
	return r.connections(ctx, 2*time.Second)
}

// Actions needing an exact pre-change snapshot bypass the display cache.
func (r *Remote) ConnectionsWithError() ([]core.Conn, error) {
	return r.connections(context.Background(), 0)
}

func (r *Remote) connections(ctx context.Context, maxAge time.Duration) ([]core.Conn, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		r.connectionsMu.Lock()
		if !r.connectionRowsAt.IsZero() && maxAge > 0 && time.Since(r.connectionRowsAt) < maxAge {
			rows := r.connectionRows
			r.connectionsMu.Unlock()
			return rows, nil
		}
		done := r.connectionRead
		if done == nil {
			done = make(chan struct{})
			r.connectionRead = done
			generation := r.connectionGeneration
			go func() {
				// The ordinary controller timeout still bounds this shared request.
				rows, err := r.readConnections(context.Background())
				r.connectionsMu.Lock()
				if generation == r.connectionGeneration {
					r.connectionResultGeneration = generation
					r.connectionError = err
					if err == nil {
						r.connectionRows, r.connectionRowsAt = rows, time.Now()
					}
				}
				r.connectionRead = nil
				close(done)
				r.connectionsMu.Unlock()
			}()
		}
		generation := r.connectionGeneration
		r.connectionsMu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-done:
		}
		r.connectionsMu.Lock()
		if generation == r.connectionGeneration && r.connectionResultGeneration == generation {
			rows, err := r.connectionRows, r.connectionError
			r.connectionsMu.Unlock()
			return rows, err
		}
		r.connectionsMu.Unlock()
	}
}

func (r *Remote) invalidateConnections() {
	r.connectionsMu.Lock()
	r.connectionRowsAt = time.Time{}
	r.connectionGeneration++
	r.connectionsMu.Unlock()
}

func (k *Kernel) CachedConnections(ctx context.Context) ([]core.Conn, error) {
	return k.r.CachedConnections(ctx)
}

func (k *Kernel) CloseConnectionContext(ctx context.Context, id string) error {
	return k.r.CloseConnectionContext(ctx, id)
}
