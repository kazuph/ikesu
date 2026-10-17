package vt

import (
	"reflect"
	"testing"
)

func TestCcHandlerDispatch(t *testing.T) {
	for code := 0; code < 1<<8; code++ {
		var h handlers
		r := byte(code)
		if h.handleCc(r) {
			t.Fatalf("unregistered byte %d was handled", code)
		}
		var called []int
		h.registerCcHandler(r, func() bool { called = append(called, 1); return false })
		h.registerCcHandler(r, func() bool { called = append(called, 2); return false })
		if h.handleCc(r) || !reflect.DeepEqual(called, []int{2, 1}) {
			t.Fatalf("byte %d: false handlers called %v", code, called)
		}
		called = nil
		h.registerCcHandler(r, func() bool { called = append(called, 3); return true })
		h.registerCcHandler(r, func() bool { called = append(called, 4); return false })
		if !h.handleCc(r) || !reflect.DeepEqual(called, []int{4, 3}) {
			t.Fatalf("byte %d: stopping handlers called %v", code, called)
		}
	}
}

func TestCcHandlerRegistrationDuringDispatch(t *testing.T) {
	var h handlers
	const r = '\r'
	var called []int
	h.registerCcHandler(r, func() bool { called = append(called, 1); return false })
	h.registerCcHandler(r, func() bool {
		called = append(called, 2)
		h.ccHandlers[r][0] = func() bool { called = append(called, 3); return true }
		h.registerCcHandler(r, func() bool { called = append(called, 4); return true })
		return false
	})
	if !h.handleCc(r) || !reflect.DeepEqual(called, []int{2, 3}) {
		t.Fatalf("registration during dispatch called %v", called)
	}
	called = nil
	if !h.handleCc(r) || !reflect.DeepEqual(called, []int{4}) {
		t.Fatalf("next dispatch called %v", called)
	}
}
