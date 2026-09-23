package inbox_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/integration"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/integration/stub"
	"github.com/wodi-crm/wodi-crm-be/internal/app/inbox"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/inbox"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

func TestConnectAllSocialChannels(t *testing.T) {
	repo := newMem()
	svc := inbox.NewService(repo, integration.NewRegistry(stub.New()), tx.Nop{}, nil)
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
			want: []string{"phone_number_id", "access_token_hint", "verify_token"},
		},
		{
			name: "instagram",
			in: inbox.ConnectInput{
				BranchID: branch, Provider: domain.ChannelInstagram,
				DisplayName: "IG", AccessToken: "tok-ig", PageID: "page-1",
				IGUserID: "ig-1", VerifyToken: "v-ig",
			},
			want: []string{"page_id", "ig_user_id", "access_token_hint", "verify_token"},
		},
		{
			name: "facebook",
			in: inbox.ConnectInput{
				BranchID: branch, Provider: domain.ChannelFacebook,
				DisplayName: "FB", AccessToken: "tok-fb", PageID: "page-2",
				VerifyToken: "v-fb",
			},
			want: []string{"page_id", "access_token_hint", "verify_token"},
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
			if res.Account.ConfigJSON != nil {
				t.Fatal("secrets must be stripped from account response")
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
	svc := inbox.NewService(repo, integration.NewRegistry(stub.New()), tx.Nop{}, nil)
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

func TestConnectPersistsConfigJSONSecrets(t *testing.T) {
	repo := newMem()
	svc := inbox.NewService(repo, integration.NewRegistry(stub.New()), tx.Nop{}, nil)
	branch := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	ctx := context.Background()

	_, err := svc.Connect(ctx, inbox.ConnectInput{
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
	if cfg["access_token"] != "secret-token-XYZ9" || cfg["page_id"] != "page-9" {
		t.Fatalf("config_json=%v", cfg)
	}
}
