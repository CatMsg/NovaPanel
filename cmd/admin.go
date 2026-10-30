package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/CatMsg/NovaPanel/config"
	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/security"
	"github.com/CatMsg/NovaPanel/service"
	"golang.org/x/term"
)

const maxAdminPasswordInput = 4096

func hasPlaintextAdminPasswordFlag(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if arg == "-password" || arg == "--password" || strings.HasPrefix(arg, "-password=") || strings.HasPrefix(arg, "--password=") {
			return true
		}
	}
	return false
}

func readAdminPassword(reader io.Reader) (string, error) {
	input, err := io.ReadAll(io.LimitReader(reader, maxAdminPasswordInput+2))
	if err != nil {
		return "", fmt.Errorf("read administrator password from stdin: %w", err)
	}
	input = bytes.TrimSuffix(input, []byte("\n"))
	input = bytes.TrimSuffix(input, []byte("\r"))
	if len(input) == 0 || len(input) > maxAdminPasswordInput {
		return "", fmt.Errorf("password from stdin must contain 1 to %d bytes", maxAdminPasswordInput)
	}
	return string(input), nil
}

func promptAdminPassword() (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("password prompt requires an interactive terminal; use -password-stdin instead")
	}
	fmt.Fprint(os.Stderr, "Enter new administrator password: ")
	input, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read administrator password: %w", err)
	}
	if len(input) == 0 || len(input) > maxAdminPasswordInput {
		return "", fmt.Errorf("password must contain 1 to %d bytes", maxAdminPasswordInput)
	}
	return string(input), nil
}

func resetAdmin() error {
	err := database.InitDB(config.GetDBPath())
	if err != nil {
		return fmt.Errorf("initialize database: %w", err)
	}

	userService := service.UserService{}
	password, err := resetAdminCredentials(&userService)
	if err != nil {
		return fmt.Errorf("reset admin credentials: %w", err)
	} else {
		fmt.Println("Admin credentials reset. Save this one-time password now:")
		fmt.Println("Username: admin")
		fmt.Println(password)
	}
	return nil
}

func resetAdminCredentials(userService *service.UserService) (string, error) {
	password, err := security.GenerateRandomPassword()
	if err != nil {
		return "", fmt.Errorf("generate one-time administrator password: %w", err)
	}
	if err := userService.UpdateFirstUser("admin", password); err != nil {
		return "", err
	}
	return password, nil
}

func updateAdmin(username string, password string) error {
	err := database.InitDB(config.GetDBPath())
	if err != nil {
		return fmt.Errorf("initialize database: %w", err)
	}

	if username != "" || password != "" {
		userService := service.UserService{}
		err = userService.UpdateFirstUser(username, password)
		if err != nil {
			return fmt.Errorf("reset admin credentials: %w", err)
		}
		fmt.Println("reset admin credentials success")
	}
	return nil
}

func showAdmin() error {
	err := database.InitDB(config.GetDBPath())
	if err != nil {
		return fmt.Errorf("initialize database: %w", err)
	}
	userService := service.UserService{}
	userModel, err := userService.GetFirstUser()
	if err != nil {
		return fmt.Errorf("get current user info: %w", err)
	}
	if userModel == nil {
		return fmt.Errorf("get current user info: no administrator exists")
	}
	username := userModel.Username
	if username == "" {
		return fmt.Errorf("current username is empty")
	}
	fmt.Println("First admin credentials:")
	fmt.Println("\tUsername:\t", username)
	fmt.Println("\tPassword:\t stored as a one-way Argon2id hash; cannot be displayed")
	fmt.Println("\tReset:\t\t run novas admin -username <username> -password-prompt for a hidden terminal prompt")
	return nil
}
