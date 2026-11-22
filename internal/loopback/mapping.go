package loopback

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ReservedFPMPort is owned by PHP-FPM inside the container and must never be
// claimed by the loopback forwarder.
const ReservedFPMPort = 9000

// Entry describes the container bridge target for a forwarded loopback port.
type Entry struct {
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Service string `json:"service"`
}

// PortMap maps a published host port to its container bridge target.
type PortMap = map[int]Entry

// ServicePorts pairs a lerd service name with its published port specs
// (e.g. "3306:3306", "127.0.0.1:3307:3306", "8025:8025/tcp").
type ServicePorts struct {
	Service string
	Specs   []string
}

// BuildPortMap builds a loopback port map from each service's published port
// specs. It skips port 9000 (reserved for PHP-FPM), UDP mappings, and invalid
// or zero ports. Services are sorted by name for deterministic conflict resolution.
func BuildPortMap(services []ServicePorts) PortMap {
	sorted := make([]ServicePorts, len(services))
	copy(sorted, services)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Service < sorted[j].Service
	})

	out := make(PortMap)
	for _, sp := range sorted {
		svc := strings.TrimPrefix(strings.TrimSpace(sp.Service), "lerd-")
		if svc == "" {
			continue
		}
		for _, spec := range sp.Specs {
			hostPort, containerPort, ok := parseTCPPortSpec(spec)
			if !ok {
				continue
			}
			if hostPort == ReservedFPMPort {
				continue
			}
			if _, exists := out[hostPort]; exists {
				continue
			}
			out[hostPort] = Entry{
				Host:    "lerd-" + svc,
				Port:    containerPort,
				Service: svc,
			}
		}
	}
	return out
}

// parseTCPPortSpec parses "port", "host:container", or "ip:host:container"
// with an optional "/tcp" or "/udp" suffix. Non-TCP or out-of-range ports
// return ok=false.
func parseTCPPortSpec(spec string) (hostPort, containerPort int, ok bool) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return 0, 0, false
	}
	body, proto, hasProto := strings.Cut(spec, "/")
	if hasProto && !strings.EqualFold(strings.TrimSpace(proto), "tcp") {
		return 0, 0, false
	}

	parts := strings.Split(body, ":")
	var hostStr, containerStr string
	switch len(parts) {
	case 1:
		hostStr = parts[0]
		containerStr = parts[0]
	case 2:
		hostStr = parts[0]
		containerStr = parts[1]
	case 3:
		hostStr = parts[1]
		containerStr = parts[2]
	default:
		return 0, 0, false
	}

	hp, err := strconv.Atoi(strings.TrimSpace(hostStr))
	if err != nil || hp <= 0 || hp > 65535 {
		return 0, 0, false
	}
	cp, err := strconv.Atoi(strings.TrimSpace(containerStr))
	if err != nil || cp <= 0 || cp > 65535 {
		return 0, 0, false
	}
	return hp, cp, true
}

// WritePortMap atomically writes m to path as formatted JSON using a temp file
// and rename in the same directory.
func WritePortMap(path string, m PortMap) error {
	if m == nil {
		m = make(PortMap)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating port map directory: %w", err)
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling port map: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, ".loopback-ports-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp port map: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing temp port map: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp port map: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp port map: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("renaming port map: %w", err)
	}
	cleanup = false
	return nil
}

// ReadPortMap reads and decodes the JSON port map at path.
func ReadPortMap(path string) (PortMap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m := make(PortMap)
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing port map %s: %w", path, err)
	}
	return m, nil
}
