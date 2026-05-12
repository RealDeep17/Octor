package services

import (
	"bufio"
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"sync"

	webp "github.com/nickalie/go-webpbin"

	"github.com/nfnt/resize"
	"github.com/pkg/errors"
)

// Transformer generates previews for video content
type Transformer struct {
	sourceURL string
	format    string
	width     int
	inited    bool
	b         []byte
	err       error
	mux       sync.Mutex
}

// NewTransformer initializes new Transformer instance
func NewTransformer(sourceURL string, format string, width int) *Transformer {
	return &Transformer{sourceURL: sourceURL, format: format, width: width}
}

func (s *Transformer) get() ([]byte, error) {
	resp, err := http.Get(s.sourceURL)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to fetch %v", s.sourceURL)
	}
	defer resp.Body.Close()
	var ct string
	cts := resp.Header["Content-Type"]
	if len(cts) > 0 {
		ct = cts[0]
	}
	var im image.Image
	var f string
	if ct == "image/webp" {
		im, err = webp.Decode(resp.Body)
		f = "webp"
	} else {
		im, f, err = image.Decode(resp.Body)
	}
	if err != nil {
		return nil, errors.Wrapf(err, "failed to decode %v", s.sourceURL)
	}
	if s.width > 0 {
		im = resize.Resize(uint(s.width), 0, im, resize.Lanczos3)
	}
	var b bytes.Buffer
	w := bufio.NewWriter(&b)
	if s.format == "jpg" || s.format == "jpeg" {
		err = jpeg.Encode(w, im, nil)
	} else if s.format == "png" {
		err = png.Encode(w, im)
	} else if s.format == "webp" {
		err = webp.Encode(w, im)
	} else {
		err = errors.Errorf("Unknown format %v", s.format)
	}
	if err != nil {
		return nil, errors.Wrapf(err, "failed to convert %v from %v to %v", s.sourceURL, f, s.format)
	}
	return b.Bytes(), nil
}

// Get gets transformed image
func (s *Transformer) Get() (io.Reader, error) {
	s.mux.Lock()
	defer s.mux.Unlock()
	if s.inited {
		return bytes.NewReader(s.b), s.err
	}
	s.b, s.err = s.get()
	s.inited = true
	return bytes.NewReader(s.b), s.err
}
