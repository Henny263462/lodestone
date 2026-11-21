package panel

import (
	"context"
	"errors"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/lodestone-mc/lodestone/internal/node"
)

// registerAutomatikRoutes hängt die Routen für Schlafmodus, Absturzanalyse, Konsolen-Auslöser, Discord-Bot und
// Prometheus ein.
func (p *Panel) registerAutomatikRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/servers/{id}/sleep", p.authenticate(p.getSleep))
	mux.HandleFunc("PUT /api/servers/{id}/sleep", p.authenticate(p.putSleep))
	mux.HandleFunc("GET /api/servers/{id}/crash-analysis", p.authenticate(p.getCrashAnalysis))
	mux.HandleFunc("GET /api/projects/{id}/console-triggers", p.authenticate(p.listTriggers))
	mux.HandleFunc("POST /api/projects/{id}/console-triggers", p.authenticate(p.createTrigger))
	mux.HandleFunc("PATCH /api/console-triggers/{id}", p.authenticate(p.updateTrigger))
	mux.HandleFunc("DELETE /api/console-triggers/{id}", p.authenticate(p.deleteTrigger))
	mux.HandleFunc("GET /api/projects/{id}/discord", p.authenticate(p.getDiscord))
	mux.HandleFunc("PUT /api/projects/{id}/discord", p.authenticate(p.putDiscord))
	mux.HandleFunc("DELETE /api/projects/{id}/discord", p.authenticate(p.deleteDiscord))
	mux.HandleFunc("POST /api/projects/{id}/discord/register", p.authenticate(p.registerDiscord))
	mux.HandleFunc("POST /api/public/discord/{id}/interactions", p.discordInteraction)
	mux.HandleFunc("GET /metrics", p.prometheus(os.Getenv("LODESTONE_METRICS_TOKEN")))
}

// ---------------------------------------------------------------- Schlafmodus

type sleepDTO struct {
	node.SleepStatus
	// Allowed ist false für Server, bei denen ein Beitritt nicht ankommt (Backend hinter dem Proxy, Gruppen-Instanz).
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
}

func sleepAllowed(row ServerRow) (bool, string) {
	switch {
	case row.GroupID != "":
		return false, "Instanzen einer Gruppe startet und stoppt der Autoscaler, der Schlafmodus gilt hier nicht."
	case row.Role == "backend":
		return false, "Backend-Server hinter dem Proxy erreichen Spieler nicht direkt, ein Beitritt kann sie also nicht wecken. Schalte den Schlafmodus beim Proxy ein: Schläft er ein, stoppt Lodestone auch die Backend-Server, und wacht er auf, startet es sie mit."
	}
	return true, ""
}

func (p *Panel) getSleep(w http.ResponseWriter, r *http.Request) {
	row, _, _, err := p.requireServer(r, rankViewer)
	if err != nil {
		writeError(w, err)
		return
	}
	client, _, err := p.client(row.NodeID)
	if err != nil {
		writeError(w, err)
		return
	}
	var status node.SleepStatus
	if err := client.Do(r.Context(), http.MethodGet, "/api/servers/"+row.ID+"/sleep", nil, &status); err != nil {
		writeError(w, err)
		return
	}
	allowed, reason := sleepAllowed(row)
	writeJSON(w, http.StatusOK, sleepDTO{SleepStatus: status, Allowed: allowed, Reason: reason})
}

