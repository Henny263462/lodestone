package node

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/ssh"
)

const (
	sftpHostKeyFile = "sftp_host_ed25519_key"
	// Höchstens so viele Verbindungen gleichzeitig; mehr werden sofort getrennt.
	maxSFTPConns = 64
	// Bis zur fertigen Anmeldung hat eine Verbindung so lange Zeit.
	sftpHandshakeTimeout = 30 * time.Second
	// Drosselung: so viele Fehlversuche je Adresse im Fenster, danach ist die Adresse gesperrt.
	sftpThrottleWindow = 10 * time.Minute
	sftpThrottleLimit  = 8
	sftpEventKeep      = 200
)

// SFTPService ist der SFTP-Zugang zu den Serverordnern. Wer hinein darf, bestimmt allein das Panel: Es schickt je
// Server die Zugänge (SetAccess). Jede Dateioperation prüft erneut, ob der Zugang noch gilt.
type SFTPService struct {
	cfg     Config
	manager *Manager
	log     *slog.Logger
	bootID  string

	signer      ssh.Signer
	fingerprint string

	// hashSlots begrenzt gleichzeitige argon2-Prüfungen (je 32 MB), damit Anmeldeversuche den Speicher nicht fluten.
	hashSlots chan struct{}
	// serving ist wahr, solange Verbindungen angenommen werden.
	serving atomic.Bool

	mu       sync.Mutex
	access   map[string][]sftpGrant // Server-ID → Zugänge
	conns    map[*sftpConn]struct{}
	events   []SFTPEvent
	seq      int64
	failures map[string][]time.Time
}

// sftpConn ist eine angemeldete Verbindung.
type sftpConn struct {
	conn     *ssh.ServerConn
	serverID string
	username string
}

// NewSFTPService baut den Dienst und liest die gespeicherten Zugänge. Ist SFTP eingeschaltet, wird der Host-Schlüssel
// geladen oder beim ersten Mal erzeugt (im Datenordner).
func NewSFTPService(cfg Config, manager *Manager, logger *slog.Logger) (*SFTPService, error) {
	if err := validateSFTPListen(cfg.SFTPListen); err != nil {
		return nil, err
	}
	boot := make([]byte, 8)
	if _, err := rand.Read(boot); err != nil {
		return nil, err
	}
	s := &SFTPService{
		cfg: cfg, manager: manager, log: logger, bootID: hex.EncodeToString(boot),
		hashSlots: make(chan struct{}, 2),
		access:    map[string][]sftpGrant{}, conns: map[*sftpConn]struct{}{}, failures: map[string][]time.Time{},
	}
	if cfg.SFTPListen != "" {
		signer, err := loadHostKey(filepath.Join(cfg.DataDir, sftpHostKeyFile))
		if err != nil {
			return nil, fmt.Errorf("SFTP-Host-Schlüssel: %w", err)
		}
		s.signer, s.fingerprint = signer, ssh.FingerprintSHA256(signer.PublicKey())
	}
	s.loadAccess()
	return s, nil
}

// Fingerprint ist der SHA256-Fingerabdruck des Host-Schlüssels (leer, wenn SFTP aus ist).
func (s *SFTPService) Fingerprint() string { return s.fingerprint }

// loadHostKey liest den Host-Schlüssel oder legt ihn einmalig an. Er ändert sich danach nie, damit Programme wie
// FileZilla ihn wiedererkennen.
func loadHostKey(path string) (ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		_, private, genErr := ed25519.GenerateKey(rand.Reader)
		if genErr != nil {
			return nil, genErr
		}
		block, genErr := ssh.MarshalPrivateKey(private, "lodestone-node")
		if genErr != nil {
			return nil, genErr
		}
		data = pem.EncodeToMemory(block)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(data)
}

