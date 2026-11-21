package panel

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lodestone-mc/lodestone/internal/node"
)

// Discord-Bot über den Interactions-Endpunkt: Discord schickt jeden Slash-Befehl als signierte HTTP-Anfrage an das
// Panel (kein Gateway, keine dauerhafte Verbindung). Das geht nur, wenn Discord das Panel öffentlich per HTTPS erreicht.

const (
	discordAPI          = "https://discord.com/api/v10"
	discordBodyLimit    = 64 << 10
	discordClockSkew    = 5 * time.Minute
	discordRequestLimit = 120 // pro Minute und Projekt
	discordAnswerWithin = 2500 * time.Millisecond
	discordEphemeral    = 1 << 6
)

// discordCommands sind die Slash-Befehle mit dem Befehl im Panel, den sie brauchen.
var discordCommands = []struct {
	name, action, description string
}{
	{"status", "status", "Zeigt, welche Server laufen"},
	{"spieler", "players", "Zeigt, wer gerade online ist"},
	{"start", "start", "Startet einen Server"},
	{"stop", "stop", "Stoppt einen Server"},
	{"restart", "restart", "Startet einen Server neu"},
}

func (p *Panel) discordBase() string {
	if p.auto.discordBase != "" {
		return p.auto.discordBase
	}
	return discordAPI
}

