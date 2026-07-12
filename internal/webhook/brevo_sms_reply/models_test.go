package brevo_sms_reply

import "testing"

func TestParseIntent(t *testing.T) {
	cases := []struct {
		text string
		want Intent
	}{
		{"CONFIRMER", IntentReconfirm},
		{"oui", IntentReconfirm},
		{"  Yes please  ", IntentReconfirm},
		{"ANNULER", IntentCancel},
		{"non merci", IntentCancel},
		{"CANCEL", IntentCancel},
		{"no", IntentCancel},
		{"peut-être", IntentIgnore},
		{"", IntentIgnore},
		{"   ", IntentIgnore},
	}
	for _, c := range cases {
		if got := ParseIntent(c.text); got != c.want {
			t.Errorf("ParseIntent(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}

func TestBrevoSMSReplyAliases(t *testing.T) {
	r := BrevoSMSReply{Sender: "+33612345678", Message: "OUI"}
	if r.Phone() != "+33612345678" {
		t.Errorf("Phone() = %q", r.Phone())
	}
	if r.Body() != "OUI" {
		t.Errorf("Body() = %q", r.Body())
	}

	empty := BrevoSMSReply{}
	if empty.Phone() != "" || empty.Body() != "" {
		t.Errorf("expected empty phone/body, got %q / %q", empty.Phone(), empty.Body())
	}
}
