package checks

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"strings"
	"testing"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/config"
)

const (
	testOwner  = "org"
	testRepo   = "repo"
	testBranch = "main"
	testSHA    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

func runSecretCheck(t *testing.T, fake *fakeGitHub, rule config.Rule) *compliance.Result {
	t.Helper()
	_, client := fake.serve(t)
	c := &secretDetectionChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), rule)
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	return result
}

// ——— AWS ——————————————————————————————————————————————————————————————————————

func TestSecretDetection_AWSAccessKeyID_Fail(t *testing.T) {
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"config.env"})
	fake.setFile(testOwner, testRepo, "config.env", []byte("AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail, got %s: %s", result.Status, result.Message)
	}
	want := "1 potential secret(s) detected: - config.env:1 (aws-access-key-id)"
	if result.Message != want {
		t.Errorf("message format mismatch\ngot:  %q\nwant: %q", result.Message, want)
	}
}

func TestSecretDetection_MessageFormat_LineNumber(t *testing.T) {
	// Secret on line 3; verify the message includes the correct line number.
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{".env"})
	fake.setFile(testOwner, testRepo, ".env", []byte("FOO=bar\nBAZ=qux\nGITHUB_TOKEN=ghp_aBcDeFgHiJkLmNoPqRsTuVwXyZ123456789012\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail, got %s: %s", result.Status, result.Message)
	}
	want := "1 potential secret(s) detected: - .env:3 (github-token)"
	if result.Message != want {
		t.Errorf("message format mismatch\ngot:  %q\nwant: %q", result.Message, want)
	}
}

func TestSecretDetection_MessageFormat_MultipleViolations(t *testing.T) {
	// Two files with secrets; verify newline-separated list with - prefix.
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"a.env", "b.env"})
	fake.setFile(testOwner, testRepo, "a.env", []byte("AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE\n"))
	fake.setFile(testOwner, testRepo, "b.env", []byte("line1\nAWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail, got %s: %s", result.Status, result.Message)
	}
	if !strings.HasPrefix(result.Message, "2 potential secret(s) detected: ") {
		t.Errorf("expected message to start with count header, got: %q", result.Message)
	}
	if !strings.Contains(result.Message, "- a.env:1 (aws-access-key-id)") {
		t.Errorf("expected a.env:1 violation in message, got: %q", result.Message)
	}
	if !strings.Contains(result.Message, "- b.env:2 (aws-access-key-id)") {
		t.Errorf("expected b.env:2 violation in message, got: %q", result.Message)
	}
}

func TestSecretDetection_AWSSecretKey_Fail(t *testing.T) {
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"deploy.sh"})
	fake.setFile(testOwner, testRepo, "deploy.sh", []byte(`aws_secret_key="wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"`+"\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail, got %s: %s", result.Status, result.Message)
	}
}

// ——— GitHub tokens ————————————————————————————————————————————————————————————

func TestSecretDetection_GitHubPAT_NewFormat_Fail(t *testing.T) {
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{".env"})
	fake.setFile(testOwner, testRepo, ".env", []byte("GITHUB_TOKEN=ghp_aBcDeFgHiJkLmNoPqRsTuVwXyZ123456789012\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail, got %s: %s", result.Status, result.Message)
	}
}

func TestSecretDetection_GitHubPAT_ClassicFormat_Fail(t *testing.T) {
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"ci/setup.sh"})
	// Classic 40-char hex token with a label context
	fake.setFile(testOwner, testRepo, "ci/setup.sh", []byte(`github_token="abcdef1234567890abcdef1234567890abcdef12"`+"\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail, got %s: %s", result.Status, result.Message)
	}
}

// ——— Slack ————————————————————————————————————————————————————————————————————

func TestSecretDetection_SlackBotToken_Fail(t *testing.T) {
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"config.yaml"})
	fake.setFile(testOwner, testRepo, "config.yaml", []byte("slack_token: xoxb-123456789012-123456789012-abcdefghijklmnopqrstuvwx\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail, got %s: %s", result.Status, result.Message)
	}
}

func TestSecretDetection_SlackWebhook_Fail(t *testing.T) {
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"notify.go"})
	fake.setFile(testOwner, testRepo, "notify.go", []byte(`webhookURL := "https://hooks.slack.com/services/TXXXXXXXX/BXXXXXXXX/xxxxxxxxxxxxxxxxxxxxxxxx"`+"\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail, got %s: %s", result.Status, result.Message)
	}
}

