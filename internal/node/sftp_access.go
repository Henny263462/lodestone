package node

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/crypto/ssh"
)

// SFTPGrant ist ein Zugang zu einem Server über SFTP. Das Panel schickt die vollständige Liste je Server; der Node
// entscheidet nichts selbst, er prüft nur dagegen.
type SFTPGrant struct {
	// Username ist der Anmeldename, z. B. "anna.3f2a9c1d" (Benutzer + Kurz-ID des Servers), immer klein geschrieben.
	Username string `json:"username"`
	// PasswordHash ist ein argon2id-Hash im Format des Panels; leer heißt: keine Anmeldung mit Passwort.
	PasswordHash string `json:"passwordHash,omitempty"`
	// Keys sind öffentliche SSH-Schlüssel im authorized_keys-Format.
	Keys []string `json:"keys,omitempty"`
	// ReadOnly erlaubt nur Lesen.
	ReadOnly bool `json:"readOnly"`
}

// SFTPAccess ist die vollständige Liste der Zugänge eines Servers.
type SFTPAccess struct {
	Grants []SFTPGrant `json:"grants"`
}

// SFTPEvent ist eine Anmeldung über SFTP (gelungen oder mit falschem Passwort für einen bekannten Zugang).
type SFTPEvent struct {
	Seq      int64  `json:"seq"`
	At       int64  `json:"at"`
	ServerID string `json:"serverId"`
	Username string `json:"username"`
	IP       string `json:"ip"`
	// Method ist "password" oder "publickey".
	Method string `json:"method"`
	// KeyFingerprint ist der SHA256-Fingerabdruck des benutzten Schlüssels.
	KeyFingerprint string `json:"keyFingerprint,omitempty"`
	Success        bool   `json:"success"`
}

// SFTPStatus beschreibt den SFTP-Zugang des Nodes.
type SFTPStatus struct {
	Enabled bool `json:"enabled"`
	// Port ist der Port, auf dem der Node SFTP annimmt (0, wenn aus).
	Port int `json:"port"`
	// Fingerprint ist der SHA256-Fingerabdruck des Host-Schlüssels.
	Fingerprint string `json:"fingerprint"`
	// BootID wechselt bei jedem Start des Daemons; das Panel gleicht dann alle Zugänge neu ab.
	BootID string      `json:"bootId"`
	Events []SFTPEvent `json:"events"`
}

const (
	sftpAccessFile = "sftp-access.json"
	maxSFTPGrants  = 200
	maxSFTPKeys    = 50
)

var sftpUsername = regexp.MustCompile(`^[a-z0-9_.-]{1,64}$`)

// sftpGrant ist ein geprüfter Zugang mit eingelesenen Schlüsseln.
type sftpGrant struct {
	SFTPGrant
	keys [][]byte // ssh.PublicKey.Marshal()
}

// parseGrants prüft die Liste des Panels streng, bevor sie gilt.
func parseGrants(access SFTPAccess) ([]sftpGrant, error) {
	if len(access.Grants) > maxSFTPGrants {
		return nil, badRequest("höchstens %d Zugänge je Server", maxSFTPGrants)
	}
	seen := map[string]bool{}
	grants := make([]sftpGrant, 0, len(access.Grants))
	for _, grant := range access.Grants {
		if !sftpUsername.MatchString(grant.Username) {
			return nil, badRequest("ungültiger SFTP-Benutzername %q", grant.Username)
		}
		if seen[grant.Username] {
			return nil, badRequest("der SFTP-Benutzername %q kommt doppelt vor", grant.Username)
		}
		seen[grant.Username] = true
		if grant.PasswordHash != "" {
			if _, err := parseArgon2id(grant.PasswordHash); err != nil {
				return nil, badRequest("ungültiger Passwort-Hash für %q", grant.Username)
			}
		}
		if len(grant.Keys) > maxSFTPKeys {
			return nil, badRequest("höchstens %d Schlüssel je Zugang", maxSFTPKeys)
		}
		parsed := sftpGrant{SFTPGrant: grant}
		for _, line := range grant.Keys {
			key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
			if err != nil {
				return nil, badRequest("ungültiger SSH-Schlüssel für %q", grant.Username)
			}
			parsed.keys = append(parsed.keys, key.Marshal())
		}
		grants = append(grants, parsed)
	}
	return grants, nil
}

