package shared

import "github.com/webtor-io/web-ui/models"

// WatchedFilter is a tri-state filter for movie/series listings in the library:
// "" (all), "unwatched" (exclude watched), "watched" (only watched).
type WatchedFilter string

const (
	WatchedFilterAll       WatchedFilter = ""
	WatchedFilterUnwatched WatchedFilter = "unwatched"
	WatchedFilterWatched   WatchedFilter = "watched"
	WatchedFilterVaulted   WatchedFilter = "vaulted"
)

type GroupBy string

const (
	GroupByNone      GroupBy = ""
	GroupByStudio    GroupBy = "studio"
	GroupByPerformer GroupBy = "performer"
)

type IndexArgs struct {
	Sort    models.SortType
	Section SectionType
	Watched WatchedFilter
	Query   string
	IsAdmin bool
	GroupBy GroupBy
	Subtype string
}
