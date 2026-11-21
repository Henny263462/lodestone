package panel

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Ein S3-kompatibler Client ohne SDK: AWS Signature Version 4 für Anfragen des Panels (Kopf-Signatur) und für
// vorsignierte URLs, mit denen der Node direkt hoch- und herunterlädt. Die Zugangsdaten verlassen das Panel nie.

const (
	s3Service       = "s3"
	s3Algorithm     = "AWS4-HMAC-SHA256"
	s3AmzDate       = "20060102T150405Z"
	s3ShortDate     = "20060102"
	s3Unsigned      = "UNSIGNED-PAYLOAD"
	s3EmptySHA256   = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	s3MaxPresign    = 7 * 24 * time.Hour // Obergrenze von SigV4
	s3MinPartSize   = 64 << 20
	s3MaxParts      = 1000       // hält die Liste der Part-URLs unter der Größengrenze der Node-Schnittstelle
	s3MaxPartSize   = 5 << 30    // Obergrenze von S3 für einen Teil
	s3MaxObjectSize = 5000 << 30 // 1000 Teile zu je 5 GiB
)

// s3Target ist ein entschlüsseltes Backup-Ziel.
type s3Target struct {
	Endpoint  string // https://s3.example.com (ohne Pfad)
	Region    string
	Bucket    string
	Prefix    string // endet mit "/" oder ist leer
	AccessKey string
	Secret    string
	PathStyle bool
}

// objectURL liefert die Adresse eines Objekts im Bucket.
func (t s3Target) objectURL(key string, query url.Values) *url.URL {
	base, _ := url.Parse(t.Endpoint)
	out := &url.URL{Scheme: base.Scheme, Host: base.Host}
	if t.PathStyle {
		out.Path = "/" + t.Bucket + "/" + key
	} else {
		out.Host = t.Bucket + "." + base.Host
		out.Path = "/" + key
	}
	if key == "" {
		out.Path = strings.TrimSuffix(out.Path, "/")
		if out.Path == "" {
			out.Path = "/"
		}
	}
	// Gesendet wird genau die Kodierung, die auch signiert wird
	out.RawPath = s3Escape(out.Path, true)
	if query != nil {
		out.RawQuery = s3Query(query)
	}
	return out
}

// identity beschreibt, wo ein Objekt liegt; ändert sich Endpoint oder Bucket, sind alte Backups dort nicht mehr erreichbar.
func (t s3Target) identity() string { return strings.TrimRight(t.Endpoint, "/") + "/" + t.Bucket }

// ---------------------------------------------------------------- Signatur Version 4

// s3Escape kodiert wie von SigV4 verlangt: alles außer A-Z a-z 0-9 - . _ ~ wird zu %XX (Großbuchstaben).
func s3Escape(text string, keepSlash bool) string {
	var out strings.Builder
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case 'A' <= c && c <= 'Z', 'a' <= c && c <= 'z', '0' <= c && c <= '9', c == '-', c == '.', c == '_', c == '~':
			out.WriteByte(c)
		case c == '/' && keepSlash:
			out.WriteByte(c)
		default:
			fmt.Fprintf(&out, "%%%02X", c)
		}
	}
	return out.String()
}

// s3Query baut die kanonische Abfrage: nach Schlüssel (und Wert) sortiert, streng kodiert.
func s3Query(values url.Values) string {
	type pair struct{ key, value string }
	pairs := []pair{}
	for key, list := range values {
		for _, value := range list {
			pairs = append(pairs, pair{s3Escape(key, false), s3Escape(value, false)})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].key != pairs[j].key {
			return pairs[i].key < pairs[j].key
		}
		return pairs[i].value < pairs[j].value
	})
	parts := make([]string, len(pairs))
	for i, p := range pairs {
		parts[i] = p.key + "=" + p.value
	}
	return strings.Join(parts, "&")
}

func s3HMAC(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}

