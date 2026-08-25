package port

import (
	"net"
	"strconv"
	"testing"
)

// A port held by another process on 127.0.0.1 only (e.g. an IDE helper)
// must not be handed out: with SO_REUSEADDR the allocator's wildcard
// 0.0.0.0 bind check succeeds on macOS even though a consumer binding
// 127.0.0.1 will fail with "address already in use".
func TestGetSkipsLoopbackOnlyListeners(t *testing.T) {
	const from, to = 25311, 25312
	occupied, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(from))
	if err != nil {
		t.Skipf("cannot occupy test port: %v", err)
	}
	defer occupied.Close()

	pa := NewAllocator(from, to, 1, 0)
	for range 2 {
		if got := pa.Get(); got == from {
			t.Fatalf("allocator handed out port %d, which is held on 127.0.0.1 by another listener", got)
		}
	}
}
