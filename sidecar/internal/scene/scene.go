package scene

type Scene struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	Date        string      `json:"date"`
	Site        string      `json:"site"`
	Parent      string      `json:"parent"`
	Network     string      `json:"network"`
	Description string      `json:"description"`
	Performers  []Performer `json:"performers"`
	Tags        []string    `json:"tags"`
	Poster      string      `json:"poster"`
	Duration    *float64    `json:"duration"`
	Rating      *float64    `json:"rating"`
	URL         string      `json:"url"`
	Source      string      `json:"_source"` // "tpdb" | "tpdb_jav" | "stashdb"
	ExternalID  string      `json:"external_id,omitempty"`
}

type Performer struct {
	Name string `json:"name"`
}

type OmdbResponse struct {
	Title      string   `json:"Title"`
	Year       string   `json:"Year"`
	Rated      string   `json:"Rated"`
	Released   string   `json:"Released"`
	Runtime    string   `json:"Runtime"`
	Genre      string   `json:"Genre"`
	Director   string   `json:"Director"`
	Actors     string   `json:"Actors"`
	Plot       string   `json:"Plot"`
	Language   string   `json:"Language"`
	Country    string   `json:"Country"`
	Awards     string   `json:"Awards"`
	Poster     string   `json:"Poster"`
	Ratings    []any    `json:"Ratings"`
	Metascore  string   `json:"Metascore"`
	ImdbRating string   `json:"imdbRating"`
	ImdbVotes  string   `json:"imdbVotes"`
	ImdbID     string   `json:"imdbID"`
	Type       string   `json:"Type"`
	DVD        string   `json:"DVD"`
	BoxOffice  string   `json:"BoxOffice"`
	Production string   `json:"Production"`
	Website    string   `json:"Website"`
	Response   string   `json:"Response"`
}
