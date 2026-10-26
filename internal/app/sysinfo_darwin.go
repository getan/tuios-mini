package app

import "github.com/shirou/gopsutil/v4/cpu"

// readCPUTicks reads the machine-wide CPU times through gopsutil, which asks
// the Mach host_statistics call. gopsutil reports seconds; the reading keeps
// microseconds so the integer delta stays fine-grained.
func readCPUTicks() (cpuTicks, bool) {
	times, err := cpu.Times(false)
	if err != nil || len(times) == 0 {
		return cpuTicks{}, false
	}
	t := times[0]
	// Keep the same sum without gopsutil's deprecated internal Total helper.
	total := t.User + t.System + t.Idle + t.Nice + t.Iowait + t.Irq +
		t.Softirq + t.Steal + t.Guest + t.GuestNice
	return cpuTicks{
		idle:  uint64(t.Idle * 1e6),
		total: uint64(total * 1e6),
	}, true
}
