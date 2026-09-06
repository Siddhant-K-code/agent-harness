package dashboard

import (
	"strings"
	"testing"
)

func TestDeliveryRequiresAuthenticationOriginAndExplicitApproval(t *testing.T) {
	s := setup(t)
	path := "/api/runs/" + strings.Repeat("a", 32) + "/delivery"
	for _, method := range []string{"GET", "POST"} {
		if w := request(s, method, path, `{"action":"publish"}`, "", "", s.host); w.Code != 401 {
			t.Fatal("delivery authentication bypass", w.Code)
		}
		if w := request(s, method, path, `{"action":"publish"}`, s.token, "https://evil.invalid", s.host); w.Code != 403 {
			t.Fatal("delivery origin bypass", w.Code)
		}
	}
	w := request(s, "POST", path, `{"action":"publish","approval_hash":"`+strings.Repeat("b", 64)+`"}`, s.token, "", s.host)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "explicit approval") {
		t.Fatal("publish accepted without approval", w.Body.String())
	}
	w = request(s, "POST", path, `{"action":"publish","approve":true,"repository":"another/target"}`, s.token, "", s.host)
	if w.Code != 400 {
		t.Fatal("accepted user-selected destination")
	}
}