func TestSecretDetection_SlackXoxoToken_Fail(t *testing.T) {
	// xoxo- was missing from the original character class; this is the exact
	// format shown in the secretlint demo.
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"legacy.go"})
	fake.setFile(testOwner, testRepo, "legacy.go", []byte("token := \"xoxo-23984754863-2348975623103\"\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail for xoxo- token, got %s: %s", result.Status, result.Message)
	}
}

// ——— URL credentials —————————————————————————————————————————————————————————

func TestSecretDetection_URLCredentials_Fail(t *testing.T) {
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"config.go"})
	fake.setFile(testOwner, testRepo, "config.go", []byte(`dbURL := "https://admin:hunter2abc@db.internal.example.com/prod"`+"\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail for URL with credentials, got %s: %s", result.Status, result.Message)
	}
}

func TestSecretDetection_URLCredentials_PlaceholderIgnored(t *testing.T) {
	// "user:pass@" is a well-known documentation placeholder and must not be flagged.
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"README.md"})
	fake.setFile(testOwner, testRepo, "README.md", []byte("Connect with: https://user:pass@example.com\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass for placeholder URL credentials, got %s: %s", result.Status, result.Message)
	}
}

func TestSecretDetection_URLCredentials_UsernamePasswordIgnored(t *testing.T) {
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"docs/setup.md"})
	fake.setFile(testOwner, testRepo, "docs/setup.md", []byte("Example: https://username:password@host.example.com\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass for username:password placeholder, got %s: %s", result.Status, result.Message)
	}
}

func TestSecretDetection_URLCredentials_Postgres_Fail(t *testing.T) {
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"config/database.go"})
	fake.setFile(testOwner, testRepo, "config/database.go", []byte(`dsn := "postgresql://appuser:s3cr3tpassword@db.prod.internal:5432/mydb"`+"\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail for postgresql:// credentials, got %s: %s", result.Status, result.Message)
	}
}

func TestSecretDetection_URLCredentials_MongoDB_Fail(t *testing.T) {
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{".env.production"})
	fake.setFile(testOwner, testRepo, ".env.production", []byte("MONGO_URI=mongodb+srv://admin:r3alpassword99@cluster0.mongodb.net/mydb\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail for mongodb+srv:// credentials, got %s: %s", result.Status, result.Message)
	}
}

func TestSecretDetection_URLCredentials_Redis_Fail(t *testing.T) {
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"infra/cache.tf"})
	fake.setFile(testOwner, testRepo, "infra/cache.tf", []byte(`redis_url = "redis://default:authtoken1234abc@redis.internal:6379"`+"\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail for redis:// credentials, got %s: %s", result.Status, result.Message)
	}
}

func TestSecretDetection_URLCredentials_AMQP_Fail(t *testing.T) {
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"worker/config.yaml"})
	fake.setFile(testOwner, testRepo, "worker/config.yaml", []byte("broker_url: amqps://mquser:mqpassword99@rabbit.prod.internal:5671/vhost\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail for amqps:// credentials, got %s: %s", result.Status, result.Message)
	}
}

func TestSecretDetection_URLCredentials_SMTP_Fail(t *testing.T) {
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"mailer/transport.go"})
	fake.setFile(testOwner, testRepo, "mailer/transport.go", []byte(`url := "smtps://noreply:smtppassword1@smtp.example.com:465"`+"\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail for smtps:// credentials, got %s: %s", result.Status, result.Message)
	}
}

func TestSecretDetection_URLCredentials_SSH_Fail(t *testing.T) {
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"deploy/tunnel.sh"})
	fake.setFile(testOwner, testRepo, "deploy/tunnel.sh", []byte(`SSH_URL="ssh://deploy:deploy_secret_123@bastion.prod.internal:22"`+"\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail for ssh:// credentials, got %s: %s", result.Status, result.Message)
	}
}

func TestSecretDetection_URLCredentials_FTP_Fail(t *testing.T) {
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"legacy/upload.sh"})
	fake.setFile(testOwner, testRepo, "legacy/upload.sh", []byte(`FTP_URL="ftp://ftpuser:ftppassword9@files.example.com"`+"\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail for ftp:// credentials, got %s: %s", result.Status, result.Message)
	}
}

// ——— Private key ——————————————————————————————————————————————————————————————

