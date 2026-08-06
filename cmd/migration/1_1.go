package migration

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/CatMsg/NovaPanel/database/model"

	"gorm.io/gorm"
)

func migrateClientSchema(db *gorm.DB) error {
	rows, err := db.Raw("PRAGMA table_info(clients)").Rows()
	if err != nil {
		fmt.Println(err)
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid       int
			cname     string
			ctype     string
			notnull   int
			dfltValue interface{}
			pk        int
		)

		if err := rows.Scan(&cid, &cname, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return err
		}
		if cname == "config" || cname == "inbounds" || cname == "links" {
			if strings.EqualFold(ctype, "text") {
				fmt.Printf("Column %s has type TEXT\n", cname)
				oldData := make([]struct {
					Id   uint
					Data string
				}, 0)
				if err := db.Model(model.Client{}).Select("id", cname+" as data").Scan(&oldData).Error; err != nil {
					return err
				}
				for _, data := range oldData {
					var newData []byte
					switch cname {
					case "inbounds":
						inbounds := strings.Split(data.Data, ",")
						newData, err = json.MarshalIndent(inbounds, "", "  ")
					case "config":
						jsonData := map[string]interface{}{}
						if strings.TrimSpace(data.Data) != "" {
							err = json.Unmarshal([]byte(data.Data), &jsonData)
						}
						if err == nil {
							newData, err = json.MarshalIndent(jsonData, "", "  ")
						}
					case "links":
						jsonData := make([]interface{}, 0)
						if strings.TrimSpace(data.Data) != "" {
							err = json.Unmarshal([]byte(data.Data), &jsonData)
						}
						if err == nil {
							newData, err = json.MarshalIndent(jsonData, "", "  ")
						}
					}
					if err != nil {
						return fmt.Errorf("convert client %d column %s: %w", data.Id, cname, err)
					}
					err = db.Model(model.Client{}).Where("id = ?", data.Id).UpdateColumn(cname, newData).Error
					if err != nil {
						return err
					}
				}
			}
		}
	}
	return rows.Err()
}

func deleteOldWebSecret(db *gorm.DB) error {
	return db.Exec("DELETE FROM settings WHERE key = ?", "webSecret").Error
}

func changesObj(db *gorm.DB) error {
	return db.Exec("UPDATE changes SET obj = CAST('\"' || CAST(obj AS TEXT) || '\"' AS BLOB) WHERE actor = ? and obj not like ?", "DepleteJob", "\"%\"").Error
}

func to1_1(db *gorm.DB) error {
	err := migrateClientSchema(db)
	if err != nil {
		return err
	}
	err = deleteOldWebSecret(db)
	if err != nil {
		return err
	}
	err = changesObj(db)
	if err != nil {
		return err
	}
	return nil
}
