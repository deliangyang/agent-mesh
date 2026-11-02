//go:build windows

package workbuddy

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// processBasicInformation is the layout returned by NtQueryInformationProcess
// for ProcessBasicInformation on both 32-bit and 64-bit Windows.
type processBasicInformation struct {
	Reserved1       uintptr
	PebBaseAddress  uintptr
	Reserved2       [2]uintptr
	UniqueProcessId uintptr
	Reserved3       uintptr
}

func processEnviron(pid int) map[string]string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION|windows.PROCESS_VM_READ, false, uint32(pid))
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(h)

	var pbi processBasicInformation
	if err := windows.NtQueryInformationProcess(h, windows.ProcessBasicInformation, unsafe.Pointer(&pbi), uint32(unsafe.Sizeof(pbi)), nil); err != nil {
		return nil
	}
	if pbi.PebBaseAddress == 0 {
		return nil
	}

	// PEB.ProcessParameters: 0x10 on 32-bit, 0x20 on 64-bit.
	paramsOff := uintptr(0x10)
	if unsafe.Sizeof(uintptr(0)) == 8 {
		paramsOff = 0x20
	}
	var paramsAddr uintptr
	if err := readProcessMemory(h, pbi.PebBaseAddress+paramsOff, (*byte)(unsafe.Pointer(&paramsAddr)), unsafe.Sizeof(paramsAddr)); err != nil || paramsAddr == 0 {
		return nil
	}

	// RTL_USER_PROCESS_PARAMETERS.Environment: 0x48 on 32-bit, 0x80 on 64-bit.
	envOff := uintptr(0x48)
	if unsafe.Sizeof(uintptr(0)) == 8 {
		envOff = 0x80
	}
	var envAddr uintptr
	if err := readProcessMemory(h, paramsAddr+envOff, (*byte)(unsafe.Pointer(&envAddr)), unsafe.Sizeof(envAddr)); err != nil || envAddr == 0 {
		return nil
	}

	buf := make([]byte, 64*1024)
	var n uintptr
	if err := windows.ReadProcessMemory(h, envAddr, &buf[0], uintptr(len(buf)), &n); err != nil && n == 0 {
		return nil
	}
	return parseEnvironUTF16(buf[:n])
}

func readProcessMemory(h windows.Handle, addr uintptr, dst *byte, size uintptr) error {
	var n uintptr
	return windows.ReadProcessMemory(h, addr, dst, size, &n)
}
