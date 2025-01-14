// Copyright 2017-Present Couchbase, Inc.
//
// Use of this software is governed by the Business Source License included
// in the file licenses/BSL-Couchbase.txt.  As of the Change Date specified
// in that file, in accordance with the Business Source License, use of this
// software will be governed by the Apache License, Version 2.0, included in
// the file licenses/APL2.txt.

package system

//#cgo LDFLAGS: -lsigar
//#include <sigar.h>
//#include <sigar_control_group.h>
import "C"

import (
	"errors"
	"fmt"

	"github.com/couchbase/indexing/secondary/common"
	"github.com/couchbase/indexing/secondary/logging"
)

type SystemStats struct {
	handle *C.sigar_t
	pid    C.sigar_pid_t
}

//
// Open a new handle
//
func NewSystemStats() (*SystemStats, error) {

	var handle *C.sigar_t

	if err := C.sigar_open(&handle); err != C.SIGAR_OK {
		return nil, errors.New(fmt.Sprintf("Fail to open sigar.  Error code = %v", err))
	}

	s := &SystemStats{}
	s.handle = handle
	s.pid = C.sigar_pid_get(handle)

	return s, nil
}

//
// Close handle
//
func (s *SystemStats) Close() {
	C.sigar_close(s.handle)
}

// ProcessCpuPercent gets the percent CPU this Go runtime has consumed recently. This is in range
// [0,GOMAXPROCS]*100%, so a value of 123.4 means it is consuming 1.234 CPU cores. This behavior
// was confirmed by empirical experiments running different numbers of CPU-bound spinners in Indexer
// and externally (not part of Couchbase). E.g. in an otherwise idle cluster if each of three
// Indexers runs two CPU spinners in a dev environment with all on the same laptop, each one reports
// ~200% CPU usage even though there is at least 600% CPU being consumed on the entire machine. At
// the same time Projector reports ~0% CPU usage. Neither of these is affected by the number of
// external spinners that are also consuming CPU.
func (s *SystemStats) ProcessCpuPercent() (C.sigar_pid_t, float64, error) {
	// Sigar returns a ratio of (system_time + user_time) / elapsed time
	var cpu C.sigar_proc_cpu_t
	if err := C.sigar_proc_cpu_get(s.handle, s.pid, &cpu); err != C.SIGAR_OK {
		return C.sigar_pid_t(0), float64(0), errors.New(fmt.Sprintf("Fail to get CPU.  Err=%v", C.sigar_strerror(s.handle, err)))
	}
	// Despite its name, cpu.percent is not a percent. It is in range [0, GOMAXPROCS] so needs * 100
	// to convert it to a percent. It is a double in sigar (C++ equivalent of Go float64).
	return s.pid, float64(cpu.percent) * 100, nil
}

// ProcessRSS gets the size in bytes of the memory-resident portion of this Go runtime.
func (s *SystemStats) ProcessRSS() (C.sigar_pid_t, uint64, error) {
	var mem C.sigar_proc_mem_t
	if err := C.sigar_proc_mem_get(s.handle, s.pid, &mem); err != C.SIGAR_OK {
		return C.sigar_pid_t(0), uint64(0), errors.New(fmt.Sprintf("Fail to get RSS.  Err=%v", C.sigar_strerror(s.handle, err)))
	}
	return s.pid, uint64(mem.resident), nil
}

// SystemFreeMem gets the current free memory in bytes i.e bare node's
// free memory EXCLUDING inactive OS kernel pages.
// (Sister method SystemStats.ActualFreeMem includes inactive OS kernel pages in bare node case.)
func (s *SystemStats) SystemFreeMem() (uint64, error) {
	// Return the node-level free memory EXCLUDING inactive OS kernel pages
	var mem C.sigar_mem_t
	if err := C.sigar_mem_get(s.handle, &mem); err != C.SIGAR_OK {
		return uint64(0), errors.New(fmt.Sprintf("Fail to get free memory.  Err=%v", C.sigar_strerror(s.handle, err)))
	}
	return uint64(mem.free), nil
}

// SystemActualFreeMem gets the current free memory in bytes i.e. bare node's free
// memory INCLUDING inactive OS kernel pages.
// (Sister method SystemStats.FreeMem excludes inactive OS kernel pages in bare node case.)
func (s *SystemStats) SystemActualFreeMem() (uint64, error) {
	//Return the node-level free memory INCLUDING inactive OS kernel pages
	var mem C.sigar_mem_t
	if err := C.sigar_mem_get(s.handle, &mem); err != C.SIGAR_OK {
		return uint64(0), errors.New(fmt.Sprintf("Fail to get free memory.  Err=%v", C.sigar_strerror(s.handle, err)))
	}
	return uint64(mem.actual_free), nil
}

// SystemTotalMem gets the total memory in bytes available to this Go runtime
// on the bare node's total memory.
func (s *SystemStats) SystemTotalMem() (uint64, error) {
	// return the node-level memory limit
	var mem C.sigar_mem_t
	if err := C.sigar_mem_get(s.handle, &mem); err != C.SIGAR_OK {
		return uint64(0), errors.New(fmt.Sprintf("Fail to get total memory.  Err=%v", C.sigar_strerror(s.handle, err)))
	}
	return uint64(mem.total), nil
}

