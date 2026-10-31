package node

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func newClaimTestManager(t *testing.T, fake *fakePlayit) *Manager {
	t.Helper()
	server := httptest.NewServer(fake.handler(t))
	t.Cleanup(server.Close)
	manager := newTestManager(t, testConfig(t), newFakeRuntime())
	manager.playit.apiBase = server.URL
	return manager
}

func TestPlayitClaimStoresTheKeyOnceAccepted(t *testing.T) {
	fake := &fakePlayit{claimState: "WaitingForUserVisit"}
	manager := newClaimTestManager(t, fake)
	ctx := context.Background()

	if claim, err := manager.PollPlayitClaim(ctx); err != nil || claim.State != ClaimNone {
		t.Fatalf("ohne laufende Verbindung: %+v %v", claim, err)
	}

	started, err := manager.StartPlayitClaim(ctx)
	if err != nil || started.State != ClaimWaiting {
		t.Fatalf("Start: %+v %v", started, err)
	}
	if len(fake.claimCode) != 10 || started.URL != "https://playit.gg/claim/"+fake.claimCode {
		t.Errorf("Link %q passt nicht zum Code %q", started.URL, fake.claimCode)
	}

	for _, state := range []string{"WaitingForUserVisit", "WaitingForUser"} {
		fake.claimState = state
		claim, err := manager.PollPlayitClaim(ctx)
		if err != nil || claim.State != ClaimWaiting || claim.URL != started.URL {
			t.Fatalf("%s: %+v %v", state, claim, err)
		}
	}
	if secret, _, _ := manager.readPlayit(); secret != "" || fake.exchanges != 0 {
		t.Fatal("vor der Bestätigung darf nichts getauscht oder gespeichert werden")
	}

	fake.claimState = "UserAccepted"
	claim, err := manager.PollPlayitClaim(ctx)
	if err != nil || claim.State != ClaimAccepted || claim.URL != "" {
		t.Fatalf("bestätigt: %+v %v", claim, err)
	}
	if secret, _, _ := manager.readPlayit(); secret != testSecret {
		t.Errorf("der Schlüssel muss gespeichert sein: %q", secret)
	}
	if !manager.PlayitStatus(ctx).Running {
		t.Error("der Agent muss mit dem neuen Schlüssel laufen")
	}
	if claim, _ := manager.PollPlayitClaim(ctx); claim.State != ClaimNone || fake.exchanges != 1 {
		t.Errorf("danach ist die Verbindung beendet und wird nicht noch einmal getauscht: %+v, %d Tausche", claim, fake.exchanges)
	}
}

func TestPlayitClaimEnds(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(*fakePlayit)
		want  string
	}{
		{"abgelehnt", func(f *fakePlayit) { f.claimState = "UserRejected" }, ClaimRejected},
		{"abgelaufen", func(f *fakePlayit) { f.claimFail = "CodeExpired" }, ClaimExpired},
	} {
		fake := &fakePlayit{claimState: "WaitingForUser"}
		manager := newClaimTestManager(t, fake)
		if _, err := manager.StartPlayitClaim(context.Background()); err != nil {
			t.Fatal(err)
		}
		test.setup(fake)
		claim, err := manager.PollPlayitClaim(context.Background())
		if err != nil || claim.State != test.want {
			t.Errorf("%s: %+v %v", test.name, claim, err)
		}
		if secret, _, _ := manager.readPlayit(); secret != "" {
			t.Errorf("%s: es darf kein Schlüssel gespeichert werden", test.name)
		}
		if again, _ := manager.PollPlayitClaim(context.Background()); again.State != ClaimNone {
			t.Errorf("%s: die Verbindung muss danach beendet sein: %+v", test.name, again)
		}
	}
}

func TestPlayitClaimStartFailsWhenPlayitRefuses(t *testing.T) {
	fake := &fakePlayit{claimFail: "InvalidCode"}
	manager := newClaimTestManager(t, fake)
	if _, err := manager.StartPlayitClaim(context.Background()); !errors.Is(err, errUpstream) {
		t.Errorf("abgelehnter Code: %v", err)
	}
	if claim, _ := manager.PollPlayitClaim(context.Background()); claim.State != ClaimNone {
		t.Errorf("ohne erfolgreichen Start darf nichts laufen: %+v", claim)
	}
}

func TestPlayitReadOnlyKeyPointsToTheClaim(t *testing.T) {
	fake := &fakePlayit{}
	manager := newPlayitTestManager(t, fake)
	fake.errorWith = map[string]any{"type": "auth", "message": "NotAllowedWithReadOnly"}
	_, err := manager.CreatePlayitTunnel(context.Background(), PlayitTunnelRequest{Name: "ls-abc-java", Kind: TunnelJava, LocalPort: 25565})
	if !errors.Is(err, errBadRequest) || !strings.Contains(err.Error(), "Mit playit verbinden") {
		t.Errorf("Schlüssel mit Leserechten: %v", err)
	}
}
