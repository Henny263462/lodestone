package node

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/ssh"
)

// testHash erzeugt einen argon2id-Hash im Format des Panels (mit kleinen Parametern, damit Tests schnell sind).
func testHash(password string) string {
	salt := []byte("salz-fuer-tests!")
	key := argon2.IDKey([]byte(password), salt, 1, 1024, 1, 32)
	return fmt.Sprintf("$argon2id$v=19$m=1024,t=1,p=1$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))
}

type sftpTest struct {
	t       *testing.T
	service *SFTPService
	manager *Manager
	server  *Server
	address string
}

func newSFTPTest(t *testing.T) *sftpTest {
	t.Helper()
	cfg := testConfig(t)
	cfg.SFTPListen = "127.0.0.1:0"
	manager := newTestManager(t, cfg, newFakeRuntime())
	server := createPaper(t, manager, "SFTP")
	service, err := NewSFTPService(cfg, manager, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = service.Serve(ctx, listener)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	waitFor(t, "SFTP nimmt Verbindungen an", func() bool { return service.Status(0).Enabled })
	return &sftpTest{t: t, service: service, manager: manager, server: server, address: listener.Addr().String()}
}

func (s *sftpTest) dial(user string, methods ...ssh.AuthMethod) (*sftp.Client, error) {
	s.t.Helper()
	conn, err := ssh.Dial("tcp", s.address, &ssh.ClientConfig{
		User: user, Auth: methods, HostKeyCallback: ssh.FixedHostKey(s.service.signer.PublicKey()),
	})
	if err != nil {
		return nil, err
	}
	client, err := sftp.NewClient(conn)
	if err != nil {
		conn.Close()
		return nil, err
	}
	s.t.Cleanup(func() { client.Close(); conn.Close() })
	return client, nil
}

func (s *sftpTest) grant(grants ...SFTPGrant) {
	s.t.Helper()
	if err := s.service.SetAccess(s.server.ID(), SFTPAccess{Grants: grants}); err != nil {
		s.t.Fatal(err)
	}
}

func newClientKey(t *testing.T) (ssh.Signer, string) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	sshPublic, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	return signer, strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPublic))) + " test@lodestone"
}