func (p *Panel) discordClient() *http.Client {
	if p.auto.discordHTTP != nil {
		return p.auto.discordHTTP
	}
	return &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// commandPayload baut die Befehle für Discord. Die Server des Projekts stehen als Auswahl darin (höchstens 25).
func commandPayload(servers []ServerRow) []map[string]any {
	choices := []map[string]string{}
	for _, row := range servers {
		if len(choices) == 25 {
			break
		}
		choices = append(choices, map[string]string{"name": clip(row.Name, 100), "value": row.ID})
	}
	commands := []map[string]any{}
	for _, command := range discordCommands {
		option := map[string]any{"type": 3, "name": "server", "description": "Welcher Server", "required": false}
		if len(choices) > 0 {
			option["choices"] = choices
		}
		commands = append(commands, map[string]any{"name": command.name, "description": command.description, "type": 1, "options": []any{option}})
	}
	return commands
}

// registerDiscord trägt die Slash-Befehle bei Discord ein (ersetzt die vorhandenen der Anwendung).
func (p *Panel) registerDiscord(w http.ResponseWriter, r *http.Request) {
	project, _, err := p.requireProject(r, r.PathValue("id"), rankAdmin)
	if err != nil {
		writeError(w, err)
		return
	}
	bot, err := p.store.DiscordBot(project.ID)
	if errors.Is(err, ErrNotFound) || err == nil && bot.BotToken == "" {
		writeError(w, fail(http.StatusConflict, "trag zuerst Application-ID, Public Key und Bot-Token ein"))
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	token, err := p.unseal(bot.BotToken)
	if err != nil {
		writeError(w, fail(http.StatusConflict, "das gespeicherte Bot-Token lässt sich nicht mehr lesen (secret.key geändert?). Trag es neu ein"))
		return
	}
	servers, err := p.store.ListServers(project.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	payload, _ := json.Marshal(commandPayload(servers))
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, p.discordBase()+"/applications/"+bot.ApplicationID+"/commands", bytes.NewReader(payload))
	if err != nil {
		writeError(w, err)
		return
	}
	request.Header.Set("Authorization", "Bot "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "DiscordBot (https://github.com/lodestone-mc/lodestone, 1)")
	response, err := p.discordClient().Do(request)
	if err != nil {
		writeError(w, fail(http.StatusBadGateway, "Discord ist nicht erreichbar. Prüfe die Internetverbindung des Panels"))
		return
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	switch {
	case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
		writeError(w, fail(http.StatusBadGateway, "Discord lehnt das Bot-Token ab. Prüfe Application-ID und Bot-Token"))
		return
	case response.StatusCode >= 300:
		writeError(w, fail(http.StatusBadGateway, fmt.Sprintf("Discord hat die Befehle abgelehnt (Antwort %d). Prüfe die Application-ID", response.StatusCode)))
		return
	}
	bot.RegisteredAt = time.Now().UnixMilli()
	if err := p.store.SaveDiscordBot(bot); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, discordView(project.ID, bot, true))
}

// ---------------------------------------------------------------- Interactions

type discordInteraction struct {
	Type int `json:"type"`
	Data struct {
		Name    string `json:"name"`
		Options []struct {
			Name  string          `json:"name"`
			Value json.RawMessage `json:"value"`
		} `json:"options"`
	} `json:"data"`
	Member *struct {
		User  discordUser `json:"user"`
		Roles []string    `json:"roles"`
	} `json:"member"`
	User *discordUser `json:"user"`
}

type discordUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

// verifyDiscord prüft die Signatur einer Anfrage von Discord (ed25519 über Zeitstempel + Körper).
func verifyDiscord(publicKey, signature, timestamp string, body []byte, now time.Time) bool {
	key, err := hex.DecodeString(publicKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return false
	}
	sig, err := hex.DecodeString(signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return false
	}
	if skew := now.Sub(time.Unix(seconds, 0)); skew > discordClockSkew || skew < -discordClockSkew {
		return false // alte Anfragen nicht noch einmal annehmen
	}
	return ed25519.Verify(key, append([]byte(timestamp), body...), sig)
}

func discordReply(w http.ResponseWriter, content string, private bool) {
	data := map[string]any{"content": clip(content, 1900), "allowed_mentions": map[string]any{"parse": []string{}}}
	if private {
		data["flags"] = discordEphemeral
	}
	writeJSON(w, http.StatusOK, map[string]any{"type": 4, "data": data})
}

// discordInteraction ist der öffentliche Endpunkt für Discord. Ohne gültige Signatur antwortet er mit 401, so wie
// Discord es beim Eintragen der Adresse prüft.
func (p *Panel) discordInteraction(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	if !p.auto.discordLimit.allow(projectID, discordRequestLimit) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "zu viele Anfragen"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, discordBodyLimit+1))
	if err != nil || len(body) > discordBodyLimit {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ungültige Anfrage"})
		return
	}
	bot, err := p.store.DiscordBot(projectID)
	if err != nil || !bot.Enabled || !verifyDiscord(bot.PublicKey, r.Header.Get("X-Signature-Ed25519"), r.Header.Get("X-Signature-Timestamp"), body, time.Now()) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "ungültige Signatur"})
		return
	}
	var interaction discordInteraction
	if err := json.Unmarshal(body, &interaction); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ungültige Anfrage"})
		return
	}
	switch interaction.Type {
	case 1: // PING beim Eintragen der Adresse
		writeJSON(w, http.StatusOK, map[string]int{"type": 1})
		return
	case 2: // Slash-Befehl
	default:
		discordReply(w, "Das kann Lodestone nicht beantworten.", true)
		return
	}

	action := ""
	for _, command := range discordCommands {
		if command.name == interaction.Data.Name {
			action = command.action
		}
	}
	if action == "" {
		discordReply(w, "Diesen Befehl kennt Lodestone nicht. Ein Projekt-Admin kann die Befehle im Panel neu eintragen.", true)
		return
	}
	userID, username, roles := "", "", []string{}
	if interaction.Member != nil {
		userID, username, roles = interaction.Member.User.ID, interaction.Member.User.Username, interaction.Member.Roles
	} else if interaction.User != nil {
		userID, username = interaction.User.ID, interaction.User.Username
	}
	if !discordAllowed(bot.Rules, userID, roles, action) {
		discordReply(w, fmt.Sprintf("Dafür hast du keine Berechtigung. Ein Projekt-Admin muss dir im Panel den Befehl /%s erlauben.", interaction.Data.Name), true)
		return
	}
	serverArg := ""
	for _, option := range interaction.Data.Options {
		if option.Name == "server" {
			_ = json.Unmarshal(option.Value, &serverArg)
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), discordAnswerWithin)
	defer cancel()
	text, private := p.discordCommand(ctx, projectID, action, serverArg, userID, username)
	discordReply(w, text, private)
}

// discordAllowed prüft, ob eine Regel dem Benutzer (direkt oder über eine seiner Rollen) den Befehl erlaubt.
func discordAllowed(rules []DiscordRule, userID string, roles []string, action string) bool {
	for _, rule := range rules {
		if !slices.Contains(rule.Actions, action) {
			continue
		}
		if rule.Kind == "user" && rule.ID == userID && userID != "" || rule.Kind == "role" && slices.Contains(roles, rule.ID) {
			return true
		}
	}
	return false
}

var stateWords = map[node.State]string{
	node.StateRunning: "läuft", node.StateStarting: "startet", node.StateStopping: "stoppt", node.StateOffline: "aus",
	node.StateInstalling: "wird installiert", node.StateRestoring: "stellt ein Backup wieder her",
}