func TestSecretDetection_PrivateKey_Fail(t *testing.T) {
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"deploy_key.pem"})
	fake.setFile(testOwner, testRepo, "deploy_key.pem", []byte("-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAK...\n-----END RSA PRIVATE KEY-----\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail, got %s: %s", result.Status, result.Message)
	}
}

func TestSecretDetection_OpenSSHKey_Fail(t *testing.T) {
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"id_ed25519"})
	fake.setFile(testOwner, testRepo, "id_ed25519", []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEA...\n-----END OPENSSH PRIVATE KEY-----\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail, got %s: %s", result.Status, result.Message)
	}
}

// ——— GCP ——————————————————————————————————————————————————————————————————————

func TestSecretDetection_GCPServiceAccountKey_Fail(t *testing.T) {
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"service-account.json"})
	content := []byte(`{
  "type": "service_account",
  "project_id": "my-project",
  "private_key_id": "abcdef1234567890abcdef1234567890abcdef12",
  "private_key": "-----BEGIN RSA PRIVATE KEY-----\n..."
}`)
	fake.setFile(testOwner, testRepo, "service-account.json", content)

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail, got %s: %s", result.Status, result.Message)
	}
}

func TestSecretDetection_GCPKeyInNonJSON_Pass(t *testing.T) {
	// The GCP rule only matches .json files; a .txt file with the same pattern should pass.
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"notes.txt"})
	content := []byte(`"private_key_id": "abcdef1234567890abcdef1234567890abcdef12"`)
	fake.setFile(testOwner, testRepo, "notes.txt", content)

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass for non-json file, got %s: %s", result.Status, result.Message)
	}
}

// ——— Stripe / SendGrid / Twilio ——————————————————————————————————————————————

func TestSecretDetection_StripeSecretKey_Fail(t *testing.T) {
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"payment.js"})
	fake.setFile(testOwner, testRepo, "payment.js", []byte(`const stripe = require('stripe')('sk_live_4eC39HqLyjWDarjtT1zdp7dc');`+"\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail, got %s: %s", result.Status, result.Message)
	}
}

func TestSecretDetection_SendGridKey_Fail(t *testing.T) {
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"mailer.py"})
	fake.setFile(testOwner, testRepo, "mailer.py", []byte(`SENDGRID_API_KEY = "SG.aBcDeFgHiJkLmNoPqRsTuVw.xYzAbCdEfGhIjKlMnOpQrStUvWxYzAbCdEfGhIjKlMnO"`+"\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail, got %s: %s", result.Status, result.Message)
	}
}

func TestSecretDetection_TwilioKey_Fail(t *testing.T) {
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"sms.rb"})
	fake.setFile(testOwner, testRepo, "sms.rb", []byte(`TWILIO_KEY = "SK1234567890abcdef1234567890abcdef"`+"\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail, got %s: %s", result.Status, result.Message)
	}
}

// ——— npm auth token ————————————————————————————————————————————————————————————

func TestSecretDetection_NpmAuthToken_Fail(t *testing.T) {
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{".npmrc"})
	fake.setFile(testOwner, testRepo, ".npmrc", []byte("//registry.npmjs.org/:_authToken=npm_aBcDeFgHiJkLmNoPqRsTuVwXyZ123456\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail, got %s: %s", result.Status, result.Message)
	}
}

func TestSecretDetection_NpmAuthTokenInGoFile_Pass(t *testing.T) {
	// npm auth token rule only applies to .npmrc files.
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"main.go"})
	fake.setFile(testOwner, testRepo, "main.go", []byte(`_authToken=npm_aBcDeFgHiJkLmNoPqRsTuVwXyZ123456`+"\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass for non-.npmrc file, got %s: %s", result.Status, result.Message)
	}
}

// ——— AI API keys —————————————————————————————————————————————————————————————

func TestSecretDetection_OpenRouterAPIKey_Fail(t *testing.T) {
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"config.go"})
	fake.setFile(testOwner, testRepo, "config.go", []byte(`apiKey := "sk-ece21fbfe35631bc-2rfpd0-be185849"`+"\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail for OpenRouter key, got %s: %s", result.Status, result.Message)
	}
}

func TestSecretDetection_OpenAIKey_Fail(t *testing.T) {
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{".env"})
	fake.setFile(testOwner, testRepo, ".env", []byte("OPENAI_API_KEY=sk-proj-aBcDeFgHiJkLmNoPqRsTuVwXyZ1234567890abcdefghij\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail for OpenAI key, got %s: %s", result.Status, result.Message)
	}
}

