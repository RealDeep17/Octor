package services

import (
	"crypto/tls"
	"fmt"
	"github.com/urfave/cli"
	"net/mail"
	"net/smtp"
)

const (
	smtpHostFlag      = "smtp-host"
	smtpPortFlag      = "smtp-port"
	smtpPassFlag      = "smtp-pass"
	smtpUserFlag      = "smtp-user"
	smtpTLSFlag       = "smtp-tls"
	smtpStartTLSFlag  = "smtp-start-tls"
	smtpTLSSecureFlag = "smtp-tls-secure"
)

func RegisterSMTPFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.StringFlag{
			Name:   smtpHostFlag,
			Usage:  "smtp host",
			EnvVar: "SMTP_HOST",
		},
		cli.IntFlag{
			Name:   smtpPortFlag,
			Usage:  "smtp port",
			EnvVar: "SMTP_PORT",
		},
		cli.StringFlag{
			Name:   smtpUserFlag,
			Usage:  "smtp user",
			EnvVar: "SMTP_USER",
		},
		cli.StringFlag{
			Name:   smtpPassFlag,
			Usage:  "smtp pass",
			EnvVar: "SMTP_PASS",
		},
		cli.BoolFlag{
			Name:   smtpTLSFlag,
			Usage:  "smtp tls",
			EnvVar: "SMTP_TLS",
		},
		cli.BoolFlag{
			Name:   smtpStartTLSFlag,
			Usage:  "smtp starttls",
			EnvVar: "SMTP_STARTTLS",
		},
		cli.BoolTFlag{
			Name:   smtpTLSSecureFlag,
			Usage:  "smtp tls secure",
			EnvVar: "SMTP_TLS_SECURE",
		},
	)
}

type SMTP struct {
	host      string
	port      int
	user      string
	pass      string
	tls       bool
	startTLS  bool
	tlsSecure bool
}

func (s *SMTP) Send(from string, to string, replyTo string, subj string, body string) error {
	fromAddr := mail.Address{Address: from}
	toAddr := mail.Address{Address: to}

	// Setup headers
	headers := make(map[string]string)
	headers["From"] = fromAddr.String()
	headers["To"] = toAddr.String()
	if replyTo != "" {
		headers["Reply-To"] = replyTo
	}
	headers["Subject"] = subj

	// Setup message
	message := ""
	for k, v := range headers {
		message += fmt.Sprintf("%s: %s\r\n", k, v)
	}
	message += "\r\n" + body

	// Connect to the SMTP Server
	servername := fmt.Sprintf("%v:%v", s.host, s.port)

	auth := smtp.PlainAuth("", s.user, s.pass, s.host)
	var c *smtp.Client
	var err error

	if s.tls {
		// TLS config
		tlsConfig := &tls.Config{
			InsecureSkipVerify: !s.tlsSecure,
			ServerName:         s.host,
		}

		// Here is the key, you need to call tls.Dial instead of smtp.Dial
		// for smtp servers running on 465 that require an ssl connection
		// from the very beginning (no starttls)
		conn, err := tls.Dial("tcp", servername, tlsConfig)
		if err != nil {
			return err
		}

		c, err = smtp.NewClient(conn, s.host)
		if err != nil {
			return err
		}
	} else {
		c, err = smtp.Dial(servername)
		if err != nil {
			return err
		}
		if s.startTLS {
			tlsConfig := &tls.Config{
				InsecureSkipVerify: !s.tlsSecure,
				ServerName:         s.host,
			}
			if err = c.StartTLS(tlsConfig); err != nil {
				return err
			}
		}
	}
	if c == nil {
		return fmt.Errorf("smtp: failed to connect")
	}

	// Auth
	if err = c.Auth(auth); err != nil {
		return err
	}

	// To && From
	if err = c.Mail(fromAddr.Address); err != nil {
		return err
	}

	if err = c.Rcpt(toAddr.Address); err != nil {
		return err
	}

	// Data
	w, err := c.Data()
	if err != nil {
		return err
	}

	_, err = w.Write([]byte(message))
	if err != nil {
		return err
	}

	err = w.Close()
	if err != nil {
		return err
	}

	return c.Quit()
}

func NewSMTP(c *cli.Context) *SMTP {
	return &SMTP{
		host:      c.String(smtpHostFlag),
		port:      c.Int(smtpPortFlag),
		user:      c.String(smtpUserFlag),
		pass:      c.String(smtpPassFlag),
		tls:       c.Bool(smtpTLSFlag),
		startTLS:  c.Bool(smtpStartTLSFlag),
		tlsSecure: c.Bool(smtpTLSSecureFlag),
	}
}
