package store

import "time"

// Shop is one row per shop that ever installed the app. Tokens are stored
// sealed (see internal/crypto); Store decrypts them into an Install.
type Shop struct {
	ID                    int64  `gorm:"primaryKey"`
	ShopDomain            string `gorm:"not null;uniqueIndex"`
	AccessTokenEnc        *string
	RefreshTokenEnc       *string
	Scopes                string `gorm:"not null"`
	AccessTokenExpiresAt  *time.Time
	RefreshTokenExpiresAt *time.Time
	NeedsReauth           bool      `gorm:"not null"`
	InstalledAt           time.Time `gorm:"not null"`
	UninstalledAt         *time.Time
	CreatedAt             time.Time `gorm:"not null"`
	UpdatedAt             time.Time `gorm:"not null"`

	// Settings cascades: shop/redact deletes the shop and its settings go
	// with it, at the database level.
	Settings *ShopSettings `gorm:"foreignKey:ShopDomain;references:ShopDomain;constraint:OnDelete:CASCADE"`
}

// TableName pins the table name so a struct rename never moves the data.
func (Shop) TableName() string { return "shops" }

// ShopSettings are the per-shop settings, the canonical "store something per
// shop" pattern. Copy it for your own per-shop data.
//
// No GORM `default:` tags on purpose: GORM omits a zero value on create when
// the column has a default, so `SyncEnabled: false` would silently become
// the default instead. Defaults live in Go (see Store.GetSettings).
type ShopSettings struct {
	ShopDomain        string    `gorm:"primaryKey"`
	NotificationEmail string    `gorm:"not null"`
	SyncEnabled       bool      `gorm:"not null"`
	CreatedAt         time.Time `gorm:"not null"`
	UpdatedAt         time.Time `gorm:"not null"`
}

// TableName pins the table name.
func (ShopSettings) TableName() string { return "shop_settings" }

// WebhookDelivery is one processed webhook delivery, keyed on
// X-Shopify-Webhook-Id. Claiming a delivery and enqueuing its job happen in
// one transaction, so a delivery is either fully handed to the queue or not
// claimed at all.
type WebhookDelivery struct {
	WebhookID  string    `gorm:"primaryKey"`
	ShopDomain string    `gorm:"not null"`
	Topic      string    `gorm:"not null"`
	ReceivedAt time.Time `gorm:"not null;index;autoCreateTime"`
}

// TableName pins the table name.
func (WebhookDelivery) TableName() string { return "webhook_deliveries" }

// Models lists every model. Add new models here: the drift test
// (internal/db, TestMigrationsMatchModels) builds a schema from them and
// fails when internal/db/migrations doesn't produce the same one.
func Models() []any {
	return []any{&Shop{}, &ShopSettings{}, &WebhookDelivery{}}
}