func writeRemote(client *sftp.Client, name, content string) error {
	file, err := client.Create(name)
	if err != nil {
		return err
	}
	if _, err := file.Write([]byte(content)); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func readRemote(client *sftp.Client, name string) (string, error) {
	file, err := client.Open(name)
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	return string(data), err
}

func TestSFTPPasswordAndKeyLogin(t *testing.T) {
	s := newSFTPTest(t)
	signer, authorized := newClientKey(t)
	user := "anna." + s.server.ID()[:8]
	s.grant(SFTPGrant{Username: user, PasswordHash: testHash("richtiges-passwort"), Keys: []string{authorized}})

	client, err := s.dial(user, ssh.Password("richtiges-passwort"))
	if err != nil {
		t.Fatalf("Anmeldung mit richtigem Passwort: %v", err)
	}
	if err := client.MkdirAll("/plugins/Test"); err != nil {
		t.Fatal(err)
	}
	if err := writeRemote(client, "/plugins/Test/config.yml", "hallo: welt\n"); err != nil {
		t.Fatal(err)
	}
	onDisk, err := os.ReadFile(filepath.Join(s.server.dir, "data", "plugins", "Test", "config.yml"))
	if err != nil || string(onDisk) != "hallo: welt\n" {
		t.Fatalf("Datei auf der Platte: %q %v", onDisk, err)
	}
	if entries, err := client.ReadDir("/plugins"); err != nil || len(entries) != 1 || entries[0].Name() != "Test" {
		t.Fatalf("Auflistung: %v %v", entries, err)
	}
	if err := client.Rename("/plugins/Test/config.yml", "/plugins/Test/alt.yml"); err != nil {
		t.Fatal(err)
	}
	if err := client.Remove("/plugins/Test/alt.yml"); err != nil {
		t.Fatal(err)
	}

	// Der Name ist unabhängig von Groß- und Kleinschreibung, der Schlüssel reicht ohne Passwort
	keyClient, err := s.dial(strings.ToUpper(user), ssh.PublicKeys(signer))
	if err != nil {
		t.Fatalf("Anmeldung mit Schlüssel: %v", err)
	}
	if _, err := keyClient.Stat("/plugins"); err != nil {
		t.Fatal(err)
	}

	if _, err := s.dial(user, ssh.Password("falsches-passwort")); err == nil {
		t.Fatal("falsches Passwort wurde angenommen")
	}
	other, _ := newClientKey(t)
	if _, err := s.dial(user, ssh.PublicKeys(other)); err == nil {
		t.Fatal("fremder Schlüssel wurde angenommen")
	}
	if _, err := s.dial("bernd."+s.server.ID()[:8], ssh.Password("richtiges-passwort")); err == nil {
		t.Fatal("unbekannter Benutzer wurde angenommen")
	}

	status := s.service.Status(0)
	if !status.Enabled || status.Fingerprint == "" || !strings.HasPrefix(status.Fingerprint, "SHA256:") {
		t.Fatalf("Status: %+v", status)
	}
	var success, failed int
	for _, event := range status.Events {
		if event.Username != user || event.ServerID != s.server.ID() {
			t.Errorf("Ereignis gehört zu wem anders: %+v", event)
		}
		if event.Success {
			success++
		} else {
			failed++
		}
	}
	if success != 2 || failed != 1 {
		t.Fatalf("Ereignisse: %d gelungen, %d fehlgeschlagen: %+v", success, failed, status.Events)
	}
	if last := status.Events[len(status.Events)-1].Seq; len(s.service.Status(last).Events) != 0 {
		t.Fatal("after liefert alte Ereignisse")
	}
	if !hasLine(s.server, "SFTP: "+user+" hat sich angemeldet") {
		t.Error("die Anmeldung steht nicht in der Konsole")
	}
}

func TestSFTPPasswordLoginCanBeDisabled(t *testing.T) {
	s := newSFTPTest(t)
	signer, authorized := newClientKey(t)
	user := "anna." + s.server.ID()[:8]
	s.grant(SFTPGrant{Username: user, Keys: []string{authorized}}) // kein Hash: Passwort aus
	if _, err := s.dial(user, ssh.Password("")); err == nil {
		t.Fatal("leeres Passwort wurde angenommen")
	}
	if _, err := s.dial(user, ssh.Password("irgendwas")); err == nil {
		t.Fatal("Passwort wurde angenommen, obwohl der Zugang keins hat")
	}
	if _, err := s.dial(user, ssh.PublicKeys(signer)); err != nil {
		t.Fatalf("Schlüssel: %v", err)
	}
}

func TestSFTPReadOnly(t *testing.T) {
	s := newSFTPTest(t)
	if err := s.server.FS().Write("server.properties", strings.NewReader("motd=Hallo\n"), 1<<20, true); err != nil {
		t.Fatal(err)
	}
	user := "leser." + s.server.ID()[:8]
	s.grant(SFTPGrant{Username: user, PasswordHash: testHash("ein-passwort"), ReadOnly: true})
	client, err := s.dial(user, ssh.Password("ein-passwort"))
	if err != nil {
		t.Fatal(err)
	}
	if content, err := readRemote(client, "/server.properties"); err != nil || content != "motd=Hallo\n" {
		t.Fatalf("lesen: %q %v", content, err)
	}
	if err := writeRemote(client, "/neu.txt", "x"); err == nil {
		t.Error("nur lesen: Schreiben ging")
	}
	if file, err := client.OpenFile("/server.properties", os.O_WRONLY|os.O_TRUNC); err == nil {
		file.Close()
		t.Error("nur lesen: Überschreiben ging")
	}
	if err := client.Mkdir("/ordner"); err == nil {
		t.Error("nur lesen: Ordner anlegen ging")
	}
	if err := client.Remove("/server.properties"); err == nil {
		t.Error("nur lesen: Löschen ging")
	}
	if err := client.Chmod("/server.properties", 0o777); err == nil {
		t.Error("nur lesen: Rechte ändern ging")
	}
	if data, _ := s.server.FS().Read("server.properties", 1<<20); string(data) != "motd=Hallo\n" {
		t.Fatalf("Datei wurde verändert: %q", data)
	}

	// Wird der Zugang beschreibbar, gilt das sofort für die laufende Verbindung
	s.grant(SFTPGrant{Username: user, PasswordHash: testHash("ein-passwort")})
	if err := writeRemote(client, "/neu.txt", "x"); err != nil {
		t.Fatalf("nach Freigabe: %v", err)
	}
}

func TestSFTPRevokedAccess(t *testing.T) {
	s := newSFTPTest(t)
	user := "anna." + s.server.ID()[:8]
	s.grant(SFTPGrant{Username: user, PasswordHash: testHash("ein-passwort")})
	client, err := s.dial(user, ssh.Password("ein-passwort"))
	if err != nil {
		t.Fatal(err)
	}
	s.grant() // widerrufen
	if _, err := client.ReadDir("/"); err == nil {
		t.Fatal("die Verbindung arbeitet nach dem Widerruf weiter")
	}
	if _, err := s.dial(user, ssh.Password("ein-passwort")); err == nil {
		t.Fatal("Anmeldung nach dem Widerruf ging")
	}

	// Ein gelöschter Server nimmt seine Zugänge mit
	s.grant(SFTPGrant{Username: user, PasswordHash: testHash("ein-passwort")})
	if _, err := s.dial(user, ssh.Password("ein-passwort")); err != nil {
		t.Fatal(err)
	}
	if err := s.manager.Delete(s.server.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.dial(user, ssh.Password("ein-passwort")); err == nil {
		t.Fatal("Anmeldung an einem gelöschten Server ging")
	}
}

func TestSFTPStaysInsideTheServerFolder(t *testing.T) {
	s := newSFTPTest(t)
	user := "anna." + s.server.ID()[:8]
	s.grant(SFTPGrant{Username: user, PasswordHash: testHash("ein-passwort")})
	client, err := s.dial(user, ssh.Password("ein-passwort"))
	if err != nil {
		t.Fatal(err)
	}
	// spec.json und sftp-access.json liegen eine Ebene über den Serverdateien
	for _, name := range []string{"/../spec.json", "../sftp-access.json", "/../../servers/" + s.server.ID() + "/spec.json", `..\spec.json`} {
		if content, err := readRemote(client, name); err == nil {
			t.Errorf("%s ließ sich lesen: %q", name, content)
		}
	}
	if err := writeRemote(client, "/../ausbruch.txt", "x"); err != nil {
		t.Fatal(err) // landet als /ausbruch.txt im Serverordner
	}
	if _, err := os.Stat(filepath.Join(s.server.dir, "ausbruch.txt")); err == nil {
		t.Fatal("die Datei liegt außerhalb des Serverordners")
	}
	if _, err := os.Stat(filepath.Join(s.server.dir, "data", "ausbruch.txt")); err != nil {
		t.Fatal("die Datei liegt nicht im Serverordner")
	}
	if err := client.Symlink("/../spec.json", "/link"); err == nil {
		t.Error("Symlinks über SFTP sind erlaubt")
	}

	// Ein Symlink, der hinauszeigt (z. B. aus einem Archiv), führt nicht hinaus
	if err := os.Symlink(filepath.Join(s.server.dir, "spec.json"), filepath.Join(s.server.dir, "data", "raus")); err != nil {
		t.Logf("Symlink lässt sich hier nicht anlegen (%v), Teil übersprungen", err)
	} else {
		if content, err := readRemote(client, "/raus"); err == nil {
			t.Errorf("über den Symlink ließ sich lesen: %q", content)
		}
		if err := writeRemote(client, "/raus", "überschrieben"); err == nil {
			t.Error("über den Symlink ließ sich schreiben")
		}
	}

	// Uploads halten die Grenze des Nodes ein (testConfig: 5 MB)
	big := bytes.Repeat([]byte("x"), 6<<20)
	file, err := client.Create("/gross.bin")
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.Write(big)
	file.Close()
	if writeErr == nil {
		t.Fatal("eine Datei über der Upload-Grenze ließ sich schreiben")
	}
}

func TestSFTPThrottle(t *testing.T) {
	s := newSFTPTest(t)
	user := "anna." + s.server.ID()[:8]
	s.grant(SFTPGrant{Username: user, PasswordHash: testHash("ein-passwort")})
	for i := 0; i < sftpThrottleLimit; i++ {
		if _, err := s.dial(user, ssh.Password("falsch")); err == nil {
			t.Fatal("falsches Passwort wurde angenommen")
		}
	}
	if _, err := s.dial(user, ssh.Password("ein-passwort")); err == nil {
		t.Fatal("nach zu vielen Fehlversuchen ging die Anmeldung trotzdem")
	}
}

func TestSFTPAccessAPIAndPersistence(t *testing.T) {
	cfg := testConfig(t)
	cfg.SFTPListen = "127.0.0.1:2022"
	rt := newFakeRuntime()
	manager := newTestManager(t, cfg, rt)
	server := createPaper(t, manager, "API")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service, err := NewSFTPService(cfg, manager, logger)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = service.Serve(ctx, listener) }()
	waitFor(t, "SFTP nimmt Verbindungen an", func() bool { return service.Status(0).Enabled })
	api := NewAPI(cfg, manager, rt, fakeSoftware{}, logger, "test")
	api.SetSFTP(service)
	web := httptest.NewServer(api.Handler())
	t.Cleanup(web.Close)
	ta := &testAPI{t: t, url: web.URL, token: cfg.Token, manager: manager, rt: rt}

	_, authorized := newClientKey(t)
	good := SFTPAccess{Grants: []SFTPGrant{{Username: "anna." + server.ID()[:8], PasswordHash: testHash("x"), Keys: []string{authorized}, ReadOnly: true}}}
	if status, body := ta.do("PUT", "/api/servers/"+server.ID()+"/sftp-access", good); status != http.StatusOK {
		t.Fatalf("PUT: %d %s", status, body)
	}
	if status, _ := ta.do("PUT", "/api/servers/unbekannt/sftp-access", good); status != http.StatusNotFound {
		t.Errorf("unbekannter Server: %d", status)
	}
	bad := []SFTPAccess{
		{Grants: []SFTPGrant{{Username: "Anna Groß"}}},
		{Grants: []SFTPGrant{{Username: "anna.x", PasswordHash: "klartext"}}},
		{Grants: []SFTPGrant{{Username: "anna.x", Keys: []string{"kein schlüssel"}}}},
		{Grants: []SFTPGrant{{Username: "anna.x"}, {Username: "anna.x"}}},
	}
	for _, access := range bad {
		if status, body := ta.do("PUT", "/api/servers/"+server.ID()+"/sftp-access", access); status != http.StatusBadRequest {
			t.Errorf("%+v: %d %s", access, status, body)
		}
	}
	if status, _ := ta.do("PUT", "/api/servers/"+server.ID()+"/sftp-access", `{"grants":[],"extra":1}`); status != http.StatusBadRequest {
		t.Errorf("unbekanntes Feld: %d", status)
	}
	// ohne Token geht nichts
	request, _ := http.NewRequest("GET", web.URL+"/api/sftp", nil)
	if response, err := http.DefaultClient.Do(request); err != nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("ohne Token: %v %v", response, err)
	}

	status, body := ta.do("GET", "/api/sftp", nil)
	var info SFTPStatus
	if status != 200 || json.Unmarshal(body, &info) != nil || !info.Enabled || info.Port != 2022 || info.Fingerprint != service.Fingerprint() || info.BootID == "" {
		t.Fatalf("Status: %d %s", status, body)
	}

	// Der Host-Schlüssel bleibt, die Zugänge überstehen einen Neustart, die BootID wechselt
	again, err := NewSFTPService(cfg, manager, logger)
	if err != nil {
		t.Fatal(err)
	}
	if again.Fingerprint() != service.Fingerprint() {
		t.Error("der Host-Schlüssel hat sich geändert")
	}
	if again.bootID == service.bootID {
		t.Error("die BootID hat sich nicht geändert")
	}
	if _, grant, ok := again.lookup("anna." + server.ID()[:8]); !ok || !grant.ReadOnly || len(grant.keys) != 1 {
		t.Fatalf("Zugang nach Neustart: %+v %v", grant, ok)
	}
	info2, err := os.Stat(filepath.Join(cfg.DataDir, sftpHostKeyFile))
	if err != nil || info2.Size() == 0 {
		t.Fatalf("Host-Schlüssel: %v", err)
	}
}