func s3Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// s3Signature berechnet die Signatur zu einer kanonischen Anfrage.
func s3Signature(secret, region string, at time.Time, canonical string) (signature, scope string) {
	day := at.UTC().Format(s3ShortDate)
	scope = day + "/" + region + "/" + s3Service + "/aws4_request"
	toSign := s3Algorithm + "\n" + at.UTC().Format(s3AmzDate) + "\n" + scope + "\n" + s3Hash([]byte(canonical))
	key := s3HMAC([]byte("AWS4"+secret), day)
	key = s3HMAC(key, region)
	key = s3HMAC(key, s3Service)
	key = s3HMAC(key, "aws4_request")
	return hex.EncodeToString(s3HMAC(key, toSign)), scope
}

func s3CanonicalURI(u *url.URL) string {
	if u.Path == "" {
		return "/"
	}
	return s3Escape(u.Path, true)
}

func s3TrimSpaces(value string) string { return strings.Join(strings.Fields(value), " ") }

// signS3 signiert eine Anfrage im Kopf (Authorization). Signiert werden host und alle gesetzten Köpfe.
// payloadHash ist der SHA-256 des Körpers (hex) oder UNSIGNED-PAYLOAD.
func signS3(request *http.Request, accessKey, secret, region, payloadHash string, at time.Time) {
	request.Header.Set("X-Amz-Date", at.UTC().Format(s3AmzDate))
	request.Header.Set("X-Amz-Content-Sha256", payloadHash)
	host := request.Host
	if host == "" {
		host = request.URL.Host
	}
	headers := map[string]string{"host": host}
	for name, values := range request.Header {
		lower := strings.ToLower(name)
		if lower == "authorization" || lower == "user-agent" || lower == "content-length" {
			continue
		}
		trimmed := make([]string, len(values))
		for i, value := range values {
			trimmed[i] = s3TrimSpaces(value)
		}
		headers[lower] = strings.Join(trimmed, ",")
	}
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	var canonicalHeaders strings.Builder
	for _, name := range names {
		canonicalHeaders.WriteString(name + ":" + headers[name] + "\n")
	}
	signed := strings.Join(names, ";")
	canonical := strings.Join([]string{
		request.Method, s3CanonicalURI(request.URL), s3Query(request.URL.Query()), canonicalHeaders.String(), signed, payloadHash,
	}, "\n")
	signature, scope := s3Signature(secret, region, at, canonical)
	request.Header.Set("Authorization", fmt.Sprintf("%s Credential=%s/%s,SignedHeaders=%s,Signature=%s", s3Algorithm, accessKey, scope, signed, signature))
}

// presignS3 liefert eine vorsignierte Adresse (nur host signiert, Körper unsigniert).
func presignS3(method string, target *url.URL, accessKey, secret, region string, at time.Time, expires time.Duration) string {
	if expires > s3MaxPresign {
		expires = s3MaxPresign
	}
	u := *target
	query := u.Query()
	day := at.UTC().Format(s3ShortDate)
	query.Set("X-Amz-Algorithm", s3Algorithm)
	query.Set("X-Amz-Credential", accessKey+"/"+day+"/"+region+"/"+s3Service+"/aws4_request")
	query.Set("X-Amz-Date", at.UTC().Format(s3AmzDate))
	query.Set("X-Amz-Expires", strconv.Itoa(int(expires/time.Second)))
	query.Set("X-Amz-SignedHeaders", "host")
	canonicalQuery := s3Query(query)
	canonical := strings.Join([]string{method, s3CanonicalURI(&u), canonicalQuery, "host:" + u.Host + "\n", "host", s3Unsigned}, "\n")
	signature, _ := s3Signature(secret, region, at, canonical)
	u.RawQuery = canonicalQuery + "&X-Amz-Signature=" + signature
	return u.String()
}

// ---------------------------------------------------------------- Client

// s3Client spricht mit einem Ziel. Interne Adressen sind gesperrt, außer LODESTONE_BACKUP_ALLOW_INTERNAL ist gesetzt.
type s3Client struct {
	target s3Target
	http   *http.Client
	now    func() time.Time
}

