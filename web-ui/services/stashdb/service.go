package stashdb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/services/tpdb"
)

type Service struct {
	client                 *http.Client
	apiKey                 string
	resolvedStudioIDs      []string
	resolvedStudioNameToID map[string]string
	resolvedStudiosLock    sync.RWMutex
}

func New(client *http.Client, apiKey string) *Service {
	s := &Service{
		client:                 client,
		apiKey:                 apiKey,
		resolvedStudioNameToID: make(map[string]string),
	}
	// Start dynamic studio resolving in background
	go s.resolveStudiosJob()
	return s
}

func (s *Service) GetResolvedStudioIDs() []string {
	s.resolvedStudiosLock.RLock()
	defer s.resolvedStudiosLock.RUnlock()
	if len(s.resolvedStudioIDs) == 0 {
		return fallbackStudioIDs
	}
	return s.resolvedStudioIDs
}

func (s *Service) GetResolvedStudioNameToID() map[string]string {
	s.resolvedStudiosLock.RLock()
	defer s.resolvedStudiosLock.RUnlock()
	return s.resolvedStudioNameToID
}

func (s *Service) resolveStudiosJob() {
	log.Info("Resolving mainstream adult studios dynamically from StashDB (recursively up to 3 levels)...")
	if s.apiKey == "" {
		log.Warn("StashDB API key is empty; using fallback studio list")
		s.resolvedStudiosLock.Lock()
		s.resolvedStudioIDs = fallbackStudioIDs
		s.resolvedStudiosLock.Unlock()
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
		"b4a8d39e-6ed8-401e-b8d1-3e3f107b70ca", // Property Sex
		"475bdee5-3b4a-4153-9706-01f457a59e4a", // Elegant Angel
		"e83ce38f-ddf4-45a7-800e-c199392a09c4", // Private
		"a7cba57b-cc9d-47ae-8468-af1e20b6f61a", // DDF Network
		"046ea0c9-c1ae-47bf-9a7e-ba27706da1c7", // Dane Jones
		"21397546-2256-4ed9-9a5b-08c10ca56e3e", // Dorcel
		"c1cf3e96-6324-4d61-8114-a42f12aef348", // ArchAngel Video
		"2c9d7ab5-3db2-4744-af76-d9e920c4dd9b", // MetArt Network
		"1faa9636-6eb8-4c3b-bcf9-d34670c21506", // Deeper
		"842cc5ba-4f5c-4bba-bc4e-04637f4d5c13", // SpyFam
		"70b41fb2-aa50-46bc-bb66-090c38be7149", // Devil's Film (Network)
		"dba7de20-ee98-4563-90a6-c697dc54e375", // Cherry Pimps
		"647792dd-9ccc-49b4-8b85-b7f4de0b5d48", // Sweet Sinner
		"f6ae4372-00df-463f-9551-442d6650b855", // Wicked Pictures
		"1a6c64c5-dde7-477c-addb-c3d1381cf593", // Digital Sin
		"233babe6-ff73-40ef-8650-cb2c95ab6a4e", // Penthouse
		"8c4e72ad-4dfb-4e00-bb28-ab92cb9c61a0", // BaDoink
		"91792902-ba1e-4c5f-b7f5-c129e626ce8a", // JoyMii
		"643bb8a1-9556-443d-950b-e016218c2ed4", // Scoreland
		"bf892f77-11a3-483e-8a41-2a94146dc4c2", // 21 Sextury (Network)
		"d22943f5-9bb5-495e-8b01-6e12d2fffc80", // X-Art
		"f47c35d9-a62b-417a-a492-33bab9c9cd64", // Mom Lover (Network)
		"75fab6e9-275b-4348-936f-3184d034e19b", // ExploitedX
		"98c4c6c5-cf89-487c-91cf-902aa121eb3b", // MYLF
		"a37b47ad-0854-4f74-84ba-9d8c70ba1dd9", // Fakehub
		"059a602f-7ba6-419e-a422-e688c87359be", // Fantasy Massage (Network)
		"3396ac96-80e9-40aa-825f-fcc09159835f", // Dogfart Network
		"eb711259-b4fa-49a4-a572-ebfaba0f93e7", // NF Media
		"b464d27d-2611-4f78-b7ee-68065a305b6e", // Fuck You Cash
	}

	excludeRx := regexp.MustCompile(`(?i)\b(lesbians?|gays?|bi-?empire|boys?|males?|men|trans|t-?girls?|midgets?)\b`)

	resolvedIDs := make(map[string]bool)
	for _, id := range parentIDs {
		resolvedIDs[id] = true
	}

	newStudioNameToID := make(map[string]string)
	// We can manually add some fallback names here
	fallbackStudioNameToID := map[string]string{
		"vixen":              "732560e2-c42b-426c-9471-f92e0ab77609",
		"blacked":            "b281cfc0-d31e-450f-936b-35201aa14234",
		"tushy":              "829bf298-2457-41ec-b8eb-9d107a0e3037",
		"blacked raw":        "800c4c53-a5cf-4063-9b92-2bed1024eaf4",
		"tushy raw":          "3901a1c9-7df9-42b7-8db1-9be0613afb65",
		"brazzers":           "eec95cdd-9f58-4fc7-b7d1-e98786453d27",
		"bang bros":          "525f8c32-d14f-42c0-939c-bb5d8eae8bcf",
		"naughty america":    "2be8463b-0505-479e-a07d-5abc7a6edd54",
		"adult time":         "6fa161d2-3c28-44b7-86a2-d98f5a8373aa",
		"reality kings":      "4bed0748-a3aa-4e22-8c46-f5ef732b5149",
		"jules jordan":       "b8e5b226-503b-4852-ad89-5e76853728e3",
		"digital playground": "1e8d8028-fb8c-4286-b742-e62e5667c576",
		"twistys":            "ec61a0b4-d5ae-4c87-9d2d-68e6674790f6",
		"mile high media":    "6070e7f0-b8a1-463b-bf0f-0cb3499fd00c",
		"newsensations":      "3e8fb21e-1c95-44f6-839c-8f24ae7d48f9",
		"property sex":       "b4a8d39e-6ed8-401e-b8d1-3e3f107b70ca",
		"elegant angel":      "475bdee5-3b4a-4153-9706-01f457a59e4a",
		"private":            "e83ce38f-ddf4-45a7-800e-c199392a09c4",
		"ddf network":        "a7cba57b-cc9d-47ae-8468-af1e20b6f61a",
		"dane jones":         "046ea0c9-c1ae-47bf-9a7e-ba27706da1c7",
		"dorcel":             "21397546-2256-4ed9-9a5b-08c10ca56e3e",
		"archangel":          "c1cf3e96-6324-4d61-8114-a42f12aef348",
	}
	for name, id := range fallbackStudioNameToID {
		newStudioNameToID[strings.ToLower(name)] = id
	}

	currentParents := parentIDs

	// Query StashDB recursively up to 3 levels
	for level := 1; level <= 3; level++ {
		if len(currentParents) == 0 {
			break
		}

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

			bodyBytes, err := json.Marshal(body)
			if err != nil {
				break
			}

			req, err := http.NewRequest("POST", "https://stashdb.org/graphql", strings.NewReader(string(bodyBytes)))
			if err != nil {
				break
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("ApiKey", s.apiKey)

			resp, err := s.client.Do(req)
			if err != nil {
				log.WithError(err).Warn("StashDB queryStudios request failed")
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
			err = json.NewDecoder(resp.Body).Decode(&qResp)
			resp.Body.Close()

			if err != nil {
				break
			}

			studios := qResp.Data.QueryStudios.Studios
			if len(studios) == 0 {
				break
			}

			for _, st := range studios {
				if resolvedIDs[st.ID] {
					continue
				}
				if !excludeRx.MatchString(st.Name) {
					resolvedIDs[st.ID] = true
					newStudioNameToID[strings.ToLower(st.Name)] = st.ID
					nextParents = append(nextParents, st.ID)
				}
			}

			if len(studios) < 500 {
				break
			}
			page++
		}
		currentParents = nextParents
	}

	var allChildIDs []string
	for id := range resolvedIDs {
		allChildIDs = append(allChildIDs, id)
	}

	s.resolvedStudiosLock.Lock()
	if len(allChildIDs) > len(parentIDs) {
		s.resolvedStudioIDs = allChildIDs
		s.resolvedStudioNameToID = newStudioNameToID
		log.Infof("Successfully resolved %d straight mainstream studios recursively from StashDB dynamically!", len(allChildIDs))
	} else {
		s.resolvedStudioIDs = fallbackStudioIDs
		// Also restore basic fallback name mappings
		s.resolvedStudioNameToID = make(map[string]string)
		for name, id := range fallbackStudioNameToID {
			s.resolvedStudioNameToID[strings.ToLower(name)] = id
		}
		log.Infof("StashDB query failed or returned no children; falling back to pre-resolved studios")
	}
	s.resolvedStudiosLock.Unlock()
}

func (s *Service) FindStudioIDByName(ctx context.Context, name string) (string, error) {
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

	req, err := http.NewRequestWithContext(ctx, "POST", "https://stashdb.org/graphql", strings.NewReader(string(bodyBytes)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ApiKey", s.apiKey)

	resp, err := s.client.Do(req)
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
		return studios[0].ID, nil
	}

	return "", fmt.Errorf("studio not found: %s", name)
}

func (s *Service) FindPerformerIDByName(ctx context.Context, name string) (string, error) {
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

	req, err := http.NewRequestWithContext(ctx, "POST", "https://stashdb.org/graphql", strings.NewReader(string(bodyBytes)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ApiKey", s.apiKey)

	resp, err := s.client.Do(req)
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

	// Prefer FEMALE performers; skip MALE-only results (avoids gay/trans noise)
	for _, p := range qResp.Data.QueryPerformers.Performers {
		if strings.EqualFold(p.Gender, "FEMALE") || strings.EqualFold(p.Gender, "TRANSGENDER_FEMALE") {
			return p.ID, nil
		}
	}
	// Accept any gender match if no female found (e.g. male performers in straight scenes)
	for _, p := range qResp.Data.QueryPerformers.Performers {
		if p.ID != "" {
			return p.ID, nil
		}
	}
	return "", fmt.Errorf("performer not found: %s", name)
}

type StashDBSceneListItem struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Date    string `json:"date"`
	Details string `json:"details"`
	Studio  struct {
		Name string `json:"name"`
	} `json:"studio"`
	Images []struct {
		URL string `json:"url"`
	} `json:"images"`
}

func (s *Service) FetchStashDBScenes(ctx context.Context, sortBy string, page int, searchQuery string) ([]tpdb.TpdbScene, error) {
	if s.apiKey == "" {
		return nil, fmt.Errorf("STASHDB_API_KEY not configured")
	}

	studios := s.GetResolvedStudioIDs()

	sortEnum := "DATE"
	direction := "DESC"
	if sortBy == "trending" || sortBy == "top-rated" {
		sortEnum = "TRENDING"
	}

	query := `query GetScenes($studios: MultiIDCriterionInput, $performers: MultiIDCriterionInput, $page: Int!, $sort: SceneSortEnum!, $direction: SortDirectionEnum!, $text: String) {
		queryScenes(input: { page: $page, per_page: 60, sort: $sort, direction: $direction, studios: $studios, performers: $performers, text: $text }) {
			scenes {
				id
				title
				date
				details
				studio {
					name
				}
				images {
					url
				}
			}
		}
	}`

	var searchStudioID string
	if searchQuery != "" {
		processedQuery := strings.ToLower(strings.TrimSpace(searchQuery))
		squash := func(str string) string { return strings.ReplaceAll(str, " ", "") }
		squashedQuery := squash(processedQuery)

		resolvedNameToID := s.GetResolvedStudioNameToID()
		if id, ok := resolvedNameToID[processedQuery]; ok {
			searchStudioID = id
		} else {
			for name, id := range resolvedNameToID {
				if name == processedQuery ||
					strings.Contains(processedQuery, name) ||
					strings.Contains(name, processedQuery) ||
					squash(name) == squashedQuery {
					searchStudioID = id
					break
				}
			}
		}

		if searchStudioID == "" {
			// Query StashDB dynamically
			id, err := s.FindStudioIDByName(ctx, processedQuery)
			if err == nil && id != "" {
				searchStudioID = id
				s.resolvedStudiosLock.Lock()
				s.resolvedStudioNameToID[processedQuery] = id
				s.resolvedStudiosLock.Unlock()
			}
		}
	}

	var searchPerformerID string
	if searchQuery != "" && searchStudioID == "" {
		if pid, err := s.FindPerformerIDByName(ctx, strings.ToLower(strings.TrimSpace(searchQuery))); err == nil && pid != "" {
			searchPerformerID = pid
		}
	}

	vars := map[string]interface{}{
		"page":      page,
		"sort":      sortEnum,
		"direction": direction,
	}
	if searchStudioID != "" {
		vars["studios"] = map[string]interface{}{
			"value":    []string{searchStudioID},
			"modifier": "INCLUDES",
		}
	} else if searchPerformerID != "" {
		vars["performers"] = map[string]interface{}{
			"value":    []string{searchPerformerID},
			"modifier": "INCLUDES",
		}
		vars["studios"] = map[string]interface{}{
			"value":    studios,
			"modifier": "INCLUDES",
		}
	} else if searchQuery != "" {
		vars["text"] = strings.ToLower(strings.TrimSpace(searchQuery))
		vars["studios"] = map[string]interface{}{
			"value":    studios,
			"modifier": "INCLUDES",
		}
	} else {
		vars["studios"] = map[string]interface{}{
			"value":    studios,
			"modifier": "INCLUDES",
		}
	}

	body := map[string]interface{}{
		"query":     query,
		"variables": vars,
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://stashdb.org/graphql", strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ApiKey", s.apiKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("stashdb status: %d", resp.StatusCode)
	}

	type QueryScenesResp struct {
		Data struct {
			QueryScenes struct {
				Scenes []StashDBSceneListItem `json:"scenes"`
			} `json:"queryScenes"`
		} `json:"data"`
	}

	var qResp QueryScenesResp
	if err := json.NewDecoder(resp.Body).Decode(&qResp); err != nil {
		return nil, err
	}

	scenes := qResp.Data.QueryScenes.Scenes
	tpdbScenes := make([]tpdb.TpdbScene, len(scenes))
	for i, sc := range scenes {
		poster := ""
		if len(sc.Images) > 0 {
			poster = sc.Images[0].URL
		}
		tpdbScenes[i] = tpdb.TpdbScene{
			ID:          sc.ID,
			Title:       sc.Title,
			Description: sc.Details,
			Date:        sc.Date,
			Poster:      poster,
			Site: &tpdb.TpdbSceneSite{
				Name: sc.Studio.Name,
			},
		}
	}

	return tpdbScenes, nil
}

func (s *Service) FetchStashDBSingleScene(ctx context.Context, id string) (*tpdb.TpdbScene, error) {
	if s.apiKey == "" {
		return nil, fmt.Errorf("STASHDB_API_KEY not configured")
	}

	query := `query ($id: ID!) {
		findScene(id: $id) {
			id
			title
			release_date
			details
			studio {
				name
				parent {
					name
				}
			}
			performers {
				performer {
					name
				}
			}
			images {
				url
			}
		}
	}`

	body := map[string]interface{}{
		"query": query,
		"variables": map[string]interface{}{
			"id": id,
		},
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://stashdb.org/graphql", strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ApiKey", s.apiKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("stashdb graphql status: %d", resp.StatusCode)
	}

	type FindSceneResp struct {
		Data struct {
			FindScene *struct {
				ID          string `json:"id"`
				Title       string `json:"title"`
				ReleaseDate string `json:"release_date"`
				Details     string `json:"details"`
				Studio      struct {
					Name   string `json:"name"`
					Parent *struct {
						Name string `json:"name"`
					} `json:"parent"`
				} `json:"studio"`
				Performers []struct {
					Performer struct {
						Name string `json:"name"`
					} `json:"performer"`
				} `json:"performers"`
				Images []struct {
					URL string `json:"url"`
				} `json:"images"`
			} `json:"findScene"`
		} `json:"data"`
	}

	var fResp FindSceneResp
	if err := json.NewDecoder(resp.Body).Decode(&fResp); err != nil {
		return nil, err
	}

	fs := fResp.Data.FindScene
	if fs == nil {
		return nil, fmt.Errorf("scene not found in stashdb: %s", id)
	}

	poster := ""
	if len(fs.Images) > 0 {
		poster = fs.Images[0].URL
	}

	var performers []tpdb.TpdbScenePerformer
	for _, p := range fs.Performers {
		if p.Performer.Name != "" {
			performers = append(performers, tpdb.TpdbScenePerformer{
				Name: p.Performer.Name,
			})
		}
	}

	parentName := ""
	if fs.Studio.Parent != nil {
		parentName = strings.TrimSpace(fs.Studio.Parent.Name)
	}

	return &tpdb.TpdbScene{
		ID:          fs.ID,
		Title:       fs.Title,
		Description: fs.Details,
		Date:        fs.ReleaseDate,
		Poster:      poster,
		Site: &tpdb.TpdbSceneSite{
			Name:   fs.Studio.Name,
			Parent: parentName,
		},
		Performers: performers,
	}, nil
}
