package panel

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// propertyRule beschreibt, welche Werte ein bekannter Schlüssel der server.properties annimmt. Unbekannte Schlüssel
// (Plugins, neuere Versionen) sind freier Text.
type propertyRule struct {
	kind     string // bool, int, enum, text
	min, max int
	options  []string
	pattern  *regexp.Regexp
	maxLen   int
}

var worldNameRule = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9 _.+-]{0,63}$`)

var propertyRules = map[string]propertyRule{
	"difficulty":                {kind: "enum", options: []string{"peaceful", "easy", "normal", "hard"}},
	"gamemode":                  {kind: "enum", options: []string{"survival", "creative", "adventure", "spectator"}},
	"pvp":                       {kind: "bool"},
	"hardcore":                  {kind: "bool"},
	"force-gamemode":            {kind: "bool"},
	"white-list":                {kind: "bool"},
	"enforce-whitelist":         {kind: "bool"},
	"online-mode":               {kind: "bool"},
	"allow-flight":              {kind: "bool"},
	"allow-nether":              {kind: "bool"},
	"generate-structures":       {kind: "bool"},
	"enable-command-block":      {kind: "bool"},
	"spawn-monsters":            {kind: "bool"},
	"spawn-animals":             {kind: "bool"},
	"spawn-npcs":                {kind: "bool"},
	"max-players":               {kind: "int", min: 1, max: 100000},
	"view-distance":             {kind: "int", min: 2, max: 32},
	"simulation-distance":       {kind: "int", min: 2, max: 32},
	"spawn-protection":          {kind: "int", min: 0, max: 10000},
	"player-idle-timeout":       {kind: "int", min: 0, max: 100000},
	"max-world-size":            {kind: "int", min: 1, max: 29999984},
	"op-permission-level":       {kind: "int", min: 1, max: 4},
	"function-permission-level": {kind: "int", min: 1, max: 4},
	"motd":                      {kind: "text", maxLen: 512},
	"level-seed":                {kind: "text", maxLen: 128},
	"level-name":                {kind: "text", pattern: worldNameRule},
	"level-type":                {kind: "text", pattern: regexp.MustCompile(`^[A-Za-z0-9_:.-]{1,64}$`)},
}

// validateProperty prüft einen Wert für die server.properties.
func validateProperty(key, value string) error {
	if !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
		return fail(http.StatusBadRequest, key+": der Wert enthält ungültige Zeichen")
	}
	rule, known := propertyRules[key]
	if !known {
		if len(value) > 4096 {
			return fail(http.StatusBadRequest, key+": der Wert ist zu lang (höchstens 4096 Zeichen)")
		}
		return nil
	}
	switch rule.kind {
	case "bool":
		if value != "true" && value != "false" {
			return fail(http.StatusBadRequest, key+": erlaubt sind nur true und false")
		}
	case "int":
		number, err := strconv.Atoi(value)
		if err != nil || number < rule.min || number > rule.max {
			return fail(http.StatusBadRequest, fmt.Sprintf("%s: erlaubt ist eine ganze Zahl von %d bis %d", key, rule.min, rule.max))
		}
	case "enum":
		for _, option := range rule.options {
			if value == option {
				return nil
			}
		}
		return fail(http.StatusBadRequest, key+": erlaubt sind "+strings.Join(rule.options, ", "))
	case "text":
		if rule.maxLen > 0 && utf8.RuneCountInString(value) > rule.maxLen {
			return fail(http.StatusBadRequest, fmt.Sprintf("%s: höchstens %d Zeichen", key, rule.maxLen))
		}
		if rule.pattern != nil && !rule.pattern.MatchString(value) {
			return fail(http.StatusBadRequest, key+": der Wert hat ein ungültiges Format")
		}
		if key != "motd" && strings.ContainsAny(value, "\r\n") {
			return fail(http.StatusBadRequest, key+": Zeilenumbrüche sind nicht erlaubt")
		}
	}
	return nil
}

// lockedProperties nennt Schlüssel, die Lodestone selbst verwaltet, mit dem Grund. Die Oberfläche sperrt sie, das
// Panel lehnt Änderungen daran ab.
func lockedProperties(row ServerRow) map[string]string {
	locked := map[string]string{
		"server-port": "Lodestone verbindet den Port im Container (25565) mit einem Port der Maschine. Welchen, stellst du unter Einstellungen ein.",
	}
	if row.Role == RoleBackend {
		locked["online-mode"] = "Im Netzwerk prüft der Velocity-Proxy die Konten. Die Autokonfiguration setzt online-mode hier deshalb auf false; ein anderer Wert würde den Beitritt über den Proxy verhindern."
	}
	return locked
}

// tagPattern erlaubt Buchstaben (auch Umlaute), Ziffern, Leerzeichen und _ . - in Schlagwörtern.
var tagPattern = regexp.MustCompile(`^[\p{L}\p{N} _.-]{1,24}$`)

const (
	maxTagsPerProject = 8
	maxOrderedIDs     = 2000
)

// cleanTags bereinigt Schlagwörter: Leerraum weg, doppelte (ohne Groß/Klein) raus, Länge und Zeichen geprüft.
func cleanTags(tags []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, tag := range tags {
		tag = strings.Join(strings.Fields(tag), " ")
		if tag == "" {
			continue
		}
		if !tagPattern.MatchString(tag) {
			return nil, fail(http.StatusBadRequest, "ein Schlagwort hat 1 bis 24 Zeichen: Buchstaben, Ziffern, Leerzeichen und _ . -")
		}
		lower := strings.ToLower(tag)
		if seen[lower] {
			continue
		}
		seen[lower] = true
		out = append(out, tag)
	}
	if len(out) > maxTagsPerProject {
		return nil, fail(http.StatusBadRequest, fmt.Sprintf("höchstens %d Schlagwörter pro Projekt", maxTagsPerProject))
	}
	return out, nil
}
