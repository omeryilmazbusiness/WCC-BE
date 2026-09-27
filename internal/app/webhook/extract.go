package webhook

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/inbox"
)

// metaEnvelope covers WhatsApp Cloud API (changes[].value) and
// Messenger/Instagram (messaging[]) webhook shapes.
type metaEnvelope struct {
	Object string `json:"object"`
	Entry  []struct {
		ID      string `json:"id"`
		Changes []struct {
			Value struct {
				Metadata struct {
					PhoneNumberID string `json:"phone_number_id"`
				} `json:"metadata"`
				Messages []struct {
					ID string `json:"id"`
				} `json:"messages"`
				Statuses []struct {
					ID string `json:"id"`
				} `json:"statuses"`
			} `json:"value"`
		} `json:"changes"`
		Messaging []struct {
			Recipient struct {
				ID string `json:"id"`
			} `json:"recipient"`
			Message struct {
				MID string `json:"mid"`
			} `json:"message"`
		} `json:"messaging"`
	} `json:"entry"`
}

// flatPayload covers the internal/stub JSON shape, email relays and Google
// Pub/Sub push envelopes.
type flatPayload struct {
	ExternalAccountID string          `json:"external_account_id"`
	AccountID         string          `json:"account_id"`
	PhoneNumberID     string          `json:"phone_number_id"`
	PageID            string          `json:"page_id"`
	IGUserID          string          `json:"ig_user_id"`
	MailboxEmail      string          `json:"mailbox_email"`
	To                json.RawMessage `json:"to"`
	EventID           string          `json:"event_id"`
	MessageID         string          `json:"message_id"`
	Message           *struct {
		Data      string `json:"data"`
		MessageID string `json:"messageId"`
	} `json:"message"`
}

type parsed struct {
	meta metaEnvelope
	flat flatPayload
}

func parse(body []byte) parsed {
	var p parsed
	_ = json.Unmarshal(body, &p.meta)
	_ = json.Unmarshal(body, &p.flat)
	return p
}

// ExternalAccountID returns the provider-side account identifier (phone
// number id, page / IG user id, mailbox) that maps to an integration account.
func ExternalAccountID(provider domain.Channel, body []byte) string {
	p := parse(body)
	if v := first(p.flat.ExternalAccountID, p.flat.AccountID); v != "" {
		return normalizeAccountID(provider, v)
	}
	var v string
	switch provider {
	case domain.ChannelWhatsApp:
		for _, e := range p.meta.Entry {
			for _, c := range e.Changes {
				if v == "" {
					v = c.Value.Metadata.PhoneNumberID
				}
			}
		}
		v = first(v, p.flat.PhoneNumberID)
	case domain.ChannelInstagram, domain.ChannelFacebook:
		for _, e := range p.meta.Entry {
			v = first(v, e.ID)
			for _, m := range e.Messaging {
				v = first(v, m.Recipient.ID)
			}
		}
		v = first(v, p.flat.IGUserID, p.flat.PageID)
	case domain.ChannelGmail, domain.ChannelEmail:
		v = first(p.flat.MailboxEmail, pubSubEmail(p.flat), firstAddress(p.flat.To))
	}
	return normalizeAccountID(provider, v)
}

// ExternalEventID returns the provider event id used for idempotency, or a
// content hash when the payload carries none.
func ExternalEventID(provider domain.Channel, body []byte) string {
	p := parse(body)
	var v string
	for _, e := range p.meta.Entry {
		for _, c := range e.Changes {
			for _, m := range c.Value.Messages {
				v = first(v, m.ID)
			}
			for _, s := range c.Value.Statuses {
				v = first(v, s.ID)
			}
		}
		for _, m := range e.Messaging {
			v = first(v, m.Message.MID)
		}
	}
	v = first(v, p.flat.EventID, p.flat.MessageID)
	if p.flat.Message != nil {
		v = first(v, p.flat.Message.MessageID)
	}
	if v != "" {
		return v
	}
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func normalizeAccountID(provider domain.Channel, v string) string {
	v = strings.TrimSpace(v)
	if provider == domain.ChannelGmail || provider == domain.ChannelEmail {
		return strings.ToLower(v)
	}
	return v
}

func pubSubEmail(f flatPayload) string {
	if f.Message == nil || f.Message.Data == "" {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(f.Message.Data)
	if err != nil {
		if raw, err = base64.URLEncoding.DecodeString(f.Message.Data); err != nil {
			return ""
		}
	}
	var data struct {
		EmailAddress string `json:"emailAddress"`
	}
	if json.Unmarshal(raw, &data) != nil {
		return ""
	}
	return data.EmailAddress
}

func firstAddress(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil && len(list) > 0 {
		return list[0]
	}
	return ""
}

func first(vals ...string) string {
	for _, v := range vals {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}
