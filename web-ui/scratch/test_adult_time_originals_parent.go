package main

import (
	"bytes"
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

	query := `query FindStudio($id: ID!) {
		findStudio(id: $id) {
			id
			name
			parent {
				id
				name
			}
		}
	}`

	body := map[string]interface{}{
		"query": query,
		"variables": map[string]interface{}{
			"id": "91bfd32c-2f56-43a1-8bb9-55d3a84d88d5",
		},
	}

	bodyBytes, _ := json.Marshal(body)
	cl := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest("POST", "https://stashdb.org/graphql", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ApiKey", apiKey)

	resp, err := cl.Do(req)
	if err != nil {
		fmt.Printf("HTTP request failed: %v\n", err)
		return
	}
	defer resp.Body.Close()

	type StudioResp struct {
		Data struct {
			FindStudio struct {
				ID     string `json:"id"`
				Name   string `json:"name"`
				Parent struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"parent"`
			} `json:"findStudio"`
		} `json:"data"`
	}

	var qResp StudioResp
	json.NewDecoder(resp.Body).Decode(&qResp)

	studio := qResp.Data.FindStudio
	fmt.Printf("Studio: %s (ID: %s)\n", studio.Name, studio.ID)
	if studio.Parent.ID != "" {
		fmt.Printf("Parent: %s (ID: %s)\n", studio.Parent.Name, studio.Parent.ID)
	} else {
		fmt.Println("Parent: NONE")
	}
}