// discordCommand führt den Befehl aus und liefert die Antwort (private = nur für den Fragenden sichtbar).
func (p *Panel) discordCommand(ctx context.Context, projectID, action, serverArg, userID, username string) (string, bool) {
	servers, err := p.store.ListServers(projectID)
	if err != nil || len(servers) == 0 {
		return "In diesem Projekt gibt es noch keinen Server.", true
	}
	if action == "status" && serverArg == "" {
		var lines []string
		for _, row := range servers {
			lines = append(lines, p.discordStatusLine(ctx, row))
		}
		return strings.Join(lines, "\n"), false
	}
	// Ohne Angabe darf Lesen den Proxy nehmen; Starten und Stoppen braucht bei mehreren Servern eine klare Wahl.
	row, problem := pickServer(servers, serverArg, action == "status" || action == "players")
	if problem != "" {
		return problem, true
	}
	switch action {
	case "status":
		return p.discordStatusLine(ctx, row), false
	case "players":
		live := p.liveOf(row.ID)
		names := live.Names
		if client, _, err := p.client(row.NodeID); err == nil {
			var info node.PlayerInfo
			if client.Do(ctx, http.MethodGet, "/api/servers/"+row.ID+"/players", nil, &info) == nil {
				names, live.Players = info.Names, info.Online
			}
		}
		if live.Players == 0 {
			return fmt.Sprintf("Auf **%s** ist gerade niemand online.", row.Name), false
		}
		if len(names) == 0 {
			return fmt.Sprintf("Auf **%s** sind %d Spieler online.", row.Name, live.Players), false
		}
		return fmt.Sprintf("Auf **%s** sind %d Spieler online: %s", row.Name, live.Players, strings.Join(names, ", ")), false
	}
	client, _, err := p.client(row.NodeID)
	if err != nil {
		return "Der Node dieses Servers ist nicht eingetragen.", true
	}
	if err := client.Do(ctx, http.MethodPost, "/api/servers/"+row.ID+"/power", map[string]string{"action": action}, nil); err != nil {
		var nodeErr *NodeError
		if errors.As(err, &nodeErr) && nodeErr.Status == http.StatusConflict {
			return fmt.Sprintf("**%s** kann das gerade nicht: %s.", row.Name, nodeErr.Message), true
		}
		return fmt.Sprintf("Der Node von **%s** antwortet nicht. Versuch es gleich noch einmal.", row.Name), true
	}
	p.invalidate(row.NodeID)
	p.recordAudit("", "discord:"+clip(username, 32), "POST discord /"+action, row.Name+" (Discord-ID "+userID+")", http.StatusOK, "")
	words := map[string]string{"start": "wird gestartet", "stop": "wird gestoppt", "restart": "wird neu gestartet"}
	return fmt.Sprintf("**%s** %s.", row.Name, words[action]), false
}

func (p *Panel) discordStatusLine(ctx context.Context, row ServerRow) string {
	views, err := p.nodeViews(ctx, row.NodeID)
	view, ok := views[row.ID]
	if err != nil || !ok {
		return fmt.Sprintf("**%s** – Node nicht erreichbar", row.Name)
	}
	line := fmt.Sprintf("**%s** – %s", row.Name, stateWords[view.State])
	if view.State == node.StateRunning {
		live := p.liveOf(row.ID)
		if live.MaxPlayers > 0 {
			line += fmt.Sprintf(" · %d/%d Spieler", live.Players, live.MaxPlayers)
		} else {
			line += fmt.Sprintf(" · %d Spieler", live.Players)
		}
	}
	return line
}

// pickServer findet den gemeinten Server: über die ID (Auswahl in Discord), den Namen oder, wenn es nur einen gibt, ihn.
// Mit proxyDefault nimmt es ohne Angabe den Proxy eines Netzwerks.
func pickServer(servers []ServerRow, arg string, proxyDefault bool) (ServerRow, string) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		if len(servers) == 1 {
			return servers[0], ""
		}
		for _, row := range servers {
			if proxyDefault && row.Role == "proxy" {
				return row, ""
			}
		}
		names := []string{}
		for _, row := range servers {
			names = append(names, row.Name)
		}
		return ServerRow{}, "Gib mit server: an, welchen Server du meinst: " + strings.Join(names, ", ")
	}
	for _, row := range servers {
		if row.ID == arg || strings.EqualFold(row.Name, arg) || strings.EqualFold(row.Slug, arg) {
			return row, ""
		}
	}
	return ServerRow{}, "Diesen Server gibt es im Projekt nicht."
}
