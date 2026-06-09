package shared

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/webtor-io/web-ui/models"
)

type TorrentNode struct {
	Name     string          `json:"name"`
	Type     string          `json:"type"` // "folder" or "item"
	Category string          `json:"category,omitempty"` // "movie", "series", "adult", "other"
	Item     *models.Library `json:"item,omitempty"`
	Children []*TorrentNode  `json:"children,omitempty"`
}

func (n *TorrentNode) ItemCount() int {
	if n.Type == "item" {
		return 1
	}
	count := 0
	for _, child := range n.Children {
		count += child.ItemCount()
	}
	return count
}

func GetTorrentCategory(lib *models.Library) (category string, groupKey string, groupName string) {
	isAdult := false
	isJAV := false

	if lib.MediaInfo != nil {
		for _, m := range lib.MediaInfo.Movies {
			hasAdultID := false
			if m.MovieMetadata != nil {
				vid := m.MovieMetadata.VideoID
				if strings.HasPrefix(vid, "tpdb:") || strings.HasPrefix(vid, "tpdb_jav:") || strings.HasPrefix(vid, "stash:") {
					hasAdultID = true
					if strings.HasPrefix(vid, "tpdb_jav:") {
						isJAV = true
					}
				}
			}
			hasAdultPath := false
			if m.Path != nil {
				lowerPath := strings.ToLower(*m.Path)
				if strings.Contains(lowerPath, "jav") {
					isJAV = true
					isAdult = true
				}
				keywords := []string{"porn", "adult", "xxx", "brazzers", "bangbros", "hentai", "slut", "pornstar", "nude"}
				for _, kw := range keywords {
					if strings.Contains(lowerPath, kw) {
						hasAdultPath = true
						break
					}
				}
			}
			if hasAdultID || hasAdultPath {
				isAdult = true
			}
		}
	}

	if isAdult {
		if isJAV {
			return "adult_jav", "", ""
		}
		return "adult_porn", "", ""
	}

	if lib.MediaInfo != nil && len(lib.MediaInfo.SeriesList) > 0 {
		series := lib.MediaInfo.SeriesList[0]
		groupKey := "series_unknown"
		groupName := "Unknown Series"
		if series.SeriesMetadata != nil {
			if series.SeriesMetadata.VideoID != "" {
				groupKey = series.SeriesMetadata.VideoID
			} else if series.SeriesMetadata.Title != "" {
				groupKey = "title:" + series.SeriesMetadata.Title
			}
			if series.SeriesMetadata.Title != "" {
				groupName = series.SeriesMetadata.Title
				if series.SeriesMetadata.Year != nil && *series.SeriesMetadata.Year > 0 {
					groupName = fmt.Sprintf("%s (%d)", series.SeriesMetadata.Title, *series.SeriesMetadata.Year)
				}
			}
		} else if series.Title != "" {
			groupKey = "title:" + series.Title
			groupName = series.Title
		} else {
			groupKey = "title:" + lib.Name
			groupName = lib.Name
		}
		return "series", groupKey, groupName
	}

	if lib.MediaInfo != nil && len(lib.MediaInfo.Movies) > 0 {
		movie := lib.MediaInfo.Movies[0]
		groupKey := "movie_unknown"
		groupName := "Unknown Movie"
		if movie.MovieMetadata != nil {
			if movie.MovieMetadata.VideoID != "" {
				groupKey = movie.MovieMetadata.VideoID
			} else if movie.MovieMetadata.Title != "" {
				groupKey = "title:" + movie.MovieMetadata.Title
			}
			if movie.MovieMetadata.Title != "" {
				groupName = movie.MovieMetadata.Title
				if movie.MovieMetadata.Year != nil && *movie.MovieMetadata.Year > 0 {
					groupName = fmt.Sprintf("%s (%d)", movie.MovieMetadata.Title, *movie.MovieMetadata.Year)
				}
			}
		} else if movie.Title != "" {
			groupKey = "title:" + movie.Title
			groupName = movie.Title
		} else {
			groupKey = "title:" + lib.Name
			groupName = lib.Name
		}
		return "movie", groupKey, groupName
	}

	return "other", "", ""
}

