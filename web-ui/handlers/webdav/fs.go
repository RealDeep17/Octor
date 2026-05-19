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
	children := map[string]webdav.FileSystem{
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
		"admin": NewAdminDirectory(pg, sapi, jobs, admin),
	}
	return &DebugDirectory{
		Inner: &PrefixDirectory{
			Separator: sep,
			Inner: &RootDirectory{
				Admin:    admin,
				Children: children,
			},
		},
	}
}
