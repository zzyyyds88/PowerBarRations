package model

import (
	"errors"
	"fmt"

	"github.com/zzyyyds88/PowerBarRations/common"
	"gorm.io/gorm"
)

const retiredThemeOptionKey = "theme.frontend"

// retiredOptionKeys 是已随功能删除、需要从库里幂等清理的 option 键。
//
// API 信息面板（前端面板 + 系统设置「API 地址」分节 + console_setting.api_info）
// 已整体删除：这里把历史遗留的旧键（ApiInfo）与新键（console_setting.api_info）
// 一并清掉，避免死配置残留。清理是幂等的——键不存在时静默跳过。
var retiredOptionKeys = []string{
	"ApiInfo",
	"console_setting.api_info",
	"console_setting.api_info_enabled",
}

// MigrateRetiredFrontendOptions normalizes options that belonged to the
// removed dashboard frontend. Each legacy console setting is migrated in its
// own transaction so one malformed value cannot block the other settings.
func MigrateRetiredFrontendOptions() error {
	if DB == nil {
		return errors.New("database is not initialized")
	}

	var migrationErrors []error
	if err := normalizeRetiredThemeOption(); err != nil {
		migrationErrors = append(migrationErrors, fmt.Errorf("normalize %s: %w", retiredThemeOptionKey, err))
	}
	for _, key := range retiredOptionKeys {
		if err := deleteRetiredOption(key); err != nil {
			migrationErrors = append(migrationErrors, err)
		}
	}
	return errors.Join(migrationErrors...)
}

func normalizeRetiredThemeOption() error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var option Option
		err := tx.Where(&Option{Key: retiredThemeOptionKey}).First(&option).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tx.Create(&Option{Key: retiredThemeOptionKey, Value: "default"}).Error
		}
		if err != nil {
			return err
		}
		if option.Value == "default" {
			return nil
		}
		return tx.Model(&option).Update("value", "default").Error
	})
}

// deleteRetiredOption 幂等删除一个已退役的 option 键；键不存在不算错误。
func deleteRetiredOption(key string) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var option Option
		err := tx.Where(&Option{Key: key}).First(&option).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read retired option %s: %w", key, err)
		}
		if err := tx.Delete(&option).Error; err != nil {
			return fmt.Errorf("delete retired option %s: %w", key, err)
		}
		common.SysLog(fmt.Sprintf("removed retired option %s", key))
		return nil
	})
}
