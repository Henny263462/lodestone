//go:build windows

package node

import (
	"syscall"
	"unsafe"
)

// diskSpace liefert den freien und den gesamten Platz des Laufwerks, auf dem path liegt (0, 0 = unbekannt).
func diskSpace(path string) (free, total int64) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0
	}
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")
	var available, size, totalFree uint64
	ok, _, _ := proc.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&available)), uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&totalFree)))
	if ok == 0 {
		return 0, 0
	}
	return int64(available), int64(size)
}
