package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

func main() {
	apiKey := os.Getenv("STASHDB_API_KEY")
	if apiKey == "" {
		fmt.Println("STASHDB_API_KEY not set!")
		return
	}

	// Test 1: queryPerformers with nested criteria (original code)
	query1 := `query FindPerformer1($name: String!) {
		queryPerformers(input: {
			name: { value: $name, modifier: INCLUDES }
		}) {
			performers {
				id
				name
			}
		}
	}`
	body1 := map[string]interface{}{
		"query": query1,
		"variables": map[string]interface{}{
			"name": "Angela White",
		},
	}
	bodyBytes1, _ := json.Marshal(body1)

	req1, _ := http.NewRequest("POST", "https://stashdb.org/graphql", bytes.NewReader(bodyBytes1))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("ApiKey", apiKey)

	resp1, _ := http.DefaultClient.Do(req1)
	respBytes1, _ := io.ReadAll(resp1.Body)
	resp1.Body.Close()
	fmt.Printf("Nested Query Response: %s\n\n", string(respBytes1))

	// Test 2: queryPerformers with direct string (correct schema)
	query2 := `query FindPerformer2($name: String!) {
		queryPerformers(input: {
			name: $name
		}) {
			performers {
				id
				name
			}
		}
	}`
	body2 := map[string]interface{}{
		"query": query2,
		"variables": map[string]interface{}{
			"name": "Angela White",
		},
	}
	bodyBytes2, _ := json.Marshal(body2)

	req2, _ := http.NewRequest("POST", "https://stashdb.org/graphql", bytes.NewReader(bodyBytes2))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("ApiKey", apiKey)

	resp2, _ := http.DefaultClient.Do(req2)
	respBytes2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	fmt.Printf("Direct Query Response: %s\n", string(respBytes2))
}
