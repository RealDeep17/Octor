package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
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
		"525f8c32-d14f-42c0-939c-bb5d8eae8bcf", // BangBros
		"568c160d-ad70-42f3-9e17-03a69641b14e", // TeamSkeet
		"2be8463b-0505-479e-a07d-5abc7a6edd54", // Naughty America
		"6fa161d2-3c28-44b7-86a2-d98f5a8373aa", // Adult Time
		"4bed0748-a3aa-4e22-8c46-f5ef732b5149", // Reality Kings
		"b8e5b226-503b-4852-ad89-5e76853728e3", // Jules Jordan
		"1e8d8028-fb8c-4286-b742-e62e5667c576", // Digital Playground
		"ec61a0b4-d5ae-4c87-9d2d-68e6674790f6", // Twistys
		"6070e7f0-b8a1-463b-bf0f-0cb3499fd00c", // Mile High Media
		"de45e4b3-7204-4393-a2b3-c485dd106644", // Pornbox
		"c5f4f1d2-bf00-4e7a-9fc1-3d4e171dd246", // Nubiles Porn
		"8218160c-2dcb-4fe8-8acb-9937c5a2a61f", // Metro HD
		"3e8fb21e-1c95-44f6-839c-8f24ae7d48f9", // New Sensations
		"9be5eddc-a9f2-4e5c-9bf1-086ddf08a907", // BLT Innovations
		"e42f2af3-d410-4bb4-bb5c-1b5fbbb07eae", // Alex Adams Media
	}

	excludeRx := strings.NewReplacer("lesbian", "", "gay", "", "bi-empire", "", "boys", "", "males", "", "men", "", "trans", "", "t-girls", "", "midgets", "")

	resolvedIDs := make(map[string]bool)
	for _, id := range parentIDs {
		resolvedIDs[id] = true
	}
	resolvedStudioNameToID := make(map[string]string)

	currentParents := parentIDs
	cl := &http.Client{Timeout: 20 * time.Second}

	// Run recursive query for 3 levels (child, grandchild, great-grandchild)
	for level := 1; level <= 3; level++ {
		if len(currentParents) == 0 {
			break
		}
		fmt.Printf("Level %d: Resolving children of %d parents...\n", level, len(currentParents))
		
		var nextParents []string
		page := 1

		for {
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
					"parent_ids": currentParents,
					"page":       page,
				},
			}

			bodyBytes, _ := json.Marshal(body)
			req, _ := http.NewRequest("POST", "https://stashdb.org/graphql", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("ApiKey", apiKey)

			resp, err := cl.Do(req)
			if err != nil {
				fmt.Printf("Request error: %v\n", err)
				break
			}

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
			resp.Body.Close()

			studios := qResp.Data.QueryStudios.Studios
			if len(studios) == 0 {
				break
			}

			for _, s := range studios {
				if resolvedIDs[s.ID] {
					continue
				}
				lowerName := strings.ToLower(s.Name)
				if excludeRx.Replace(lowerName) == lowerName {
					resolvedIDs[s.ID] = true
					resolvedStudioNameToID[lowerName] = s.ID
					nextParents = append(nextParents, s.ID)
				}
			}

			if len(studios) < 500 {
				break
			}
			page++
		}
		currentParents = nextParents
	}

	fmt.Printf("\nTOTAL RESOLVED STUDIOS: %d\n", len(resolvedIDs))
	if resolvedIDs["eb3ed4ed-c23f-43cd-9f1f-8a80452a562c"] {
		fmt.Println(">>> SUCCESS: Pure Taboo was successfully resolved recursively!")
	} else {
		fmt.Println(">>> FAILURE: Pure Taboo is still missing!")
	}
}
