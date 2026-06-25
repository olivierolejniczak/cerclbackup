//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

func diskFreeBytes(dir string) (uint64, bool) {
	ptr, err := syscall.UTF16PtrFromString(dir)
	if err != nil {
		return 0, false
	}
	var freeBytes, totalBytes, totalFree uint64
	r, _, _ := syscall.NewLazyDLL("kernel32.dll").
		NewProc("GetDiskFreeSpaceExW").
		Call(
			uintptr(unsafe.Pointer(ptr)),
			uintptr(unsafe.Pointer(&freeBytes)),
			uintptr(unsafe.Pointer(&totalBytes)),
			uintptr(unsafe.Pointer(&totalFree)),
		)
	if r == 0 {
		return 0, false
	}
	return freeBytes, true
}
