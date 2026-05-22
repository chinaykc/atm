package engine

import (
	"atm/pkg/dsl"
	"context"
	"fmt"
	"sync"
)

type inFlightKey struct {
	index int
	hash  string
}

type asyncTask struct {
	id   int
	key  inFlightKey
	pool string
	done chan struct{}
	err  error
}

type asyncGroup struct {
	mu       sync.Mutex
	nextID   int
	tasks    map[int]*asyncTask
	inFlight map[inFlightKey]struct{}
	pending  map[inFlightKey]struct{}
}

func newAsyncGroup() *asyncGroup {
	return &asyncGroup{
		tasks:    make(map[int]*asyncTask),
		inFlight: make(map[inFlightKey]struct{}),
		pending:  make(map[inFlightKey]struct{}),
	}
}

func (g *asyncGroup) register(key inFlightKey, pool string) *asyncTask {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.nextID++
	task := &asyncTask{id: g.nextID, key: key, pool: pool, done: make(chan struct{})}
	g.tasks[task.id] = task
	g.inFlight[key] = struct{}{}
	g.pending[key] = struct{}{}
	return task
}

func (g *asyncGroup) complete(task *asyncTask, err error) {
	g.mu.Lock()
	task.err = err
	delete(g.inFlight, task.key)
	g.mu.Unlock()
	close(task.done)
}

func (g *asyncGroup) hasPendingKey(key inFlightKey) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, ok := g.pending[key]
	return ok
}

func (g *asyncGroup) hasPending() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.tasks) > 0
}

func (g *asyncGroup) currentMaxID() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.nextID
}

func (g *asyncGroup) waitAll() error {
	return g.waitUpTo(0, "")
}

func (g *asyncGroup) waitUpTo(maxID int, pool string) error {
	g.mu.Lock()
	tasks := make([]*asyncTask, 0, len(g.tasks))
	for _, task := range g.tasks {
		if (maxID == 0 || task.id <= maxID) && (pool == "" || task.pool == pool) {
			tasks = append(tasks, task)
		}
	}
	g.mu.Unlock()

	var firstErr error
	for _, task := range tasks {
		<-task.done
		if task.err != nil && firstErr == nil {
			firstErr = task.err
		}
	}

	g.mu.Lock()
	for _, task := range tasks {
		delete(g.tasks, task.id)
		delete(g.pending, task.key)
	}
	g.mu.Unlock()
	return firstErr
}

type poolManager struct {
	global chan struct{}
	mu     sync.Mutex
	pools  map[string]*workerPool
}

type workerPool struct {
	name   string
	limit  chan struct{}
	queue  chan struct{}
	max    int
	buffer int
}

func newPoolManager(globalLimit int) *poolManager {
	if globalLimit <= 0 {
		globalLimit = 1
	}
	return &poolManager{
		global: make(chan struct{}, globalLimit),
		pools:  make(map[string]*workerPool),
	}
}

func (m *poolManager) declare(decl dsl.PoolDecl) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.pools[decl.Name]; ok {
		if existing.max == decl.Max && existing.buffer == decl.Buffer {
			return nil
		}
		return fmt.Errorf("/pool %s already declared with different limits", decl.Name)
	}
	pool := &workerPool{
		name:   decl.Name,
		limit:  make(chan struct{}, decl.Max),
		max:    decl.Max,
		buffer: decl.Buffer,
	}
	if decl.Buffer >= 0 {
		pool.queue = make(chan struct{}, decl.Max+decl.Buffer)
	}
	m.pools[decl.Name] = pool
	return nil
}

func (m *poolManager) submit(ctx context.Context, name string, fn func()) error {
	pool, err := m.pool(name)
	if err != nil {
		return err
	}
	if pool != nil && pool.queue != nil {
		if err := acquire(ctx, pool.queue); err != nil {
			return err
		}
	}
	go func() {
		queued := pool != nil && pool.queue != nil
		if pool != nil {
			if err := acquire(ctx, pool.limit); err != nil {
				if queued {
					release(pool.queue)
				}
				fn()
				return
			}
		}
		if err := acquire(ctx, m.global); err != nil {
			if pool != nil {
				release(pool.limit)
			}
			if queued {
				release(pool.queue)
			}
			fn()
			return
		}
		defer release(m.global)
		if pool != nil {
			defer release(pool.limit)
		}
		if queued {
			defer release(pool.queue)
		}
		fn()
	}()
	return nil
}

func (m *poolManager) pool(name string) (*workerPool, error) {
	if name == "" {
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	pool, ok := m.pools[name]
	if !ok {
		return nil, fmt.Errorf("/go references undeclared pool %q", name)
	}
	return pool, nil
}

func acquire(ctx context.Context, ch chan struct{}) error {
	select {
	case ch <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func release(ch chan struct{}) {
	select {
	case <-ch:
	default:
	}
}
