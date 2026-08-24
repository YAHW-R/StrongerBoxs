package sync

import (
	"time"

	"github.com/yahwr/strongboxs/client/internal/store"
)

// Mapeo entidad local ⇄ ítem de red. Los valores viajan TAL CUAL están en
// la BD local: los sensibles ya son sobres "sb1.…" (cifrado en la capa
// store/crypto), así que el motor de sync jamás necesita la DEK.

func noteToItem(n store.Note) ItemIn {
	return ItemIn{
		ItemUUID: n.UUID,
		Kind:     KindNote,
		Payload: ItemPayload{
			Title:    n.Title,
			Body:     n.Body,
			Color:    n.Color,
			Pinned:   n.Pinned,
			Archived: n.Archived,
		},
		Version:   int(n.Version),
		Deleted:   n.DeletedAt != nil,
		UpdatedAt: n.UpdatedAt,
	}
}

func secretToItem(s store.Secret) ItemIn {
	return ItemIn{
		ItemUUID: s.UUID,
		Kind:     KindSecret,
		Payload: ItemPayload{
			Title:    s.Title,
			Template: s.Template,
			Extra:    s.Extra,
			Username: s.Username,
			Password: s.Password,
			URL:      s.URL,
			Notes:    s.Notes,
		},
		Version:   int(s.Version),
		Deleted:   s.DeletedAt != nil,
		UpdatedAt: s.UpdatedAt,
	}
}

func deletedPtr(t time.Time) *time.Time { return &t }

func remoteToNote(it ItemOut) store.Note {
	var d *time.Time
	if it.Deleted {
		d = deletedPtr(it.SyncedAt)
	}
	return store.Note{
		UUID:      it.ItemUUID,
		Title:     it.Payload.Title,
		Body:      it.Payload.Body,
		Color:     it.Payload.Color,
		Pinned:    it.Payload.Pinned,
		Archived:  it.Payload.Archived,
		Version:   int64(it.Version),
		CreatedAt: it.UpdatedAt,
		UpdatedAt: it.UpdatedAt,
		DeletedAt: d,
	}
}

func remoteToSecret(it ItemOut) store.Secret {
	var d *time.Time
	if it.Deleted {
		d = deletedPtr(it.SyncedAt)
	}
	return store.Secret{
		UUID:      it.ItemUUID,
		Template:  it.Payload.Template,
		Extra:     it.Payload.Extra,
		Title:     it.Payload.Title,
		Username:  it.Payload.Username,
		Password:  it.Payload.Password,
		URL:       it.Payload.URL,
		Notes:     it.Payload.Notes,
		Version:   int64(it.Version),
		CreatedAt: it.UpdatedAt,
		UpdatedAt: it.UpdatedAt,
		DeletedAt: d,
	}
}
