package redact

import (
	"strings"
	"testing"
)

// Test secrets are constructed at runtime from deterministic character
// sequences: they exercise the scanner's patterns without putting
// credential-shaped literals in source code.

func seq(charset string, n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteByte(charset[i%len(charset)])
	}
	return b.String()
}

var (
	fakeAWSKey     = "AKIA" + seq("ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789", 16)
	fakeASIAKey    = "ASIA" + seq("ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789", 16)
	fakeGHPToken   = "ghp_" + seq("0123456789abcdefghijklmnopqrstuvwxyz", 36)
	fakeGHPat      = "github_pat_" + seq("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ_", 36)
	fakeGitlab     = "glpat-" + seq("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ", 24)
	fakeAliyun     = "LTAI" + seq("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ", 18)
	fakeTencent    = "AKID" + seq("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ", 32)
	fakeGoogle     = "AIza" + seq("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ", 35)
	fakeSlack      = "xoxb-" + seq("0123456789", 12) + "-" + seq("0123456789", 12) + "-" + seq("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ", 24)
	fakeModelKey   = "sk-" + seq("0123456789abcdefghijklmnopqrstuvwxyz", 24)
	fakeBearer     = "abcdef0123456789deadbeef=="
	fakeFieldValue = seq("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ", 16)
	fakeKeyBody    = seq("0123456789ABCDEF", 48)
	fakeSSHBody    = seq("0123456789ABCDEF", 24)
)

func TestScanRedactsKnownSecretFormats(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		secret string
	}{
		{"aws access key id", "deploy with " + fakeAWSKey + " first", fakeAWSKey},
		{"aws session key", "temp creds " + fakeASIAKey + " expire hourly", fakeASIAKey},
		{"github token", "push using " + fakeGHPToken, fakeGHPToken},
		{"github fine-grained pat", "auth " + fakeGHPat, fakeGHPat},
		{"gitlab token", "ci uses " + fakeGitlab, fakeGitlab},
		{"aliyun access key", "oss 上传用 " + fakeAliyun, fakeAliyun},
		{"tencent key", "cos uses " + fakeTencent, fakeTencent},
		{"google api key", "map key " + fakeGoogle, fakeGoogle},
		{"slack token", "notify " + fakeSlack, fakeSlack},
		{"openai-style key", "model key " + fakeModelKey, fakeModelKey},
		{"bearer header", "Authorization: Bearer " + fakeBearer, fakeBearer},
		{"private key block", "sign with\n-----BEGIN RSA PRIVATE KEY-----\n" + fakeKeyBody + "\n-----END RSA PRIVATE KEY-----\nafter", fakeKeyBody},
		{"openssh key block", "-----BEGIN OPENSSH PRIVATE KEY-----\n" + fakeSSHBody + "\n-----END OPENSSH PRIVATE KEY-----", fakeSSHBody},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, n := Scan(tc.input)
			if n < 1 {
				t.Fatalf("nothing redacted in %q", tc.input)
			}
			if strings.Contains(out, tc.secret) {
				t.Fatalf("secret survived: %q", out)
			}
			if !strings.Contains(out, "[REDACTED]") {
				t.Fatalf("no [REDACTED] marker in %q", out)
			}
		})
	}
}

// Credential-bearing fields are redacted regardless of label, separator,
// or quoting; the label itself stays readable.
func TestScanRedactsLabeledSecretFields(t *testing.T) {
	labels := []string{"password", "passwd", "pwd", "secret", "token", "api_key", "apiKey"}
	separators := []string{": ", " = ", ":"}
	quotes := []string{"", `"`, "'"}
	for _, label := range labels {
		for _, sep := range separators {
			for _, q := range quotes {
				value := q + fakeFieldValue + q
				in := "cfg " + label + sep + value + " end"
				out, n := Scan(in)
				if n < 1 {
					t.Fatalf("field not redacted: %q", in)
				}
				if strings.Contains(out, fakeFieldValue) {
					t.Fatalf("field value survived: %q", out)
				}
				if !strings.Contains(out, label) || !strings.Contains(out, "[REDACTED]") {
					t.Fatalf("label lost or marker missing: %q", out)
				}
			}
		}
	}
}

func TestScanKeepsNormalText(t *testing.T) {
	normal := []string{
		"完全正常的中文记忆，含 MemoryTree 与 gardener。",
		"see https://example.com/docs?query=memory-tree for details",
		"the token budget defaults to 1200",
		"AKIA is just a prefix mention, not a key",
		"password: [REDACTED] already cleaned",
		"sk- short prefix alone stays",
		"date 2026-10-27 and time 08:00:00Z",
	}
	for _, in := range normal {
		out, n := Scan(in)
		if out != in || n != 0 {
			t.Errorf("Scan(%q) = %q, %d; want unchanged, 0", in, out, n)
		}
	}
}

// Short field values (fewer than 6 characters) look like prose, not
// credentials, and must stay untouched.
func TestScanKeepsShortFieldValues(t *testing.T) {
	for _, in := range []string{"password: abc", "pwd: 123", "short password: abc is fine in prose"} {
		out, n := Scan(in)
		if out != in || n != 0 {
			t.Errorf("Scan(%q) = %q, %d; want unchanged, 0", in, out, n)
		}
	}
}

func TestScanCountsEachReplacement(t *testing.T) {
	in := "keys: " + fakeGHPToken + " and " + fakeGHPToken
	out, n := Scan(in)
	if n != 2 {
		t.Fatalf("count = %d, want 2 (%q)", n, out)
	}
	if got := strings.Count(out, "[REDACTED]"); got != 2 {
		t.Fatalf("markers = %d, want 2 (%q)", got, out)
	}
}

func TestScanPrivateKeyBlockCountsOnce(t *testing.T) {
	in := "-----BEGIN PRIVATE KEY-----\n" + fakeKeyBody + "\nsecond line\n-----END PRIVATE KEY-----"
	out, n := Scan(in)
	if n != 1 {
		t.Fatalf("count = %d, want 1 (%q)", n, out)
	}
	if strings.Contains(out, fakeKeyBody) {
		t.Fatalf("key body survived: %q", out)
	}
}
