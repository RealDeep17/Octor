package services

import (
	"context"
	"sync"
	"time"

	"github.com/pkg/errors"
	pb "github.com/webtor-io/abuse-store/abuse-store"
)

type AbuseCheck struct {
	inited bool
	hash   string
	mux    sync.Mutex
	err    error
	cl     *AbuseStoreClient
	check  bool
}

func NewAbuseCheck(cl *AbuseStoreClient, hash string) *AbuseCheck {
	return &AbuseCheck{
		cl:   cl,
		hash: hash,
	}
}

func (s *AbuseCheck) get() (bool, error) {
	cl, err := s.cl.Get()
	if err != nil {
		return false, errors.Wrapf(err, "failed to get abuse store client")
	}
	if cl == nil {
		return true, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := cl.Check(ctx, &pb.CheckRequest{Infohash: s.hash})
	if err != nil {
		return false, err
	}
	return !r.Exists, nil
}

func (s *AbuseCheck) Get() (bool, error) {
	s.mux.Lock()
	defer s.mux.Unlock()
	if s.inited {
		return s.check, s.err
	}
	s.check, s.err = s.get()
	s.inited = true
	return s.check, s.err
}
