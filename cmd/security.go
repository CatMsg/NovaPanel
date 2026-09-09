package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/CatMsg/NovaPanel/config"
	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/service"
)

func runSecurityCommand(args []string) {
	if err := database.InitDB(config.GetDBPath()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	guard := &service.LoginGuardService{}
	action := "status"
	if len(args) > 0 {
		action = args[0]
	}
	switch action {
	case "status", "bans":
		status := guard.GetLoginProtectionStatus()
		fmt.Printf("supported=%t installed=%t active=%t jail=%s banned=%d\n", status.Supported, status.Installed, status.Active, status.Jail, len(status.BannedIPs))
		for _, ip := range status.BannedIPs {
			fmt.Println(ip)
		}
		if status.Error != "" {
			fmt.Fprintln(os.Stderr, status.Error)
			os.Exit(1)
		}
	case "sync":
		if err := guard.SyncLoginProtection(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("login protection synced")
	case "unban":
		if len(args) != 2 || strings.TrimSpace(args[1]) == "" {
			fmt.Fprintln(os.Stderr, "usage: novas security unban IP")
			os.Exit(2)
		}
		if err := guard.UnbanLoginIP(args[1]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("login IP unbanned")
	default:
		fmt.Fprintln(os.Stderr, "usage: novas security {status|bans|sync|unban IP}")
		os.Exit(2)
	}
}