func TestSFTPDisabled(t *testing.T) {
	cfg := testConfig(t) // SFTPListen leer
	manager := newTestManager(t, cfg, newFakeRuntime())
	service, err := NewSFTPService(cfg, manager, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if status := service.Status(0); status.Enabled || status.Fingerprint != "" || status.Port != 0 {
		t.Fatalf("Status: %+v", status)
	}
	if err := service.ListenAndServe(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cfg.DataDir, sftpHostKeyFile)); !errors.Is(err, os.ErrNotExist) {
		t.Error("ohne SFTP wurde ein Host-Schlüssel angelegt")
	}
	if _, err := NewSFTPService(Config{SFTPListen: "kein-port", DataDir: cfg.DataDir}, manager, slog.New(slog.NewTextHandler(io.Discard, nil))); err == nil {
		t.Error("ungültige Adresse wurde angenommen")
	}
}

func TestSFTPListenFromEnvironment(t *testing.T) {
	config := DefaultConfig()
	if config.SFTPListen != ":2022" {
		t.Fatalf("Standard: %q", config.SFTPListen)
	}
	t.Setenv("LODESTONE_NODE_SFTP_LISTEN", "")
	config.applyEnvironment()
	if config.SFTPListen != "" {
		t.Fatalf("leer sollte aus heißen: %q", config.SFTPListen)
	}
	t.Setenv("LODESTONE_NODE_SFTP_LISTEN", "0.0.0.0:2200")
	config.applyEnvironment()
	if config.SFTPListen != "0.0.0.0:2200" {
		t.Fatalf("Adresse: %q", config.SFTPListen)
	}
}

func TestSFTPAcceptsPanelHashes(t *testing.T) {
	// Dasselbe Format und dieselben Parameter wie panel.HashPassword
	salt := []byte("0123456789abcdef")
	key := argon2.IDKey([]byte("sftp-passwort-123"), salt, 2, 32*1024, 2, 32)
	encoded := fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", 32*1024, 2, 2, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))
	if !checkArgon2id([]byte("sftp-passwort-123"), encoded) {
		t.Fatal("richtiges Passwort abgelehnt")
	}
	if checkArgon2id([]byte("sftp-passwort-124"), encoded) {
		t.Fatal("falsches Passwort angenommen")
	}
	if _, err := parseArgon2id(sftpDummyHash); err != nil {
		t.Fatalf("Ersatz-Hash: %v", err)
	}
	for _, bad := range []string{"", "$2a$10$bcrypt", "$argon2id$v=19$m=999999999,t=2,p=2$c2FsdHNhbHQ$" + base64.RawStdEncoding.EncodeToString(key)} {
		if _, err := parseArgon2id(bad); err == nil {
			t.Errorf("%q wurde angenommen", bad)
		}
	}
}
