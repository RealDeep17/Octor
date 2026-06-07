package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

func main() {
	apiKey := os.Getenv("STASHDB_API_KEY")
	if apiKey == "" {
		fmt.Println("STASHDB_API_KEY is empty!")
		return
	}

	parentIDs := []string{
		"b62bc449-c3d9-49ff-9a16-8f5b1bfa20b9", // Vixen Media Group
		"eec95cdd-9f58-4fc7-b7d1-e98786453d27", // Brazzers
		"6fa161d2-3c28-44b7-86a2-d98f5a8373aa", // Adult Time
	}

	query := `query GetChildStudios($parent_ids: [ID!]!, $page: Int!) {
		queryStudios(input: {
			parent: { value: $parent_ids, modifier: INCLUDES }
			page: $page
			per_page: 500
		}) {
			studios {
				id
				name
			}
		}
	}`

	body := map[string]interface{}{
		"query": query,
		"variables": map[string]interface{}{
			"parent_ids": parentIDs,
			"page":       1,
		},
	}

	bodyBytes, _ := json.Marshal(body)

	cl := &http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "https://stashdb.org/graphql", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ApiKey", apiKey)

	resp, err := cl.Do(req)
	if err != nil {
		fmt.Printf("HTTP request failed: %v\n", err)
		return
	}
	defer resp.Body.Close()

	type StudioEntry struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	type QueryStudiosResp struct {
		Data struct {
			QueryStudios struct {
				Studios []StudioEntry `json:"studios"`
			} `json:"queryStudios"`
		} `json:"data"`
	}

	var qResp QueryStudiosResp
	json.NewDecoder(resp.Body).Decode(&qResp)

	found := false
	for _, s := range qResp.Data.QueryStudios.Studios {
		if s.Name == "Pure Taboo" {
			fmt.Printf(">>> FOUND Pure Taboo! ID: %s\n", s.ID)
			found = true
		}
	}
	if !found {
		fmt.Println(">>> Pure Taboo NOT found in child studios!")
	}
}
