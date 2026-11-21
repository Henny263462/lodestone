package node

import (
	"context"
	"fmt"
	"strings"

	"github.com/lodestone-mc/lodestone/internal/software"
)

// pinModpackLoader stellt bei NeoForge- und Forge-Modpacks genau die Loader-Version ein, die das Modpack in
// modrinth.index.json verlangt, und lädt deren Installer. Das Panel kennt beim Anlegen nur die Minecraft-Version.
// Gibt es die verlangte Version nicht, bleibt die bisherige installiert und die Konsole sagt es.
func (m *Manager) pinModpackLoader(ctx context.Context, s *Server, index mrpackIndex) error {
	spec := s.snapshot()
	var want string
	switch spec.Software {
	case software.NeoForge:
		want = index.Dependencies["neoforge"]
	case software.Forge:
		want = index.Dependencies["forge"]
		if want != "" && !strings.Contains(want, "-") {
			want = index.Dependencies["minecraft"] + "-" + want
		}
	default:
		return nil
	}
	want = strings.TrimSpace(want)
	if want == "" || want == spec.Version {
		return nil
	}
	if len(want) > 64 || strings.ContainsAny(want, "/\\ ?#") {
		return fmt.Errorf("das Modpack nennt eine ungültige Loader-Version %q", want)
	}
	info, _ := software.Lookup(spec.Software)
	s.setModpack(func(status *ModpackStatus) { status.Phase = info.Name + " " + want + " wird installiert" })
	s.logf("Das Modpack verlangt %s %s, der Installer wird geladen", info.Name, want)
	s.mu.Lock()
	previous := s.spec
	s.spec.Version, s.spec.Installed, s.spec.JavaMajor = want, false, 0
	err := s.saveLocked()
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if err := s.install(ctx); err != nil {
		s.logf("%s %s ließ sich nicht installieren (%v), es bleibt bei %s", info.Name, want, err, previous.Version)
		s.mu.Lock()
		s.spec.Version, s.spec.Installed, s.spec.JavaMajor = previous.Version, previous.Installed, previous.JavaMajor
		err = s.saveLocked()
		s.mu.Unlock()
		return err
	}
	return nil
}

// ---------------------------------------------------------------- HOCON

// editHOCON setzt Schlüssel der obersten Ebene einer HOCON-Datei (BlueMap: core.conf). Kommentare und alle anderen
// Zeilen bleiben, wie sie sind; verschachtelte Werte kann diese Bearbeitung bewusst nicht.
func editHOCON(data []byte, edit Edit) ([]byte, bool) {
	text := string(data)
	newline := "\n"
	if strings.Contains(text, "\r\n") {
		newline = "\r\n"
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	changed := false
	for _, op := range edit.Set {
		key := op.Path[0]
		want := key + ": " + hoconValue(op.Value)
		found := false
		depth := 0
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if depth == 0 && !strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "//") {
				name := trimmed
				if cut := strings.IndexAny(trimmed, ":={ "); cut >= 0 {
					name = trimmed[:cut]
				}
				if strings.Trim(name, `"`) == key {
					found = true
					if line != want {
						lines[i] = want
						changed = true
					}
					break
				}
			}
			depth += strings.Count(trimmed, "{") + strings.Count(trimmed, "[") - strings.Count(trimmed, "}") - strings.Count(trimmed, "]")
		}
		if !found {
			lines = append(lines, want)
			changed = true
		}
	}
	return []byte(strings.Join(lines, newline) + newline), changed
}

func hoconValue(value any) string {
	switch v := normalize(value).(type) {
	case string:
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(v) + `"`
	default:
		return scalarString(v)
	}
}
