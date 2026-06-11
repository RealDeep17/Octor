package webdav

import (
	"context"
	"io"
	"net/url"
	"strings"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	services "github.com/webtor-io/common-services"
	j "github.com/webtor-io/web-ui/jobs"
	"github.com/webtor-io/web-ui/models"
	adminsvc "github.com/webtor-io/web-ui/services/admin"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/webdav"
)

type AllUsersLibrary struct{}

func (s *AllUsersLibrary) GetContent(ctx context.Context, db *pg.DB, _ uuid.UUID) ([]*models.Library, error) {
	return models.GetLibraryTorrentsListAll(ctx, db, models.SortTypeName, "")
}

type AllUsersMovieLibrary struct{}

func (s *AllUsersMovieLibrary) GetContent(ctx context.Context, db *pg.DB, _ uuid.UUID) ([]*models.Library, error) {
	return models.GetLibraryMovieTorrentListAll(ctx, db, models.SortTypeName, "")
}

type AllUsersSeriesLibrary struct{}

func (s *AllUsersSeriesLibrary) GetContent(ctx context.Context, db *pg.DB, _ uuid.UUID) ([]*models.Library, error) {
	return models.GetLibrarySeriesTorrentListAll(ctx, db, models.SortTypeName, "")
}

type AllUsersAdultLibrary struct{}

func (s *AllUsersAdultLibrary) GetContent(ctx context.Context, db *pg.DB, _ uuid.UUID) ([]*models.Library, error) {
	return models.GetLibraryAdultTorrentListAll(ctx, db, models.SortTypeName, "")
}

func NewAdminDirectory(pg *services.PG, sapi *api.Api, jobs *j.Jobs, admin *adminsvc.Admin) webdav.FileSystem {
	td := &TorrentDirectory{api: sapi}
	return &RootDirectory{
		Admin: admin,
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
			"users": &AdminUsersDirectory{pg: pg, api: sapi, jobs: jobs, admin: admin},
		},
	}
}

// AdminUsersDirectory lists all users and lets the admin browse each user's files.
// The admin field is used to determine whether to show adult content for a given user
// (admin users browsing their own entry see adult; normal users browsed by admin do not).
type AdminUsersDirectory struct {
	BaseDirectory
	pg    *services.PG
	api   *api.Api
	jobs  *j.Jobs
	admin *adminsvc.Admin
}

func (s *AdminUsersDirectory) Open(ctx context.Context, path string) (io.ReadCloser, *url.URL, error) {
	fs, newPath, err := s.userFileSystem(ctx, path)
	if err != nil {
		return nil, nil, err
	}
	return fs.Open(ctx, newPath)
}

func (s *AdminUsersDirectory) Stat(ctx context.Context, path string) (*webdav.FileInfo, error) {
	if isRoot(path) {
		fi := newDirectoryFileInfo("/")
		return &fi, nil
	}
	fs, newPath, err := s.userFileSystem(ctx, path)
	if err != nil {
		return nil, err
	}
	if isRoot(newPath) {
		email := strings.SplitN(strings.Trim(path, "/"), "/", 2)[0]
		fi := newDirectoryFileInfo(email)
		return &fi, nil
	}
	fi, err := fs.Stat(ctx, newPath)
	if err != nil {
		return nil, err
	}
	parts := strings.SplitN(strings.Trim(path, "/"), "/", 2)
	return addPrefix(fi, "/"+parts[0]+"/"), nil
}

func (s *AdminUsersDirectory) ReadDir(ctx context.Context, path string, recursive bool) ([]webdav.FileInfo, error) {
	if isRoot(path) {
		users, err := s.users(ctx)
		if err != nil {
			return nil, err
		}
		fis := make([]webdav.FileInfo, 0, len(users))
		for _, u := range users {
			fis = append(fis, newDirectoryFileInfo(u.Email))
		}
		return fis, nil
	}
	fs, newPath, err := s.userFileSystem(ctx, path)
	if err != nil {
		return nil, err
	}
	fis, err := fs.ReadDir(ctx, newPath, recursive)
	if err != nil {
		return nil, err
	}
	parts := strings.SplitN(strings.Trim(path, "/"), "/", 2)
	return addPrefixes(fis, "/"+parts[0]+"/"), nil
}