// loadAccess liest die gespeicherten Zugänge aller Server (sie liegen neben spec.json, außerhalb der Serverdateien).
func (s *SFTPService) loadAccess() {
	for _, server := range s.manager.List() {
		data, err := os.ReadFile(filepath.Join(server.dir, sftpAccessFile))
		if err != nil {
			continue
		}
		var access SFTPAccess
		if err := json.Unmarshal(data, &access); err != nil {
			s.log.Warn("SFTP-Zugänge sind unlesbar und werden ignoriert", "server", server.ID(), "error", err)
			continue
		}
		grants, err := parseGrants(access)
		if err != nil {
			s.log.Warn("SFTP-Zugänge sind ungültig und werden ignoriert", "server", server.ID(), "error", err)
			continue
		}
		s.access[server.ID()] = grants
	}
}

// SetAccess ersetzt die Zugänge eines Servers, speichert sie und trennt Verbindungen, deren Zugang, Passwort oder
// Schlüssel weggefallen ist oder die nur noch lesen dürfen (siehe sftpConn.refresh).
func (s *SFTPService) SetAccess(serverID string, access SFTPAccess) error {
	server, ok := s.manager.Get(serverID)
	if !ok {
		return ErrNotFound
	}
	grants, err := parseGrants(access)
	if err != nil {
		return err
	}
	if access.Grants == nil {
		access.Grants = []SFTPGrant{}
	}
	data, err := json.MarshalIndent(access, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(server.dir, sftpAccessFile)
	if err := os.WriteFile(path+".tmp", data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		return err
	}
	s.mu.Lock()
	s.access[serverID] = grants
	var stale []*sftpConn
	for conn := range s.conns {
		if conn.serverID == serverID && !conn.refresh(grants) {
			stale = append(stale, conn)
		}
	}
	s.mu.Unlock()
	for _, conn := range stale {
		s.log.Info("SFTP-Verbindung getrennt, der Zugang wurde widerrufen", "user", conn.username, "server", serverID)
		_ = conn.conn.Close()
	}
	return nil
}

// Status liefert den Zustand und die Anmeldungen nach der Folgenummer after.
func (s *SFTPService) Status(after int64) SFTPStatus {
	// Eingeschaltet heißt: Der Dienst nimmt gerade Verbindungen an (ein belegter Port zählt als aus).
	status := SFTPStatus{Enabled: s.serving.Load(), Fingerprint: s.fingerprint, BootID: s.bootID, Events: []SFTPEvent{}}
	if status.Enabled {
		if _, port, err := net.SplitHostPort(s.cfg.SFTPListen); err == nil {
			status.Port, _ = strconv.Atoi(port)
		}
	}
	s.mu.Lock()
	for _, event := range s.events {
		if event.Seq > after {
			status.Events = append(status.Events, event)
		}
	}
	s.mu.Unlock()
	return status
}

// ---------------------------------------------------------------- Schnittstelle

// SetSFTP hängt den SFTP-Dienst an die Schnittstelle (vor Handler aufrufen).
func (a *API) SetSFTP(service *SFTPService) { a.sftp = service }

// registerSFTPRoutes trägt die Routen des SFTP-Zugangs ein. Ohne Dienst (Tests) antworten sie mit „aus“.
func (a *API) registerSFTPRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/sftp", func(w http.ResponseWriter, r *http.Request) {
		if a.sftp == nil {
			writeJSON(w, http.StatusOK, SFTPStatus{Events: []SFTPEvent{}})
			return
		}
		after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		writeJSON(w, http.StatusOK, a.sftp.Status(after))
	})
	mux.HandleFunc("PUT /api/servers/{id}/sftp-access", func(w http.ResponseWriter, r *http.Request) {
		if a.sftp == nil {
			writeError(w, errors.New("der SFTP-Dienst ist nicht eingerichtet"))
			return
		}
		var access SFTPAccess
		if err := decode(r, &access); err != nil {
			writeError(w, err)
			return
		}
		if err := a.sftp.SetAccess(r.PathValue("id"), access); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]int{"grants": len(access.Grants)})
	})
}

// applySFTPEnvironment liest LODESTONE_NODE_SFTP_LISTEN. Gesetzt, aber leer (oder "off") schaltet SFTP ab.
func (c *Config) applySFTPEnvironment() {
	value, ok := os.LookupEnv("LODESTONE_NODE_SFTP_LISTEN")
	if !ok {
		return
	}
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, "off") {
		value = ""
	}
	c.SFTPListen = value
}

// validateSFTPListen prüft die Adresse, bevor der Dienst startet.
func validateSFTPListen(address string) error {
	if address == "" {
		return nil
	}
	if _, port, err := net.SplitHostPort(address); err != nil || port == "" {
		return fmt.Errorf("LODESTONE_NODE_SFTP_LISTEN=%q ist keine gültige Adresse (Beispiel: :2022)", address)
	}
	return nil
}
