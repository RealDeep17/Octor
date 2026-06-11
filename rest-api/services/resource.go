package services

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	gcodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/pkg/errors"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/urfave/cli"
	"github.com/webtor-io/lazymap"

	m2tp "github.com/webtor-io/magnet2torrent/proto"
	tsp "github.com/webtor-io/torrent-store/proto"
)

type ResourceType int

const (
	ResourceTypeHash ResourceType = iota
	ResourceTypeMagnet
	ResourceTypeTorrent
	ResourceTypeSha1
)

type Resource struct {
	ID        string
	Name      string
	Size      int64
	Files     []*File
	Type      ResourceType
	MagnetURI string
	Torrent   []byte
}

type File struct {
	Path   []string
	Size   int64
	Pieces []Hash
}

const (
	HashSize int = 20
)

type Hash [HashSize]byte

func splitPieces(buf []byte) []Hash {
	var chunk []byte
	hashes := make([]Hash, 0, len(buf)/HashSize+1)
	for len(buf) >= HashSize {
		chunk, buf = buf[:HashSize], buf[HashSize:]
		var h Hash
		copy(h[:], chunk)
		hashes = append(hashes, h)
	}
	return hashes
}

type ResourceMap struct {
	*lazymap.LazyMap[*Resource]
	ts                  TorrentStoreGetter
	m2t                 Magnet2TorrentGetter
	magnetTimeout       time.Duration
	torrentStoreTimeout time.Duration
}

const resourceCacheExpireFlag = "resource-cache-expire"

func RegisterResourceMapFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.DurationFlag{
			Name:   resourceCacheExpireFlag,
			Usage:  "resource metadata cache expiration",
			EnvVar: "CACHED_METADATA_EXPIRE,REST_API_RESOURCE_CACHE_EXPIRE",
			Value:  10 * time.Minute,
		},
	)
}

type TorrentStoreGetter interface {
	Get() (tsp.TorrentStoreClient, error)
}

type Magnet2TorrentGetter interface {
	Get() (m2tp.Magnet2TorrentClient, error)
}

func NewResourceMap(ts TorrentStoreGetter, m2t Magnet2TorrentGetter, cliCtx ...*cli.Context) *ResourceMap {
	cacheExpire := 10 * time.Minute
	if len(cliCtx) > 0 && cliCtx[0] != nil {
		cacheExpire = cliCtx[0].Duration(resourceCacheExpireFlag)
	}
	return &ResourceMap{
		LazyMap: lazymap.New[*Resource](&lazymap.Config{
			Concurrency: 100,
			Expire:      cacheExpire,
			Capacity:    1000,
		}),
		ts:                  ts,
		m2t:                 m2t,
		torrentStoreTimeout: 10 * time.Second,
		magnetTimeout:       3 * time.Minute,
	}
}

func (s *ResourceMap) parseMagnet(b []byte) (*Resource, error) {
	ma, err := metainfo.ParseMagnetUri(string(b))
	if err != nil {
		return nil, err
	}
	return &Resource{
		ID:   ma.InfoHash.HexString(),
		Type: ResourceTypeMagnet,
	}, nil
}

func (s *ResourceMap) parseTorrent(b []byte) (*Resource, error) {
	br := bytes.NewReader(b)
	mi, err := metainfo.Load(br)
	if err != nil {
		return nil, err
	}
	i, err := mi.UnmarshalInfo()
	if err != nil {
		return nil, err
	}
	name := i.Name
	if i.NameUtf8 != "" {
		name = i.NameUtf8
	}
	r := &Resource{
		ID:   mi.HashInfoBytes().HexString(),
		Name: name,
		Size: i.PieceLength * int64(i.NumPieces()),
		Type: ResourceTypeTorrent,
	}
	pieces := splitPieces(i.Pieces)

	offset := int64(0)
	for _, f := range i.UpvertedFiles() {
		start, end := offset/i.PieceLength, (offset+f.Length)/i.PieceLength
		path := f.Path
		if len(f.PathUtf8) > 0 {
			path = f.PathUtf8
		}
		r.Files = append(r.Files, &File{
			Path:   append([]string{name}, path...),
			Size:   f.Length,
			Pieces: pieces[start : end+1],
		})
		offset += f.Length
	}
	r.MagnetURI = mi.Magnet(nil, &i).String()
	r.Torrent = b
	return r, nil
}

func (s *ResourceMap) parse(b []byte) (*Resource, error) {
	if sha1R.Match(b) {
		return &Resource{
			ID:   string(b),
			Type: ResourceTypeSha1,
		}, nil
	} else if strings.HasPrefix(string(b), "magnet:") {
		r, err := s.parseMagnet(b)
		if err != nil {
			return nil, errors.Wrap(err, "failed to parse magnet")
		}
		return r, nil
	} else {
		r, err := s.parseTorrent(b)
		if err != nil {
			return nil, errors.Wrap(err, "failed to parse torrent")
		}
		return r, nil
	}
}

