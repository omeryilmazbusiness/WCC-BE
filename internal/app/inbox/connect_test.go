package inbox_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/integration"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/integration/stub"
	"github.com/wodi-crm/wodi-crm-be/internal/app/inbox"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/inbox"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/crypto"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

func TestConnectAllSocialChannels(t *testing.T) {
	repo := newMem()
	svc := newConnectService(t, repo)
	branch := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	ctx := context.Background()

	cases := []struct {
		name string
		in   inbox.ConnectInput
		want []string // public_meta keys that must exist
	}{
		{
			name: "whatsapp",
			in: inbox.ConnectInput{
				BranchID: branch, Provider: domain.ChannelWhatsApp,
				DisplayName: "WA", AccessToken: "tok-wa", PhoneNumberID: "pn-1",
				WABAID: "waba-1", DisplayPhone: "+9665", VerifyToken: "v-wa",
			},
			want: []string{"phone_number_id", "access_token_hint", "verify_token_hint"},
		},
		{
			name: "instagram",
			in: inbox.ConnectInput{
				BranchID: branch, Provider: domain.ChannelInstagram,
				DisplayName: "IG", AccessToken: "tok-ig", PageID: "page-1",
				IGUserID: "ig-1", VerifyToken: "v-ig",
			},
			want: []string{"page_id", "ig_user_id", "access_token_hint", "verify_token_hint"},
		},
		{
			name: "facebook",
			in: inbox.ConnectInput{
				BranchID: branch, Provider: domain.ChannelFacebook,
				DisplayName: "FB", AccessToken: "tok-fb", PageID: "page-2",
				VerifyToken: "v-fb",
			},
			want: []string{"page_id", "access_token_hint", "verify_token_hint"},
		},
		{
			name: "gmail",
			in: inbox.ConnectInput{
				BranchID: branch, Provider: domain.ChannelGmail,
				DisplayName: "GM", ClientID: "cid", ClientSecret: "sec",
				RefreshToken: "rt", MailboxEmail: "a@b.com",
			},
			want: []string{"mailbox_email", "has_refresh_token"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := svc.Connect(ctx, tc.in)
			if err != nil {
				t.Fatalf("Connect: %v", err)
			}
			if res == nil || res.Account == nil {
				t.Fatal("nil result")
			}
			if !res.Account.Connected {
				t.Fatalf("expected connected=true, got %#v", res.Account)
			}
			if res.Account.Status != domain.AccountConnected {
				t.Fatalf("status=%s", res.Account.Status)
			}
			if !strings.Contains(res.WebhookURL, "/v1/webhooks/"+tc.name) {
				t.Fatalf("webhook_url=%q", res.WebhookURL)
			}
			if res.Account.ConfigJSON != nil || res.Account.SecretsEnc != "" {
				t.Fatal("secrets must be stripped from account response")
			}
			if res.VerifyToken == "" {
				t.Fatal("verify token must be returned once on connect")
			}
			if tc.in.VerifyToken != "" && res.VerifyToken != tc.in.VerifyToken {
				t.Fatalf("verify token=%q", res.VerifyToken)
			}
			if _, ok := res.Account.PublicMeta["verify_token"]; ok {
				t.Fatal("public_meta must not carry the verify token")
			}
			for _, k := range tc.want {
				if res.Account.PublicMeta[k] == "" {
					t.Fatalf("missing public_meta[%s]: %#v", k, res.Account.PublicMeta)
				}
			}
		})
	}

	accounts, _, err := svc.IntegrationHealth(ctx, branch)
	if err != nil {
		t.Fatal(err)
	}
	by := map[domain.Channel]domain.IntegrationAccount{}
	for _, a := range accounts {
		by[a.Provider] = a
	}
	for _, ch := range domain.ConnectableChannels() {
		a, ok := by[ch]
		if !ok {
			t.Fatalf("health missing %s", ch)
		}
		if !a.Connected {
			t.Fatalf("%s should be connected after Connect", ch)
		}
	}

	for _, ch := range domain.ConnectableChannels() {
		a, err := svc.Disconnect(ctx, branch, ch)
		if err != nil {
			t.Fatalf("Disconnect %s: %v", ch, err)
		}
		if a.Connected {
			t.Fatalf("%s still connected after disconnect", ch)
		}
	}
}

func TestConnectValidationRejectsIncomplete(t *testing.T) {
	repo := newMem()
	svc := newConnectService(t, repo)
	branch := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	ctx := context.Background()

	_, err := svc.Connect(ctx, inbox.ConnectInput{
		BranchID: branch, Provider: domain.ChannelInstagram,
		AccessToken: "x", PageID: "y", // missing ig_user_id
	})
	if err == nil {
		t.Fatal("expected validation error")
	}

	_, err = svc.Connect(ctx, inbox.ConnectInput{
		BranchID: branch, Provider: domain.ChannelWhatsApp,
		AccessToken: "only-token",
	})
	if err == nil {
		t.Fatal("expected whatsapp validation error")
	}

	_, err = svc.Connect(ctx, inbox.ConnectInput{
		BranchID: branch, Provider: domain.ChannelGmail,
		ClientID: "c", ClientSecret: "s", RefreshToken: "r",
	})
	if err == nil {
		t.Fatal("expected gmail validation error")
	}
}

