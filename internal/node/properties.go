package node

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
)

// Property ist ein Schlüssel mit seinem Wert aus einer .properties-Datei (Werte ohne Escapes).
type Property struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// propertiesLine ist eine logische Zeile: ein Eintrag (eventuell über mehrere Zeilen fortgesetzt), ein Kommentar oder
// eine Leerzeile. raw hält die Zeilen so, wie sie in der Datei stehen, damit unveränderte Zeilen unverändert bleiben.
type propertiesLine struct {
	raw   []string
	entry bool
	key   string
	value string
}

// PropertiesFile ist eine geparste .properties-Datei (Format von java.util.Properties). Sie behält Kommentare,
// Reihenfolge und unbekannte Schlüssel; geändert werden nur die Zeilen der Schlüssel, die man setzt.
type PropertiesFile struct {
	lines   []propertiesLine
	newline string
}

// ParseProperties liest den Inhalt einer .properties-Datei. Es gibt keinen Fehler: Was Java lesen kann, lesen wir auch.
func ParseProperties(data []byte) *PropertiesFile {
	text := string(data)
	file := &PropertiesFile{newline: "\n"}
	if strings.Contains(text, "\r\n") {
		file.newline = "\r\n"
	}
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	text = strings.TrimPrefix(text, "\ufeff")
	if text == "" {
		return file
	}
	physical := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	for i := 0; i < len(physical); i++ {
		line := physical[i]
		trimmed := strings.TrimLeft(line, " \t\f")
		if trimmed == "" || trimmed[0] == '#' || trimmed[0] == '!' {
			file.lines = append(file.lines, propertiesLine{raw: []string{line}})
			continue
		}
		raw := []string{line}
		logical := trimmed
		// Eine ungerade Zahl Backslashes am Ende setzt die Zeile fort; führende Leerzeichen der Folgezeile zählen nicht
		for continues(logical) && i+1 < len(physical) {
			i++
			raw = append(raw, physical[i])
			logical = logical[:len(logical)-1] + strings.TrimLeft(physical[i], " \t\f")
		}
		if continues(logical) {
			logical = logical[:len(logical)-1]
		}
		key, value := splitProperty(logical)
		file.lines = append(file.lines, propertiesLine{raw: raw, entry: true, key: key, value: value})
	}
	return file
}

func continues(line string) bool {
	count := 0
	for i := len(line) - 1; i >= 0 && line[i] == '\\'; i-- {
		count++
	}
	return count%2 == 1
}

// splitProperty trennt Schlüssel und Wert: Der Schlüssel endet am ersten nicht maskierten =, : oder Leerraum.
func splitProperty(line string) (string, string) {
	end := len(line)
	for i := 0; i < len(line); i++ {
		c := line[i]
		if c == '\\' {
			i++
			continue
		}
		if c == '=' || c == ':' || c == ' ' || c == '\t' || c == '\f' {
			end = i
			break
		}
	}
	key := line[:end]
	rest := strings.TrimLeft(line[end:], " \t\f")
	if rest != "" && (rest[0] == '=' || rest[0] == ':') {
		rest = strings.TrimLeft(rest[1:], " \t\f")
	}
	return unescapeProperty(key), unescapeProperty(rest)
}

func unescapeProperty(text string) string {
	if !strings.Contains(text, "\\") {
		return text
	}
	var out strings.Builder
	var units []uint16 // \u-Folgen sammeln, damit Ersatzpaare (Emoji) zusammenfinden
	flush := func() {
		if len(units) > 0 {
			out.WriteString(string(utf16.Decode(units)))
			units = units[:0]
		}
	}
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c != '\\' || i+1 >= len(text) {
			flush()
			if c != '\\' {
				out.WriteByte(c)
			}
			continue
		}
		i++
		if text[i] == 'u' && i+4 < len(text) {
			if unit, err := strconv.ParseUint(text[i+1:i+5], 16, 16); err == nil {
				units = append(units, uint16(unit))
				i += 4
				continue
			}
		}
		flush()
		switch text[i] {
		case 't':
			out.WriteByte('\t')
		case 'n':
			out.WriteByte('\n')
		case 'r':
			out.WriteByte('\r')
		case 'f':
			out.WriteByte('\f')
		default:
			out.WriteByte(text[i])
		}
	}
	flush()
	return out.String()
}

// escapeProperty schreibt Text so, wie java.util.Properties ihn speichert. Alles außerhalb von ASCII wird zu \uXXXX:
// So lesen ihn alte Server (ISO-8859-1) und neue (UTF-8) gleich.
func escapeProperty(text string, key bool) string {
	var out strings.Builder
	for i, r := range text {
		switch r {
		case '\\':
			out.WriteString(`\\`)
		case '\t':
			out.WriteString(`\t`)
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\f':
			out.WriteString(`\f`)
		case '=', ':', '#', '!':
			out.WriteByte('\\')
			out.WriteRune(r)
		case ' ':
			if key || i == 0 {
				out.WriteString(`\ `)
			} else {
				out.WriteByte(' ')
			}
		default:
			if r < 0x20 || r > 0x7e {
				for _, unit := range utf16.Encode([]rune{r}) {
					fmt.Fprintf(&out, `\u%04X`, unit)
				}
				continue
			}
			out.WriteRune(r)
		}
	}
	return out.String()
}

// Entries liefert die Einträge in der Reihenfolge der Datei. Steht ein Schlüssel mehrfach da, gilt wie in Java der
// letzte Wert.
func (p *PropertiesFile) Entries() []Property {
	index := map[string]int{}
	entries := []Property{}
	for _, line := range p.lines {
		if !line.entry {
			continue
		}
		if i, ok := index[line.key]; ok {
			entries[i].Value = line.value
			continue
		}
		index[line.key] = len(entries)
		entries = append(entries, Property{Key: line.key, Value: line.value})
	}
	return entries
}

// Get liefert den Wert eines Schlüssels.
func (p *PropertiesFile) Get(key string) (string, bool) {
	for i := len(p.lines) - 1; i >= 0; i-- {
		if p.lines[i].entry && p.lines[i].key == key {
			return p.lines[i].value, true
		}
	}
	return "", false
}

// Set setzt einen Wert. Ein vorhandener Schlüssel behält seinen Platz, ein neuer kommt ans Ende. changed sagt, ob sich
// etwas geändert hat.
func (p *PropertiesFile) Set(key, value string) (changed bool) {
	line := propertiesLine{raw: []string{escapeProperty(key, true) + "=" + escapeProperty(value, false)}, entry: true, key: key, value: value}
	for i := len(p.lines) - 1; i >= 0; i-- {
		if p.lines[i].entry && p.lines[i].key == key {
			if p.lines[i].value == value {
				return false
			}
			p.lines[i] = line
			return true
		}
	}
	p.lines = append(p.lines, line)
	return true
}

// Bytes liefert den Dateiinhalt mit dem Zeilenende der ursprünglichen Datei.
func (p *PropertiesFile) Bytes() []byte {
	var out strings.Builder
	for _, line := range p.lines {
		for _, raw := range line.raw {
			out.WriteString(raw)
			out.WriteString(p.newline)
		}
	}
	return []byte(out.String())
}