func BuildTorrentTree(ctx context.Context, db *pg.DB, list []*models.Library, isAdmin bool, sortType models.SortType) []*TorrentNode {
	// Populate series anime flags
	var seriesSlice []*models.Series
	for _, lib := range list {
		if lib.MediaInfo != nil {
			seriesSlice = append(seriesSlice, lib.MediaInfo.SeriesList...)
		}
	}
	models.PopulateSeriesAnimeFlags(ctx, db, seriesSlice)

	movieGroups := make(map[string][]*models.Library)
	movieGroupNames := make(map[string]string)
	var movieGroupOrder []string

	seriesGroups := make(map[string][]*models.Library)
	seriesGroupNames := make(map[string]string)
	var seriesGroupOrder []string

	var adultJAVList []*models.Library
	var adultPornList []*models.Library
	var otherList []*models.Library

	for _, lib := range list {
		cat, key, groupName := GetTorrentCategory(lib)
		switch cat {
		case "movie":
			if _, ok := movieGroups[key]; !ok {
				movieGroupOrder = append(movieGroupOrder, key)
				movieGroupNames[key] = groupName
			}
			movieGroups[key] = append(movieGroups[key], lib)
		case "series":
			if _, ok := seriesGroups[key]; !ok {
				seriesGroupOrder = append(seriesGroupOrder, key)
				seriesGroupNames[key] = groupName
			}
			seriesGroups[key] = append(seriesGroups[key], lib)
		case "adult_jav":
			if isAdmin {
				adultJAVList = append(adultJAVList, lib)
			}
		case "adult_porn":
			if isAdmin {
				adultPornList = append(adultPornList, lib)
			}
		default:
			otherList = append(otherList, lib)
		}
	}

	var movieChildren []*TorrentNode
	for _, key := range movieGroupOrder {
		libs := movieGroups[key]
		if len(libs) == 1 {
			movieChildren = append(movieChildren, &TorrentNode{
				Name:     movieGroupNames[key],
				Type:     "item",
				Category: "movie",
				Item:     libs[0],
			})
		} else {
			var subChildren []*TorrentNode
			for _, lib := range libs {
				subChildren = append(subChildren, &TorrentNode{
					Name:     lib.Name,
					Type:     "item",
					Category: "movie",
					Item:     lib,
				})
			}
			movieChildren = append(movieChildren, &TorrentNode{
				Name:     movieGroupNames[key],
				Type:     "folder",
				Category: "movie",
				Children: subChildren,
			})
		}
	}

	var tvShowsChildren []*TorrentNode
	var animeChildren []*TorrentNode

	for _, key := range seriesGroupOrder {
		libs := seriesGroups[key]
		isAnime := false
		if len(libs) > 0 && libs[0].MediaInfo != nil && len(libs[0].MediaInfo.SeriesList) > 0 {
			isAnime = libs[0].MediaInfo.SeriesList[0].IsAnime
		}

		var node *TorrentNode
		if len(libs) == 1 {
			node = &TorrentNode{
				Name:     seriesGroupNames[key],
				Type:     "item",
				Category: "series",
				Item:     libs[0],
			}
		} else {
			var subChildren []*TorrentNode
			for _, lib := range libs {
				subChildren = append(subChildren, &TorrentNode{
					Name:     lib.Name,
					Type:     "item",
					Category: "series",
					Item:     lib,
				})
			}
			node = &TorrentNode{
				Name:     seriesGroupNames[key],
				Type:     "folder",
				Category: "series",
				Children: subChildren,
			}
		}

		if isAnime {
			animeChildren = append(animeChildren, node)
		} else {
			tvShowsChildren = append(tvShowsChildren, node)
		}
	}

	var adultChildren []*TorrentNode
	if len(adultJAVList) > 0 {
		var javChildren []*TorrentNode
		for _, lib := range adultJAVList {
			javChildren = append(javChildren, &TorrentNode{
				Name:     lib.Name,
				Type:     "item",
				Category: "adult",
				Item:     lib,
			})
		}
		adultChildren = append(adultChildren, &TorrentNode{
			Name:     "JAV",
			Type:     "folder",
			Category: "adult",
			Children: javChildren,
		})
	}
	if len(adultPornList) > 0 {
		var pornChildren []*TorrentNode
		for _, lib := range adultPornList {
			pornChildren = append(pornChildren, &TorrentNode{
				Name:     lib.Name,
				Type:     "item",
				Category: "adult",
				Item:     lib,
			})
		}
		adultChildren = append(adultChildren, &TorrentNode{
			Name:     "Porn",
			Type:     "folder",
			Category: "adult",
			Children: pornChildren,
		})
	}

	var otherChildren []*TorrentNode
	for _, lib := range otherList {
		otherChildren = append(otherChildren, &TorrentNode{
			Name:     lib.Name,
			Type:     "item",
			Category: "other",
			Item:     lib,
		})
	}

	sortNodes(movieChildren, sortType)
	sortNodes(tvShowsChildren, sortType)
	sortNodes(animeChildren, sortType)
	sortNodes(adultChildren, sortType)
	sortNodes(otherChildren, sortType)

	var tvSeriesChildren []*TorrentNode
	if len(tvShowsChildren) > 0 {
		tvSeriesChildren = append(tvSeriesChildren, &TorrentNode{
			Name:     "TV Shows",
			Type:     "folder",
			Category: "series",
			Children: tvShowsChildren,
		})
	}
	if len(animeChildren) > 0 {
		tvSeriesChildren = append(tvSeriesChildren, &TorrentNode{
			Name:     "Anime",
			Type:     "folder",
			Category: "series",
			Children: animeChildren,
		})
	}

	var rootNodes []*TorrentNode
	if len(movieChildren) > 0 {
		rootNodes = append(rootNodes, &TorrentNode{
			Name:     "Movies",
			Type:     "folder",
			Category: "movie",
			Children: movieChildren,
		})
	}
	if len(tvSeriesChildren) > 0 {
		rootNodes = append(rootNodes, &TorrentNode{
			Name:     "Series",
			Type:     "folder",
			Category: "series",
			Children: tvSeriesChildren,
		})
	}
	if isAdmin && len(adultChildren) > 0 {
		rootNodes = append(rootNodes, &TorrentNode{
			Name:     "Adult",
			Type:     "folder",
			Category: "adult",
			Children: adultChildren,
		})
	}
	if len(otherChildren) > 0 {
		rootNodes = append(rootNodes, &TorrentNode{
			Name:     "Others",
			Type:     "folder",
			Category: "other",
			Children: otherChildren,
		})
	}

	return rootNodes
}

func getNodeCreatedAt(node *TorrentNode) time.Time {
	if node.Type == "item" && node.Item != nil {
		return node.Item.CreatedAt
	}
	var maxTime time.Time
	for _, child := range node.Children {
		childTime := getNodeCreatedAt(child)
		if childTime.After(maxTime) {
			maxTime = childTime
		}
	}
	return maxTime
}

func sortNodes(nodes []*TorrentNode, sortType models.SortType) {
	for _, node := range nodes {
		if node.Type == "folder" && len(node.Children) > 0 {
			sortNodes(node.Children, sortType)
		}
	}

	if sortType == models.SortTypeName {
		sort.SliceStable(nodes, func(i, j int) bool {
			nameI := strings.ToLower(nodes[i].Name)
			nameJ := strings.ToLower(nodes[j].Name)
			return nameI < nameJ
		})
	} else {
		sort.SliceStable(nodes, func(i, j int) bool {
			timeI := getNodeCreatedAt(nodes[i])
			timeJ := getNodeCreatedAt(nodes[j])
			return timeI.After(timeJ)
		})
	}
}
