package cache

import (
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/webtor-io/sidecar/internal/scene"
)

var Cache *lru.Cache[string, *scene.OmdbResponse]

func init() {
	var err error
	Cache, err = lru.New[string, *scene.OmdbResponse](500)
	if err != nil {
		panic(err)
	}
}
