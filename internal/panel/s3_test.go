package panel

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Testvektoren aus der AWS-Dokumentation „Signature Calculations for the Authorization Header“ und
// „Authenticating Requests: Using Query Parameters“ (S3, us-east-1, 24.05.2013).
const (
	awsAccessKey = "AKIAIOSFODNN7EXAMPLE"
	awsSecret    = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
)

var awsTime = time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)

func signedAuthorization(t *testing.T, method, address string, header http.Header, payloadHash string) string {
	t.Helper()
	request, err := http.NewRequest(method, address, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, values := range header {
		request.Header[name] = values
	}
	signS3(request, awsAccessKey, awsSecret, "us-east-1", payloadHash, awsTime)
	return request.Header.Get("Authorization")
}

func TestSigV4HeaderVectors(t *testing.T) {
	cases := []struct {
		name, method, address string
		header                http.Header
		payload               string
		signed, signature     string
	}{
		{
			name: "GET Object mit Range", method: "GET", address: "https://examplebucket.s3.amazonaws.com/test.txt",
			header: http.Header{"Range": {"bytes=0-9"}}, payload: s3EmptySHA256,
			signed:    "host;range;x-amz-content-sha256;x-amz-date",
			signature: "f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41",
		},
		{
			name: "PUT Object", method: "PUT", address: "https://examplebucket.s3.amazonaws.com/test$file.text",
			header:    http.Header{"Date": {"Fri, 24 May 2013 00:00:00 GMT"}, "X-Amz-Storage-Class": {"REDUCED_REDUNDANCY"}},
			payload:   s3Hash([]byte("Welcome to Amazon S3.")),
			signed:    "date;host;x-amz-content-sha256;x-amz-date;x-amz-storage-class",
			signature: "98ad721746da40c64f1a55b78f14c238d841ea1380cd77a1b5971af0ece108bd",
		},
		{
			name: "GET Bucket Lifecycle", method: "GET", address: "https://examplebucket.s3.amazonaws.com/?lifecycle", payload: s3EmptySHA256,
			signed:    "host;x-amz-content-sha256;x-amz-date",
			signature: "fea454ca298b7da1c68078a5d1bdbfbbe0d65c699e0f91ac7a200a0136783543",
		},
		{
			name: "GET Bucket (Objekte auflisten)", method: "GET", address: "https://examplebucket.s3.amazonaws.com/?max-keys=2&prefix=J", payload: s3EmptySHA256,
			signed:    "host;x-amz-content-sha256;x-amz-date",
			signature: "34b48302e7b5fa45bde8084f4b7868a86f0a534bc59db6670ed5711ef69dc6f7",
		},
	}
	for _, c := range cases {
		want := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request,SignedHeaders=" + c.signed + ",Signature=" + c.signature
		if got := signedAuthorization(t, c.method, c.address, c.header, c.payload); got != want {
			t.Errorf("%s:\n got  %s\n want %s", c.name, got, want)
		}
	}
}

func TestSigV4PresignVector(t *testing.T) {
	target, _ := url.Parse("https://examplebucket.s3.amazonaws.com/test.txt")
	got := presignS3("GET", target, awsAccessKey, awsSecret, "us-east-1", awsTime, 86400*time.Second)
	want := "https://examplebucket.s3.amazonaws.com/test.txt?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=AKIAIOSFODNN7EXAMPLE%2F20130524%2Fus-east-1%2Fs3%2Faws4_request&X-Amz-Date=20130524T000000Z&X-Amz-Expires=86400&X-Amz-SignedHeaders=host&X-Amz-Signature=aeeed9bbccd4d02ee5c0109b86d86835f995330da4c265957d157751f604d404"
	if got != want {
		t.Fatalf("vorsignierte Adresse:\n got  %s\n want %s", got, want)
	}
	// Mehr als sieben Tage lässt SigV4 nicht zu
	if long := presignS3("GET", target, awsAccessKey, awsSecret, "us-east-1", awsTime, 30*24*time.Hour); !strings.Contains(long, "X-Amz-Expires=604800&") {
		t.Errorf("Ablauf nicht begrenzt: %s", long)
	}
}

func TestS3EscapeAndQuery(t *testing.T) {
	if got := s3Escape("a b/c+d~e*", true); got != "a%20b/c%2Bd~e%2A" {
		t.Errorf("s3Escape = %q", got)
	}
	if got := s3Escape("a/b", false); got != "a%2Fb" {
		t.Errorf("s3Escape ohne / = %q", got)
	}
	if got := s3Query(url.Values{"uploads": {""}, "b": {"2", "1"}, "a": {"x y"}}); got != "a=x%20y&b=1&b=2&uploads=" {
		t.Errorf("s3Query = %q", got)
	}
}

func TestS3ObjectURL(t *testing.T) {
	path := s3Target{Endpoint: "https://s3.example.com", Bucket: "meine-backups", PathStyle: true}
	if got := path.objectURL("lodestone/abc/x.tar.gz", url.Values{"partNumber": {"1"}}).String(); got != "https://s3.example.com/meine-backups/lodestone/abc/x.tar.gz?partNumber=1" {
		t.Errorf("Pfad-Adresse: %s", got)
	}
	virtual := s3Target{Endpoint: "https://s3.example.com", Bucket: "meine-backups"}
	if got := virtual.objectURL("x.tar.gz", nil).String(); got != "https://meine-backups.s3.example.com/x.tar.gz" {
		t.Errorf("Virtual-Host-Adresse: %s", got)
	}
}

func TestS3PartSize(t *testing.T) {
	if _, _, err := s3PartSize(0); err == nil {
		t.Error("leere Datei angenommen")
	}
	if size, parts, _ := s3PartSize(10 << 20); size != s3MinPartSize || parts != 1 {
		t.Errorf("klein: %d/%d", size, parts)
	}
	if size, parts, _ := s3PartSize(200 << 30); parts > s3MaxParts || size%(1<<20) != 0 || int64(parts)*size < 200<<30 {
		t.Errorf("groß: %d/%d", size, parts)
	}
	if _, _, err := s3PartSize(s3MaxObjectSize + 1); err == nil {
		t.Error("zu groß angenommen")
	}
}

func TestS3ErrorsAreGerman(t *testing.T) {
	err := parseS3Error(403, []byte(`<?xml version="1.0"?><Error><Code>SignatureDoesNotMatch</Code><Message>The request signature…</Message></Error>`))
	if !strings.Contains(err.Error(), "Signatur passt nicht") {
		t.Errorf("Meldung: %v", err)
	}
	if err := parseS3Error(500, []byte("kaputt")); !strings.Contains(err.Error(), "500") {
		t.Errorf("ohne Code: %v", err)
	}
}
