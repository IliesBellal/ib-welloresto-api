package cash_registers

import (
	"errors"
	"testing"
	"time"
)

func TestValidateClosingModeSchedule(t *testing.T) {
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	// 31/10/2026 à 23h30 heure de Paris = 22h30 UTC : encore octobre dans le
	// calendrier de l'établissement, alors que la date UTC est la même — et un
	// 1er novembre est donc le « mois prochain ».
	now := time.Date(2026, 10, 31, 22, 30, 0, 0, time.UTC)

	tests := []struct {
		name    string
		req     ScheduleClosingModeRequest
		wantErr error
		want    string
	}{
		{"1er du mois prochain", ScheduleClosingModeRequest{Mode: ClosingModeAuto, EffectiveFrom: "2026-11-01"}, nil, "2026-11-01"},
		{"1er d'un mois plus lointain", ScheduleClosingModeRequest{Mode: ClosingModeManual, EffectiveFrom: "2027-03-01"}, nil, "2027-03-01"},
		{"mois en cours", ScheduleClosingModeRequest{Mode: ClosingModeAuto, EffectiveFrom: "2026-10-01"}, ErrClosingModeRetroactive, ""},
		{"passé", ScheduleClosingModeRequest{Mode: ClosingModeAuto, EffectiveFrom: "2026-01-01"}, ErrClosingModeRetroactive, ""},
		{"pas un 1er", ScheduleClosingModeRequest{Mode: ClosingModeAuto, EffectiveFrom: "2026-11-15"}, ErrClosingModeNotMonthStart, ""},
		{"date mal formée", ScheduleClosingModeRequest{Mode: ClosingModeAuto, EffectiveFrom: "01/11/2026"}, ErrClosingModeInvalidDate, ""},
		{"mode inconnu", ScheduleClosingModeRequest{Mode: "auto", EffectiveFrom: "2026-11-01"}, ErrClosingModeInvalidMode, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := validateClosingModeSchedule(tc.req, now, paris)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil && got.Format(closingModeDateLayout) != tc.want {
				t.Fatalf("effective_from = %s, want %s", got.Format(closingModeDateLayout), tc.want)
			}
		})
	}

	// Le 1er novembre à 00h30 heure de Paris (31/10 23h30 UTC), novembre a
	// commencé pour l'établissement : novembre devient rétroactif.
	nowNovember := time.Date(2026, 10, 31, 23, 30, 0, 0, time.UTC)
	if _, err := validateClosingModeSchedule(ScheduleClosingModeRequest{Mode: ClosingModeAuto, EffectiveFrom: "2026-11-01"}, nowNovember, paris); !errors.Is(err, ErrClosingModeRetroactive) {
		t.Fatalf("1er novembre 00h30 Paris : err = %v, want ErrClosingModeRetroactive", err)
	}
}
