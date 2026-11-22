package loopback

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultPollInterval is how often Forwarder checks the port map file for changes.
const DefaultPollInterval = 2 * time.Second

const defaultDialTimeout = 5 * time.Second

type portListener struct {
	ln     net.Listener
	target atomic.Pointer[Entry]
	done   chan struct{}
}

// Forwarder watches a JSON port map file and maintains 127.0.0.1:<port> TCP
// listeners that proxy connections to the container bridge target.
type Forwarder struct {
	mapPath      string
	pollInterval time.Duration
	dialTimeout  time.Duration
	logf         func(format string, args ...any)

	mu        sync.Mutex
	listeners map[int]*portListener
}

// Option configures a Forwarder.
type Option func(*Forwarder)

// WithPollInterval overrides how often the port map file is polled.
func WithPollInterval(d time.Duration) Option {
	return func(f *Forwarder) {
		if d > 0 {
			f.pollInterval = d
		}
	}
}

// WithDialTimeout overrides the upstream TCP dial timeout.
func WithDialTimeout(d time.Duration) Option {
	return func(f *Forwarder) {
		if d > 0 {
			f.dialTimeout = d
		}
	}
}

// WithLogger sets a custom printf-style logger for operational messages.
func WithLogger(fn func(format string, args ...any)) Option {
	return func(f *Forwarder) {
		if fn != nil {
			f.logf = fn
		}
	}
}

// NewForwarder constructs a Forwarder for mapPath.
func NewForwarder(mapPath string, opts ...Option) *Forwarder {
	f := &Forwarder{
		mapPath:      mapPath,
		pollInterval: DefaultPollInterval,
		dialTimeout:  defaultDialTimeout,
		logf:         log.Printf,
		listeners:    make(map[int]*portListener),
	}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

// Run watches mapPath and forwards loopback ports until ctx is cancelled,
// closing all listeners cleanly on shutdown.
func Run(ctx context.Context, mapPath string) error {
	return NewForwarder(mapPath).Run(ctx)
}

// Run starts the reconciliation loop and blocks until ctx is cancelled.
func (f *Forwarder) Run(ctx context.Context) error {
	f.reconcile(ctx)

	ticker := time.NewTicker(f.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			f.closeAll()
			return nil
		case <-ticker.C:
			f.reconcile(ctx)
		}
	}
}

func (f *Forwarder) reconcile(ctx context.Context) {
	desired, err := ReadPortMap(f.mapPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			desired = PortMap{}
		} else {
			f.logf("lerd-loopback: reading %s: %v", f.mapPath, err)
			return
		}
	}

	var toWait []chan struct{}

	f.mu.Lock()
	for port, pl := range f.listeners {
		if _, ok := desired[port]; !ok {
			delete(f.listeners, port)
			_ = pl.ln.Close()
			toWait = append(toWait, pl.done)
		}
	}

	for port, entry := range desired {
		if port <= 0 || port > 65535 || port == ReservedFPMPort {
			continue
		}
		if entry.Host == "" || entry.Port <= 0 || entry.Port > 65535 {
			continue
		}
		if pl, exists := f.listeners[port]; exists {
			cur := pl.target.Load()
			if cur == nil || *cur != entry {
				e := entry
				pl.target.Store(&e)
			}
			continue
		}

		ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			f.logf("lerd-loopback: listen 127.0.0.1:%d (%s): %v", port, entry.Service, err)
			continue
		}

		pl := &portListener{
			ln:   ln,
			done: make(chan struct{}),
		}
		e := entry
		pl.target.Store(&e)
		f.listeners[port] = pl
		go f.acceptLoop(ctx, pl)
	}
	f.mu.Unlock()

	for _, ch := range toWait {
		<-ch
	}
}

func (f *Forwarder) acceptLoop(ctx context.Context, pl *portListener) {
	defer close(pl.done)
	for {
		conn, err := pl.ln.Accept()
		if err != nil {
			return
		}
		target := pl.target.Load()
		if target == nil {
			_ = conn.Close()
			continue
		}
		go f.proxyConn(ctx, conn, *target)
	}
}

func (f *Forwarder) proxyConn(ctx context.Context, client net.Conn, target Entry) {
	defer client.Close()

	dialer := net.Dialer{Timeout: f.dialTimeout}
	addr := net.JoinHostPort(target.Host, strconv.Itoa(target.Port))
	upstream, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		f.logf("lerd-loopback: dial %s (%s): %v", addr, target.Service, err)
		return
	}
	defer upstream.Close()

	done := make(chan struct{}, 2)
	pipe := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		if tc, ok := dst.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
		done <- struct{}{}
	}

	go pipe(upstream, client)
	go pipe(client, upstream)

	<-done
	<-done
}

func (f *Forwarder) closeAll() {
	f.mu.Lock()
	toWait := make([]chan struct{}, 0, len(f.listeners))
	for port, pl := range f.listeners {
		delete(f.listeners, port)
		_ = pl.ln.Close()
		toWait = append(toWait, pl.done)
	}
	f.mu.Unlock()

	for _, ch := range toWait {
		<-ch
	}
}

// ActivePorts returns the set of host ports currently bound by the forwarder.
func (f *Forwarder) ActivePorts() map[int]Entry {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[int]Entry, len(f.listeners))
	for port, pl := range f.listeners {
		if e := pl.target.Load(); e != nil {
			out[port] = *e
		}
	}
	return out
}

// FormatAddr formats a host:port pair for display.
func FormatAddr(host string, port int) string {
	return fmt.Sprintf("%s:%d", host, port)
}
