package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

func findPerformerIDByName(ctx context.Context, apiKey, name string) (string, error) {
	query := `query FindPerformer($name: String!) {
		queryPerformers(input: {
			name: $name
			per_page: 5
		}) {
			performers {
				id
				name
				gender
			}
		}
	}`

	body := map[string]interface{}{
		"query": query,
		"variables": map[string]interface{}{
			"name": name,
		},
	}
	bodyBytes, _ := json.Marshal(body)

	cl := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://stashdb.org/graphql", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ApiKey", apiKey)

	resp, err := cl.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("stashdb queryPerformers status: %d", resp.StatusCode)
	}

	type PerformerEntry struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Gender string `json:"gender"`
	}
	type QueryPerformersResp struct {
		Data struct {
			QueryPerformers struct {
				Performers []PerformerEntry `json:"performers"`
			} `json:"queryPerformers"`
		} `json:"data"`
	}

	var qResp QueryPerformersResp
	if err := json.NewDecoder(resp.Body).Decode(&qResp); err != nil {
		return "", err
	}

	performers := qResp.Data.QueryPerformers.Performers
	if len(performers) == 0 {
		return "", fmt.Errorf("no performers found")
	}

	// Prefer FEMALE performers; skip MALE-only results
	for _, p := range performers {
		if strings.EqualFold(p.Gender, "FEMALE") || strings.EqualFold(p.Gender, "TRANSGENDER_FEMALE") {
			return fmt.Sprintf("ID: %s, Name: %s, Gender: %s", p.ID, p.Name, p.Gender), nil
		}
	}
	return fmt.Sprintf("ID: %s, Name: %s, Gender: %s", performers[0].ID, performers[0].Name, performers[0].Gender), nil
}

func main() {
	apiKey := os.Getenv("STASHDB_API_KEY")
	if apiKey == "" {
		fmt.Println("STASHDB_API_KEY is empty!")
		return
	}

	ctx := context.Background()

	performersToTest := []string{"Eva Elfie", "Liya Silver", "Johnny Sins"}
	for _, perf := range performersToTest {
		res, err := findPerformerIDByName(ctx, apiKey, perf)
		if err != nil {
			fmt.Printf("Performer '%s' query failed: %v\n", perf, err)
		} else {
			fmt.Printf("Performer '%s' found: %s\n", perf, res)
		}
	}
}
