package sub

import (
	"testing"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/service"
)

func seedSubscriptionTestSettings(t *testing.T, overrides ...model.Setting) {
	t.Helper()
	if _, err := (&service.SettingService{}).GetAllSetting(); err != nil {
		t.Fatalf("initialize complete test settings: %v", err)
	}
	for _, setting := range overrides {
		result := database.GetDB().Model(&model.Setting{}).Where("key = ?", setting.Key).Update("value", setting.Value)
		if result.Error != nil {
			t.Fatalf("set test setting %s: %v", setting.Key, result.Error)
		}
		if result.RowsAffected != 1 {
			t.Fatalf("test setting %s was not initialized", setting.Key)
		}
	}
}
