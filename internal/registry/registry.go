// Package registry is the single source of truth for provisioned instances. It
// persists state as JSON, allocates collision-free host ports, and records every
// lifecycle action to an append-only audit log.
package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/svallejo-dev/keel-cli/internal/domain"
)

// ErrNotFound is returned when an instance name is unknown.
var ErrNotFound = errors.New("instance not found")

// ErrExists is returned when creating an instance whose name is taken.
var ErrExists = errors.New("instance already exists")

const stateFile = "state.json"

// State is the persisted registry document.
type State struct {
	Version   int                         `json:"version"`
	Instances map[string]*domain.Instance `json:"instances"`
	UsedPorts []int                       `json:"used_ports"`
}

// Store loads, mutates and persists registry State. It is safe for concurrent use.
type Store struct {
	dir   string
	mu    sync.Mutex
	state State
	ports *portAllocator
}

// Open loads (or initializes) the registry rooted at dir, creating it if needed.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("registry: create dir: %w", err)
	}
	s := &Store{
		dir:   dir,
		state: State{Version: 1, Instances: map[string]*domain.Instance{}},
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	s.ports = newPortAllocator(s)
	return s, nil
}

// Dir returns the registry root directory.
func (s *Store) Dir() string { return s.dir }

func (s *Store) load() error {
	b, err := os.ReadFile(filepath.Join(s.dir, stateFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil // fresh registry
	}
	if err != nil {
		return fmt.Errorf("registry: read state: %w", err)
	}
	if len(b) == 0 {
		return nil
	}
	if err := json.Unmarshal(b, &s.state); err != nil {
		return fmt.Errorf("registry: parse state: %w", err)
	}
	if s.state.Instances == nil {
		s.state.Instances = map[string]*domain.Instance{}
	}
	// State written before multi-cloud support stored the parity target as
	// aws_service; carry it over so existing instances keep their label.
	for _, inst := range s.state.Instances {
		if inst.CloudService == "" && inst.LegacyAWSService != "" {
			inst.CloudService = inst.LegacyAWSService
		}
		inst.LegacyAWSService = ""
	}
	return nil
}

// persist writes state atomically. Caller must hold s.mu.
func (s *Store) persist() error {
	b, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return fmt.Errorf("registry: encode state: %w", err)
	}
	tmp := filepath.Join(s.dir, stateFile+".tmp")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("registry: write state: %w", err)
	}
	if err := os.Rename(tmp, filepath.Join(s.dir, stateFile)); err != nil {
		return fmt.Errorf("registry: commit state: %w", err)
	}
	return nil
}

// Add stores a new instance, failing if the name already exists.
func (s *Store) Add(inst *domain.Instance) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.state.Instances[inst.Name]; ok {
		return fmt.Errorf("%q: %w", inst.Name, ErrExists)
	}
	s.state.Instances[inst.Name] = inst
	return s.persist()
}

// Put upserts an instance (used for status/endpoint updates).
func (s *Store) Put(inst *domain.Instance) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Instances[inst.Name] = inst
	return s.persist()
}

// Get returns a copy-by-reference of the named instance.
func (s *Store) Get(name string) (*domain.Instance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inst, ok := s.state.Instances[name]
	if !ok {
		return nil, fmt.Errorf("%q: %w", name, ErrNotFound)
	}
	return inst, nil
}

// Remove deletes an instance and releases its host ports.
func (s *Store) Remove(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	inst, ok := s.state.Instances[name]
	if !ok {
		return fmt.Errorf("%q: %w", name, ErrNotFound)
	}
	for _, ep := range inst.Endpoints {
		s.releaseLocked(ep.Port)
	}
	delete(s.state.Instances, name)
	return s.persist()
}

// List returns all instances sorted by name.
func (s *Store) List() []*domain.Instance {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*domain.Instance, 0, len(s.state.Instances))
	for _, inst := range s.state.Instances {
		out = append(out, inst)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Exists reports whether an instance name is taken.
func (s *Store) Exists(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.state.Instances[name]
	return ok
}

// Ports returns the PortAllocator backed by this store.
func (s *Store) Ports() *portAllocator { return s.ports }
