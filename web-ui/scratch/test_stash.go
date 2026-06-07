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

func findStudioIDByName(ctx context.Context, apiKey, name string) (string, error) {
	query := `query FindStudio($name: String!) {
		queryStudios(input: {
			name: $name
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
			"name": name,
		},
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

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
		return "", fmt.Errorf("stashdb queryStudios status: %d", resp.StatusCode)
	}

	type QueryStudiosResp struct {
		Data struct {
			QueryStudios struct {
				Studios []struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"studios"`
			} `json:"queryStudios"`
		} `json:"data"`
	}

	var qResp QueryStudiosResp
	if err := json.NewDecoder(resp.Body).Decode(&qResp); err != nil {
		return "", err
	}

	studios := qResp.Data.QueryStudios.Studios
	if len(studios) > 0 {
		return fmt.Sprintf("ID: %s, Name: %s", studios[0].ID, studios[0].Name), nil
	}

	return "", fmt.Errorf("studio not found: %s", name)
}

func main() {
	apiKey := os.Getenv("STASHDB_API_KEY")
	if apiKey == "" {
		fmt.Println("STASHDB_API_KEY is empty!")
		return
	}
	fmt.Printf("API Key starts with: %s...\n", apiKey[:10])

	ctx := context.Background()

	studiosToTest := []string{"Pure Taboo", "Vixen", "PervMom"}
	for _, studio := range studiosToTest {
		res, err := findStudioIDByName(ctx, apiKey, studio)
		if err != nil {
			fmt.Printf("Studio '%s' query failed: %v\n", studio, err)
		} else {
			fmt.Printf("Studio '%s' found: %s\n", studio, res)
		}
	}
}
