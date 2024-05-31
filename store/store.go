package store

import (
	"sync"
)

type StsToken struct {
	AK    string
	SK    string
	Token string
}

type StsTokenStore struct {
	stsTokenMap map[string]StsToken
	mu          sync.Mutex
}

func NewStsTokenStore() *StsTokenStore {
	return &StsTokenStore{
		stsTokenMap: make(map[string]StsToken),
	}
}

func (s *StsTokenStore) GetAk(key string) (StsToken, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	secret, exists := s.stsTokenMap[key]
	return secret, exists
}

func (s *StsTokenStore) SetAk(key string, value StsToken) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.stsTokenMap[key] = value
}

func (s *StsTokenStore) Exists(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, exists := s.stsTokenMap[key]
	return exists
}

func (s *StsTokenStore) ClearStsTokenMap() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.stsTokenMap = make(map[string]StsToken)
}
