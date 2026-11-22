package loopback

import (
	"context"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func TestBuildPortMap(t *testing.T) {
	got := BuildPortMap([]ServicePorts{
		{
			Service: "mysql",
			Specs:   []string{"3307:3306"},
		},
		{
			Service: "redis",
			Specs:   []string{"127.0.0.1:6379:6379/tcp"},
		},
		{
			Service: "rustfs",
			Specs:   []string{"9000:9000", "9001:9001"},
		},
		{
			Service: "mailpit",
			Specs:   []string{"1025:1025", "8025:8025", "5353:5353/udp", "0:1234", "bad", ":80"},
		},
		{
			Service: "",
			Specs:   []string{"4444:4444"},
		},
	})

	if _, has9000 := got[ReservedFPMPort]; has9000 {
		t.Fatalf("port %d must be skipped, got %+v", ReservedFPMPort, got[ReservedFPMPort])
	}

	want := PortMap{
		3307: {Host: "lerd-mysql", Port: 3306, Service: "mysql"},
		6379: {Host: "lerd-redis", Port: 6379, Service: "redis"},
		9001: {Host: "lerd-rustfs", Port: 9001, Service: "rustfs"},
		1025: {Host: "lerd-mailpit", Port: 1025, Service: "mailpit"},
		8025: {Host: "lerd-mailpit", Port: 8025, Service: "mailpit"},
	}

	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d: %+v", len(got), len(want), got)
	}
	for port, we := range want {
		ge, ok := got[port]
		if !ok {
			t.Errorf("missing port %d", port)
			continue
		}
		if ge != we {
			t.Errorf("port %d = %+v, want %+v", port, ge, we)
		}
	}
}

func TestWriteReadPortMapRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run", "loopback-ports.json")
	want := PortMap{
		3307: {Host: "lerd-mysql", Port: 3306, Service: "mysql"},
		6379: {Host: "lerd-redis", Port: 6379, Service: "redis"},
	}

	if err := WritePortMap(path, want); err != nil {
		t.Fatalf("WritePortMap: %v", err)
	}
	got, err := ReadPortMap(path)
	if err != nil {
		t.Fatalf("ReadPortMap: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(want))
	}
	for port, we := range want {
		if got[port] != we {
			t.Errorf("port %d = %+v, want %+v", port, got[port], we)
		}
	}
}

func startEchoServer(t *testing.T) (port int, closeFn func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen echo server: %v", err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				_, _ = io.Copy(conn, conn)
			}(c)
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, func() { _ = ln.Close() }
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

func assertEcho(t *testing.T, port int, payload string) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", FormatAddr("127.0.0.1", port), 2*time.Second)
	if err != nil {
		t.Fatalf("dial 127.0.0.1:%d: %v", port, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

	if _, err := conn.Write([]byte(payload)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.CloseWrite()
	}
	got, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != payload {
		t.Fatalf("echo = %q, want %q", string(got), payload)
	}
}

func waitUntil(t *testing.T, timeout time.Duration, cond func() bool, desc string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", desc)
}

func TestForwarderEndToEndAndHotReload(t *testing.T) {
	echoPort1, closeEcho1 := startEchoServer(t)
	defer closeEcho1()
	echoPort2, closeEcho2 := startEchoServer(t)
	defer closeEcho2()

	fwdPort1 := freeTCPPort(t)
	fwdPort2 := freeTCPPort(t)
	if fwdPort2 == fwdPort1 {
		fwdPort2 = freeTCPPort(t)
	}

	mapPath := filepath.Join(t.TempDir(), "loopback-ports.json")
	if err := WritePortMap(mapPath, PortMap{
		fwdPort1: {Host: "127.0.0.1", Port: echoPort1, Service: "echo1"},
	}); err != nil {
		t.Fatalf("initial WritePortMap: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fwd := NewForwarder(
		mapPath,
		WithPollInterval(25*time.Millisecond),
		WithLogger(func(format string, args ...any) { t.Logf(format, args...) }),
	)
	runDone := make(chan error, 1)
	go func() {
		runDone <- fwd.Run(ctx)
	}()

	waitUntil(t, 2*time.Second, func() bool {
		_, ok := fwd.ActivePorts()[fwdPort1]
		return ok
	}, "initial listener on fwdPort1")

	assertEcho(t, fwdPort1, "hello-loopback-1")

	// Remove fwdPort1 and add fwdPort2 via hot-reload.
	if err := WritePortMap(mapPath, PortMap{
		fwdPort2: {Host: "127.0.0.1", Port: echoPort2, Service: "echo2"},
	}); err != nil {
		t.Fatalf("updated WritePortMap: %v", err)
	}

	waitUntil(t, 2*time.Second, func() bool {
		active := fwd.ActivePorts()
		_, has1 := active[fwdPort1]
		_, has2 := active[fwdPort2]
		return !has1 && has2
	}, "hot-reload swap from fwdPort1 to fwdPort2")

	// Removed port must reject connections.
	if conn, err := net.DialTimeout("tcp", FormatAddr("127.0.0.1", fwdPort1), 200*time.Millisecond); err == nil {
		_ = conn.Close()
		t.Fatalf("expected fwdPort1 (%d) to be closed after removal", fwdPort1)
	}

	// Newly added port must proxy traffic.
	assertEcho(t, fwdPort2, "hello-loopback-2")

	// Retarget fwdPort2 in-place to a different upstream server.
	if err := WritePortMap(mapPath, PortMap{
		fwdPort2: {Host: "127.0.0.1", Port: echoPort1, Service: "echo1-retargeted"},
	}); err != nil {
		t.Fatalf("retarget WritePortMap: %v", err)
	}
	waitUntil(t, 2*time.Second, func() bool {
		e, ok := fwd.ActivePorts()[fwdPort2]
		return ok && e.Port == echoPort1 && e.Service == "echo1-retargeted"
	}, "in-place target update on fwdPort2")
	assertEcho(t, fwdPort2, "hello-retargeted")

	// Cancel context and verify clean shutdown closes remaining listeners.
	cancel()
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Forwarder.Run to stop")
	}

	if conn, err := net.DialTimeout("tcp", FormatAddr("127.0.0.1", fwdPort2), 200*time.Millisecond); err == nil {
		_ = conn.Close()
		t.Fatalf("expected fwdPort2 (%d) to be closed after shutdown", fwdPort2)
	}
}
