package helper

import (
	"reflect"
	"unsafe"
)

func ChanClosed(ch interface{}) bool {
	if reflect.TypeOf(ch).Kind() != reflect.Chan {
		return true
	}

	cptr := *(*uintptr)(unsafe.Pointer(
		unsafe.Pointer(uintptr(unsafe.Pointer(&ch)) + unsafe.Sizeof(uint(0))),
	))

	cptr += unsafe.Sizeof(uint(0)) * 2
	cptr += unsafe.Sizeof(unsafe.Pointer(uintptr(0)))
	cptr += unsafe.Sizeof(uint16(0))
	return *(*uint32)(unsafe.Pointer(cptr)) > 0
}
