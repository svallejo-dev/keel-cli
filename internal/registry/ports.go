package registry

import (
	"fmt"
	"net"
)

// Port allocation window for host-mapped engine ports. Chosen high to avoid
// clashing with common local services.
const (
	portRangeBase = 15000
	portRangeSize = 2000
)

// portAllocator hands out free host ports, persisting the used set in the store
// so allocations survive restarts and never collide across instances.
type portAllocator struct {
	s *Store
}

func newPortAllocator(s *Store) *portAllocator { return &portAllocator{s: s} }

// Allocate returns a free host port: one not already reserved in state and
// actually bindable on the loopback interface right now.
func (a *portAllocator) Allocate() (int, error) {
	a.s.mu.Lock()
	defer a.s.mu.Unlock()

	used := make(map[int]bool, len(a.s.state.UsedPorts))
	for _, p := range a.s.state.UsedPorts {
		used[p] = true
	}
	for p := portRangeBase; p < portRangeBase+portRangeSize; p++ {
		if used[p] || !portFree(p) {
			continue
		}
		a.s.state.UsedPorts = append(a.s.state.UsedPorts, p)
		if err := a.s.persist(); err != nil {
			return 0, err
		}
		return p, nil
	}
	return 0, fmt.Errorf("registry: no free port in range %d-%d", portRangeBase, portRangeBase+portRangeSize-1)
}

// Release returns a port to the pool.
func (a *portAllocator) Release(port int) {
	a.s.mu.Lock()
	defer a.s.mu.Unlock()
	a.s.releaseLocked(port)
	_ = a.s.persist()
}

// releaseLocked removes a port from the used set. Caller must hold s.mu.
func (s *Store) releaseLocked(port int) {
	out := s.state.UsedPorts[:0]
	for _, p := range s.state.UsedPorts {
		if p != port {
			out = append(out, p)
		}
	}
	s.state.UsedPorts = out
}

// portFree reports whether a TCP port can be bound on loopback.
func portFree(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}
