package services

import (
	"fmt"
	"sync"
	"time"
)

const (
	transformerTTL = 60 * 60
)

// TransformerPool ensures that only one specific preview is generating at time
type TransformerPool struct {
	sm     sync.Map
	timers sync.Map
	expire time.Duration
}

// NewTransformerPool initializes TransformerPool
func NewTransformerPool() *TransformerPool {
	return &TransformerPool{expire: time.Duration(transformerTTL) * time.Second}
}

// Get gets Transformer
func (s *TransformerPool) Get(sourceURL string, format string, width int, infoHash string, path string, p string) *Transformer {
	key := fmt.Sprintf("%v%v%v%v%v", format, width, infoHash, path, p)
	v, _ := s.sm.LoadOrStore(key, NewTransformer(sourceURL, format, width))
	t, tLoaded := s.timers.LoadOrStore(key, time.NewTimer(s.expire))
	timer := t.(*time.Timer)
	if !tLoaded {
		if _, err := v.(*Transformer).Get(); err != nil {
			s.sm.Delete(key)
			s.timers.Delete(key)
		} else {
			go func() {
				<-timer.C
				s.sm.Delete(key)
				s.timers.Delete(key)
			}()
		}
	} else {
		timer.Reset(s.expire)
	}
	return v.(*Transformer)
}
