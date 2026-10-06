package system

// WFP ALE filters cover loopback as well as LAN/Internet. Windows Firewall's
// ordinary offline-user rules do not consistently cover loopback on this host.
import (
	"bytes"
	"fmt"
	"syscall"
	"unsafe"
)

type wfpGUID struct {
	A    uint32
	B, C uint16
	D    [8]byte
}
type wfpBlob struct {
	Size uint32
	Data unsafe.Pointer
}
type wfpValue struct {
	Type uint32
	Data unsafe.Pointer
}
type wfpCondition struct {
	Field wfpGUID
	Match uint32
	Value wfpValue
}
type wfpAction struct {
	Type uint32
	Key  wfpGUID
}
type wfpFilter struct {
	Key               wfpGUID
	Name, Description uintptr
	Flags             uint32
	Provider          uintptr
	ProviderData      wfpBlob
	Layer, SubLayer   wfpGUID
	Weight            wfpValue
	Count             uint32
	Conditions        unsafe.Pointer
	Action            wfpAction
	Context           [2]uint64
	Reserved          uintptr
	ID                uint64
	EffectiveWeight   wfpValue
}

var fwpu = syscall.NewLazyDLL("fwpuclnt.dll")
var wfpOpen = fwpu.NewProc("FwpmEngineOpen0")
var wfpClose = fwpu.NewProc("FwpmEngineClose0")
var wfpGet = fwpu.NewProc("FwpmFilterGetByKey0")
var wfpAdd = fwpu.NewProc("FwpmFilterAdd0")
var wfpFree = fwpu.NewProc("FwpmFreeMemory0")
var sddlConvert = syscall.NewLazyDLL("advapi32.dll").NewProc("ConvertStringSecurityDescriptorToSecurityDescriptorW")
var sdLength = syscall.NewLazyDLL("advapi32.dll").NewProc("GetSecurityDescriptorLength")
var localFree = jobDLL.NewProc("LocalFree")
var wfpUser = wfpGUID{0xaf043a0a, 0xb34d, 0x4f86, [8]byte{0x97, 0x9c, 0xc9, 0x03, 0x71, 0xaf, 0x6e, 0x66}}
var wfpUniversal = wfpGUID{0xeebecc03, 0xced4, 0x4380, [8]byte{0x81, 0x9a, 0x27, 0x34, 0x39, 0x7b, 0x2b, 0x74}}
var wfpLayers = []wfpGUID{
	{0xc38d57d1, 0x05a7, 0x4c33, [8]byte{0x90, 0x4f, 0x7f, 0xbc, 0xee, 0xe6, 0x0e, 0x82}},
	{0x4a72393b, 0x319f, 0x44bc, [8]byte{0x84, 0xc3, 0xba, 0x54, 0xdc, 0xb3, 0xb6, 0xb4}},
	{0xe1cd9fe7, 0xf4b5, 0x4273, [8]byte{0x96, 0xc0, 0x59, 0x2e, 0x48, 0x7b, 0x86, 0x50}},
	{0xa3b42c97, 0x9f04, 0x4672, [8]byte{0xb8, 0x7e, 0xce, 0xe9, 0xc4, 0x83, 0x25, 0x7f}},
}

func wfpKey(index int) wfpGUID {
	return wfpGUID{0xa054d2d2 + uint32(index), 0x12dc, 0x4fc8, [8]byte{0x9c, 0xe4, 0xd2, 0x30, 0xc1, 0xab, 0xac, 0x41}}
}
func NativeNetworkPolicy(install bool) error {
	sid, _, _, e := syscall.LookupSID("", "CodexSandboxOffline")
	if e != nil {
		return fmt.Errorf("Codex elevated sandbox account is not provisioned")
	}
	s, e := sid.String()
	if e != nil {
		return e
	}
	sddl, e := syscall.UTF16PtrFromString("D:(A;;CC;;;" + s + ")")
	if e != nil {
		return e
	}
	var descriptor unsafe.Pointer
	var length uint32
	ok, _, e := sddlConvert.Call(uintptr(unsafe.Pointer(sddl)), 1, uintptr(unsafe.Pointer(&descriptor)), uintptr(unsafe.Pointer(&length)))
	if ok == 0 {
		return fmt.Errorf("offline user descriptor: %w", e)
	}
	defer localFree.Call(uintptr(descriptor))
	blob := wfpBlob{length, descriptor}
	expected := unsafe.Slice((*byte)(descriptor), int(length))
	var engine syscall.Handle
	code, _, _ := wfpOpen.Call(0, 10, 0, 0, uintptr(unsafe.Pointer(&engine)))
	if code != 0 {
		return fmt.Errorf("open WFP engine: 0x%x", code)
	}
	defer wfpClose.Call(uintptr(engine))
	for i, layer := range wfpLayers {
		key := wfpKey(i)
		var existing *wfpFilter
		code, _, _ = wfpGet.Call(uintptr(engine), uintptr(unsafe.Pointer(&key)), uintptr(unsafe.Pointer(&existing)))
		if code == 0 {
			valid := existing.Layer == layer && existing.Action.Type == 0x1001 && existing.Count == 1 && existing.Flags&1 != 0
			if valid {
				condition := (*wfpCondition)(existing.Conditions)
				valid = condition.Field == wfpUser && condition.Match == 0 && condition.Value.Type == 14
				if valid {
					stored := (*wfpBlob)(condition.Value.Data)
					valid = stored.Size == length && stored.Data != nil && bytes.Equal(unsafe.Slice((*byte)(stored.Data), int(stored.Size)), expected)
				}
			}
			wfpFree.Call(uintptr(unsafe.Pointer(&existing)))
			if !valid {
				return fmt.Errorf("offline WFP policy differs; administrator must reconcile it")
			}
			continue
		}
		if !install {
			return fmt.Errorf("offline WFP filters missing; rerun Setup-Native.ps1")
		}
		if uint32(code) != 0x80320003 {
			return fmt.Errorf("inspect WFP filter: 0x%x", code)
		}
		condition := wfpCondition{Field: wfpUser, Match: 0, Value: wfpValue{Type: 14, Data: unsafe.Pointer(&blob)}}
		label, _ := syscall.UTF16PtrFromString("CodexOrchestrator offline sandbox network block")
		filter := wfpFilter{Key: key, Name: uintptr(unsafe.Pointer(label)), Flags: 1, Layer: layer, SubLayer: wfpUniversal, Count: 1, Conditions: unsafe.Pointer(&condition), Action: wfpAction{Type: 0x1001}}
		code, _, _ = wfpAdd.Call(uintptr(engine), uintptr(unsafe.Pointer(&filter)), 0, 0)
		if code != 0 {
			return fmt.Errorf("install WFP filter: 0x%x", code)
		}
	}
	return nil
}
