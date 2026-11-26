// Package nudge implements the fixed-time window nudge schedule.
package nudge

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

const (
	SessionName = "window-nudge-chore"
	Prompt      = "hi, please kill this tmux session " + SessionName
	UTCOffset   = 3 * time.Hour
)

// Preset identifies one fixed daily set of UTC+03:00 wall-clock hours.
type Preset string

const (
	Preset01 Preset = "01-06-11-16-21"
	Preset02 Preset = "02-07-12-17-22"
	Preset03 Preset = "03-08-13-18-23"
	Preset04 Preset = "04-09-14-19-00"
	Preset05 Preset = "05-10-15-20-00"
)

var presets = map[Preset][]int{
	Preset01: {1, 6, 11, 16, 21},
	Preset02: {2, 7, 12, 17, 22},
	Preset03: {3, 8, 13, 18, 23},
	Preset04: {4, 9, 14, 19, 0},
	Preset05: {5, 10, 15, 20, 0},
}

var fixedUTC3 = time.FixedZone("UTC+03:00", int(UTCOffset.Seconds()))

// Presets returns the supported presets in UI order.
func Presets() []Preset { return []Preset{Preset01, Preset02, Preset03, Preset04, Preset05} }

// ValidPreset reports whether p is one of the five fixed daily schedules.
func ValidPreset(p Preset) bool { _, ok := presets[p]; return ok }

// Hours returns the five daily clock hours for a preset.
func Hours(p Preset) ([]int, error) {
	hours, ok := presets[p]
	if !ok {
		return nil, fmt.Errorf("unknown window nudge schedule %q", p)
	}
	return append([]int(nil), hours...), nil
}

// NextAfter returns the first scheduled instant strictly after after. It uses
// a fixed UTC+03:00 offset, independent of the host timezone and DST.
func NextAfter(p Preset, after time.Time) (time.Time, error) {
	hours, err := Hours(p)
	if err != nil {
		return time.Time{}, err
	}
	slices.Sort(hours)
	local := after.In(fixedUTC3)
	y, month, day := local.Date()
	for offset := 0; offset <= 1; offset++ {
		date := time.Date(y, month, day+offset, 0, 0, 0, 0, fixedUTC3)
		for _, hour := range hours {
			candidate := time.Date(date.Year(), date.Month(), date.Day(), hour, 0, 0, 0, fixedUTC3)
			if candidate.After(after) {
				return candidate.UTC(), nil
			}
		}
	}
	return time.Time{}, errors.New("window nudge schedule has no future occurrence")
}

// NormalizeProvider validates the two supported interactive providers.
func NormalizeProvider(provider string) (string, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider != "claude" && provider != "codex" {
		return "", errors.New("choose Claude or Codex")
	}
	return provider, nil
}
