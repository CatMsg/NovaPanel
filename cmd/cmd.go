package cmd

import (
	"flag"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/CatMsg/NovaPanel/config"
)

func ParseCmd() {
	var showVersion bool
	flag.BoolVar(&showVersion, "v", false, "show version")

	adminCmd := flag.NewFlagSet("admin", flag.ExitOnError)
	settingCmd := flag.NewFlagSet("setting", flag.ExitOnError)

	var username string
	var password string
	var port int
	var path string
	var subPort int
	var subPath string
	var webCertFile string
	var webKeyFile string
	var subCertFile string
	var subKeyFile string
	var clearWebTLS bool
	var clearSubTLS bool
	var reset bool
	var show bool
	var passwordStdin bool
	var passwordPrompt bool
	settingCmd.BoolVar(&reset, "reset", false, "reset all settings")
	settingCmd.BoolVar(&show, "show", false, "show current settings")
	settingCmd.IntVar(&port, "port", 0, "set panel port")
	settingCmd.StringVar(&path, "path", "", "set panel path")
	settingCmd.IntVar(&subPort, "subPort", 0, "set sub port")
	settingCmd.StringVar(&subPath, "subPath", "", "set sub path")
	settingCmd.StringVar(&webCertFile, "webCertFile", "", "set panel cert file")
	settingCmd.StringVar(&webKeyFile, "webKeyFile", "", "set panel key file")
	settingCmd.StringVar(&subCertFile, "subCertFile", "", "set sub cert file")
	settingCmd.StringVar(&subKeyFile, "subKeyFile", "", "set sub key file")
	settingCmd.BoolVar(&clearWebTLS, "clearWebTLS", false, "clear panel cert and key file")
	settingCmd.BoolVar(&clearSubTLS, "clearSubTLS", false, "clear sub cert and key file")

	adminCmd.BoolVar(&show, "show", false, "show first admin credentials")
	adminCmd.BoolVar(&reset, "reset", false, "reset first admin credentials")
	adminCmd.StringVar(&username, "username", "", "set login username")
	adminCmd.StringVar(&password, "password", "", "set login password (legacy; visible in process arguments)")
	adminCmd.BoolVar(&passwordStdin, "password-stdin", false, "read login password from standard input")
	adminCmd.BoolVar(&passwordPrompt, "password-prompt", false, "read login password without terminal echo")

	oldUsage := flag.Usage
	flag.Usage = func() {
		oldUsage()
		fmt.Println()
		fmt.Println("Commands:")
		fmt.Println("    admin          set/reset/show first admin credentials")
		fmt.Println("    uri            Show panel URI")
		fmt.Println("    upgrade-db     apply the supported offline schema upgrade")
		fmt.Println("    checkdb        validate the offline database")
		fmt.Println("    healthcheck    check local panel availability")
		fmt.Println("    setting        set/reset/show settings")
		fmt.Println("    security       status/list/sync/unban login protection")
		fmt.Println()
		adminCmd.Usage()
		fmt.Println()
		settingCmd.Usage()
	}

	flag.Parse()
	if showVersion {
		fmt.Println("NovaPanel Panel\t", config.GetVersion())
		info, ok := debug.ReadBuildInfo()
		if ok {
			for _, dep := range info.Deps {
				if dep.Path == "github.com/sagernet/sing-box" {
					fmt.Println("Sing-Box\t", dep.Version)
					break
				}
			}
		}
		return
	}
	if len(os.Args) < 2 {
		flag.Usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "admin":
		err := adminCmd.Parse(os.Args[2:])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		switch {
		case show:
			if err := showAdmin(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		case reset:
			if err := resetAdmin(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		default:
			if (passwordStdin && passwordPrompt) || ((passwordStdin || passwordPrompt) && password != "") {
				fmt.Fprintln(os.Stderr, "use only one of -password, -password-stdin, or -password-prompt")
				os.Exit(2)
			}
			if passwordStdin || passwordPrompt {
				var err error
				if passwordStdin {
					password, err = readAdminPassword(os.Stdin)
				} else {
					password, err = promptAdminPassword()
				}
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}
			}
			if err := updateAdmin(username, password); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			if err := showAdmin(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}

	case "uri":
		if err := getPanelURI(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

	case "healthcheck":
		if err := checkLocalPanel(); err != nil {
			fmt.Fprintln(os.Stderr, "Panel health check failed:", err)
			os.Exit(1)
		}
		fmt.Println("Panel health check passed")

	case "checkdb":
		if err := checkRuntimeDatabase(config.GetDBPath()); err != nil {
			fmt.Fprintln(os.Stderr, "Database check failed:", err)
			os.Exit(1)
		}
	case "upgrade-db":
		if err := upgradeRuntimeDatabase(config.GetDBPath()); err != nil {
			fmt.Fprintln(os.Stderr, "Database upgrade failed:", err)
			os.Exit(1)
		}
		fmt.Println("Database upgrade preflight passed")

	case "setting":
		err := settingCmd.Parse(os.Args[2:])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		switch {
		case show:
			if err := showSetting(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		case reset:
			if err := resetSetting(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		default:
			if err := updateSetting(port, path, subPort, subPath, webCertFile, webKeyFile, subCertFile, subKeyFile, clearWebTLS, clearSubTLS); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			if err := showSetting(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
	case "security":
		runSecurityCommand(os.Args[2:])
	default:
		fmt.Println("Invalid subcommands")
		flag.Usage()
		os.Exit(2)
	}
}
