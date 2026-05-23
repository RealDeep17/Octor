package main

import (
	"encoding/json"
	"fmt"
	"log"

	ptn "github.com/webtor-io/web-ui/services/parse_torrent_name"
)

func main() {
	tor := &ptn.TorrentInfo{}
	res, err := ptn.Parse(tor, "LegalPorno - Nala Brooks (21.05.2026) rq.mp4")
	if err != nil {
		log.Fatalf("Parse error: %v", err)
	}

	b, _ := json.MarshalIndent(res, "", "  ")
	fmt.Println(string(b))
}