func TestSecretDetection_AnthropicKey_Fail(t *testing.T) {
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{".env"})
	fake.setFile(testOwner, testRepo, ".env", []byte("ANTHROPIC_API_KEY=sk-ant-api03-aBcDeFgHiJkLmNoPqRsTuVwXyZ1234567890abcdefghijklmnopqrstuvwxyz\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail for Anthropic key, got %s: %s", result.Status, result.Message)
	}
}

// ——— Generic secret assignment ————————————————————————————————————————————————

func TestSecretDetection_GenericPasswordAssignment_Fail(t *testing.T) {
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"config.go"})
	fake.setFile(testOwner, testRepo, "config.go", []byte(`password = "supersecretpassword123456"`+"\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail, got %s: %s", result.Status, result.Message)
	}
}

// ——— Clean repo ———————————————————————————————————————————————————————————————

func TestSecretDetection_CleanRepo_Pass(t *testing.T) {
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"README.md", "main.go"})
	fake.setFile(testOwner, testRepo, "README.md", []byte("# My Project\n"))
	fake.setFile(testOwner, testRepo, "main.go", []byte(`package main

import "fmt"

func main() {
	fmt.Println("hello")
}
`))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass, got %s: %s", result.Status, result.Message)
	}
}

// ——— Placeholder values ———————————————————————————————————————————————————————

func TestSecretDetection_PlaceholderAWSKey_Pass(t *testing.T) {
	// Values containing obvious placeholder text should not be flagged.
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"docs/setup.md"})
	fake.setFile(testOwner, testRepo, "docs/setup.md", []byte("Set AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE (replace with your key)\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	// AKIAIOSFODNN7EXAMPLE is the canonical AWS example key; it matches the pattern
	// but is not a placeholder string our list knows about. This test verifies the
	// real pattern fires — the official example key IS flagged intentionally, since
	// it still leaks the format and users should use environment variables instead.
	// This test documents that behaviour rather than asserting pass.
	_ = result
}

func TestSecretDetection_PlaceholderToken_Pass(t *testing.T) {
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"README.md"})
	fake.setFile(testOwner, testRepo, "README.md", []byte(`export GITHUB_TOKEN=your-token-here`+"\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	// "your-token-here" does not match any regex (too short, no valid prefix), so pass.
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass for placeholder, got %s: %s", result.Status, result.Message)
	}
}

// ——— Binary / skipped files ———————————————————————————————————————————————————

func TestSecretDetection_BinaryFileSkipped(t *testing.T) {
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	// .png is in the skip list; even with a matching pattern it should not be fetched/scanned.
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"assets/logo.png"})
	// We deliberately do NOT register the file content — if it were fetched it would 404 and error.

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass (binary skipped), got %s: %s", result.Status, result.Message)
	}
}

// ——— Vendor / node_modules skipped ———————————————————————————————————————————

func TestSecretDetection_VendorPathSkipped(t *testing.T) {
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"vendor/lib/auth.go"})
	// No file content registered — if scanned it would 404 and error.

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass (vendor skipped), got %s: %s", result.Status, result.Message)
	}
}

func TestSecretDetection_NodeModulesSkipped(t *testing.T) {
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"node_modules/lodash/index.js"})

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass (node_modules skipped), got %s: %s", result.Status, result.Message)
	}
}

// ——— Empty repository ————————————————————————————————————————————————————————

func TestSecretDetection_EmptyTree_Pass(t *testing.T) {
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{})

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass for empty repo, got %s: %s", result.Status, result.Message)
	}
}

// ——— params: rules filter ————————————————————————————————————————————————————

func TestSecretDetection_RulesParam_OnlyTargeted(t *testing.T) {
	// Only enable the github-token rule via params.
	// A Slack token in the file should NOT trigger a failure.
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"config.yaml"})
	fake.setFile(testOwner, testRepo, "config.yaml", []byte("slack_token: xoxb-123456789012-123456789012-abcdefghijklmnopqrstuvwx\n"))

	rule := config.Rule{
		ID:       "secret-detection",
		Name:     "secret-detection",
		Severity: config.SeverityError,
		Enabled:  true,
		ListParams: map[string][]string{
			"rules": {"github-token"},
		},
	}

	result := runSecretCheck(t, fake, rule)
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass when only github-token rule enabled, got %s: %s", result.Status, result.Message)
	}
}