// actual = true means include inactive OS Kernel pages in free memory computation
// actual = false means exclude inactive OS Kernel pages in free memory computation
// Return Values: (TotalMem, FreeMem, cGroupValues, error)
// cGroupValues => true if the limits of the container are returned
//              => false if the system limits are returned
func (s *SystemStats) GetTotalAndFreeMem(actual bool) (uint64, uint64, bool, error) {
	var sysTotal, sysFree uint64
	var cGroupTotal uint64
	var err error

	sysTotal, err = s.SystemTotalMem()
	if err != nil {
		logging.Debugf("SystemStats::GetTotalAndFreeMem Failed to get total memory, err: %v", err)
		return 0, 0, false, err
	}

	cgroupInfo := s.GetControlGroupInfo()
	if cgroupInfo.Supported == common.SIGAR_CGROUP_SUPPORTED {
		cGroupTotal = cgroupInfo.MemoryMax
		cGroupCurr := cgroupInfo.MemoryCurrent
		// cGroupTotal is with-in valid system limits
		if cGroupTotal > 0 && cGroupTotal <= sysTotal {
			return cGroupTotal, cGroupTotal - cGroupCurr, true, nil
		}
	}
	if actual {
		sysFree, err = s.SystemActualFreeMem()
		if err != nil {
			logging.Debugf("SystemStats::GetTotalAndFreeMem Failed to actual free memory, err: %v", err)
			return 0, 0, false, err
		}
	} else {
		sysFree, err = s.SystemFreeMem()
		if err != nil {
			logging.Debugf("SystemStats::GetTotalAndFreeMem Failed to free memory, err: %v", err)
			return 0, 0, false, err
		}
	}
	return sysTotal, sysFree, false, nil
}

// SigarCpuT type Go-wraps the sigar C library sigar_cpu_t type. CPU in use should sum Sys + User +
// Nice + Irq + SoftIrq. (This is different from the sigar_cpu_perc_calculate function's
// perc.combined calculation, whose semantics are unclear.)
type SigarCpuT struct {
	Sys     uint64 // CPU executing system thread
	User    uint64 // CPU executing user thread at normal priority [-20, 0]
	Nice    uint64 // CPU executing user thread at low priority [1, 20]
	Irq     uint64 // CPU executing "top half" (uninterruptible) part of a HW interrupt handler
	SoftIrq uint64 // CPU executing "bottom half" (long, slow) part of a HW interrupt handler
	Wait    uint64 // CPU not executing anything but an IO is outstanding (a type of idle time)
	Idle    uint64 // CPU not executing anything and no IO is outstanding
	Stolen  uint64 // CPU time given to other virtual machines in a VM or cloud environment

	Total uint64 // total elapsed time
}

// SigarCpuGet Go-wraps the sigar C library sigar_cpu_get function.
func (h *SystemStats) SigarCpuGet() (*SigarCpuT, error) {
	var cpu C.sigar_cpu_t
	if err := C.sigar_cpu_get(h.handle, &cpu); err != C.SIGAR_OK {
		return nil, errors.New(fmt.Sprintf("sigar_cpu_get failed.  Err=%v", C.sigar_strerror(h.handle, err)))
	}
	return &SigarCpuT{
		Sys:     uint64(cpu.sys),
		User:    uint64(cpu.user),
		Nice:    uint64(cpu.nice),
		Irq:     uint64(cpu.irq),
		SoftIrq: uint64(cpu.soft_irq),
		Wait:    uint64(cpu.wait),
		Idle:    uint64(cpu.idle),
		Stolen:  uint64(cpu.stolen),

		Total: uint64(cpu.total),
	}, nil
}

// SigarControlGroupInfo holds just the subset of C.sigar_control_group_info_t GSI uses. There are
// many more fields available at time of writing.
type SigarControlGroupInfo struct {
	Supported uint8 // "1" if cgroup info is supprted, "0" otherwise
	Version   uint8 // "1" for cgroup v1, "2" for cgroup v2

	// The number of CPUs available in the cgroup (in % where 100% represents 1 full core)
	// Derived from (cpu.cfs_quota_us/cpu.cfs_period_us) or COUCHBASE_CPU_COUNT env variable
	NumCpuPrc uint16

	// Maximum memory available in the group. Derived from memory.max
	MemoryMax uint64

	// Current memory usage by this cgroup. Derived from memory.usage_in_bytes
	MemoryCurrent uint64

	// UsageUsec gives the total microseconds of CPU used from sigar start across all available
	// cores, so this can increase at a rate of N times real time if there are N cores in use
	UsageUsec uint64
}

// GetControlGroupInfo returns the fields of C.sigar_control_group_info_t GSI uses. These reflect
// Linux control group settings, which are used by Kubernetes to set pod memory and CPU limits.
func (h *SystemStats) GetControlGroupInfo() *SigarControlGroupInfo {
	var info C.sigar_control_group_info_t
	C.sigar_get_control_group_info(&info)

	return &SigarControlGroupInfo{
		Supported:     uint8(info.supported),
		Version:       uint8(info.version),
		NumCpuPrc:     uint16(info.num_cpu_prc),
		MemoryMax:     uint64(info.memory_max),
		MemoryCurrent: uint64(info.memory_current),
		UsageUsec:     uint64(info.usage_usec),
	}
}
