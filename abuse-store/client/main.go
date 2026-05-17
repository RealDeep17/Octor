package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/urfave/cli"
	pb "github.com/webtor-io/abuse-store/proto"
	"google.golang.org/grpc"
)

func main() {
	app := cli.NewApp()
	app.Name = "abuse-store-cli"
	app.Usage = "interacts with abuse store"
	app.Version = "0.0.1"
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "host, H",
			Usage: "listening host",
			Value: "",
		},
		cli.IntFlag{
			Name:  "port, P",
			Usage: "listening port",
			Value: 50051,
		},
		cli.StringFlag{
			Name:  "info-hash, hash, ha",
			Usage: "info hash",
		},
	}

	app.Commands = []cli.Command{
		{
			Name:    "push",
			Aliases: []string{"ps"},
			Usage:   "pushes abuse to the store",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "work, w",
					Usage: "Infringed work (required)",
				},
				cli.StringFlag{
					Name:  "filename, file, f",
					Usage: "Infringed file (empty by default)",
				},
				cli.StringFlag{
					Name:  "infohash, hash, ha",
					Usage: "Infringed torrent infohash (required)",
				},
				cli.StringFlag{
					Name:  "email, mail",
					Usage: "Rightholder notify email (empty by default)",
				},
				cli.StringFlag{
					Name:  "description, desc, d",
					Usage: "Description of DMCA abuse (empty by default)",
				},
				cli.StringFlag{
					Name:  "notice-id, id",
					Usage: "ID of DMCA abuse (uuid by default)",
				},
				cli.StringFlag{
					Name:  "started-at, st",
					Usage: "Start time of abusive activity (current time by default)",
				},
				cli.StringFlag{
					Name:  "subject, subj",
					Usage: "Subject (empty by default)",
				},
			},
			Action: func(c *cli.Context) error {
				addr := fmt.Sprintf("%s:%d", c.GlobalString("host"), c.GlobalInt("port"))
				conn, err := grpc.Dial(addr, grpc.WithInsecure())
				if err != nil {
					return err
				}
				defer conn.Close()
				cl := pb.NewAbuseStoreClient(conn)

				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				_, err = cl.Push(ctx, &pb.PushRequest{
					Work:        c.String("work"),
					Filename:    c.String("filename"),
					Infohash:    c.String("infohash"),
					Email:       c.String("email"),
					Description: c.String("description"),
					NoticeId:    c.String("notice-id"),
					Source:      pb.PushRequest_FORM,
					Cause:       pb.PushRequest_ILLEGAL_CONTENT,
				})
				if err != nil {
					return err
				}
				fmt.Println("Done")

				return nil
			},
		},
		{
			Name:    "check",
			Aliases: []string{"ch"},
			Usage:   "pulls torrent from the store",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "infohash, hash, ha",
					Usage: "info hash of the torrent file",
				},
			},
			Action: func(c *cli.Context) error {
				addr := fmt.Sprintf("%s:%d", c.GlobalString("host"), c.GlobalInt("port"))
				conn, err := grpc.Dial(addr, grpc.WithInsecure())
				if err != nil {
					return err
				}
				defer conn.Close()
				cl := pb.NewAbuseStoreClient(conn)

				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				r, err := cl.Check(ctx, &pb.CheckRequest{
					Infohash: c.String("infohash"),
				})
				if err != nil {
					return err
				}
				fmt.Println(r.Exists)

				return nil
			},
		},
	}
	err := app.Run(os.Args)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