func newS3HTTPClient(allowInternal bool) *http.Client {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			if allowInternal {
				return nil
			}
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if ip := net.ParseIP(host); ip == nil || isInternalIP(ip) {
				return fmt.Errorf("Verbindungen ins interne Netz sind für Backup-Ziele gesperrt (%s)", host)
			}
			return nil
		},
	}
	return &http.Client{
		Timeout:       60 * time.Second,
		Transport:     &http.Transport{DialContext: dialer.DialContext, TLSHandshakeTimeout: 10 * time.Second, IdleConnTimeout: 30 * time.Second},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func (p *Panel) s3(target s3Target) *s3Client {
	return &s3Client{target: target, http: newS3HTTPClient(p.cfg.AllowInternalBackupTargets), now: time.Now}
}

// s3Error ist eine Fehlerantwort des Ziels.
type s3Error struct {
	Status  int
	Code    string `xml:"Code"`
	Message string `xml:"Message"`
}

func (e *s3Error) Error() string {
	switch e.Code {
	case "AccessDenied":
		return "Das Backup-Ziel verweigert den Zugriff. Prüfe Access Key, Secret und die Rechte am Bucket."
	case "InvalidAccessKeyId":
		return "Das Backup-Ziel kennt den Access Key nicht. Prüfe ihn in den Projekt-Einstellungen."
	case "SignatureDoesNotMatch":
		return "Die Signatur passt nicht. Prüfe das Secret und die Region des Backup-Ziels."
	case "AuthorizationHeaderMalformed", "IllegalLocationConstraintException", "PermanentRedirect":
		return "Die Region passt nicht zum Bucket. Prüfe die Region des Backup-Ziels."
	case "NoSuchBucket":
		return "Den Bucket gibt es beim Backup-Ziel nicht. Prüfe den Namen."
	case "NoSuchKey":
		return "Das Backup liegt nicht (mehr) im Backup-Ziel."
	case "NoSuchUpload":
		return "Das Backup-Ziel kennt den Upload nicht mehr. Lade das Backup erneut hoch."
	case "RequestTimeTooSkewed":
		return "Die Uhr des Panels weicht zu stark von der des Backup-Ziels ab. Stelle die Systemzeit richtig."
	case "EntityTooLarge":
		return "Das Backup ist für das Backup-Ziel zu groß."
	}
	if e.Code != "" {
		return fmt.Sprintf("Das Backup-Ziel antwortet mit %d (%s).", e.Status, e.Code)
	}
	return fmt.Sprintf("Das Backup-Ziel antwortet mit %d.", e.Status)
}

// do sendet eine signierte Anfrage und liefert den Körper der Antwort (höchstens 1 MiB).
func (c *s3Client) do(ctx context.Context, method, key string, query url.Values, body []byte, header http.Header) ([]byte, http.Header, error) {
	target := c.target.objectURL(key, query)
	request, err := http.NewRequestWithContext(ctx, method, target.String(), bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	for name, values := range header {
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}
	request.ContentLength = int64(len(body))
	hash := s3EmptySHA256
	if len(body) > 0 {
		hash = s3Hash(body)
	}
	signS3(request, c.target.AccessKey, c.target.Secret, c.target.Region, hash, c.now())
	response, err := c.http.Do(request)
	if err != nil {
		return nil, nil, fmt.Errorf("das Backup-Ziel ist nicht erreichbar: %w", unwrapURLError(err))
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode >= 300 {
		return nil, nil, parseS3Error(response.StatusCode, data)
	}
	// CompleteMultipartUpload meldet Fehler mit Status 200 im Körper
	if bytes.Contains(data, []byte("<Error>")) {
		return nil, nil, parseS3Error(response.StatusCode, data)
	}
	return data, response.Header, nil
}

func parseS3Error(status int, data []byte) error {
	failure := &s3Error{Status: status}
	_ = xml.Unmarshal(data, failure)
	return failure
}

func unwrapURLError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}

// presign liefert eine vorsignierte Adresse zu einem Objekt.
func (c *s3Client) presign(method, key string, query url.Values, expires time.Duration) string {
	return presignS3(method, c.target.objectURL(key, query), c.target.AccessKey, c.target.Secret, c.target.Region, c.now(), expires)
}

// createMultipart beginnt einen Multipart-Upload und liefert dessen ID.
func (c *s3Client) createMultipart(ctx context.Context, key string) (string, error) {
	data, _, err := c.do(ctx, http.MethodPost, key, url.Values{"uploads": {""}}, nil, http.Header{"Content-Type": {"application/gzip"}})
	if err != nil {
		return "", err
	}
	var result struct {
		UploadID string `xml:"UploadId"`
	}
	if err := xml.Unmarshal(data, &result); err != nil || result.UploadID == "" {
		return "", errors.New("das Backup-Ziel hat keinen Multipart-Upload begonnen")
	}
	return result.UploadID, nil
}

// s3Part ist ein hochgeladener Teil.
type s3Part struct {
	Number int    `xml:"PartNumber"`
	ETag   string `xml:"ETag"`
}

// completeMultipart setzt die Teile zum Objekt zusammen.
func (c *s3Client) completeMultipart(ctx context.Context, key, uploadID string, parts []s3Part) error {
	payload := struct {
		XMLName xml.Name `xml:"CompleteMultipartUpload"`
		Parts   []s3Part `xml:"Part"`
	}{Parts: parts}
	body, err := xml.Marshal(payload)
	if err != nil {
		return err
	}
	_, _, err = c.do(ctx, http.MethodPost, key, url.Values{"uploadId": {uploadID}}, body, http.Header{"Content-Type": {"application/xml"}})
	return err
}

// abortMultipart verwirft einen begonnenen Upload, damit keine Teile im Bucket liegen bleiben.
func (c *s3Client) abortMultipart(ctx context.Context, key, uploadID string) error {
	_, _, err := c.do(ctx, http.MethodDelete, key, url.Values{"uploadId": {uploadID}}, nil, nil)
	return err
}

// deleteObject löscht ein Objekt. Gibt es das Objekt nicht, ist das kein Fehler.
func (c *s3Client) deleteObject(ctx context.Context, key string) error {
	_, _, err := c.do(ctx, http.MethodDelete, key, nil, nil, nil)
	var failure *s3Error
	if errors.As(err, &failure) && (failure.Status == http.StatusNotFound || failure.Code == "NoSuchKey") {
		return nil
	}
	return err
}

// test prüft das Ziel: eine kleine Datei über eine vorsignierte Adresse hochladen (so wie es der Node tut),
// und sie mit einer signierten Anfrage wieder löschen.
func (c *s3Client) test(ctx context.Context) error {
	key := c.target.Prefix + ".lodestone-test"
	content := []byte("Lodestone prüft hier das Backup-Ziel. Diese Datei darf gelöscht werden.\n")
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, c.presign(http.MethodPut, key, nil, 5*time.Minute), bytes.NewReader(content))
	if err != nil {
		return err
	}
	request.ContentLength = int64(len(content))
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("das Backup-Ziel ist nicht erreichbar: %w", unwrapURLError(err))
	}
	data, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	response.Body.Close()
	if response.StatusCode >= 300 {
		return parseS3Error(response.StatusCode, data)
	}
	return c.deleteObject(ctx, key)
}

// s3PartSize wählt die Teilgröße so, dass höchstens s3MaxParts Teile entstehen (auf ganze MiB gerundet).
func s3PartSize(size int64) (partSize int64, parts int, err error) {
	if size <= 0 {
		return 0, 0, errors.New("das Backup ist leer")
	}
	if size > s3MaxObjectSize {
		return 0, 0, fmt.Errorf("das Backup ist mit %s zu groß für das Hochladen (höchstens %s)", byteSize(size), byteSize(s3MaxObjectSize))
	}
	partSize = int64(s3MinPartSize)
	if need := (size + s3MaxParts - 1) / s3MaxParts; need > partSize {
		partSize = (need + (1<<20 - 1)) / (1 << 20) * (1 << 20)
	}
	if partSize > s3MaxPartSize {
		partSize = s3MaxPartSize
	}
	parts = int((size + partSize - 1) / partSize)
	return partSize, parts, nil
}
