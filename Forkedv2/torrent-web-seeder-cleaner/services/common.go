package services

import (
	"os"
	"time"

	"github.com/urfave/cli"
)

const (
	CleanerKeepFreeFlag = "keep-free"
	CleanerFreeFlag     = "free"
	CleanerMaxAgeFlag   = "max-age"
	CleanerIntervalFlag = "interval"
	DataDirFlag         = "data-dir"
)

func RegisterCleanerFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.StringFlag{
			Name:   CleanerKeepFreeFlag,
			Usage:  "keep free",
			Value:  "25%",
			EnvVar: "CLEANER_KEEP_FREE",
		},
		cli.StringFlag{
			Name:   CleanerFreeFlag,
			Usage:  "free",
			Value:  "35%",
			EnvVar: "CLEANER_FREE",
		},
		cli.DurationFlag{
			Name:   CleanerMaxAgeFlag,
			Usage:  "maximum age for inactive media cache; set 0 to disable age cleanup",
			Value:  23 * time.Hour,
			EnvVar: "CACHED_MEDIA_MAX_AGE,CLEANER_MAX_AGE",
		},
		cli.DurationFlag{
			Name:   CleanerIntervalFlag,
			Usage:  "cleaning interval",
			Value:  5 * time.Minute,
			EnvVar: "CACHED_MEDIA_CLEAN_INTERVAL,CLEANER_INTERVAL",
		},
		cli.StringFlag{
			Name:   DataDirFlag,
			Usage:  "data dir",
			Value:  os.TempDir(),
			EnvVar: "DATA_DIR",
		},
	)
}