func (p *Panel) putSleep(w http.ResponseWriter, r *http.Request) {
	row, _, _, err := p.requireServer(r, rankAdmin)
	if err != nil {
		writeError(w, err)
		return
	}
	var body struct {
		Enabled     bool `json:"enabled"`
		IdleMinutes int  `json:"idleMinutes"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	allowed, reason := sleepAllowed(row)
	if body.Enabled && !allowed {
		writeError(w, fail(http.StatusBadRequest, reason))
		return
	}
	if body.IdleMinutes < 0 || body.IdleMinutes > 1440 {
		writeError(w, fail(http.StatusBadRequest, "die Leerlaufzeit muss zwischen 1 und 1440 Minuten liegen"))
		return
	}
	client, _, err := p.client(row.NodeID)
	if err != nil {
		writeError(w, err)
		return
	}
	var status node.SleepStatus
	if err := client.Do(r.Context(), http.MethodPut, "/api/servers/"+row.ID+"/sleep", node.SleepConfig{Enabled: body.Enabled, IdleMinutes: body.IdleMinutes}, &status); err != nil {
		writeError(w, err)
		return
	}
	p.invalidate(row.NodeID)
	writeJSON(w, http.StatusOK, sleepDTO{SleepStatus: status, Allowed: allowed, Reason: reason})
}

// ---------------------------------------------------------------- Absturzanalyse

func (p *Panel) getCrashAnalysis(w http.ResponseWriter, r *http.Request) {
	row, _, _, err := p.requireServer(r, rankViewer)
	if err != nil {
		writeError(w, err)
		return
	}
	client, _, err := p.client(row.NodeID)
	if err != nil {
		writeError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	var analysis node.CrashAnalysis
	if err := client.Do(ctx, http.MethodGet, "/api/servers/"+row.ID+"/crash-analysis", nil, &analysis); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, analysis)
}

// ---------------------------------------------------------------- Konsolen-Auslöser

const (
	maxTriggers       = 20
	maxTriggerPattern = 200
)

var triggerLevels = []string{"info", "warn", "error"}

type triggerRequest struct {
	Name       *string `json:"name"`
	ServerID   *string `json:"serverId"`
	Pattern    *string `json:"pattern"`
	Level      *string `json:"level"`
	ScheduleID *string `json:"scheduleId"`
	Enabled    *bool   `json:"enabled"`
}

func (p *Panel) applyTrigger(trigger *ConsoleTrigger, body triggerRequest) error {
	if body.Name != nil {
		trigger.Name = strings.TrimSpace(*body.Name)
	}
	if body.ServerID != nil {
		trigger.ServerID = *body.ServerID
	}
	if body.Pattern != nil {
		trigger.Pattern = *body.Pattern
	}
	if body.Level != nil {
		trigger.Level = *body.Level
	}
	if body.ScheduleID != nil {
		trigger.ScheduleID = *body.ScheduleID
	}
	if body.Enabled != nil {
		trigger.Enabled = *body.Enabled
	}
	if trigger.Name == "" || len(trigger.Name) > 48 {
		return fail(http.StatusBadRequest, "der Name braucht 1 bis 48 Zeichen")
	}
	if strings.TrimSpace(trigger.Pattern) == "" || len(trigger.Pattern) > maxTriggerPattern || strings.ContainsAny(trigger.Pattern, "\r\n\x00") {
		return fail(http.StatusBadRequest, "das Muster braucht 1 bis 200 Zeichen in einer Zeile")
	}
	re, err := regexp.Compile(trigger.Pattern)
	if err != nil {
		return fail(http.StatusBadRequest, "das Muster ist kein gültiger regulärer Ausdruck. Prüfe Klammern und Sonderzeichen (eine echte Klammer schreibst du als \\( )")
	}
	if re.MatchString("") {
		return fail(http.StatusBadRequest, "das Muster passt auf jede Zeile. Mach es genauer, z. B. mit einem festen Wort")
	}
	if !slices.Contains(triggerLevels, trigger.Level) {
		return fail(http.StatusBadRequest, "die Stufe muss info, warn oder error sein")
	}
	if trigger.ServerID != "" {
		if row, err := p.store.ServerByID(trigger.ServerID); err != nil || row.ProjectID != trigger.ProjectID {
			return fail(http.StatusBadRequest, "diesen Server gibt es im Projekt nicht")
		}
	}
	if trigger.ScheduleID != "" {
		if schedule, err := p.store.ScheduleByID(trigger.ScheduleID); err != nil || schedule.ProjectID != trigger.ProjectID {
			return fail(http.StatusBadRequest, "diesen Zeitplan gibt es im Projekt nicht")
		}
	}
	return nil
}

func (p *Panel) listTriggers(w http.ResponseWriter, r *http.Request) {
	project, _, err := p.requireProject(r, r.PathValue("id"), rankAdmin)
	if err != nil {
		writeError(w, err)
		return
	}
	list, err := p.store.ListTriggers(project.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (p *Panel) createTrigger(w http.ResponseWriter, r *http.Request) {
	project, _, err := p.requireProject(r, r.PathValue("id"), rankAdmin)
	if err != nil {
		writeError(w, err)
		return
	}
	var body triggerRequest
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	existing, err := p.store.ListTriggers(project.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	if len(existing) >= maxTriggers {
		writeError(w, fail(http.StatusConflict, "höchstens 20 Konsolen-Auslöser pro Projekt"))
		return
	}
	trigger := ConsoleTrigger{ProjectID: project.ID, Level: "info", Enabled: true}
	if err := p.applyTrigger(&trigger, body); err != nil {
		writeError(w, err)
		return
	}
	created, err := p.store.CreateTrigger(trigger)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (p *Panel) requireTrigger(r *http.Request) (ConsoleTrigger, error) {
	trigger, err := p.store.TriggerByID(r.PathValue("id"))
	if err != nil {
		return ConsoleTrigger{}, errNotFound
	}
	if _, _, err := p.requireProject(r, trigger.ProjectID, rankAdmin); err != nil {
		return ConsoleTrigger{}, err
	}
	return trigger, nil
}

func (p *Panel) updateTrigger(w http.ResponseWriter, r *http.Request) {
	trigger, err := p.requireTrigger(r)
	if err != nil {
		writeError(w, err)
		return
	}
	var body triggerRequest
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if err := p.applyTrigger(&trigger, body); err != nil {
		writeError(w, err)
		return
	}
	if err := p.store.UpdateTrigger(trigger); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, trigger)
}

func (p *Panel) deleteTrigger(w http.ResponseWriter, r *http.Request) {
	trigger, err := p.requireTrigger(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := p.store.DeleteTrigger(trigger.ID); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------- Discord-Bot: Einstellungen

var (
	snowflake      = regexp.MustCompile(`^[0-9]{17,20}$`)
	publicKeyHex   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	botTokenFormat = regexp.MustCompile(`^[A-Za-z0-9._-]{50,100}$`)
	discordActions = []string{"status", "players", "start", "stop", "restart"}
)

const maxDiscordRules = 25

type discordDTO struct {
	Configured    bool          `json:"configured"`
	ApplicationID string        `json:"applicationId"`
	PublicKey     string        `json:"publicKey"`
	HasToken      bool          `json:"hasToken"`
	Rules         []DiscordRule `json:"rules"`
	Enabled       bool          `json:"enabled"`
	RegisteredAt  int64         `json:"registeredAt"`
	// InteractionsPath ist der Pfad, den man in Discord als „Interactions Endpoint URL“ einträgt (mit der HTTPS-Adresse des Panels davor).
	InteractionsPath string `json:"interactionsPath"`
}

func discordView(projectID string, bot DiscordBot, configured bool) discordDTO {
	rules := bot.Rules
	if rules == nil {
		rules = []DiscordRule{}
	}
	return discordDTO{
		Configured: configured, ApplicationID: bot.ApplicationID, PublicKey: bot.PublicKey, HasToken: bot.BotToken != "",
		Rules: rules, Enabled: bot.Enabled, RegisteredAt: bot.RegisteredAt, InteractionsPath: "/api/public/discord/" + projectID + "/interactions",
	}
}

func (p *Panel) getDiscord(w http.ResponseWriter, r *http.Request) {
	project, _, err := p.requireProject(r, r.PathValue("id"), rankAdmin)
	if err != nil {
		writeError(w, err)
		return
	}
	bot, err := p.store.DiscordBot(project.ID)
	if errors.Is(err, ErrNotFound) {
		writeJSON(w, http.StatusOK, discordView(project.ID, DiscordBot{}, false))
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, discordView(project.ID, bot, true))
}

func validateRules(rules []DiscordRule) ([]DiscordRule, error) {
	if len(rules) > maxDiscordRules {
		return nil, fail(http.StatusBadRequest, "höchstens 25 Regeln")
	}
	clean := make([]DiscordRule, 0, len(rules))
	for _, rule := range rules {
		rule.Label = strings.TrimSpace(rule.Label)
		if rule.Kind != "role" && rule.Kind != "user" {
			return nil, fail(http.StatusBadRequest, "eine Regel gilt für eine Rolle (role) oder einen Benutzer (user)")
		}
		if !snowflake.MatchString(rule.ID) {
			return nil, fail(http.StatusBadRequest, "die Discord-ID „"+clip(rule.ID, 30)+"“ ist ungültig. Kopiere sie in Discord mit Rechtsklick → ID kopieren (Entwicklermodus)")
		}
		if len(rule.Label) > 48 {
			return nil, fail(http.StatusBadRequest, "die Bezeichnung einer Regel darf höchstens 48 Zeichen haben")
		}
		if len(rule.Actions) == 0 {
			return nil, fail(http.StatusBadRequest, "jede Regel braucht mindestens einen Befehl")
		}
		actions := []string{}
		for _, action := range rule.Actions {
			if !slices.Contains(discordActions, action) {
				return nil, fail(http.StatusBadRequest, "unbekannter Befehl: "+clip(action, 20))
			}
			if !slices.Contains(actions, action) {
				actions = append(actions, action)
			}
		}
		rule.Actions = actions
		clean = append(clean, rule)
	}
	return clean, nil
}

func (p *Panel) putDiscord(w http.ResponseWriter, r *http.Request) {
	project, _, err := p.requireProject(r, r.PathValue("id"), rankAdmin)
	if err != nil {
		writeError(w, err)
		return
	}
	var body struct {
		ApplicationID string        `json:"applicationId"`
		PublicKey     string        `json:"publicKey"`
		BotToken      *string       `json:"botToken"`
		Rules         []DiscordRule `json:"rules"`
		Enabled       bool          `json:"enabled"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	bot, err := p.store.DiscordBot(project.ID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		writeError(w, err)
		return
	}
	bot.ProjectID = project.ID
	bot.ApplicationID = strings.TrimSpace(body.ApplicationID)
	bot.PublicKey = strings.ToLower(strings.TrimSpace(body.PublicKey))
	bot.Enabled = body.Enabled
	if !snowflake.MatchString(bot.ApplicationID) {
		writeError(w, fail(http.StatusBadRequest, "die Application-ID ist ungültig. Du findest sie im Discord Developer Portal unter General Information"))
		return
	}
	if !publicKeyHex.MatchString(bot.PublicKey) {
		writeError(w, fail(http.StatusBadRequest, "der Public Key ist ungültig. Er hat 64 Zeichen (0–9, a–f) und steht unter General Information"))
		return
	}
	rules, err := validateRules(body.Rules)
	if err != nil {
		writeError(w, err)
		return
	}
	bot.Rules = rules
	if body.BotToken != nil && strings.TrimSpace(*body.BotToken) != "" {
		token := strings.TrimSpace(*body.BotToken)
		if !botTokenFormat.MatchString(token) {
			writeError(w, fail(http.StatusBadRequest, "das Bot-Token ist ungültig. Du bekommst es im Developer Portal unter Bot → Reset Token"))
			return
		}
		sealed, err := p.seal(token)
		if err != nil {
			writeError(w, err)
			return
		}
		bot.BotToken = sealed
	}
	if err := p.store.SaveDiscordBot(bot); err != nil {
		writeError(w, err)
		return
	}
	saved, err := p.store.DiscordBot(project.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, discordView(project.ID, saved, true))
}

func (p *Panel) deleteDiscord(w http.ResponseWriter, r *http.Request) {
	project, _, err := p.requireProject(r, r.PathValue("id"), rankAdmin)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := p.store.DeleteDiscordBot(project.ID); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
