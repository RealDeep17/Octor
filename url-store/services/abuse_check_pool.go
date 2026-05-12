package services

import (
	"sync"
	"time"
)

const (
	AbuseCheckTTL = 600
)

type AbuseCheckPool struct {
	sm     sync.Map
	expire time.Duration
	cl     *AbuseStoreClient
}

func NewAbuseCheckPool(cl *AbuseStoreClient) *AbuseCheckPool {
	return &AbuseCheckPool{expire: time.Duration(AbuseCheckTTL) * time.Second, cl: cl}
}

func (s *AbuseCheckPool) Check(hash string) (bool, error) {
	a, loaded := s.sm.LoadOrStore(hash, NewAbuseCheck(s.cl, hash))
	if !loaded {
		go func() {
			<-time.After(s.expire)
			s.sm.Delete(hash)
		}()
	}
	return a.(*AbuseCheck).Get()
}
