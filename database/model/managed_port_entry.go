package model

type ManagedPortEntry struct {
	Id       uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	Scope    string `json:"scope" gorm:"size:16;not null"`
	OwnerId  uint   `json:"ownerId" gorm:"not null"`
	OwnerTag string `json:"ownerTag" gorm:"size:255;not null"`
	Port     int    `json:"port" gorm:"not null"`
}
