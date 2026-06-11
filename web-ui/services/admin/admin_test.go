package admin

import (
	"flag"
	"testing"

	uuid "github.com/satori/go.uuid"
	"github.com/urfave/cli"
	"github.com/webtor-io/web-ui/services/auth"
)

func testContext(value string) *cli.Context {
	app := cli.NewApp()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.String(EmailsFlag, "", "")
	_ = fs.Set(EmailsFlag, value)
	return cli.NewContext(app, fs, nil)
}

func TestAdminEmailMatching(t *testing.T) {
	a := New(testContext(" admin@mail.com,ADMIN2@mail.com ,, "))

	if !a.IsAdminEmail("ADMIN@mail.com") {
		t.Fatal("expected case-insensitive admin email match")
	}
	if !a.IsAdminEmail(" admin2@mail.com ") {
		t.Fatal("expected trimmed admin email match")
	}
	if a.IsAdminEmail("user@mail.com") {
		t.Fatal("unexpected admin match for normal user")
	}
}

func TestAdminUserRequiresAuthenticatedConfiguredEmail(t *testing.T) {
	a := New(testContext("admin@mail.com"))

	if a.IsAdminUser(&auth.User{Email: "admin@mail.com"}) {
		t.Fatal("expected unauthenticated email-only user to be rejected")
	}
	if !a.IsAdminUser(&auth.User{ID: uuid.NewV4(), Email: "admin@mail.com"}) {
		t.Fatal("expected authenticated configured user to be admin")
	}
	if a.IsAdminUser(&auth.User{ID: uuid.NewV4(), Email: "user@mail.com"}) {
		t.Fatal("expected authenticated unconfigured user to be rejected")
	}
}
