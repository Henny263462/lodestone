package node

import (
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// CrashAnalysis ist das Ergebnis der Absturzanalyse: Was im neuesten Absturzbericht (crash-reports/) und in den letzten
// Zeilen der Konsole auf die Ursache hinweist.
type CrashAnalysis struct {
	Found bool `json:"found"`
	// Report ist der ausgewertete Absturzbericht (leer, wenn es keinen passenden gibt).
	Report   string              `json:"report,omitempty"`
	Suspects []CrashSuspect      `json:"suspects"`
	Missing  []MissingDependency `json:"missing"`
	Java     *JavaMismatch       `json:"java,omitempty"`
	Mixins   []string            `json:"mixins"`
	// Summary nennt in ein bis zwei Sätzen den wahrscheinlichsten Verdächtigen.
	Summary string `json:"summary,omitempty"`
}

// CrashSuspect ist eine Mod, die als Ursache in Frage kommt.
type CrashSuspect struct {
	ModID string `json:"modId"`
	Name  string `json:"name,omitempty"`
	// Reason: suspected (vom Absturzbericht genannt), stacktrace (taucht im Stacktrace auf) oder mixin.
	Reason string `json:"reason"`
}

// MissingDependency ist eine fehlende Abhängigkeit einer Mod.
type MissingDependency struct {
	Mod      string `json:"mod"`
	Requires string `json:"requires"`
	Version  string `json:"version,omitempty"`
}

// JavaMismatch sagt, welche Java-Version gebraucht wird und welche läuft (0 = unbekannt).
type JavaMismatch struct {
	Required int `json:"required"`
	Current  int `json:"current,omitempty"`
}

const (
	crashReportDir   = "crash-reports"
	crashReportLimit = 512 << 10
	crashReportAge   = 30 * time.Minute
)

var (
	suspectEntry   = regexp.MustCompile(`([^\t\n(]{1,80}?) \(([a-z][a-z0-9_\-]{1,63})\), Version: ([^\s,]+)`)
	forgeFrame     = regexp.MustCompile(`(?m)^\s*at (?:[A-Z_]+/)?([a-z][a-z0-9_\-]{1,63})@[^/\s]+/`)
	jarFrame       = regexp.MustCompile(`(?m)^\s*at .*~\[([a-z][a-z0-9_\-]{1,63})-[0-9][^\]]*\.jar`)
	mixinHandler   = regexp.MustCompile(`(?m)^\s*at .*\$(?:[a-z]{3}[0-9a-f]{3})\$([a-z][a-z0-9_]{1,63})\$`)
	fabricMissing  = regexp.MustCompile(`Mod '([^']{1,80})' \(([a-z][a-z0-9_\-]{1,63})\) \S+ requires (?:version (\S+(?: or later)?)|any version) of (?:mod )?'?([^',()]{1,80}?)'?(?: \(([a-z][a-z0-9_\-]{1,63})\))?, which is missing`)
	forgeMissing   = regexp.MustCompile(`Mod ID: '([a-z][a-z0-9_\-]{1,63})', Requested by: '([a-z][a-z0-9_\-]{1,63})', Expected range: '([^']*)'`)
	classVersion   = regexp.MustCompile(`class file version (\d{2})\.0\)?,? this version of the Java Runtime only recognizes class file versions up to (\d{2})\.0`)
	requiresJava   = regexp.MustCompile(`(?i)requires (?:at least )?Java (\d{1,2})`)
	mixinConfig    = regexp.MustCompile(`Mixin \[([a-z][a-z0-9_\-]{1,63})\.mixins?\.json:([A-Za-z0-9_.$]{1,120})\](?:[^\n]*? from mod ([a-z][a-z0-9_\-]{1,63}))?`)
	mixinApply     = regexp.MustCompile(`Mixin apply (?:for mod ([a-z][a-z0-9_\-]{1,63}) )?failed ([a-z][a-z0-9_\-]{1,63})\.mixins?\.json:([A-Za-z0-9_.$]{1,120})`)
	mixinFailure   = regexp.MustCompile(`MixinApplyError|InvalidMixinException|InjectionError|MixinTransformerError|InvalidInjectionException`)
	platformModIDs = map[string]bool{
		"minecraft": true, "forge": true, "neoforge": true, "fml": true, "fmlcore": true, "javafmllanguage": true, "lowcodelanguage": true,
		"mclanguage": true, "fabricloader": true, "fabric": true, "java": true, "mixin": true, "mixinextras": true, "modlauncher": true,
		"securejarhandler": true, "bootstraplauncher": true, "eventbus": true, "coremods": true, "unknown": true, "none": true,
	}
)

// analyzeCrash wertet den Text eines Absturzberichts und der Konsole aus. Reine Funktion, damit sie gut testbar ist.
func analyzeCrash(text string) CrashAnalysis {
	result := CrashAnalysis{Suspects: []CrashSuspect{}, Missing: []MissingDependency{}, Mixins: []string{}}
	seen := map[string]bool{}
	addSuspect := func(id, name, reason string) {
		if platformModIDs[id] || strings.HasPrefix(id, "fabric-") || seen[id] {
			return
		}
		seen[id] = true
		result.Suspects = append(result.Suspects, CrashSuspect{ModID: id, Name: strings.TrimSpace(name), Reason: reason})
	}

	// 1. Der Absturzbericht nennt Verdächtige ausdrücklich (Forge/NeoForge, Fabric mit Zusatzmods)
	if index := strings.Index(text, "Suspected Mod"); index >= 0 {
		block := text[index:]
		if end := strings.Index(block, "\n\n"); end > 0 {
			block = block[:end]
		}
		if len(block) > 4000 {
			block = block[:4000]
		}
		for _, match := range suspectEntry.FindAllStringSubmatch(block, 10) {
			name := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(match[1]), "Suspected Mods:"))
			name = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(name, "Suspected Mod(s):"), "Suspected Mod:"))
			addSuspect(match[2], name, "suspected")
		}
	}

	// 2. Fehlende Abhängigkeiten
	for _, match := range fabricMissing.FindAllStringSubmatch(text, 10) {
		requires := match[5]
		if requires == "" {
			requires = strings.TrimSpace(match[4])
		}
		result.Missing = appendMissing(result.Missing, MissingDependency{Mod: match[2], Requires: requires, Version: match[3]})
	}
	for _, match := range forgeMissing.FindAllStringSubmatch(text, 10) {
		result.Missing = appendMissing(result.Missing, MissingDependency{Mod: match[2], Requires: match[1], Version: match[3]})
	}

	// 3. Falsche Java-Version (Klassenversion 52 = Java 8, 61 = Java 17, 65 = Java 21 …)
	if match := classVersion.FindStringSubmatch(text); match != nil {
		required, _ := strconv.Atoi(match[1])
		current, _ := strconv.Atoi(match[2])
		result.Java = &JavaMismatch{Required: required - 44, Current: current - 44}
	} else if match := requiresJava.FindStringSubmatch(text); match != nil && strings.Contains(text, "Unsupported") {
		required, _ := strconv.Atoi(match[1])
		result.Java = &JavaMismatch{Required: required}
	}

	// 4. Mixin-Fehler: Die Konfiguration heißt fast immer wie die Mod
	for _, match := range mixinApply.FindAllStringSubmatch(text, 5) {
		id := match[1]
		if id == "" {
			id = match[2]
		}
		result.Mixins = appendUnique(result.Mixins, match[2]+".mixins.json:"+match[3])
		addSuspect(id, "", "mixin")
	}
	if mixinFailure.MatchString(text) || len(result.Mixins) > 0 {
		for _, match := range mixinConfig.FindAllStringSubmatch(text, 5) {
			id := match[3]
			if id == "" {
				id = match[1]
			}
			result.Mixins = appendUnique(result.Mixins, match[1]+".mixins.json:"+match[2])
			addSuspect(id, "", "mixin")
		}
	}

	// 5. Mod-IDs im Stacktrace: die häufigste zuerst
	counts := map[string]int{}
	for _, re := range []*regexp.Regexp{forgeFrame, jarFrame, mixinHandler} {
		for _, match := range re.FindAllStringSubmatch(text, 400) {
			counts[match[1]]++
		}
	}
	ids := make([]string, 0, len(counts))
	for id := range counts {
		if !platformModIDs[id] && !strings.HasPrefix(id, "fabric-") {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		if counts[ids[i]] != counts[ids[j]] {
			return counts[ids[i]] > counts[ids[j]]
		}
		return ids[i] < ids[j]
	})
	for _, id := range ids[:min(len(ids), 3)] {
		addSuspect(id, "", "stacktrace")
	}

	result.Found = len(result.Suspects) > 0 || len(result.Missing) > 0 || result.Java != nil || len(result.Mixins) > 0
	result.Summary = crashSummary(result)
	return result
}

