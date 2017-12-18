package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/urfave/cli"
)

var cl = &Client{}

func main() {

	//初始global 变量
	CfgDir = getCurrentPath()
	CurrentDir = getCurrentPath()
	CmdDir = getExecPath()

	app := cli.NewApp()
	app.Name = AppName
	app.Version = Version
	app.Usage = "golang supervisor"
	app.Before = func(c *cli.Context) error {
		var err error
		CfgFile = c.GlobalString("conf")

		if filepath.IsAbs(CfgFile) {
			CfgDir = filepath.Dir(CfgFile)
		} else {
			CfgDir = filepath.Dir(filepath.Join(getCurrentPath(), CfgFile))
		}
		Cfg, err = readConf(CfgFile)
		if err != nil {
			fmt.Printf("read conf failed,", err)
			os.Exit(-1)
		}
		//加载client配置
		cl = NewClient()
		return nil
	}
	app.Authors = []cli.Author{
		cli.Author{
			Name:  Author,
			Email: Email,
		},
	}
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "conf, c",
			Usage: "config file",
			Value: DefaultConfig,
		},
	}
	app.Commands = []cli.Command{
		{
			Name:  "start-server",
			Usage: "Start supervisor and run in background",
			Flags: []cli.Flag{
				cli.BoolFlag{
					Name:  "foreground, f",
					Usage: "start in foreground",
				},
				cli.StringFlag{
					Name:  "conf, c",
					Usage: "config file",
					Value: DefaultConfig,
				},
			},
			Action: actionStartServer,
		},
		{
			Name:    "status",
			Aliases: []string{"st"},
			Usage:   "Show program status",
			Action:  actionProgramStatus,
		},
		{
			Name:    "status-server",
			Aliases: []string{"st"},
			Usage:   "Show server status",
			Action:  actionStatus,
		},
		{
			Name:   "start",
			Usage:  "Start program",
			Action: actionStart,
		},
		{
			Name:   "stop",
			Usage:  "Stop program",
			Action: actionStop,
		},
		{
			Name:   "reload",
			Usage:  "Reload config file",
			Action: actionReload,
		},
		{
			Name:   "shutdown",
			Usage:  "Shutdown server",
			Action: actionShutdown,
		},
		{
			Name:   "kill",
			Usage:  "kill stop server by pid file.",
			Action: actionKill,
		},
		{
			Name:   "restart-server",
			Usage:  "restart server",
			Action: actionRestart,
		},
		{
			Name:    "conftest",
			Aliases: []string{"t"},
			Usage:   "Test if config file is valid",
			Action:  actionConfigTest,
		},
		{
			Name:   "edit",
			Usage:  "Edit config file",
			Action: actionEdit,
		},
		{
			Name:    "version",
			Usage:   "Show version",
			Aliases: []string{"v"},
			Action:  actionVersion,
		},
	}
	if err := app.Run(os.Args); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
