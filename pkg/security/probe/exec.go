package probe

import (
	eprobe "github.com/DataDog/datadog-agent/pkg/ebpf/probe"
)

// ExecTables - eBPF tables used by open's kProbes
var ExecTables = []KTable{}

// ExecHookPoints - list of open's hooks
var ExecHookPoints = []*HookPoint{
	{
		Name: "sys_execve",
		KProbes: []*eprobe.KProbe{{
			EntryFunc: "kprobe/" + getSyscallFnName("execve"),
		}},
		EventTypes: map[string]Capabilities{
			"*": Capabilities{},
		},
	},
	{
		Name: "sys_execveat",
		KProbes: []*eprobe.KProbe{{
			EntryFunc: "kprobe/" + getSyscallFnName("execveat"),
		}},
		EventTypes: map[string]Capabilities{
			"*": Capabilities{},
		},
		Optional: true,
	},
	{
		Name: "do_fork",
		KProbes: []*eprobe.KProbe{{
			ExitFunc: "kretprobe/_do_fork",
		}, {
			ExitFunc: "kretprobe/do_fork",
		}},
		EventTypes: map[string]Capabilities{
			"*": Capabilities{},
		},
	},
	{
		Name: "do_exit",
		KProbes: []*eprobe.KProbe{{
			ExitFunc: "kprobe/do_exit",
		}},
		EventTypes: map[string]Capabilities{
			"*": Capabilities{},
		},
	},
}