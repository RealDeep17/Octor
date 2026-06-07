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

	query := `query GetScenes($studios: MultiIDCriterionInput, $page: Int!) {
		queryScenes(input: { page: $page, per_page: 60, sort: DATE, direction: DESC, studios: $studios }) {
			scenes {
				id
				title
				studio {
					name
				}
			}
		}
	}`

	vars := map[string]interface{}{
		"page":      1,
		"studios": map[string]interface{}{
			"value":    []string{"eb3ed4ed-c23f-43cd-9f1f-8a80452a562c"}, // Pure Taboo ID
			"modifier": "INCLUDES",
		},
	}

	body := map[string]interface{}{
		"query":     query,
		"variables": vars,
	}

	bodyBytes, _ := json.Marshal(body)
	cl := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequest("POST", "https://stashdb.org/graphql", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ApiKey", apiKey)

	resp, err := cl.Do(req)
	if err != nil {
		fmt.Printf("HTTP request failed: %v\n", err)
		return
	}
	defer resp.Body.Close()

	type SceneListItem struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Studio struct {
			Name string `json:"name"`
		} `json:"studio"`
	}
	type QueryScenesResp struct {
		Data struct {
			QueryScenes struct {
				Scenes []SceneListItem `json:"scenes"`
			} `json:"queryScenes"`
		} `json:"data"`
		Errors []interface{} `json:"errors"`
	}

	var qResp QueryScenesResp
	json.NewDecoder(resp.Body).Decode(&qResp)

	if len(qResp.Errors) > 0 {
		fmt.Printf("GraphQL Errors: %v\n", qResp.Errors)
	}

	fmt.Printf("Successfully got %d scenes for Pure Taboo!\n", len(qResp.Data.QueryScenes.Scenes))
	for i, s := range qResp.Data.QueryScenes.Scenes {
		if i < 10 {
			fmt.Printf("- %s [%s]\n", s.Title, s.Studio.Name)
		}
	}
}