func appendMissing(list []MissingDependency, entry MissingDependency) []MissingDependency {
	for _, existing := range list {
		if existing.Mod == entry.Mod && existing.Requires == entry.Requires {
			return list
		}
	}
	return append(list, entry)
}

func appendUnique(list []string, entry string) []string {
	for _, existing := range list {
		if existing == entry {
			return list
		}
	}
	return append(list, entry)
}

// crashSummary formuliert den wahrscheinlichsten Grund in höchstens zwei Sätzen.
func crashSummary(result CrashAnalysis) string {
	var parts []string
	if len(result.Missing) > 0 {
		missing := result.Missing[0]
		parts = append(parts, fmt.Sprintf("Es fehlt die Mod „%s“, die „%s“ braucht.", missing.Requires, missing.Mod))
	}
	if result.Java != nil {
		if result.Java.Current > 0 {
			parts = append(parts, fmt.Sprintf("Falsche Java-Version: gebraucht wird Java %d, es läuft Java %d.", result.Java.Required, result.Java.Current))
		} else {
			parts = append(parts, fmt.Sprintf("Falsche Java-Version: gebraucht wird mindestens Java %d.", result.Java.Required))
		}
	}
	if len(parts) < 2 && len(result.Suspects) > 0 {
		suspect := result.Suspects[0]
		label := "„" + suspect.ModID + "“"
		if suspect.Name != "" && suspect.Name != suspect.ModID {
			label = "„" + suspect.Name + "“ (" + suspect.ModID + ")"
		}
		switch suspect.Reason {
		case "suspected":
			parts = append(parts, "Verdächtige Mod laut Absturzbericht: "+label+".")
		case "mixin":
			parts = append(parts, "Mixin-Fehler in der Mod "+label+".")
		default:
			parts = append(parts, "Im Stacktrace taucht die Mod "+label+" am häufigsten auf.")
		}
	}
	if len(parts) > 2 {
		parts = parts[:2]
	}
	return strings.Join(parts, " ")
}

