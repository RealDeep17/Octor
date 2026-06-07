package tpdb

type TpdbSceneBackground struct {
	Full string `json:"full"`
}

type TpdbSceneSite struct {
	Name string `json:"name"`
}

type TpdbScenePerformer struct {
	Name string `json:"name"`
}

type TpdbScene struct {
	ID          string                 `json:"id"`
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	Date        string                 `json:"date"`
	Rating      *float64               `json:"rating"`
	Duration    *float64               `json:"duration"`
	Image       string                 `json:"image"`
	Poster      string                 `json:"poster"`
	PosterImage string                 `json:"poster_image"`
	Background  *TpdbSceneBackground   `json:"background"`
	Site        *TpdbSceneSite         `json:"site"`
	Performers  []TpdbScenePerformer   `json:"performers"`
}

type TpdbResponse struct {
	Data []TpdbScene `json:"data"`
}

type TpdbSingleResponse struct {
	Data TpdbScene `json:"data"`
}
