package system

import (
	"fmt"
	"syscall"
	"unsafe"
)

var jobDLL = syscall.NewLazyDLL("kernel32.dll")
var createJob = jobDLL.NewProc("CreateJobObjectW")
var setJob = jobDLL.NewProc("SetInformationJobObject")
var terminateJob = jobDLL.NewProc("TerminateJobObject")
var assignJob = jobDLL.NewProc("AssignProcessToJobObject")
var snapshotThreads = jobDLL.NewProc("CreateToolhelp32Snapshot")
var firstThread = jobDLL.NewProc("Thread32First")
var nextThread = jobDLL.NewProc("Thread32Next")
var openNativeThread = jobDLL.NewProc("OpenThread")
var resumeNativeThread = jobDLL.NewProc("ResumeThread")

type nativeThreadEntry struct {
	Size, Usage, ID, Owner uint32
	Base, Delta            int32
	Flags                  uint32
}

func resumeNativeProcess(pid int) error {
	snapshot, _, e := snapshotThreads.Call(4, 0)
	if snapshot == ^uintptr(0) {
		return fmt.Errorf("native thread snapshot: %w", e)
	}
	defer syscall.CloseHandle(syscall.Handle(snapshot))
	entry := nativeThreadEntry{}
	entry.Size = uint32(unsafe.Sizeof(entry))
	ok, _, _ := firstThread.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	for ok != 0 {
		if entry.Owner == uint32(pid) {
			thread, _, e := openNativeThread.Call(2, 0, uintptr(entry.ID))
			if thread == 0 {
				return e
			}
			previous, _, e := resumeNativeThread.Call(thread)
			syscall.CloseHandle(syscall.Handle(thread))
			if uint32(previous) == 0xffffffff {
				return e
			}
			return nil
		}
		ok, _, _ = nextThread.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	}
	return fmt.Errorf("suspended native process has no primary thread")
}

type jobBasic struct {
	ProcessTime, JobTime   int64
	Flags                  uint32
	MinWorking, MaxWorking uintptr
	ActiveProcesses        uint32
	Affinity               uintptr
	Priority, Scheduling   uint32
}
type jobIO struct{ ReadOps, WriteOps, OtherOps, ReadBytes, WriteBytes, OtherBytes uint64 }
type jobExtended struct {
	Basic                                                      jobBasic
	IO                                                         jobIO
	ProcessMemory, JobMemory, PeakProcessMemory, PeakJobMemory uintptr
}
type windowsJob struct{ handle syscall.Handle }

func newWindowsJob() (*windowsJob, error) {
	h, _, e := createJob.Call(0, 0)
	if h == 0 {
		return nil, fmt.Errorf("create native process job: %w", e)
	}
	j := &windowsJob{syscall.Handle(h)}
	limits := jobExtended{Basic: jobBasic{Flags: 0x2000 | 0x200 | 0x8, ActiveProcesses: 256}, JobMemory: 2 << 30}
	ok, _, e := setJob.Call(h, 9, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits))
	if ok == 0 {
		j.Close()
		return nil, fmt.Errorf("set native memory/process limits: %w", e)
	}
	cpu := struct{ Flags, Rate uint32 }{Flags: 1 | 4, Rate: 2500}
	ok, _, e = setJob.Call(h, 15, uintptr(unsafe.Pointer(&cpu)), unsafe.Sizeof(cpu))
	if ok == 0 {
		j.Close()
		return nil, fmt.Errorf("set native CPU limit: %w", e)
	}
	return j, nil
}
func (j *windowsJob) Assign(pid int) error {
	p, e := syscall.OpenProcess(0x0101, false, uint32(pid))
	if e != nil {
		return e
	}
	defer syscall.CloseHandle(p)
	ok, _, e := assignJob.Call(uintptr(j.handle), uintptr(p))
	if ok == 0 {
		return fmt.Errorf("assign native process job: %w", e)
	}
	return nil
}
func (j *windowsJob) Close() {
	if j.handle != 0 {
		syscall.CloseHandle(j.handle)
		j.handle = 0
	}
}

func (j *windowsJob) Terminate() error {
	ok, _, err := terminateJob.Call(uintptr(j.handle), 1)
	if ok == 0 {
		return fmt.Errorf("terminate native process job: %w", err)
	}
	return nil
}
