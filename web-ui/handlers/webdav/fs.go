package webdav

import (
	"os"
	"path/filepath"

	services "github.com/webtor-io/common-services"
	j "github.com/webtor-io/web-ui/jobs"
	adminsvc "github.com/webtor-io/web-ui/services/admin"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/webdav"
)

func getInfraDataPath(subpath string) string {
	if root := os.Getenv("OCTOR_ROOT"); root != "" {
		return filepath.Join(root, "infra-data", subpath)
	}
	if root := os.Getenv("PROJECT_ROOT"); root != "" {
		return filepath.Join(root, "infra-data", subpath)
	}
	if _, err := os.Stat("/srv/octor"); err == nil {
		return filepath.Join("/srv/octor/infra-data", subpath)
	}
	return filepath.Join("./infra-data", subpath)
}

func NewFileSystem(pg *services.PG, sapi *api.Api, jobs *j.Jobs, sep string, admin *adminsvc.Admin) webdav.FileSystem {
	td := &TorrentDirectory{
		api: sapi,
	}

	// userChildren: standard folders for a regular (non-admin) user — no adult folder.
	userChildren := map[string]webdav.FileSystem{
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

	// adminPersonalChildren: like userChildren but also includes the admin's own adult folder.
	adminPersonalChildren := map[string]webdav.FileSystem{
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
		"adult": &ContentDirectory{
			Library:          &AdultLibrary{},
			TorrentDirectory: td,
			pg:               pg,
		},
	}

	var root webdav.FileSystem
	if admin != nil {
		// adminChildren: shown only when the requesting user IS an admin.
		adminChildren := map[string]webdav.FileSystem{
			"my": &RootDirectory{Children: adminPersonalChildren},
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
					"adult": &ContentDirectory{
						Library:          &AllUsersAdultLibrary{},
						TorrentDirectory: td,
						pg:               pg,
						AllUsers:         true,
					},
				},
			},
			"users": &AdminUsersDirectory{pg: pg, api: sapi, jobs: jobs, admin: admin},
			"drive": &LocalDirectory{Root: getInfraDataPath("drive-mount-vfs")},
		}

		// DualRootDirectory: serves adminChildren for admin users, userChildren for everyone else.
		root = &DualRootDirectory{
			Admin:         admin,
			AdminChildren: adminChildren,
			UserChildren:  userChildren,
		}
	} else {
		// No admin service configured: everyone gets the plain user tree.
		root = &RootDirectory{
			Children: userChildren,
		}
	}

	return &DebugDirectory{
		Inner: &PrefixDirectory{
			Separator: sep,
			Inner:     root,
		},
	}
}