// ListenAndServe nimmt SFTP-Verbindungen an, bis ctx endet. Ist SFTP aus, kehrt es sofort zurück.
func (s *SFTPService) ListenAndServe(ctx context.Context) error {
	if s.cfg.SFTPListen == "" {
		return nil
	}
	listener, err := net.Listen("tcp", s.cfg.SFTPListen)
	if err != nil {
		return fmt.Errorf("SFTP auf %s: %w", s.cfg.SFTPListen, err)
	}
	s.log.Info("SFTP läuft", "listen", s.cfg.SFTPListen, "fingerprint", s.fingerprint)
	return s.Serve(ctx, listener)
}

// Serve nimmt Verbindungen auf listener an, bis ctx endet; danach werden alle Verbindungen getrennt.
func (s *SFTPService) Serve(ctx context.Context, listener net.Listener) error {
	if s.signer == nil {
		return errors.New("SFTP ist ausgeschaltet")
	}
	s.serving.Store(true)
	defer s.serving.Store(false)
	go func() {
		<-ctx.Done()
		_ = listener.Close()
		s.mu.Lock()
		for conn := range s.conns {
			_ = conn.conn.Close()
		}
		s.mu.Unlock()
	}()
	slots := make(chan struct{}, maxSFTPConns)
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			s.log.Warn("SFTP: Verbindung nicht angenommen", "error", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		select {
		case slots <- struct{}{}:
		default:
			_ = conn.Close()
			continue
		}
		go func() {
			defer func() { <-slots }()
			s.handle(conn)
		}()
	}
}

// attempt merkt sich, was eine Verbindung bei der Anmeldung versucht hat.
type attempt struct {
	triedKey, triedPassword bool
}

func (s *SFTPService) serverConfig(ip string, tried *attempt) *ssh.ServerConfig {
	config := &ssh.ServerConfig{
		MaxAuthTries:  6,
		ServerVersion: "SSH-2.0-Lodestone",
		PasswordCallback: func(meta ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			tried.triedPassword = true
			return s.checkPassword(ip, strings.ToLower(meta.User()), password)
		},
		PublicKeyCallback: func(meta ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			tried.triedKey = true
			return s.checkKey(ip, strings.ToLower(meta.User()), key)
		},
	}
	config.AddHostKey(s.signer)
	return config
}

var errSFTPDenied = errors.New("Anmeldung abgelehnt")

func (s *SFTPService) checkPassword(ip, username string, password []byte) (*ssh.Permissions, error) {
	if s.blocked(ip) {
		return nil, errSFTPDenied
	}
	serverID, grant, ok := s.lookup(username)
	if !ok || grant.PasswordHash == "" {
		// Namen im Lodestone-Schema kosten gleich viel Zeit, damit man nicht erraten kann, welche es gibt
		if strings.Contains(username, ".") {
			s.verify(password, sftpDummyHash)
		}
		s.fail(ip)
		return nil, errSFTPDenied
	}
	if !s.verify(password, grant.PasswordHash) {
		s.fail(ip)
		s.record(SFTPEvent{ServerID: serverID, Username: username, IP: ip, Method: "password"})
		s.log.Warn("SFTP: falsches Passwort", "user", username, "ip", ip)
		return nil, errSFTPDenied
	}
	return &ssh.Permissions{Extensions: map[string]string{"server": serverID, "user": username, "method": "password"}}, nil
}

func (s *SFTPService) checkKey(ip, username string, key ssh.PublicKey) (*ssh.Permissions, error) {
	if s.blocked(ip) {
		return nil, errSFTPDenied
	}
	serverID, grant, ok := s.lookup(username)
	if !ok {
		return nil, errSFTPDenied
	}
	offered := key.Marshal()
	for _, known := range grant.keys {
		if subtle.ConstantTimeCompare(known, offered) == 1 {
			return &ssh.Permissions{Extensions: map[string]string{
				"server": serverID, "user": username, "method": "publickey", "key": ssh.FingerprintSHA256(key),
			}}, nil
		}
	}
	// Ein nicht passender Schlüssel ist kein Fehlversuch: Programme bieten oft mehrere an.
	return nil, errSFTPDenied
}

