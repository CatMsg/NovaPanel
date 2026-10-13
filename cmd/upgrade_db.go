package cmd

import (
	"errors"
	"fmt"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/service"
)

func upgradeRuntimeDatabase(path string) error {
	applied, err := database.MigrateMasqueIdentityColumns(path)
	if err != nil {
		return err
	}
	if applied {
		if err := database.OpenDB(path); err != nil {
			return fmt.Errorf("open upgraded database for MASQUE identity migration: %w", err)
		}
		migrationErr := service.MigrateLegacyMasqueTLSInbounds(database.GetDB())
		closeErr := database.CloseDB()
		if err := errors.Join(migrationErr, closeErr); err != nil {
			return err
		}
		fmt.Println("Applied MASQUE identity columns and migrated existing MASQUE inbounds")
	}
	return checkRuntimeDatabase(path)
}
