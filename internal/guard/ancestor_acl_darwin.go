package guard

import (
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// ATTR_CMN_EXTENDED_SECURITY returns an attrreference to the kernel ACL.
// A zero-length attribute is the ACL-free case. Report the full size so any
// ACL can be rejected without allocating or interpreting entries (sys/attr.h).
func ancestorHasNoACL(path string) bool {
	name, err := unix.BytePtrFromString(path)
	if err != nil {
		return false
	}
	attrs := unix.Attrlist{Bitmapcount: unix.ATTR_BIT_MAP_COUNT, Commonattr: unix.ATTR_CMN_EXTENDED_SECURITY}
	var result struct {
		Size uint32
		ACL  struct {
			Offset int32
			Length uint32
		}
	}
	_, _, errno := syscall.Syscall6(unix.SYS_GETATTRLIST, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&attrs)), uintptr(unsafe.Pointer(&result)), unsafe.Sizeof(result), unix.FSOPT_REPORT_FULLSIZE, 0)
	runtime.KeepAlive(name)
	return errno == 0 && result.Size == uint32(unsafe.Sizeof(result)) && result.ACL.Offset == int32(unsafe.Sizeof(result.ACL)) && result.ACL.Length == 0
}