// verify prüft ein Passwort gegen einen argon2id-Hash, höchstens zwei gleichzeitig.
func (s *SFTPService) verify(password []byte, encoded string) bool {
	s.hashSlots <- struct{}{}
	defer func() { <-s.hashSlots }()
	return checkArgon2id(password, encoded)
}

// lookup sucht den Zugang zu einem Anmeldenamen. Gibt es den Namen doppelt oder den Server nicht mehr, gilt er nicht.
func (s *SFTPService) lookup(username string) (string, sftpGrant, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var found sftpGrant
	serverID, count := "", 0
	for id, grants := range s.access {
		for _, grant := range grants {
			if grant.Username == username {
				found, serverID, count = grant, id, count+1
			}
		}
	}
	if count != 1 {
		return "", sftpGrant{}, false
	}
	if _, ok := s.manager.Get(serverID); !ok {
		delete(s.access, serverID)
		return "", sftpGrant{}, false
	}
	return serverID, found, true
}

// allowed prüft vor jeder Dateioperation, ob der Zugang noch besteht (und schreiben darf).
func (s *SFTPService) allowed(serverID, username string, write bool) (*Server, error) {
	server, ok := s.manager.Get(serverID)
	if !ok {
		return nil, sftp.ErrSSHFxPermissionDenied
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, grant := range s.access[serverID] {
		if grant.Username == username {
			if write && grant.ReadOnly {
				return nil, sftp.ErrSSHFxPermissionDenied
			}
			return server, nil
		}
	}
	return nil, sftp.ErrSSHFxPermissionDenied
}

func (s *SFTPService) handle(raw net.Conn) {
	ip := remoteIP(raw.RemoteAddr())
	if s.blocked(ip) {
		_ = raw.Close()
		return
	}
	_ = raw.SetDeadline(time.Now().Add(sftpHandshakeTimeout))
	tried := &attempt{}
	conn, channels, requests, err := ssh.NewServerConn(raw, s.serverConfig(ip, tried))
	if err != nil {
		// Nur Schlüssel probiert und keiner passte: zählt als ein Fehlversuch der Verbindung
		if tried.triedKey && !tried.triedPassword {
			s.fail(ip)
		}
		_ = raw.Close()
		return
	}
	_ = raw.SetDeadline(time.Time{})
	extensions := conn.Permissions.Extensions
	session := &sftpConn{conn: conn, serverID: extensions["server"], username: extensions["user"]}
	s.reset(ip)
	s.record(SFTPEvent{ServerID: session.serverID, Username: session.username, IP: ip, Method: extensions["method"], KeyFingerprint: extensions["key"], Success: true})
	s.log.Info("SFTP: angemeldet", "user", session.username, "server", session.serverID, "ip", ip, "method", extensions["method"])
	if server, ok := s.manager.Get(session.serverID); ok {
		server.logf("SFTP: %s hat sich angemeldet (%s)", session.username, ip)
	}

	s.mu.Lock()
	s.conns[session] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.conns, session)
		s.mu.Unlock()
		_ = conn.Close()
	}()

	go ssh.DiscardRequests(requests)
	for incoming := range channels {
		if incoming.ChannelType() != "session" {
			_ = incoming.Reject(ssh.UnknownChannelType, "hier gibt es nur SFTP")
			continue
		}
		channel, channelRequests, err := incoming.Accept()
		if err != nil {
			continue
		}
		go s.serveChannel(session, channel, channelRequests)
	}
}

// serveChannel beantwortet nur den Wunsch nach dem Subsystem „sftp“; Shell, Befehle und Weiterleitungen gibt es nicht.
func (s *SFTPService) serveChannel(session *sftpConn, channel ssh.Channel, requests <-chan *ssh.Request) {
	started := false
	for request := range requests {
		ok := !started && request.Type == "subsystem" && subsystemName(request.Payload) == "sftp"
		if request.WantReply {
			_ = request.Reply(ok, nil)
		}
		if !ok {
			continue
		}
		started = true
		go func() {
			handler := &sftpHandler{service: s, serverID: session.serverID, username: session.username, maxWrite: int64(s.cfg.MaxUploadMB) << 20}
			server := sftp.NewRequestServer(channel, sftp.Handlers{FileGet: handler, FilePut: handler, FileCmd: handler, FileList: handler})
			_ = server.Serve()
			_ = server.Close()
			_ = channel.Close()
		}()
	}
	if !started {
		_ = channel.Close()
	}
}