func (s *AdminUsersDirectory) Create(ctx context.Context, path string, body io.ReadCloser, opts *webdav.CreateOptions) (*webdav.FileInfo, bool, error) {
	fs, newPath, err := s.userFileSystem(ctx, path)
	if err != nil {
		return nil, false, err
	}
	fi, ok, err := fs.Create(ctx, newPath, body, opts)
	if err != nil {
		return nil, false, err
	}
	parts := strings.SplitN(strings.Trim(path, "/"), "/", 2)
	return addPrefix(fi, "/"+parts[0]+"/"), ok, nil
}

func (s *AdminUsersDirectory) RemoveAll(ctx context.Context, path string, opts *webdav.RemoveAllOptions) error {
	fs, newPath, err := s.userFileSystem(ctx, path)
	if err != nil {
		return err
	}
	return fs.RemoveAll(ctx, newPath, opts)
}

func (s *AdminUsersDirectory) Move(ctx context.Context, path, dest string, options *webdav.MoveOptions) (bool, error) {
	fs, newPath, err := s.userFileSystem(ctx, path)
	if err != nil {
		return false, err
	}
	parts := strings.SplitN(strings.Trim(path, "/"), "/", 2)
	destParts := strings.SplitN(strings.Trim(dest, "/"), "/", 2)
	if len(parts) == 0 || len(destParts) == 0 || parts[0] != destParts[0] {
		return false, webdav.NewHTTPError(403, errors.New("moving between users is not permitted"))
	}
	newDest := "/"
	if len(destParts) == 2 {
		newDest = "/" + destParts[1]
	}
	return fs.Move(ctx, newPath, newDest, options)
}

func (s *AdminUsersDirectory) users(ctx context.Context) ([]models.User, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("db is nil")
	}
	var users []models.User
	if err := db.Model(&users).Context(ctx).Order("email ASC").Select(); err != nil {
		return nil, errors.Wrap(err, "failed to load users")
	}
	return users, nil
}

func (s *AdminUsersDirectory) userByEmail(ctx context.Context, email string) (*models.User, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("db is nil")
	}
	var user models.User
	err := db.Model(&user).Context(ctx).Where("email = ?", email).Limit(1).Select()
	if err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			return nil, webdav.NewHTTPError(404, errors.New("user not found"))
		}
		return nil, errors.Wrap(err, "failed to load user")
	}
	return &user, nil
}

func (s *AdminUsersDirectory) userFileSystem(ctx context.Context, path string) (webdav.FileSystem, string, error) {
	parts := strings.SplitN(strings.Trim(path, "/"), "/", 2)
	if parts[0] == "" {
		return nil, "", webdav.NewHTTPError(404, errors.New("user not found"))
	}
	user, err := s.userByEmail(ctx, parts[0])
	if err != nil {
		return nil, "", err
	}
	newPath := "/"
	if len(parts) == 2 {
		newPath = "/" + parts[1]
	}
	// Give admin users browsing another user's subtree the adult folder only if
	// that target user is themselves an admin.
	isTargetAdmin := s.admin != nil && s.admin.IsAdminEmail(user.Email)
	return newUserScopedRoot(s.pg, s.api, s.jobs, user.UserID, isTargetAdmin), newPath, nil
}

// newUserScopedRoot builds the per-user filesystem for use inside users/<email>/.
// includeAdult controls whether the adult folder is exposed.
func newUserScopedRoot(pg *services.PG, sapi *api.Api, jobs *j.Jobs, userID uuid.UUID, includeAdult bool) webdav.FileSystem {
	td := &TorrentDirectory{api: sapi}
	uid := userID
	children := map[string]webdav.FileSystem{
		"torrents": &TorrentLibraryDirectory{pg: pg, api: sapi, jobs: jobs, UserID: &uid},
		"all": &ContentDirectory{
			Library:          &AllLibrary{},
			TorrentDirectory: td,
			pg:               pg,
			UserID:           &uid,
		},
		"movies": &ContentDirectory{
			Library:          &MovieLibrary{},
			TorrentDirectory: td,
			pg:               pg,
			UserID:           &uid,
		},
		"tvseries": &ContentDirectory{
			Library:          &SeriesLibrary{},
			TorrentDirectory: td,
			pg:               pg,
			UserID:           &uid,
		},
	}
	if includeAdult {
		children["adult"] = &ContentDirectory{
			Library:          &AdultLibrary{},
			TorrentDirectory: td,
			pg:               pg,
			UserID:           &uid,
		}
	}
	return &RootDirectory{Children: children}
}

var _ webdav.FileSystem = (*AdminUsersDirectory)(nil)
