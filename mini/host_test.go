package mini

import (
	"strings"
	"testing"
)

func TestDetectHost(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want Host
	}{
		{"plain", nil, HostPlain},
		{"tuios env", map[string]string{"TUIOS_ENV": "1"}, HostTuios},
		{"tuios pane", map[string]string{"TUIOS_PANE_ID": "w1"}, HostTuios},
		{"golder workspace wins", map[string]string{"TUIOS_ENV": "1", "GOLDER_WORKSPACE": "1"}, HostGolderWorkspace},
		{"tmux", map[string]string{"TMUX": "/tmp/tmux"}, HostTmux},
		{"zellij", map[string]string{"ZELLIJ": "0"}, HostZellij},
		{"screen", map[string]string{"STY": "123.pts"}, HostScreen},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectHost(func(key string) string { return tc.env[key] })
			if got != tc.want {
				t.Fatalf("DetectHost = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNestedHosts(t *testing.T) {
	for host, want := range map[Host]bool{
		HostPlain: false, HostGolderWorkspace: false,
		HostTuios: true, HostTmux: true, HostZellij: true, HostScreen: true,
	} {
		if got := host.Nested(); got != want {
			t.Errorf("%s.Nested() = %v, want %v", host, got, want)
		}
	}
}

func TestDefaultNamespaceIsNotTuios(t *testing.T) {
	ns := DefaultNamespace()
	if ns.ConfigDir == "" || ns.StateDir == "" || ns.SocketPath == "" || ns.LogFile == "" {
		t.Fatalf("namespace has an empty path: %+v", ns)
	}
	for _, path := range []string{ns.ConfigDir, ns.StateDir, ns.RuntimeDir, ns.SocketPath, ns.LogFile} {
		if strings.Contains(strings.ToLower(path), "tuios") {
			t.Errorf("workspace path %q belongs to tuios", path)
		}
	}
}