func (s *ResourceMap) get(ctx context.Context, r *Resource, b []byte) (*Resource, error) {
	ts, err := s.ts.Get()
	if err != nil {
		return nil, err
	}
	found := true
	touchCtx, touchCancel := context.WithTimeout(ctx, s.torrentStoreTimeout)
	defer touchCancel()
	_, err = ts.Touch(touchCtx, &tsp.TouchRequest{InfoHash: r.ID})
	if err != nil {
		if st, ok := status.FromError(err); ok {
			switch st.Code() {
			case gcodes.PermissionDenied:
				return nil, errors.Wrap(err, "forbidden")
			case gcodes.NotFound:
				found = false
			default:
				return nil, err
			}
		} else {
			return nil, err
		}
	}
	if found {
		if r.Type == ResourceTypeTorrent {
			return r, nil
		} else {
			pullCtx, pullCancel := context.WithTimeout(ctx, s.torrentStoreTimeout)
			defer pullCancel()
			rep, err := ts.Pull(pullCtx, &tsp.PullRequest{InfoHash: r.ID})
			if err != nil {
				return nil, err
			}
			return s.parseTorrent(rep.GetTorrent())
		}
	}
	switch r.Type {
	case ResourceTypeSha1:
		return nil, errors.Errorf("not found sha1=%v", r.ID)
	case ResourceTypeTorrent:
		pushCtx, pushCancel := context.WithTimeout(ctx, s.torrentStoreTimeout)
		defer pushCancel()
		_, err := ts.Push(pushCtx, &tsp.PushRequest{Torrent: b})
		if err != nil {
			return nil, err
		}
		return r, nil

	case ResourceTypeMagnet:
		m2t, err := s.m2t.Get()
		if err != nil {
			return nil, err
		}
		magnetCtx, magnetCancel := context.WithTimeout(ctx, s.magnetTimeout)
		defer magnetCancel()
		rep, err := m2t.Magnet2Torrent(magnetCtx, &m2tp.Magnet2TorrentRequest{Magnet: string(b)})
		if err != nil && magnetCtx.Err() != nil {
			return nil, errors.Wrap(err, "magnet timeout")
		} else if err != nil {
			return nil, err
		}
		_, err = ts.Push(ctx, &tsp.PushRequest{Torrent: rep.GetTorrent()})
		if err != nil {
			return nil, err
		}
		return s.parseTorrent(rep.GetTorrent())
	}
	return nil, nil
}

var localDownloadHosts = []string{"prowlarr", "sonarr", "radarr", "whisparr", "wishparr"}

func rewriteLocalDownloadURL(urlStr string) string {
	if strings.Contains(urlStr, "://zilean:8181") {
		return strings.Replace(urlStr, "://zilean:8181", "://127.0.0.1:8182", 1)
	}
	for _, host := range localDownloadHosts {
		if strings.Contains(urlStr, "://"+host+":") {
			return strings.Replace(urlStr, "://"+host+":", "://127.0.0.1:", 1)
		}
	}
	return urlStr
}

func (s *ResourceMap) downloadTorrentURL(ctx context.Context, urlStr string) ([]byte, error) {
	urlStr = rewriteLocalDownloadURL(urlStr)

	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/115.0.0.0 Safari/537.36")

	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if strings.HasPrefix(req.URL.String(), "magnet:") || req.URL.Scheme == "magnet" {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		loc := resp.Header.Get("Location")
		if strings.HasPrefix(loc, "magnet:") {
			return []byte(loc), nil
		}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download torrent from %s: status code %d", urlStr, resp.StatusCode)
	}

	limitReader := io.LimitReader(resp.Body, 10*1024*1024)
	bodyBytes, err := io.ReadAll(limitReader)
	if err != nil {
		return nil, err
	}

	bodyStr := strings.TrimSpace(string(bodyBytes))
	if strings.HasPrefix(bodyStr, "magnet:") {
		return []byte(bodyStr), nil
	}

	return bodyBytes, nil
}

func (s *ResourceMap) Get(ctx context.Context, b []byte) (*Resource, error) {
	str := string(b)
	if strings.HasPrefix(str, "http://") || strings.HasPrefix(str, "https://") {
		downloaded, err := s.downloadTorrentURL(ctx, str)
		if err != nil {
			return nil, errors.Wrap(err, "failed to download torrent from URL")
		}
		b = downloaded
	}
	r, err := s.parse(b)
	if err != nil {
		return nil, err
	}
	return s.LazyMap.Get(r.ID, func() (*Resource, error) {
		return s.get(ctx, r, b)
	})
}