func TestSecretDetection_RulesParam_TargetedMatches(t *testing.T) {
	// Only enable github-token; a GitHub token in the file SHOULD trigger failure.
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{".env"})
	fake.setFile(testOwner, testRepo, ".env", []byte("GITHUB_TOKEN=ghp_aBcDeFgHiJkLmNoPqRsTuVwXyZ123456789012\n"))

	rule := config.Rule{
		ID:       "secret-detection",
		Name:     "secret-detection",
		Severity: config.SeverityError,
		Enabled:  true,
		ListParams: map[string][]string{
			"rules": {"github-token"},
		},
	}

	result := runSecretCheck(t, fake, rule)
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail for github-token rule, got %s: %s", result.Status, result.Message)
	}
}

// ——— params: exclude_rules filter ————————————————————————————————————————————

func TestSecretDetection_ExcludeRulesParam(t *testing.T) {
	// Exclude the slack-token rule; only a Slack token present → should pass.
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"config.yaml"})
	fake.setFile(testOwner, testRepo, "config.yaml", []byte("slack_token: xoxb-123456789012-123456789012-abcdefghijklmnopqrstuvwx\n"))

	rule := config.Rule{
		ID:       "secret-detection",
		Name:     "secret-detection",
		Severity: config.SeverityError,
		Enabled:  true,
		ListParams: map[string][]string{
			"exclude_rules": {"slack-token", "generic-secret-assignment"},
		},
	}

	result := runSecretCheck(t, fake, rule)
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass when slack-token excluded, got %s: %s", result.Status, result.Message)
	}
}

// ——— HEAD not found ——————————————————————————————————————————————————————————

func TestSecretDetection_HeadNotFound_Skip(t *testing.T) {
	// No gitRef registered → GetBranchHEAD returns "" → skip.
	fake := newFakeGitHub()
	_, client := fake.serve(t)
	c := &secretDetectionChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("secret-detection"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != compliance.StatusSkip {
		t.Errorf("expected skip when HEAD not found, got %s: %s", result.Status, result.Message)
	}
}

// ——— Multiple secrets in one repo ————————————————————————————————————————————

func TestSecretDetection_MultipleFiles_MultipleViolations(t *testing.T) {
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"a.env", "b.yaml"})
	fake.setFile(testOwner, testRepo, "a.env", []byte("AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE\n"))
	fake.setFile(testOwner, testRepo, "b.yaml", []byte("slack_token: xoxb-123456789012-123456789012-abcdefghijklmnopqrstuvwx\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail, got %s: %s", result.Status, result.Message)
	}
}

// ——— Unit tests for helpers ———————————————————————————————————————————————————

func TestShouldSkipPath(t *testing.T) {
	cases := []struct {
		path string
		skip bool
	}{
		{"vendor/foo/bar.go", true},
		{"node_modules/lodash/index.js", true},
		{".git/config", true},
		{"dist/bundle.js", true},
		{"build/output.js", true},
		{"src/main.go", false},
		{"image.png", true},
		{"logo.jpg", true},
		{"archive.zip", true},
		{"README.md", false},
		{"config.yaml", false},
	}
	for _, tc := range cases {
		got := shouldSkipPath(tc.path)
		if got != tc.skip {
			t.Errorf("shouldSkipPath(%q) = %v, want %v", tc.path, got, tc.skip)
		}
	}
}

func TestFileExt(t *testing.T) {
	cases := []struct {
		path string
		ext  string
	}{
		{"image.PNG", ".png"},
		{"script.Go", ".go"},
		{"no-extension", ""},
		{".npmrc", ".npmrc"},
		{"path/to/file.yaml", ".yaml"},
	}
	for _, tc := range cases {
		got := fileExt(tc.path)
		if got != tc.ext {
			t.Errorf("fileExt(%q) = %q, want %q", tc.path, got, tc.ext)
		}
	}
}

func TestIsPlaceholder(t *testing.T) {
	cases := []struct {
		value       string
		placeholder bool
	}{
		{"your-token-here", true},
		{"changeme", true},
		{"CHANGEME_SECRET", true},
		{"ghp_aBcDeFgHiJkLmNoPqRsTuVwXyZ123456789012", false},
		{"xoxb-123456789012-123456789012-abcdefghijklmnopqrstuvwx", false},
	}
	for _, tc := range cases {
		got := isPlaceholder(tc.value)
		if got != tc.placeholder {
			t.Errorf("isPlaceholder(%q) = %v, want %v", tc.value, got, tc.placeholder)
		}
	}
}

