package nudge

import (
	"testing"
	"time"
)

func TestNextAfterUsesFixedUTC3ClockAndStrictFuture(t *testing.T) {
	utc := time.UTC
	cases := []struct {
		name string
		p    Preset
		now  time.Time
		want time.Time
	}{
		{"before first", Preset01, time.Date(2026, 11, 25, 21, 30, 0, 0, utc), time.Date(2026, 11, 26, 1, 0, 0, 0, fixedUTC3)},
		{"exact time is not immediate", Preset01, time.Date(2026, 11, 26, 1, 0, 0, 0, fixedUTC3), time.Date(2026, 11, 26, 6, 0, 0, 0, fixedUTC3)},
		{"midnight slot", Preset04, time.Date(2026, 11, 26, 23, 30, 0, 0, fixedUTC3), time.Date(2026, 11, 27, 0, 0, 0, 0, fixedUTC3)},
		{"after final slot", Preset05, time.Date(2026, 11, 26, 20, 0, 0, 0, fixedUTC3), time.Date(2026, 11, 27, 0, 0, 0, 0, fixedUTC3)},
		{"fixed offset ignores host zone", Preset02, time.Date(2026, 11, 26, 15, 30, 0, 0, time.FixedZone("UTC-05", -5*60*60)), time.Date(2026, 11, 27, 2, 0, 0, 0, fixedUTC3)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NextAfter(tc.p, tc.now)
			if err != nil || !got.Equal(tc.want) {
				t.Fatalf("NextAfter(%s, %s) = %s, %v; want %s", tc.p, tc.now, got, err, tc.want)
			}
		})
	}
}

func TestPresetsContainDailyFixedClockTimes(t *testing.T) {
	want := [][]int{{1, 6, 11, 16, 21}, {2, 7, 12, 17, 22}, {3, 8, 13, 18, 23}, {4, 9, 14, 19, 0}, {5, 10, 15, 20, 0}}
	for i, p := range Presets() {
		got, err := Hours(p)
		if err != nil || len(got) != len(want[i]) {
			t.Fatalf("Hours(%s) = %v, %v", p, got, err)
		}
		for j := range got {
			if got[j] != want[i][j] {
				t.Errorf("Hours(%s) = %v; want %v", p, got, want[i])
				break
			}
		}
	}
	if ValidPreset("4-9-14-19-00") {
		t.Fatal("accepted a non-canonical preset id")
	}
}

func TestNormalizeProvider(t *testing.T) {
	for _, provider := range []string{"claude", "codex", " Claude "} {
		if _, err := NormalizeProvider(provider); err != nil {
			t.Errorf("NormalizeProvider(%q): %v", provider, err)
		}
	}
	for _, provider := range []string{"", "openai", "claude code", "claude; true"} {
		if _, err := NormalizeProvider(provider); err == nil {
			t.Errorf("NormalizeProvider(%q) accepted", provider)
		}
	}
}