// CrashAnalysis wertet den neuesten Absturzbericht (nur wenn er zum letzten Lauf passt) und die letzten Konsolenzeilen aus.
func (s *Server) CrashAnalysis() CrashAnalysis {
	s.mu.Lock()
	started := s.startedAt
	s.mu.Unlock()
	report, text := s.latestCrashReport(started, time.Now())
	var builder strings.Builder
	builder.WriteString(text)
	builder.WriteString("\n\n")
	for _, line := range s.Logs(400) {
		builder.WriteString(ansi.ReplaceAllString(line.Text, ""))
		builder.WriteByte('\n')
	}
	result := analyzeCrash(builder.String())
	if result.Found {
		result.Report = report
	}
	return result
}

// latestCrashReport liefert den neuesten Bericht aus crash-reports/, wenn er nach dem Start des letzten Laufs (oder,
// ohne bekannten Start, in der letzten halben Stunde) geschrieben wurde. Große Berichte werden nur am Anfang gelesen.
func (s *Server) latestCrashReport(started, now time.Time) (string, string) {
	entries, err := s.fs.List(crashReportDir)
	if err != nil {
		return "", ""
	}
	var newest Entry
	for _, entry := range entries {
		if entry.Dir || entry.Symlink || !strings.HasSuffix(entry.Name, ".txt") {
			continue
		}
		if entry.Modified > newest.Modified {
			newest = entry
		}
	}
	if newest.Name == "" {
		return "", ""
	}
	modified := time.UnixMilli(newest.Modified)
	if !started.IsZero() && modified.Before(started.Add(-5*time.Second)) || started.IsZero() && now.Sub(modified) > crashReportAge {
		return "", ""
	}
	path := crashReportDir + "/" + newest.Name
	file, _, err := s.fs.Open(path)
	if err != nil {
		return "", ""
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, crashReportLimit))
	if err != nil {
		return "", ""
	}
	return path, string(data)
}
