package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/services/javguru"
)

func main() {
	redisAddr := "127.0.0.1:6380"
	cl := &http.Client{Timeout: 10 * time.Second}
	
	// Create a dummy redis client (doesn't need to connect if we don't cache, but let's connect)
	opts := cs.RedisClientOptions{
		Addrs: []string{redisAddr},
	}
	redisClient := cs.NewRedisClient(&opts)

	jgSvc := javguru.New(cl, redisClient)
	scenes, err := jgSvc.FetchTrending(context.Background())
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("FetchTrending returned %d scenes:\n", len(scenes))
	for i, s := range scenes {
		fmt.Printf("[%d] Title: %s\n", i+1, s.Title)
		fmt.Printf("    Poster: %s\n", s.Poster)
	}
}