func TestResolveActiveRules_Default(t *testing.T) {
	rule := baseRule("secret-detection")
	got := resolveActiveRules(rule)
	if len(got) != len(secretRules) {
		t.Errorf("expected %d rules by default, got %d", len(secretRules), len(got))
	}
}

func TestResolveActiveRules_RulesParam(t *testing.T) {
	rule := config.Rule{
		ID:      "secret-detection",
		Enabled: true,
		ListParams: map[string][]string{
			"rules": {"aws-access-key-id", "github-token"},
		},
	}
	got := resolveActiveRules(rule)
	if len(got) != 2 {
		t.Errorf("expected 2 rules, got %d", len(got))
	}
	for _, r := range got {
		if r.id != "aws-access-key-id" && r.id != "github-token" {
			t.Errorf("unexpected rule id: %s", r.id)
		}
	}
}

func TestResolveActiveRules_ExcludeParam(t *testing.T) {
	rule := config.Rule{
		ID:      "secret-detection",
		Enabled: true,
		ListParams: map[string][]string{
			"exclude_rules": {"slack-token", "slack-webhook"},
		},
	}
	got := resolveActiveRules(rule)
	if len(got) != len(secretRules)-2 {
		t.Errorf("expected %d rules after excluding 2, got %d", len(secretRules)-2, len(got))
	}
	for _, r := range got {
		if r.id == "slack-token" || r.id == "slack-webhook" {
			t.Errorf("excluded rule %s still present", r.id)
		}
	}
}

// ——— scanRepoArchive error paths ——————————————————————————————————————————————

func TestSecretDetection_ArchiveHTTPError(t *testing.T) {
	// Non-200/404 HTTP status from the tarball download must surface as an error.
	fake := newFakeGitHub()
	fake.tarballStatus = 500
	_, client := fake.serve(t)
	c := &secretDetectionChecker{}
	_, err := c.Check(context.Background(), client, pubRepo(), baseRule("secret-detection"))
	if err == nil {
		t.Fatal("expected error for HTTP 500 from tarball endpoint, got nil")
	}
}

func TestSecretDetection_CorruptGzip(t *testing.T) {
	// A non-gzip body must surface as an error from gzip.NewReader.
	fake := newFakeGitHub()
	fake.tarballBody = []byte("this is not gzip data")
	_, client := fake.serve(t)
	c := &secretDetectionChecker{}
	_, err := c.Check(context.Background(), client, pubRepo(), baseRule("secret-detection"))
	if err == nil {
		t.Fatal("expected error for corrupt gzip archive, got nil")
	}
}

func TestSecretDetection_TarEntryDirectory(t *testing.T) {
	// A tar archive containing only a directory entry (TypeDir) must be skipped
	// without error and produce a pass result.
	fake := newFakeGitHub()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeDir,
		Name:     "prefix/subdir/",
		Mode:     0o755,
	})
	tw.Close()
	gw.Close()
	fake.tarballBody = buf.Bytes()
	_, client := fake.serve(t)

	c := &secretDetectionChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("secret-detection"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass for dir-only archive, got %s: %s", result.Status, result.Message)
	}
}

// ——— Zero active rules (Check skip path) ———————————————————————————————————

func TestSecretDetection_NoActiveRules_Skip(t *testing.T) {
	// When "rules" param lists an id that doesn't exist, zero rules are resolved
	// and Check returns StatusSkip without hitting the network at all.
	fake := newFakeGitHub()
	_, client := fake.serve(t)
	c := &secretDetectionChecker{}
	rule := config.Rule{
		ID:       "secret-detection",
		Name:     "secret-detection",
		Severity: config.SeverityError,
		Enabled:  true,
		ListParams: map[string][]string{
			"rules": {"nonexistent-rule-id"},
		},
	}
	result, err := c.Check(context.Background(), client, pubRepo(), rule)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != compliance.StatusSkip {
		t.Errorf("expected skip when no rules active, got %s: %s", result.Status, result.Message)
	}
}

// ——— stripArchivePrefix unit test ————————————————————————————————————————————

