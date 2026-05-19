package webdav

import (
	"context"
	"flag"
	"testing"

	uuid "github.com/satori/go.uuid"
	"github.com/urfave/cli"
	adminsvc "github.com/webtor-io/web-ui/services/admin"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/web"
	webdavsvc "github.com/webtor-io/web-ui/services/webdav"
)

func testAdmin(value string) *adminsvc.Admin {
	app := cli.NewApp()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.String(adminsvc.EmailsFlag, "", "")
	_ = fs.Set(adminsvc.EmailsFlag, value)
	return adminsvc.New(cli.NewContext(app, fs, nil))
}

func testContext(email string) context.Context {
	return context.WithValue(context.Background(), web.Context{}, &web.Context{
		User: &auth.User{ID: uuid.NewV4(), Email: email},
	})
}

func TestRootDirectoryHidesAdminForNormalUsers(t *testing.T) {
	root := &RootDirectory{
		Admin: testAdmin("admin@mail.com"),
		Children: map[string]webdavsvc.FileSystem{
			"torrents": &BaseDirectory{},
			"admin":    &BaseDirectory{},
		},
	}

	fis, err := root.ReadDir(testContext("user@mail.com"), "/", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, fi := range fis {
		if fi.Path == "admin/" || fi.Path == "/admin/" {
			t.Fatal("normal user should not see admin directory")
		}
	}
	if root.getChild(testContext("user@mail.com"), "/admin/torrents") != nil {
		t.Fatal("normal user should not resolve admin child")
	}
}

func TestRootDirectoryShowsAdminForConfiguredAdmins(t *testing.T) {
	root := &RootDirectory{
		Admin: testAdmin("admin@mail.com"),
		Children: map[string]webdavsvc.FileSystem{
			"torrents": &BaseDirectory{},
			"admin":    &BaseDirectory{},
		},
	}

	fis, err := root.ReadDir(testContext("admin@mail.com"), "/", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, fi := range fis {
		if fi.Path == "admin/" || fi.Path == "/admin/" {
			return
		}
	}
	t.Fatal("admin user should see admin directory")
}