func TestConnectSealsSecretsOutOfConfigJSON(t *testing.T) {
	repo := newMem()
	svc := newConnectService(t, repo)
	branch := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	ctx := context.Background()

	res, err := svc.Connect(ctx, inbox.ConnectInput{
		BranchID: branch, Provider: domain.ChannelFacebook,
		AccessToken: "secret-token-XYZ9", PageID: "page-9",
	})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := repo.GetAccount(ctx, branch, domain.ChannelFacebook)
	if err != nil || stored == nil {
		t.Fatalf("stored: %v %#v", err, stored)
	}
	var cfg map[string]string
	if err := json.Unmarshal(stored.ConfigJSON, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["page_id"] != "page-9" || cfg["access_token"] != "" || cfg["verify_token"] != "" {
		t.Fatalf("config_json must keep ids only: %v", cfg)
	}
	if stored.SecretsEnc == "" || strings.Contains(stored.SecretsEnc, "secret-token") {
		t.Fatalf("secrets_enc=%q", stored.SecretsEnc)
	}
	if stored.VerifyTokenHash == "" || stored.VerifyTokenHash == res.VerifyToken {
		t.Fatalf("verify_token_hash=%q", stored.VerifyTokenHash)
	}

	accounts, err := svc.ListAccounts(ctx, branch)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range accounts {
		if a.Provider == domain.ChannelFacebook && a.PublicMeta["access_token_hint"] != "••••XYZ9" {
			t.Fatalf("public_meta=%v", a.PublicMeta)
		}
	}
}

func TestConnectFailsClosedWithoutSecrets(t *testing.T) {
	svc := inbox.NewService(newMem(), integration.NewRegistry(stub.New()), tx.Nop{}, nil)
	_, err := svc.Connect(context.Background(), inbox.ConnectInput{
		BranchID: uuid.New(), Provider: domain.ChannelFacebook, AccessToken: "t", PageID: "p",
	})
	if err == nil {
		t.Fatal("expected error without secret sealer")
	}
}

func TestWebhookAccountsOpensSecretsAndMatchesVerifyToken(t *testing.T) {
	repo := newMem()
	kr := testKeyring(t)
	svc := inbox.NewService(repo, integration.NewRegistry(stub.New()), tx.Nop{}, nil)
	svc.SetSecrets(crypto.NewSecretBox(kr), kr)
	branch := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	ctx := context.Background()
	res, err := svc.Connect(ctx, inbox.ConnectInput{
		BranchID: branch, Provider: domain.ChannelFacebook,
		AccessToken: "secret-token-XYZ9", PageID: "page-9", VerifyToken: "verify-me",
	})
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := repo.GetAccount(ctx, branch, domain.ChannelFacebook)
	lookup := &fakeSecretLookup{acc: stored}
	accounts := inbox.NewWebhookAccounts(lookup, crypto.NewSecretBox(kr), kr)

	acc, err := accounts.FindAccountByExternalID(ctx, domain.ChannelFacebook, "page-9")
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]string
	if err := json.Unmarshal(acc.ConfigJSON, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["access_token"] != "secret-token-XYZ9" || cfg["page_id"] != "page-9" {
		t.Fatalf("merged config=%v", cfg)
	}

	ok, err := accounts.AccountVerifyTokenExists(ctx, domain.ChannelFacebook, res.VerifyToken)
	if err != nil || !ok {
		t.Fatalf("verify token should match: %v %v", ok, err)
	}
	if ok, _ := accounts.AccountVerifyTokenExists(ctx, domain.ChannelFacebook, "wrong"); ok {
		t.Fatal("wrong verify token matched")
	}
}

type fakeSecretLookup struct {
	acc *domain.IntegrationAccount
}

func (f *fakeSecretLookup) FindAccountByExternalID(context.Context, domain.Channel, string) (*domain.IntegrationAccount, error) {
	cp := *f.acc
	return &cp, nil
}

func (f *fakeSecretLookup) AccountVerifyTokenMatches(_ context.Context, _ domain.Channel, _, hash string) (bool, error) {
	return hash != "" && hash == f.acc.VerifyTokenHash, nil
}

func testKeyring(t *testing.T) *crypto.Keyring {
	t.Helper()
	kr, err := crypto.NewKeyring("k1", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)), nil)
	if err != nil {
		t.Fatal(err)
	}
	return kr
}

func newConnectService(t *testing.T, repo *memRepo) *inbox.Service {
	t.Helper()
	kr := testKeyring(t)
	svc := inbox.NewService(repo, integration.NewRegistry(stub.New()), tx.Nop{}, nil)
	svc.SetSecrets(crypto.NewSecretBox(kr), kr)
	return svc
}
