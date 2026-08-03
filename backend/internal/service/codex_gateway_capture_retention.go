package service

import (
	"os"
	"path/filepath"
	"time"
)

func (m *CodexGatewayCaptureManager) ApplyRetention(now time.Time) error {
	if m == nil || m.disabled || m.cfg.RetentionDays <= 0 {
		return nil
	}
	entries, err := os.ReadDir(m.cfg.BaseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	cutoff := now.AddDate(0, 0, -m.cfg.RetentionDays)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		date, parseErr := time.Parse("2006-01-02", entry.Name())
		if parseErr != nil {
			continue
		}
		if !date.Before(cutoff) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(m.cfg.BaseDir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}