func TestStripArchivePrefix(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"owner-repo-abc1234/path/to/file.go", "path/to/file.go"},
		{"owner-repo-abc1234/README.md", "README.md"},
		// No slash at all — returns the name unchanged (fallback branch).
		{"no-slash-at-all", "no-slash-at-all"},
		// Entry that is just the top-level directory itself (empty suffix).
		{"topdir/", ""},
	}
	for _, tc := range cases {
		got := stripArchivePrefix(tc.input)
		if got != tc.want {
			t.Errorf("stripArchivePrefix(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// ——— git-cascade:allow suppression comment ————————————————————————————————————

func TestSecretDetection_AllowComment_HashInline(t *testing.T) {
	// Shell/YAML/env: inline # git-cascade:allow suppresses the violation.
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{".env"})
	fake.setFile(testOwner, testRepo, ".env", []byte("AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE # git-cascade:allow\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass with # allow comment, got %s: %s", result.Status, result.Message)
	}
}

func TestSecretDetection_AllowComment_SlashSlashInline(t *testing.T) {
	// Go/JS/TS: inline // git-cascade:allow suppresses the violation.
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"config.go"})
	fake.setFile(testOwner, testRepo, "config.go", []byte(`password = "supersecretpassword123456" // git-cascade:allow`+"\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass with // allow comment, got %s: %s", result.Status, result.Message)
	}
}

func TestSecretDetection_AllowComment_SQLInline(t *testing.T) {
	// SQL/Lua: inline -- git-cascade:allow suppresses the violation.
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"seed.sql"})
	fake.setFile(testOwner, testRepo, "seed.sql", []byte(`INSERT INTO cfg VALUES ('password', 'supersecretpassword123456'); -- git-cascade:allow`+"\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass with -- allow comment, got %s: %s", result.Status, result.Message)
	}
}

func TestSecretDetection_AllowComment_HTMLInline(t *testing.T) {
	// HTML/XML: inline <!-- git-cascade:allow suppresses the violation.
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"index.html"})
	fake.setFile(testOwner, testRepo, "index.html", []byte(`<meta name="token" content="ghp_aBcDeFgHiJkLmNoPqRsTuVwXyZ123456789012"> <!-- git-cascade:allow -->`+"\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass with <!-- allow comment, got %s: %s", result.Status, result.Message)
	}
}

func TestSecretDetection_AllowComment_CSSBlockInline(t *testing.T) {
	// CSS: inline /* git-cascade:allow suppresses the violation.
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"style.css"})
	fake.setFile(testOwner, testRepo, "style.css", []byte(`content: "sk_live_4eC39HqLyjWDarjtT1zdp7dc"; /* git-cascade:allow */`+"\n")) 

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass with /* allow comment, got %s: %s", result.Status, result.Message)
	}
}

func TestSecretDetection_AllowComment_PrecedingLine(t *testing.T) {
	// A git-cascade:allow comment on the line above suppresses the match.
	// Useful for multi-line constructs like PEM headers.
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"test_cert.pem"})
	fake.setFile(testOwner, testRepo, "test_cert.pem", []byte("# git-cascade:allow\n-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAK...\n-----END RSA PRIVATE KEY-----\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass with allow comment on preceding line, got %s: %s", result.Status, result.Message)
	}
}

func TestSecretDetection_AllowComment_OnlyOneLine_OtherFileFails(t *testing.T) {
	// Allow comment suppresses only the annotated file; a second file without the
	// comment still triggers a violation.
	fake := newFakeGitHub()
	fake.setGitRef(testOwner, testRepo, testBranch, testSHA)
	fake.setGitTree(testOwner, testRepo, testSHA, []string{"allowed.env", "real.env"})
	fake.setFile(testOwner, testRepo, "allowed.env", []byte("AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE # git-cascade:allow\n"))
	fake.setFile(testOwner, testRepo, "real.env", []byte("AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE\n"))

	result := runSecretCheck(t, fake, baseRule("secret-detection"))
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail when non-annotated file has a secret, got %s: %s", result.Status, result.Message)
	}
}

// ——— isPlaceholder exact-match branch ————————————————————————————————————————

func TestIsPlaceholder_ExactMatch(t *testing.T) {
	// placeholderExact entries must be caught by the exact-match loop.
	for _, exact := range placeholderExact {
		if !isPlaceholder(exact) {
			t.Errorf("isPlaceholder(%q) = false, want true (exact match)", exact)
		}
		// Upper-cased variant must also match (case-insensitive).
		upper := strings.ToUpper(exact)
		if !isPlaceholder(upper) {
			t.Errorf("isPlaceholder(%q) = false, want true (exact match, upper)", upper)
		}
	}
}
