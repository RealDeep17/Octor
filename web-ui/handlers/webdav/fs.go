package webdav

import (
	services "github.com/webtor-io/common-services"
	j "github.com/webtor-io/web-ui/jobs"
	adminsvc "github.com/webtor-io/web-ui/services/admin"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/webdav"
)

func NewFileSystem(pg *services.PG, sapi *api.Api, jobs *j.Jobs, sep string, admin *adminsvc.Admin) webdav.FileSystem {
	td := &TorrentDirectory{
		api: sapi,
	}
	// personalChildren defines the standard folders for a regular user
	personalChildren := map[string]webdav.FileSystem{
		"torrents": &TorrentLibraryDirectory{
			pg:   pg,
			api:  sapi,
			jobs: jobs,
		},
		"all": &ContentDirectory{
			Library:          &AllLibrary{},
			TorrentDirectory: td,
			pg:               pg,
		},
		"movies": &ContentDirectory{
			Library:          &MovieLibrary{},
			TorrentDirectory: td,
			pg:               pg,
		},
		"series": &ContentDirectory{
			Library:          &SeriesLibrary{},
			TorrentDirectory: td,
			pg:               pg,
		},
	}

	var root webdav.FileSystem
	if admin != nil {
		// Admin Root: see everything as folders at the top level
		// We use separate maps to avoid recursion
		adminChildren := map[string]webdav.FileSystem{
			"my": &RootDirectory{Children: personalChildren},
			"system": &RootDirectory{
				Children: map[string]webdav.FileSystem{
					"torrents": &TorrentLibraryDirectory{pg: pg, api: sapi, jobs: jobs, AllUsers: true},
					"all": &ContentDirectory{
						Library:          &AllUsersLibrary{},
						TorrentDirectory: td,
						pg:               pg,
						AllUsers:         true,
					},
					"movies": &ContentDirectory{
						Library:          &AllUsersMovieLibrary{},
						TorrentDirectory: td,
						pg:               pg,
						AllUsers:         true,
					},
					"tvseries": &ContentDirectory{
						Library:          &AllUsersSeriesLibrary{},
						TorrentDirectory: td,
						pg:               pg,
						AllUsers:         true,
					},
				},
			},
			"users": &AdminUsersDirectory{pg: pg, api: sapi, jobs: jobs},
			"drive": &LocalDirectory{Root: "/srv/octor/infra-data/drive-mount-vfs"},
		}
		root = &RootDirectory{
			Admin:    admin,
			Children: adminChildren,
		}
	} else {
		// User Root: see only their personal folders
		root = &RootDirectory{
			Children: personalChildren,
		}
	}

	return &DebugDirectory{
		Inner: &PrefixDirectory{
			Separator: sep,
			Inner:     root,
		},
	}
}