func subsystemName(payload []byte) string {
	if len(payload) < 4 {
		return ""
	}
	size := binary.BigEndian.Uint32(payload)
	if int(size) != len(payload)-4 {
		return ""
	}
	return string(payload[4:])
}

func remoteIP(address net.Addr) string {
	host, _, err := net.SplitHostPort(address.String())
	if err != nil {
		return address.String()
	}
	return host
}

// ---------------------------------------------------------------- Drosselung und Ereignisse

func (s *SFTPService) recentFailures(ip string) []time.Time {
	cutoff := time.Now().Add(-sftpThrottleWindow)
	list := s.failures[ip][:0]
	for _, at := range s.failures[ip] {
		if at.After(cutoff) {
			list = append(list, at)
		}
	}
	if len(list) == 0 {
		delete(s.failures, ip)
	} else {
		s.failures[ip] = list
	}
	return list
}

func (s *SFTPService) blocked(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.recentFailures(ip)) >= sftpThrottleLimit
}

func (s *SFTPService) fail(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.recentFailures(ip)
	s.failures[ip] = append(list, time.Now())
	if len(list)+1 == sftpThrottleLimit {
		s.log.Warn("SFTP: zu viele Fehlversuche, die Adresse ist vorübergehend gesperrt", "ip", ip)
	}
}

func (s *SFTPService) reset(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.failures, ip)
}

func (s *SFTPService) record(event SFTPEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	event.Seq, event.At = s.seq, time.Now().UnixMilli()
	s.events = append(s.events, event)
	if len(s.events) > sftpEventKeep {
		s.events = append([]SFTPEvent(nil), s.events[len(s.events)-sftpEventKeep:]...)
	}
}

// ---------------------------------------------------------------- argon2id (Format des Panels)

type argon2Params struct {
	memory, iterations uint32
	threads            uint8
	salt, key          []byte
}

// parseArgon2id liest einen Hash der Form $argon2id$v=19$m=…,t=…,p=…$salz$schlüssel und lehnt unvernünftige Werte ab.
func parseArgon2id(encoded string) (argon2Params, error) {
	var params argon2Params
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return params, errors.New("kein argon2id-Hash")
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &params.memory, &params.iterations, &params.threads); err != nil {
		return params, err
	}
	if params.memory < 1024 || params.memory > 256*1024 || params.iterations < 1 || params.iterations > 10 || params.threads < 1 || params.threads > 16 {
		return params, errors.New("argon2id-Parameter außerhalb des erlaubten Bereichs")
	}
	var err error
	if params.salt, err = base64.RawStdEncoding.DecodeString(parts[4]); err != nil || len(params.salt) < 8 {
		return params, errors.New("ungültiges Salz")
	}
	if params.key, err = base64.RawStdEncoding.DecodeString(parts[5]); err != nil || len(params.key) < 16 || len(params.key) > 64 {
		return params, errors.New("ungültiger Schlüssel")
	}
	return params, nil
}

func checkArgon2id(password []byte, encoded string) bool {
	params, err := parseArgon2id(encoded)
	if err != nil {
		return false
	}
	got := argon2.IDKey(password, params.salt, params.iterations, params.memory, params.threads, uint32(len(params.key)))
	return subtle.ConstantTimeCompare(got, params.key) == 1
}

// sftpDummyHash hat die Parameter des Panels; damit dauern unbekannte Namen so lange wie bekannte.
const sftpDummyHash = "$argon2id$v=19$m=32768,t=2,p=2$bG9kZXN0b25lLXNmdHA$YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWE"
