package serviceops

import (
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/loopback"
)

// SyncLoopbackPortMap writes the current published service port mappings to
// loopback-ports.json so lerd-loopback inside PHP containers stays in sync.
func SyncLoopbackPortMap() error {
	cfg, err := config.LoadGlobal()
	if err != nil || cfg == nil {
		return err
	}
	seen := make(map[string]struct{})
	var svcs []loopback.ServicePorts
	for name := range cfg.Services {
		seen[name] = struct{}{}
		if specs := HostPortMappings(name); len(specs) > 0 {
			svcs = append(svcs, loopback.ServicePorts{Service: name, Specs: specs})
		}
	}
	if custom, err := config.ListCustomServices(); err == nil {
		for _, svc := range custom {
			if _, ok := seen[svc.Name]; ok {
				continue
			}
			if specs := HostPortMappings(svc.Name); len(specs) > 0 {
				svcs = append(svcs, loopback.ServicePorts{Service: svc.Name, Specs: specs})
			}
		}
	}
	return loopback.WritePortMap(config.LoopbackPortsFile(), loopback.BuildPortMap(svcs))
}
